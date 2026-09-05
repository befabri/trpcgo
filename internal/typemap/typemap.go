package typemap

import (
	"cmp"
	"fmt"
	"go/types"
	"reflect"
	"slices"
	"strings"
	"unicode"
)

// TypeDefKind distinguishes what kind of TypeScript declaration to emit.
type TypeDefKind int

const (
	TypeDefInterface TypeDefKind = iota // export interface Foo { ... }
	TypeDefUnion                        // export type Status = "active" | "inactive"
	TypeDefAlias                        // export type UserRole = string
)

// TypeDef represents a top-level TypeScript type declaration.
type TypeDef struct {
	ID           string // fully-qualified: "github.com/foo/models.User"
	PkgPath      string // "github.com/foo/models"
	PkgName      string // "models"
	Name         string
	Kind         TypeDefKind
	Comment      string       // Go doc comment → JSDoc
	TypeParams   []string     // Generic type parameter names: ["T", "U"]
	Extends      []string     // base types for TypeScript extends clause
	Refinements  []Refinement // cross-field validation constraints → .refine()
	Fields       []Field      // Kind == TypeDefInterface
	UnionMembers []string     // Kind == TypeDefUnion (TS-formatted values)
	AliasOf      string       // Kind == TypeDefAlias (e.g., "string")
}

// IsStringUnion reports whether this is a non-empty string-literal union.
func (d TypeDef) IsStringUnion() bool {
	return d.Kind == TypeDefUnion && len(d.UnionMembers) > 0 && strings.HasPrefix(d.UnionMembers[0], `"`)
}

// Refinement represents a cross-field validation constraint emitted as .refine().
type Refinement struct {
	Field               string   // JSON name of the constrained field
	Op                  string   // JS comparison operator: ">=", "<=", ">", "<", "===", "!=="
	OtherField          string   // JSON name of the referenced field
	Tag                 string   // original validate tag name
	WhenAnyPresent      []string // JSON names from the same nil-able embedded pointer as Field; the rule applies only when one is present
	OtherWhenAnyPresent []string // the same set for OtherField when it comes from a different embedded pointer
}

// ElementType describes one level of a slice, array, or map element chain.
// The chain stops at a struct element, which has its own TypeDef.
type ElementType struct {
	GoKind    string
	IsPointer bool
	Element   *ElementType
}

// Field represents a field in a TypeScript interface.
type Field struct {
	Name              string
	Type              string
	ZodType           string // underlying type before a tstype override
	GoKind            string // Go kind for Zod: "string", "int", "int32", "float64", etc.
	IsPointer         bool   // original Go field was a pointer; affects validate:"required" semantics
	Optional          bool
	Readonly          bool           // from tstype:",readonly"
	Required          bool           // from tstype:",required" (overrides optional)
	ValidateOmitempty bool           // validate:"omitempty" — Zod should allow zero values
	Comment           string         // field doc comment → JSDoc
	Validate          []ValidateRule // parsed validate tag rules (before dive)
	ElementValidate   []ValidateRule // parsed validate tag rules after dive (for container elements)
	Element           *ElementType
	UnsupportedZod    []ValidateRule // validate rules with no Zod equivalent
	InvalidZod        []ValidateRule // validate rules that cannot be emitted safely
	ZodOmit           bool           // zod_omit:"true" — exclude from Zod schema
}

// Mapper converts Go types to TypeScript type strings and collects interface definitions.
type Mapper struct {
	defs     map[string]TypeDef  // key = TypeID (fully-qualified)
	seen     map[string]bool     // key = TypeID (fully-qualified)
	names    map[string]string   // TypeID → short name (for display name resolution)
	metas    map[string]TypeMeta // AST metadata keyed by TypeID
	resolved map[string]string   // cached: TypeID → display name
}

var basicKindNames = map[types.BasicKind]string{
	types.String:  "string",
	types.Bool:    "bool",
	types.Int:     "int",
	types.Int8:    "int8",
	types.Int16:   "int16",
	types.Int32:   "int32",
	types.Int64:   "int64",
	types.Uint:    "uint",
	types.Uint8:   "uint8",
	types.Uint16:  "uint16",
	types.Uint32:  "uint32",
	types.Uint64:  "uint64",
	types.Float32: "float32",
	types.Float64: "float64",
}

