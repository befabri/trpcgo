package typemap

import (
	"cmp"
	"fmt"
	"go/types"
	"maps"
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
	ID              string // fully-qualified: "github.com/foo/models.User"
	PkgPath         string // "github.com/foo/models"
	PkgName         string // "models"
	Name            string
	Kind            TypeDefKind
	Comment         string       // Go doc comment → JSDoc
	TypeParams      []string     // Generic type parameter names: ["T", "U"]
	ZodExtends      []string     // Concrete schema identities for generic inheritance bases.
	Extends         []string     // base types for TypeScript extends clause
	ExtendsAt       []int        // Own fields declared before each Extends base; nil places every base first.
	Refinements     []Refinement // cross-field validation constraints → .refine()
	Fields          []Field      // Kind == TypeDefInterface
	UnionMembers    []string     // Kind == TypeDefUnion (TS-formatted values)
	AliasOf         string       // Kind == TypeDefAlias (e.g., "string")
	Underlying      *Field       // Original Go metadata for an alias target.
	Specializations []TypeDef    // Concrete generic schemas; TypeScript keeps the generic declaration.
	InstanceOf      string       // Fully instantiated generic TypeScript type for a specialization.
}

// IsStringUnion reports whether this is a non-empty string-literal union.
func (d TypeDef) IsStringUnion() bool {
	return d.Kind == TypeDefUnion && len(d.UnionMembers) > 0 && strings.HasPrefix(d.UnionMembers[0], `"`)
}

// Refinement represents a cross-field validation constraint emitted as .refine().
type Refinement struct {
	Alternatives        []Refinement  // A single OR group, evaluated with one error path.
	ScalarRule          *ValidateRule // Scalar alternative inside a cross-field OR group.
	MissingTarget       bool          // Go field lookup failed; nefield succeeds, other comparisons fail.
	OtherHidden         *Field        // Go target JSON never sets (unexported or json:"-"); compared against its zero value.
	RuleIndex           int           // One-based index in the original validate rules; zero when unspecified.
	Field               string        // JSON name of the constrained field
	Op                  string        // JS comparison operator: ">=", "<=", ">", "<", "===", "!=="
	OtherField          string        // JSON name of the referenced field
	Tag                 string        // original validate tag name
	WhenAnyPresent      []string      // JSON names from the same nil-able embedded pointer as Field; the rule applies only when one is present
	OtherWhenAnyPresent []string      // the same set for OtherField when it comes from a different embedded pointer
}

// ElementType describes one level of a slice, array, or map element chain.
// The chain stops at a struct element, which has its own TypeDef.
type ElementType struct {
	Equality   *GoEqualityType // Go comparable-value shape, independent of schema aliases.
	ArrayLen   *int64          // Fixed Go array length; nil for slices and legacy metadata.
	EnumValues []string        // Declared constant union values, formatted as TypeScript literals.
	GoType     string          // Go type display for validator fallback comparisons.
	Type       string          // TypeScript representation, preserving named references.
	Inline     *TypeDef        // Anonymous struct metadata.
	Key        *ElementType    // Map key metadata.
	GoKind     string
	IsPointer  bool
	Element    *ElementType
}

