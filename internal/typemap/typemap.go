package typemap

import (
	"fmt"
	"go/types"
	"reflect"
	"sort"
	"strings"
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
	Name         string
	Kind         TypeDefKind
	Comment      string   // Go doc comment → JSDoc
	TypeParams   []string // Generic type parameter names: ["T", "U"]
	Fields       []Field  // Kind == TypeDefInterface
	UnionMembers []string // Kind == TypeDefUnion (TS-formatted values)
	AliasOf      string   // Kind == TypeDefAlias (e.g., "string")
}

// Field represents a field in a TypeScript interface.
type Field struct {
	Name     string
	Type     string
	Optional bool
	Readonly bool   // from tstype:",readonly"
	Required bool   // from tstype:",required" (overrides optional)
	Comment  string // field doc comment → JSDoc
}

// Mapper converts Go types to TypeScript type strings and collects interface definitions.
type Mapper struct {
	defs  map[string]TypeDef
	seen  map[string]bool
	metas map[string]TypeMeta // AST metadata keyed by types.Object.Id()
}

// NewMapper creates a Mapper. Pass nil for metas if no AST metadata is available.
func NewMapper(metas map[string]TypeMeta) *Mapper {
	if metas == nil {
		metas = make(map[string]TypeMeta)
	}
	return &Mapper{
		defs:  make(map[string]TypeDef),
		seen:  make(map[string]bool),
		metas: metas,
	}
}

// Defs returns all collected TypeScript type definitions, sorted by name.
func (m *Mapper) Defs() []TypeDef {
	var result []TypeDef
	for _, d := range m.defs {
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

		// Check for well-known types.
		if obj.Pkg() != nil {
			fullPath := obj.Pkg().Path() + "." + name
			switch fullPath {
			case "time.Time":
				return "string"
			case "encoding/json.RawMessage":
				return "unknown"
			}
		}

		// For named struct types, generate an interface definition.
		underlying := t.Underlying()
		if _, ok := underlying.(*types.Struct); ok {
			// Generic instantiation: Foo[string, int] → Foo<string, number>
			if t.TypeArgs() != nil && t.TypeArgs().Len() > 0 {
				var args []string
				for i := 0; i < t.TypeArgs().Len(); i++ {
					args = append(args, m.convert(t.TypeArgs().At(i)))
				}
				m.resolveGenericStruct(name, t.Origin())
				return fmt.Sprintf("%s<%s>", name, strings.Join(args, ", "))
			}

			// Generic definition: Foo[T any] — extract type params.
			if t.TypeParams() != nil && t.TypeParams().Len() > 0 {
				m.resolveGenericStruct(name, t)
				return name
			}

			m.resolveStruct(name, t)
			return name
		}

		// Named type with non-struct underlying (e.g., `type Status string`).
		// Check metadata for const groups (→ union) or alias.
		key := obj.Id()
		if meta, ok := m.metas[key]; ok {
			if len(meta.ConstValues) > 0 {
				m.registerUnion(name, meta)
				return name
			}
			if meta.IsAlias {
				m.registerAlias(name, underlying, meta)
				return name
			}
		}

		// For other named types, resolve underlying.
		return m.convert(underlying)

	case *types.Alias:
		// Go type alias (type X = Y) — resolve to the aliased type.
		// Check metadata for alias registration (e.g., to emit `export type X = string`).
		obj := t.Obj()
		name := obj.Name()
		key := obj.Id()
		if meta, ok := m.metas[key]; ok && meta.IsAlias {
			m.registerAlias(name, t.Rhs(), meta)
			return name
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
//
// Known limitation: types are keyed by short name, not package-qualified name.
// If two packages define a type with the same name (e.g., both have "User"),
// only the first one encountered is emitted. Fixing this requires type renaming
// in the TypeScript output (e.g., "PkgA_User" vs "PkgB_User").
func (m *Mapper) resolveStruct(name string, named *types.Named) {
	if m.seen[name] {
		return
	}
	m.seen[name] = true

	st := named.Underlying().(*types.Struct)
	meta := m.metas[named.Obj().Id()]
	def := TypeDef{Name: name, Kind: TypeDefInterface, Comment: meta.Comment}
	m.collectFields(st, &def.Fields, meta.FieldComments)
	m.defs[name] = def
}

func (m *Mapper) resolveGenericStruct(name string, named *types.Named) {
	if m.seen[name] {
		return
	}
	m.seen[name] = true

	var params []string
	for i := 0; i < named.TypeParams().Len(); i++ {
		params = append(params, named.TypeParams().At(i).Obj().Name())
	}

	st := named.Underlying().(*types.Struct)
	meta := m.metas[named.Obj().Id()]
	def := TypeDef{Name: name, Kind: TypeDefInterface, Comment: meta.Comment, TypeParams: params}
	m.collectFields(st, &def.Fields, meta.FieldComments)
	m.defs[name] = def
}

func (m *Mapper) registerUnion(name string, meta TypeMeta) {
	if m.seen[name] {
		return
	}
	m.seen[name] = true
	m.defs[name] = TypeDef{
		Name:         name,
		Kind:         TypeDefUnion,
		Comment:      meta.Comment,
		UnionMembers: meta.ConstValues,
	}
}

func (m *Mapper) registerAlias(name string, underlying types.Type, meta TypeMeta) {
	if m.seen[name] {
		return
	}
	m.seen[name] = true
	m.defs[name] = TypeDef{
		Name:    name,
		Kind:    TypeDefAlias,
		Comment: meta.Comment,
		AliasOf: m.convert(underlying),
	}
}

func (m *Mapper) collectFields(st *types.Struct, fields *[]Field, fieldComments map[int]string) {
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

		// Handle embedded fields: flatten promoted fields.
		if field.Embedded() && jsonName == "" {
			embType := field.Type()
			if ptr, ok := embType.(*types.Pointer); ok {
				embType = ptr.Elem()
			}
			embType = types.Unalias(embType)
			if named, ok := embType.(*types.Named); ok {
				if embSt, ok := named.Underlying().(*types.Struct); ok {
					m.collectFields(embSt, fields, nil)
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
			Optional: optional,
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
		parts = append(parts, fmt.Sprintf("%s%s%s: %s", prefix, jsonName, opt, tsType))
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
		if p == "omitempty" {
			omitempty = true
		}
	}
	return name, omitempty, false
}

func isPointer(t types.Type) bool {
	_, ok := t.(*types.Pointer)
	return ok
}