// Since Go 1.27 json.RawMessage is an alias of encoding/json/jsontext.Value,
// so alias resolution lands on the jsontext entry; the json entry still
// matches the alias by its own name and older toolchains' defined type.
var wellKnownGoKinds = map[string]string{
	"time.Time":                    "time.Time",
	"encoding/json.RawMessage":     "json.RawMessage",
	"encoding/json/jsontext.Value": "json.RawMessage",
}

var wellKnownTSTypes = map[string]string{
	"time.Time":                    "string",
	"encoding/json.RawMessage":     "unknown",
	"encoding/json/jsontext.Value": "unknown",
	"encoding/json.Number":         "number",
}

// TypeID returns a fully-qualified identifier for a types.Object.
// Unlike types.Object.Id(), this always includes the package path,
// even for exported names.
func TypeID(obj types.Object) string {
	if pkg := obj.Pkg(); pkg != nil {
		return pkg.Path() + "." + obj.Name()
	}
	return obj.Name()
}

// TokenDelim is the delimiter used to wrap type IDs in token strings.
// The § character cannot appear in valid Go identifiers or TS type strings.
const TokenDelim = "§"

// typeToken creates a resolvable token string for a named type.
// Tokens preserve type identity through string composition (arrays, generics, etc.).
func (m *Mapper) typeToken(id, shortName string) string {
	m.names[id] = shortName
	m.resolved = nil // invalidate cache
	return TokenDelim + id + TokenDelim
}

// ResolveTokens replaces all §id§ tokens in s with display names.
func ResolveTokens(s string, display map[string]string) string {
	if !strings.Contains(s, TokenDelim) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for {
		before, rest, ok := strings.Cut(s, TokenDelim)
		if !ok {
			b.WriteString(s)
			break
		}
		b.WriteString(before)
		id, after, ok := strings.Cut(rest, TokenDelim)
		if !ok {
			// Malformed token — write delimiter and continue.
			b.WriteString(TokenDelim)
			s = rest
			continue
		}
		if name, ok := display[id]; ok {
			b.WriteString(name)
		} else {
			// Unknown token — keep as-is for debugging.
			b.WriteString(id)
		}
		s = after
	}
	return b.String()
}

// displayNames computes the mapping from TypeID → display name.
// If no collisions exist, display names equal short names.
// On collision, names are prefixed with the title-cased package name.
func (m *Mapper) displayNames() map[string]string {
	if m.resolved != nil {
		return m.resolved
	}
	// Group IDs by short name.
	counts := map[string][]string{} // shortName → [IDs]
	for id, name := range m.names {
		counts[name] = append(counts[name], id)
	}
	result := make(map[string]string, len(m.names))
	for id, shortName := range m.names {
		if len(counts[shortName]) > 1 {
			// Collision — prefix with title-cased package name.
			def, ok := m.defs[id]
			if ok && def.PkgName != "" {
				// Title-case the first letter of package name.
				prefix := strings.ToUpper(def.PkgName[:1]) + def.PkgName[1:]
				result[id] = prefix + shortName
			} else {
				result[id] = shortName
			}
		} else {
			result[id] = shortName
		}
	}
	m.resolved = result
	return result
}

// Resolve resolves type tokens in a string to display names.
// Used by codegen to resolve ProcEntry InputTS/OutputTS.
func (m *Mapper) Resolve(s string) string {
	return ResolveTokens(s, m.displayNames())
}

// NewMapper creates a Mapper. Pass nil for metas if no AST metadata is available.
func NewMapper(metas map[string]TypeMeta) *Mapper {
	if metas == nil {
		metas = make(map[string]TypeMeta)
	}
	return &Mapper{
		defs:  make(map[string]TypeDef),
		seen:  make(map[string]bool),
		names: make(map[string]string),
		metas: metas,
	}
}

