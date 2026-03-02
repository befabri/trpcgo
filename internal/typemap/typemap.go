package typemap

import (
	"fmt"
	"go/types"
	"reflect"
	"sort"
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
	Comment      string   // Go doc comment → JSDoc
	TypeParams   []string // Generic type parameter names: ["T", "U"]
	Extends      []string // base types for TypeScript extends clause
	Fields       []Field  // Kind == TypeDefInterface
	UnionMembers []string // Kind == TypeDefUnion (TS-formatted values)
	AliasOf      string   // Kind == TypeDefAlias (e.g., "string")
}

// Field represents a field in a TypeScript interface.
type Field struct {
	Name              string
	Type              string
	GoKind            string // Go kind for Zod: "string", "int", "int32", "float64", etc.
	Optional          bool
	Readonly          bool           // from tstype:",readonly"
	Required          bool           // from tstype:",required" (overrides optional)
	ValidateOmitempty bool           // validate:"omitempty" — Zod should allow zero values
	Comment           string         // field doc comment → JSDoc
	Validate          []ValidateRule // parsed validate tag rules (before dive)
	ElementValidate   []ValidateRule // parsed validate tag rules after dive (for slice elements)
	ElementGoKind     string         // Go kind of slice/array element type
}

// Mapper converts Go types to TypeScript type strings and collects interface definitions.
type Mapper struct {
	defs     map[string]TypeDef  // key = TypeID (fully-qualified)
	seen     map[string]bool     // key = TypeID (fully-qualified)
	names    map[string]string   // TypeID → short name (for display name resolution)
	metas    map[string]TypeMeta // AST metadata keyed by TypeID
	resolved map[string]string   // cached: TypeID → display name
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

// tokenDelim is the delimiter used to wrap type IDs in token strings.
// The § character cannot appear in valid Go identifiers or TS type strings.
const tokenDelim = "§"

// typeToken creates a resolvable token string for a named type.
// Tokens preserve type identity through string composition (arrays, generics, etc.).
func (m *Mapper) typeToken(id, shortName string) string {
	m.names[id] = shortName
	m.resolved = nil // invalidate cache
	return tokenDelim + id + tokenDelim
}

// resolveTokens replaces all §id§ tokens in s with display names.
func resolveTokens(s string, display map[string]string) string {
	if !strings.Contains(s, tokenDelim) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for {
		start := strings.Index(s, tokenDelim)
		if start < 0 {
			b.WriteString(s)
			break
		}
		b.WriteString(s[:start])
		s = s[start+len(tokenDelim):]
		end := strings.Index(s, tokenDelim)
		if end < 0 {
			// Malformed token — write delimiter and continue.
			b.WriteString(tokenDelim)
			continue
		}
		id := s[:end]
		if name, ok := display[id]; ok {
			b.WriteString(name)
		} else {
			// Unknown token — keep as-is for debugging.
			b.WriteString(id)
		}
		s = s[end+len(tokenDelim):]
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
	return resolveTokens(s, m.displayNames())
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
			d.Fields[i].Type = resolveTokens(d.Fields[i].Type, display)
		}
		// Resolve tokens in alias target.
		if d.AliasOf != "" {
			d.AliasOf = resolveTokens(d.AliasOf, display)
		}
		result = append(result, d)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result
}

// Convert maps a Go type to its TypeScript representation.
// Named struct types generate interface definitions as a side effect.
func (m *Mapper) Convert(t types.Type) string {
	return m.convert(t)
}

func (m *Mapper) convert(t types.Type) string {
	switch t := t.(type) {
	case *types.Named:
		obj := t.Obj()
		name := obj.Name()
		id := TypeID(obj)

		// Check for well-known types.
		if obj.Pkg() != nil {
			fullPath := obj.Pkg().Path() + "." + name
			switch fullPath {
			case "time.Time":
				return "string"
			case "encoding/json.RawMessage":
				return "unknown"
			case "encoding/json.Number":
				return "string"
			}

			// TrackedEvent[T] — unwrap to T for TypeScript output.
			// The tracking ID is a transport concern, not a type concern.
			if fullPath == "github.com/befabri/trpcgo.TrackedEvent" {
				if t.TypeArgs() != nil && t.TypeArgs().Len() == 1 {
					return m.convert(t.TypeArgs().At(0))
				}
			}
		}

		// For named struct types, generate an interface definition.
		underlying := t.Underlying()
		if _, ok := underlying.(*types.Struct); ok {
			// Generic instantiation: Foo[string, int] → Foo<string, number>
			if t.TypeArgs() != nil && t.TypeArgs().Len() > 0 {
				var args []string
				for t0 := range t.TypeArgs().Types() {
					args = append(args, m.convert(t0))
				}
				originID := TypeID(t.Origin().Obj())
				m.resolveGenericStruct(originID, name, t.Origin())
				return fmt.Sprintf("%s<%s>", m.typeToken(originID, name), strings.Join(args, ", "))
			}

			// Generic definition: Foo[T any] — extract type params.
			if t.TypeParams() != nil && t.TypeParams().Len() > 0 {
				m.resolveGenericStruct(id, name, t)
				return m.typeToken(id, name)
			}

			m.resolveStruct(id, name, t)
			return m.typeToken(id, name)
		}

		// Named type with non-struct underlying (e.g., `type Status string`).
		// Check metadata for const groups (→ union) or alias.
		if meta, ok := m.metas[id]; ok {
			if len(meta.ConstValues) > 0 {
				m.registerUnion(id, name, meta, obj)
				return m.typeToken(id, name)
			}
			if meta.IsAlias {
				m.registerAlias(id, name, underlying, meta, obj)
				return m.typeToken(id, name)
			}
		}

		// For other named types, resolve underlying.
		return m.convert(underlying)

	case *types.Alias:
		// Go type alias (type X = Y) — resolve to the aliased type.
		// Check metadata for alias registration (e.g., to emit `export type X = string`).
		obj := t.Obj()
		name := obj.Name()
		id := TypeID(obj)
		if meta, ok := m.metas[id]; ok && meta.IsAlias {
			m.registerAlias(id, name, t.Rhs(), meta, obj)
			return m.typeToken(id, name)
		}
		return m.convert(t.Rhs())

	case *types.TypeParam:
		return t.Obj().Name()

	case *types.Pointer:
		return m.convert(t.Elem())

	case *types.Slice:
		// []byte marshals as base64 string in JSON.
		if basic, ok := t.Elem().(*types.Basic); ok && basic.Kind() == types.Byte {
			return "string"
		}
		elem := m.convert(t.Elem())
		if strings.Contains(elem, "|") {
			return fmt.Sprintf("(%s)[]", elem)
		}
		return elem + "[]"

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

// resolveStruct registers a named struct type as a TypeScript interface.
func (m *Mapper) resolveStruct(id, name string, named *types.Named) {
	if m.seen[id] {
		return
	}
	m.seen[id] = true

	st := named.Underlying().(*types.Struct)
	meta := m.metas[id]
	def := TypeDef{
		ID:      id,
		PkgPath: pkgPath(named.Obj()),
		PkgName: pkgName(named.Obj()),
		Name:    name,
		Kind:    TypeDefInterface,
		Comment: meta.Comment,
	}
	m.collectFields(st, &def.Fields, &def.Extends, meta.FieldComments)
	m.defs[id] = def
}

func (m *Mapper) resolveGenericStruct(id, name string, named *types.Named) {
	if m.seen[id] {
		return
	}
	m.seen[id] = true

	var params []string
	for tparam := range named.TypeParams().TypeParams() {
		params = append(params, tparam.Obj().Name())
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
	m.collectFields(st, &def.Fields, &def.Extends, meta.FieldComments)
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

func (m *Mapper) collectFields(st *types.Struct, fields *[]Field, extends *[]string, fieldComments map[int]string) {
	for i := 0; i < st.NumFields(); i++ {
		field := st.Field(i)
		tag := st.Tag(i)
		jsonName, omitempty, skip := ParseJSONTag(tag)

		if skip {
			continue
		}

		// Check tstype tag for skip.
		tstag, hasTSTag := ParseTSTypeTag(tag)
		if hasTSTag && tstag.Type == "-" {
			continue
		}

		// Handle embedded fields.
		if field.Embedded() && jsonName == "" {
			embType := field.Type()
			isPtr := false
			if ptr, ok := embType.(*types.Pointer); ok {
				embType = ptr.Elem()
				isPtr = true
			}
			embType = types.Unalias(embType)

			// tstype:",extends" — emit extends clause instead of flattening.
			if hasTSTag && tstag.Extends {
				if named, ok := embType.(*types.Named); ok {
					if _, ok := named.Underlying().(*types.Struct); ok {
						tsName := m.convert(embType)
						if isPtr && !tstag.Required {
							tsName = "Partial<" + tsName + ">"
						}
						if extends != nil {
							*extends = append(*extends, tsName)
						}
						continue
					}
				}
			}

			// Default: flatten promoted fields.
			if named, ok := embType.(*types.Named); ok {
				if embSt, ok := named.Underlying().(*types.Struct); ok {
					m.collectFields(embSt, fields, extends, nil)
					continue
				}
			}
		}

		if !field.Exported() {
			continue
		}

		if jsonName == "" {
			jsonName = field.Name()
		}

		tsType := m.convert(field.Type())
		optional := omitempty || isPointer(field.Type())

		f := Field{
			Name:     jsonName,
			Type:     tsType,
			GoKind:   goKind(field.Type()),
			Optional: optional,
		}

		// Parse validate tag and split at dive boundary.
		allRules := ParseValidateTag(tag)
		sliceRules, elemRules := SplitAtDive(allRules)
		f.Validate = sliceRules
		f.ElementValidate = elemRules

		// Extract element Go kind for slice/array fields.
		if f.GoKind == "slice" || f.GoKind == "array" {
			f.ElementGoKind = sliceElementGoKind(field.Type())
		}

		for _, rule := range f.Validate {
			if rule.Tag == "required" {
				f.Optional = false
			}
			if rule.Tag == "omitempty" {
				f.ValidateOmitempty = true
			}
		}

		// Apply tstype tag overrides.
		if hasTSTag {
			if tstag.Type != "" {
				f.Type = tstag.Type
			}
			f.Readonly = tstag.Readonly
			if tstag.Required {
				f.Required = true
				f.Optional = false
			}
		}

		// Apply field comment from metadata.
		if fieldComments != nil {
			if comment, ok := fieldComments[i]; ok {
				f.Comment = comment
			}
		}

		*fields = append(*fields, f)
	}
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
	if st.NumFields() == 0 {
		return "Record<string, never>"
	}
	var parts []string
	for i := 0; i < st.NumFields(); i++ {
		field := st.Field(i)
		if !field.Exported() {
			continue
		}
		tag := st.Tag(i)
		jsonName, omitempty, skip := ParseJSONTag(tag)
		if skip {
			continue
		}
		tstag, hasTSTag := ParseTSTypeTag(tag)
		if hasTSTag && tstag.Type == "-" {
			continue
		}
		if jsonName == "" {
			jsonName = field.Name()
		}
		tsType := m.convert(field.Type())
		if hasTSTag && tstag.Type != "" {
			tsType = tstag.Type
		}
		opt := ""
		if omitempty || isPointer(field.Type()) {
			opt = "?"
		}
		if hasTSTag && tstag.Required {
			opt = ""
		}
		prefix := ""
		if hasTSTag && tstag.Readonly {
			prefix = "readonly "
		}
		parts = append(parts, fmt.Sprintf("%s%s%s: %s", prefix, QuotePropName(jsonName), opt, tsType))
	}
	if len(parts) == 0 {
		return "Record<string, never>"
	}
	return "{ " + strings.Join(parts, "; ") + " }"
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
	parts := strings.Split(jsonTag, ",")
	name = parts[0]
	for _, p := range parts[1:] {
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

	// Check for well-known types first.
	if named, ok := t.(*types.Named); ok {
		if obj := named.Obj(); obj.Pkg() != nil {
			fullPath := obj.Pkg().Path() + "." + obj.Name()
			switch fullPath {
			case "time.Time":
				return "time.Time"
			case "encoding/json.RawMessage":
				return "json.RawMessage"
			}
		}
	}

	// Resolve to underlying type.
	switch u := t.Underlying().(type) {
	case *types.Basic:
		switch u.Kind() {
		case types.String:
			return "string"
		case types.Bool:
			return "bool"
		case types.Int:
			return "int"
		case types.Int8:
			return "int8"
		case types.Int16:
			return "int16"
		case types.Int32:
			return "int32"
		case types.Int64:
			return "int64"
		case types.Uint:
			return "uint"
		case types.Uint8:
			return "uint8"
		case types.Uint16:
			return "uint16"
		case types.Uint32:
			return "uint32"
		case types.Uint64:
			return "uint64"
		case types.Float32:
			return "float32"
		case types.Float64:
			return "float64"
		default:
			return "unknown"
		}
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

// sliceElementGoKind extracts the Go kind of a slice or array's element type.
func sliceElementGoKind(t types.Type) string {
	// Unwrap pointers.
	for {
		if ptr, ok := t.(*types.Pointer); ok {
			t = ptr.Elem()
		} else {
			break
		}
	}
	switch u := t.Underlying().(type) {
	case *types.Slice:
		return goKind(u.Elem())
	case *types.Array:
		return goKind(u.Elem())
	}
	return ""
}