// Field represents a field in a TypeScript interface.
type Field struct {
	Equality          *GoEqualityType
	GoName            string       // Original Go field name, before JSON renaming/promotion.
	TypeOverride      bool         // Explicit tstype; public writers must preserve the supplied type.
	ValidationError   string       // Configuration/tag errors reported before schema output.
	ArrayLen          *int64       // Fixed Go array length, including zero-length arrays.
	WhenAnyPresent    []string     // JSON siblings that instantiate the same embedded pointer.
	EnumValues        []string     // Declared constant union values, retained across JSON string encoding.
	GoType            string       // Go type display for validator fallback comparisons.
	Inline            *TypeDef     // Anonymous struct metadata.
	Key               *ElementType // Map key metadata.
	MapKey            bool         // Value is a JSON object key.
	JSONString        bool         // encoding/json string option for supported scalar kinds.
	Name              string
	Type              string
	ZodType           string // Schema type, preserving Go generic identity and ignoring tstype overrides.
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
	validation      *ValidationProgram
	defs            map[string]TypeDef  // key = TypeID (fully-qualified)
	seen            map[string]bool     // key = TypeID (fully-qualified)
	names           map[string]string   // TypeID → short name (for display name resolution)
	metas           map[string]TypeMeta // AST metadata keyed by TypeID
	resolved        map[string]string   // cached: TypeID → display name
	specializations map[string]TypeDef  // Fully qualified instantiated type → concrete schema definition.
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
	"encoding/json.Number":         "json.Number",
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

// displayNames computes the mapping from TypeID → display name. If no
// collisions exist, display names equal short names; otherwise
// UniqueTypeNames prefixes package path segments until the names are distinct.
func (m *Mapper) displayNames() map[string]string {
	if m.resolved != nil {
		return m.resolved
	}
	m.resolved = UniqueTypeNames(slices.Sorted(maps.Keys(m.names)), func(id string) string { return m.names[id] }, func(id string) string {
		if def, ok := m.defs[id]; ok && def.PkgPath != "" {
			return def.PkgPath
		}
		return strings.TrimSuffix(id, "."+m.names[id])
	})
	return m.resolved
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
		defs:            make(map[string]TypeDef),
		seen:            make(map[string]bool),
		names:           make(map[string]string),
		metas:           metas,
		specializations: make(map[string]TypeDef),
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
		d = ResolveTypeDef(d, display)
		for _, instance := range m.specializations {
			if strings.HasPrefix(instance.ID, d.ID+"[") {
				d.Specializations = append(d.Specializations, ResolveTypeDef(instance, display))
			}
		}
		slices.SortFunc(d.Specializations, func(a, b TypeDef) int { return cmp.Compare(a.Name, b.Name) })
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
	return m.convertSubscriptionOutput(t, m.convert)
}

// ConvertSubscriptionOutputZod preserves concrete Go identity inside tracked
// event payloads for writers that need the decoded array shape.
func (m *Mapper) ConvertSubscriptionOutputZod(t types.Type) string {
	return m.convertSubscriptionOutput(t, m.ConvertZod)
}

func (m *Mapper) convertSubscriptionOutput(t types.Type, convert func(types.Type) string) string {
	unwrapped := types.Unalias(t)
	if ptr, ok := unwrapped.(*types.Pointer); ok {
		unwrapped = types.Unalias(ptr.Elem())
	}
	if named, ok := unwrapped.(*types.Named); ok {
		obj := named.Obj()
		if obj.Pkg() != nil && obj.Pkg().Path() == "github.com/befabri/trpcgo" && obj.Name() == "TrackedEvent" && named.TypeArgs().Len() == 1 {
			return "{ id: string; data: " + convert(named.TypeArgs().At(0)) + " }"
		}
	}
	return convert(t)
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
		result := fmt.Sprintf("Record<%s, %s>", key, val)
		if named, ok := types.Unalias(t.Key()).(*types.Named); ok && len(m.metas[TypeID(named.Obj())].ConstValues) > 0 {
			result = "Partial<" + result + ">"
		}
		return result

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
	if t.TypeArgs().Len() > 0 {
		return m.convertNamedInstance(t)
	}
	underlying := t.Underlying()
	if _, ok := underlying.(*types.Struct); ok {
		return m.convertNamedStruct(t, id, name)
	}
	if token := m.convertNamedMeta(t, id, name, underlying); token != "" {
		return token
	}
	switch underlying.(type) {
	case *types.Map, *types.Slice, *types.Array:
		m.registerAlias(id, name, underlying, m.metas[id], obj)
		return m.typeToken(id, name)
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
		if t.TypeArgs().Len() > 0 {
			m.convertAlias(t.Origin())
			var args []string
			for arg := range t.TypeArgs().Types() {
				args = append(args, m.convert(arg))
			}
			return m.typeToken(id, name) + "<" + strings.Join(args, ", ") + ">"
		}
		m.registerAlias(id, name, t.Rhs(), meta, obj)
		return m.typeToken(id, name)
	}
	return m.convert(t.Rhs())
}

func (m *Mapper) convertSlice(t *types.Slice) string {
	// []byte marshals as base64 string in JSON.
	if basic, ok := types.Unalias(t.Elem()).Underlying().(*types.Basic); ok && basic.Kind() == types.Byte {
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
	def.Refinements = m.collectSchemaFields(st, &def.Fields, &def.Extends, &def.ZodExtends, &def.ExtendsAt, meta.FieldComments)
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
		Underlying:   m.typeField(obj.Type().Underlying()),
	}
}

func (m *Mapper) registerAlias(id, name string, underlying types.Type, meta TypeMeta, obj types.Object) {
	if m.seen[id] {
		return
	}
	m.seen[id] = true
	m.defs[id] = TypeDef{
		ID:         id,
		PkgPath:    pkgPath(obj),
		PkgName:    pkgName(obj),
		Name:       name,
		Kind:       TypeDefAlias,
		Comment:    meta.Comment,
		AliasOf:    m.convert(underlying),
		Underlying: m.typeField(underlying),
	}
	var params *types.TypeParamList
	switch t := obj.Type().(type) {
	case *types.Named:
		params = t.TypeParams()
	case *types.Alias:
		params = t.TypeParams()
	}
	if params.Len() > 0 {
		d := m.defs[id]
		for param := range params.TypeParams() {
			d.TypeParams = append(d.TypeParams, param.Obj().Name())
		}
		m.defs[id] = d
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
	return m.collectSchemaFields(st, fields, extends, nil, nil, fieldComments)
}

func (m *Mapper) collectSchemaFields(st *types.Struct, fields *[]Field, extends, zodExtends *[]string, extendsAt *[]int, fieldComments map[int]string) []Refinement {
	var schemaBases []string
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
		TypeName: func(t types.Type) string {
			if zodExtends != nil {
				schemaBases = append(schemaBases, m.ConvertZod(t))
			}
			return m.convert(t)
		},
		Lookup: func(t types.Type, name string) ([]int, bool) {
			obj, indexes, _ := types.LookupFieldOrMethod(t, false, structPackage(t), name)
			f, ok := obj.(*types.Var)
			return indexes, ok && f.IsField()
		},
		Describe: func(owner types.Type, index int) Field {
			return describeHiddenField(owner.Underlying().(*types.Struct).Field(index).Type())
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
	mapped, bases, positions, refs := CollectJSONFields(types.Type(st), adapter, extends != nil)
	*fields = mapped
	if extends != nil {
		*extends = bases
	}
	if extendsAt != nil {
		*extendsAt = positions
	}
	if zodExtends != nil && len(bases) > 0 {
		*zodExtends = schemaBases
		for i, base := range bases {
			if strings.HasPrefix(base, "Partial<") {
				(*zodExtends)[i] = "Partial<" + (*zodExtends)[i] + ">"
			}
		}
	}
	return refs
}

func (m *Mapper) collectField(field *types.Var, tag, jsonName string, omitempty bool, tstag TSTypeTag, hasTSTag bool, fieldComments map[int]string, index int) Field {
	f := Field{
		GoName:    field.Name(),
		Name:      jsonName,
		Type:      m.convert(field.Type()),
		ZodType:   m.ConvertZod(field.Type()),
		GoKind:    goKind(field.Type()),
		IsPointer: isPointer(field.Type()),
		Optional:  omitempty || isPointer(field.Type()),
	}
	descriptor := m.describeType(field.Type(), make(map[types.Type]bool))
	f.Inline, f.Element, f.Key = descriptor.Inline, descriptor.Element, descriptor.Key
	f.ArrayLen = descriptor.ArrayLen
	f.Equality = descriptor.Equality
	f.GoType = descriptor.GoType
	f.EnumValues = descriptor.EnumValues
	f.JSONString = JSONStringOption(tag, f.GoKind)
	if f.JSONString {
		f.Type, f.ZodType = "string", "string"
	}
	ApplyValidation(&f, tag, m.validation)
	applyTSTypeTag(&f, tstag, hasTSTag)
	f.Comment = fieldComment(tag, fieldComments, index)
	f.ZodOmit = ParseZodOmitTag(tag)
	return f
}

func applyTSTypeTag(f *Field, tstag TSTypeTag, ok bool) {
	if !ok {
		return
	}
	if tstag.Type != "" {
		f.TypeOverride = true
		if f.ZodType == "" {
			f.ZodType = f.Type
		}
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
				return ZodStringLiteral(name)
			}
		} else {
			if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' && r != '$' {
				return ZodStringLiteral(name)
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
	_, ok := types.Unalias(t).(*types.Pointer)
	return ok
}

// goKind returns a Go kind string for Zod type discrimination.
// Dereferences pointers and resolves named types to their underlying basic kind.
func goKind(t types.Type) string {
	// Resolve aliases at every pointer level; aliases of pointers have the
	// same validator semantics as the pointer they name.
	for {
		if alias, ok := t.(*types.Alias); ok {
			if kind := wellKnownGoKinds[TypeID(alias.Obj())]; kind != "" {
				return kind
			}
			t = types.Unalias(t)
		}
		ptr, ok := t.(*types.Pointer)
		if !ok {
			break
		}
		t = ptr.Elem()
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
		if basic, ok := types.Unalias(u.Elem()).Underlying().(*types.Basic); ok && basic.Kind() == types.Byte {
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

// structPackage returns the package whose unexported fields a lookup on t may
// see. validator resolves cross-field targets by Go name, so an unexported
// field of the declaring package is a valid target.
func structPackage(t types.Type) *types.Package {
	if named, ok := types.Unalias(t).(*types.Named); ok {
		return named.Obj().Pkg()
	}
	if st, ok := t.Underlying().(*types.Struct); ok && st.NumFields() > 0 {
		return st.Field(0).Pkg()
	}
	return nil
}

// describeHiddenField records the kind metadata cross-field comparisons need
// for a field JSON never sets, without mapping its type to TypeScript.
func describeHiddenField(t types.Type) Field {
	f := Field{GoKind: goKind(t), IsPointer: isPointer(t), GoType: types.TypeString(t, func(p *types.Package) string { return p.Name() })}
	if array, ok := types.Unalias(t).(*types.Array); ok {
		n := array.Len()
		f.ArrayLen = &n
	}
	return f
}