// Defs returns all collected TypeScript type definitions, sorted by name.
// All type tokens in field types and alias types are resolved to display names.
func (m *Mapper) Defs() []TypeDef {
	display := m.displayNames()
	var result []TypeDef
	for _, d := range m.defs {
		// Resolve display name.
		if name, ok := display[d.ID]; ok {
			d.Name = name
		}
		// Resolve tokens in field types.
		for i := range d.Fields {
			d.Fields[i].Type = ResolveTokens(d.Fields[i].Type, display)
			d.Fields[i].ZodType = ResolveTokens(d.Fields[i].ZodType, display)
		}
		// Resolve tokens in extends clause.
		for i := range d.Extends {
			d.Extends[i] = ResolveTokens(d.Extends[i], display)
		}
		// Resolve tokens in alias target.
		if d.AliasOf != "" {
			d.AliasOf = ResolveTokens(d.AliasOf, display)
		}
		result = append(result, d)
	}
	slices.SortFunc(result, func(a, b TypeDef) int {
		return cmp.Compare(a.Name, b.Name)
	})
	return result
}

// Convert maps a Go type to its TypeScript representation.
// Named struct types generate interface definitions as a side effect.
func (m *Mapper) Convert(t types.Type) string {
	return m.convert(t)
}

// ConvertSubscriptionOutput returns the TypeScript type httpSubscriptionLink
// delivers to onData: a top-level TrackedEvent[T] becomes
// { id: string; data: T }, and any other type converts as usual.
func (m *Mapper) ConvertSubscriptionOutput(t types.Type) string {
	unwrapped := types.Unalias(t)
	if ptr, ok := unwrapped.(*types.Pointer); ok {
		unwrapped = types.Unalias(ptr.Elem())
	}
	if named, ok := unwrapped.(*types.Named); ok {
		obj := named.Obj()
		if obj.Pkg() != nil && obj.Pkg().Path() == "github.com/befabri/trpcgo" && obj.Name() == "TrackedEvent" && named.TypeArgs().Len() == 1 {
			return "{ id: string; data: " + m.convert(named.TypeArgs().At(0)) + " }"
		}
	}
	return m.convert(t)
}

func (m *Mapper) convert(t types.Type) string {
	switch t := t.(type) {
	case *types.Named:
		return m.convertNamed(t)

	case *types.Alias:
		return m.convertAlias(t)

	case *types.TypeParam:
		return t.Obj().Name()

	case *types.Pointer:
		return m.convert(t.Elem())

	case *types.Slice:
		return m.convertSlice(t)

	case *types.Array:
		elem := m.convert(t.Elem())
		return elem + "[]"

	case *types.Map:
		key := m.convert(t.Key())
		val := m.convert(t.Elem())
		return fmt.Sprintf("Record<%s, %s>", key, val)

	case *types.Basic:
		return basicToTS(t)

	case *types.Struct:
		// Anonymous struct — inline.
		return m.inlineStruct(t)

	case *types.Interface:
		return "unknown"

	default:
		return "unknown"
	}
}

func (m *Mapper) convertNamed(t *types.Named) string {
	obj := t.Obj()
	name := obj.Name()
	id := TypeID(obj)
	if ts := m.convertWellKnownNamed(t, name); ts != "" {
		return ts
	}
	underlying := t.Underlying()
	if _, ok := underlying.(*types.Struct); ok {
		return m.convertNamedStruct(t, id, name)
	}
	if token := m.convertNamedMeta(t, id, name, underlying); token != "" {
		return token
	}
	return m.convert(underlying)
}

func (m *Mapper) convertWellKnownNamed(t *types.Named, name string) string {
	obj := t.Obj()
	if obj.Pkg() == nil {
		return ""
	}
	fullPath := obj.Pkg().Path() + "." + name
	if ts := wellKnownTSTypes[fullPath]; ts != "" {
		return ts
	}
	return ""
}

func (m *Mapper) convertNamedStruct(t *types.Named, id, name string) string {
	if t.TypeArgs() != nil && t.TypeArgs().Len() > 0 {
		var args []string
		for t0 := range t.TypeArgs().Types() {
			args = append(args, m.convert(t0))
		}
		originID := TypeID(t.Origin().Obj())
		m.resolveStructDef(originID, name, t.Origin())
		return fmt.Sprintf("%s<%s>", m.typeToken(originID, name), strings.Join(args, ", "))
	}
	m.resolveStructDef(id, name, t)
	return m.typeToken(id, name)
}

func (m *Mapper) convertNamedMeta(t *types.Named, id, name string, underlying types.Type) string {
	meta, ok := m.metas[id]
	if !ok {
		return ""
	}
	if len(meta.ConstValues) > 0 {
		m.registerUnion(id, name, meta, t.Obj())
		return m.typeToken(id, name)
	}
	if meta.IsAlias {
		m.registerAlias(id, name, underlying, meta, t.Obj())
		return m.typeToken(id, name)
	}
	return ""
}

func (m *Mapper) convertAlias(t *types.Alias) string {
	obj := t.Obj()
	name := obj.Name()
	id := TypeID(obj)
	if ts := wellKnownTSTypes[id]; ts != "" {
		return ts
	}
	if meta, ok := m.metas[id]; ok && meta.IsAlias {
		m.registerAlias(id, name, t.Rhs(), meta, obj)
		return m.typeToken(id, name)
	}
	return m.convert(t.Rhs())
}

func (m *Mapper) convertSlice(t *types.Slice) string {
	// []byte marshals as base64 string in JSON.
	if basic, ok := t.Elem().(*types.Basic); ok && basic.Kind() == types.Byte {
		return "string"
	}
	elem := m.convert(t.Elem())
	if strings.Contains(elem, "|") {
		return fmt.Sprintf("(%s)[]", elem)
	}
	return elem + "[]"
}

func basicToTS(t *types.Basic) string {
	switch t.Kind() {
	case types.String:
		return "string"
	case types.Bool:
		return "boolean"
	case types.Int, types.Int8, types.Int16, types.Int32, types.Int64,
		types.Uint, types.Uint8, types.Uint16, types.Uint32, types.Uint64,
		types.Float32, types.Float64:
		return "number"
	default:
		return "unknown"
	}
}

// resolveStructDef registers a named struct type as a TypeScript interface.
// Handles both generic and non-generic types.
func (m *Mapper) resolveStructDef(id, name string, named *types.Named) {
	if m.seen[id] {
		return
	}
	m.seen[id] = true

	var params []string
	if tp := named.TypeParams(); tp != nil {
		for tparam := range tp.TypeParams() {
			params = append(params, tparam.Obj().Name())
		}
	}

	st := named.Underlying().(*types.Struct)
	meta := m.metas[id]
	def := TypeDef{
		ID:         id,
		PkgPath:    pkgPath(named.Obj()),
		PkgName:    pkgName(named.Obj()),
		Name:       name,
		Kind:       TypeDefInterface,
		Comment:    meta.Comment,
		TypeParams: params,
	}
	def.Refinements = m.collectFields(st, &def.Fields, &def.Extends, meta.FieldComments)
	m.defs[id] = def
}

func (m *Mapper) registerUnion(id, name string, meta TypeMeta, obj types.Object) {
	if m.seen[id] {
		return
	}
	m.seen[id] = true
	m.defs[id] = TypeDef{
		ID:           id,
		PkgPath:      pkgPath(obj),
		PkgName:      pkgName(obj),
		Name:         name,
		Kind:         TypeDefUnion,
		Comment:      meta.Comment,
		UnionMembers: meta.ConstValues,
	}
}

func (m *Mapper) registerAlias(id, name string, underlying types.Type, meta TypeMeta, obj types.Object) {
	if m.seen[id] {
		return
	}
	m.seen[id] = true
	m.defs[id] = TypeDef{
		ID:      id,
		PkgPath: pkgPath(obj),
		PkgName: pkgName(obj),
		Name:    name,
		Kind:    TypeDefAlias,
		Comment: meta.Comment,
		AliasOf: m.convert(underlying),
	}
}

func pkgPath(obj types.Object) string {
	if pkg := obj.Pkg(); pkg != nil {
		return pkg.Path()
	}
	return ""
}

func pkgName(obj types.Object) string {
	if pkg := obj.Pkg(); pkg != nil {
		return pkg.Name()
	}
	return ""
}

func (m *Mapper) collectFields(st *types.Struct, fields *[]Field, extends *[]string, fieldComments map[int]string) []Refinement {
	adapter := FieldAdapter[types.Type]{
		Fields: func(t types.Type) []EmbeddedField[types.Type] {
			st := t.Underlying().(*types.Struct)
			fields := make([]EmbeddedField[types.Type], st.NumFields())
			for i := range fields {
				f := st.Field(i)
				fields[i] = EmbeddedField[types.Type]{Type: f.Type(), Name: f.Name(), Tag: st.Tag(i), Exported: f.Exported(), Embedded: f.Embedded()}
			}
			return fields
		},
		Struct: func(t types.Type) (types.Type, bool, bool) {
			t = types.Unalias(t)
			ptr, pointer := t.(*types.Pointer)
			if pointer {
				t = types.Unalias(ptr.Elem())
			}
			_, ok := t.Underlying().(*types.Struct)
			return t, ok, pointer
		},
		TypeName: m.convert,
		Lookup: func(t types.Type, name string) ([]int, bool) {
			obj, indexes, _ := types.LookupFieldOrMethod(t, false, nil, name)
			f, ok := obj.(*types.Var)
			return indexes, ok && f.IsField()
		},
		Map: func(owner types.Type, index int, name string, omitted bool, tag TSTypeTag, hasTag bool) Field {
			inner := owner.Underlying().(*types.Struct)
			var comments map[int]string
			if inner == st {
				comments = fieldComments
			}
			return m.collectField(inner.Field(index), inner.Tag(index), name, omitted, tag, hasTag, comments, index)
		},
	}
	mapped, bases, refs := CollectJSONFields(types.Type(st), adapter, extends != nil)
	*fields = mapped
	if extends != nil {
		*extends = bases
	}
	return refs
}

func (m *Mapper) collectField(field *types.Var, tag, jsonName string, omitempty bool, tstag TSTypeTag, hasTSTag bool, fieldComments map[int]string, index int) Field {
	f := Field{
		Name:      jsonName,
		Type:      m.convert(field.Type()),
		GoKind:    goKind(field.Type()),
		IsPointer: isPointer(field.Type()),
		Optional:  omitempty || isPointer(field.Type()),
	}
	applyValidateRules(&f, tag, field.Type())
	applyTSTypeTag(&f, tstag, hasTSTag)
	f.Comment = fieldComment(tag, fieldComments, index)
	f.ZodOmit = ParseZodOmitTag(tag)
	return f
}

func applyValidateRules(f *Field, tag string, typ types.Type) {
	sliceRules, elemRules := SplitAtDive(ParseValidateTag(tag))
	f.Validate = sliceRules
	f.ElementValidate = elemRules
	f.UnsupportedZod = UnsupportedZodRules(sliceRules)
	f.UnsupportedZod = append(f.UnsupportedZod, UnsupportedZodRules(elemRules)...)
	f.Element = containerElementType(typ)
	f.InvalidZod = InvalidZodRules(sliceRules, f.GoKind)
	if f.Element != nil {
		f.InvalidZod = append(f.InvalidZod, InvalidZodRules(elemRules, f.Element.GoKind)...)
	}
	for _, rule := range f.Validate {
		if rule.Tag == "required" {
			f.Optional = false
		}
		if rule.Tag == "omitempty" {
			f.ValidateOmitempty = true
		}
	}
}

func applyTSTypeTag(f *Field, tstag TSTypeTag, ok bool) {
	if !ok {
		return
	}
	if tstag.Type != "" {
		f.ZodType = f.Type
		f.Type = tstag.Type
	}
	f.Readonly = tstag.Readonly
	if tstag.Required {
		f.Required = true
		f.Optional = false
	}
}

func fieldComment(tag string, comments map[int]string, index int) string {
	if comments != nil {
		if comment, ok := comments[index]; ok {
			if comment != "" {
				return comment
			}
		}
	}
	comment, _ := ParseTSDocTag(tag)
	return comment
}

// QuotePropName wraps a property name in quotes if it is not a valid
// JavaScript identifier (e.g. contains hyphens, starts with a digit).
func QuotePropName(name string) string {
	if name == "" {
		return `""`
	}
	for i, r := range name {
		if i == 0 {
			if !unicode.IsLetter(r) && r != '_' && r != '$' {
				return fmt.Sprintf("%q", name)
			}
		} else {
			if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' && r != '$' {
				return fmt.Sprintf("%q", name)
			}
		}
	}
	return name
}

func (m *Mapper) inlineStruct(st *types.Struct) string {
	var fields []Field
	// An inline object type has no extends clause, so inherited fields are
	// flattened.
	m.collectFields(st, &fields, nil, nil)
	return InlineObjectType(fields)
}

func ParseJSONTag(rawTag string) (name string, omitempty bool, skip bool) {
	tag := reflect.StructTag(rawTag)
	jsonTag, ok := tag.Lookup("json")
	if !ok {
		return "", false, false
	}
	if jsonTag == "-" {
		return "", false, true
	}
	name, options, _ := strings.Cut(jsonTag, ",")
	for p := range strings.SplitSeq(options, ",") {
		if p == "omitempty" || p == "omitzero" {
			omitempty = true
		}
	}
	return name, omitempty, false
}

func isPointer(t types.Type) bool {
	_, ok := t.(*types.Pointer)
	return ok
}

// goKind returns a Go kind string for Zod type discrimination.
// Dereferences pointers and resolves named types to their underlying basic kind.
func goKind(t types.Type) string {
	// Unwrap pointers.
	for {
		if ptr, ok := t.(*types.Pointer); ok {
			t = ptr.Elem()
		} else {
			break
		}
	}

	// Check for well-known types first, by the alias's own name when t is an
	// alias and by the defined type it resolves to otherwise.
	if alias, ok := t.(*types.Alias); ok {
		if kind := wellKnownGoKinds[TypeID(alias.Obj())]; kind != "" {
			return kind
		}
		t = types.Unalias(t)
	}
	if named, ok := t.(*types.Named); ok {
		if obj := named.Obj(); obj.Pkg() != nil {
			fullPath := obj.Pkg().Path() + "." + obj.Name()
			if kind := wellKnownGoKinds[fullPath]; kind != "" {
				return kind
			}
		}
	}

	// Resolve to underlying type.
	switch u := t.Underlying().(type) {
	case *types.Basic:
		if kind := basicKindNames[u.Kind()]; kind != "" {
			return kind
		}
		return "unknown"
	case *types.Slice:
		// []byte is special.
		if basic, ok := u.Elem().(*types.Basic); ok && basic.Kind() == types.Byte {
			return "[]byte"
		}
		return "slice"
	case *types.Array:
		return "array"
	case *types.Map:
		return "map"
	case *types.Struct:
		return "struct"
	case *types.Interface:
		return "interface"
	default:
		return "unknown"
	}
}

// containerElementType records each container level without following recursive
// named containers indefinitely.
func containerElementType(t types.Type) *ElementType {
	var root *ElementType
	next := &root
	seen := make(map[types.Type]bool)
	for {
		t = types.Unalias(t)
		for {
			ptr, ok := t.(*types.Pointer)
			if !ok {
				break
			}
			t = types.Unalias(ptr.Elem())
		}
		if seen[t] {
			return root
		}
		seen[t] = true
		var elem types.Type
		switch u := t.Underlying().(type) {
		case *types.Slice:
			elem = u.Elem()
		case *types.Array:
			elem = u.Elem()
		case *types.Map:
			elem = u.Elem()
		default:
			return root
		}
		*next = &ElementType{GoKind: goKind(elem), IsPointer: isPointer(types.Unalias(elem))}
		next = &(*next).Element
		t = elem
	}
}
