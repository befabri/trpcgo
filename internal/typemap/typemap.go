package typemap

import (
	"fmt"
	"go/types"
	"reflect"
	"sort"
	"strings"
)

// TypeDef represents a TypeScript interface definition.
type TypeDef struct {
	Name   string
	Fields []Field
}

// Field represents a field in a TypeScript interface.
type Field struct {
	Name     string
	Type     string
	Optional bool
}

// Mapper converts Go types to TypeScript type strings and collects interface definitions.
type Mapper struct {
	defs map[string]TypeDef
	seen map[string]bool // prevent infinite recursion
}

func NewMapper() *Mapper {
	return &Mapper{
		defs: make(map[string]TypeDef),
		seen: make(map[string]bool),
	}
}

// Defs returns all collected TypeScript interface definitions, sorted by name.
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
			m.resolveStruct(name, t)
			return name
		}
		// For other named types (type aliases, named basics), resolve underlying.
		return m.convert(underlying)

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

func (m *Mapper) resolveStruct(name string, named *types.Named) {
	if m.seen[name] {
		return
	}
	m.seen[name] = true

	st := named.Underlying().(*types.Struct)
	def := TypeDef{Name: name}
	m.collectFields(st, &def.Fields)
	m.defs[name] = def
}

func (m *Mapper) collectFields(st *types.Struct, fields *[]Field) {
	for i := 0; i < st.NumFields(); i++ {
		field := st.Field(i)
		tag := st.Tag(i)
		jsonName, omitempty, skip := parseJSONTag(tag)

		if skip {
			continue
		}

		// Handle embedded fields: flatten promoted fields.
		if field.Embedded() && jsonName == "" {
			embType := field.Type()
			if ptr, ok := embType.(*types.Pointer); ok {
				embType = ptr.Elem()
			}
			if named, ok := embType.(*types.Named); ok {
				if embSt, ok := named.Underlying().(*types.Struct); ok {
					m.collectFields(embSt, fields)
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

		*fields = append(*fields, Field{
			Name:     jsonName,
			Type:     tsType,
			Optional: optional,
		})
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
		jsonName, omitempty, skip := parseJSONTag(tag)
		if skip {
			continue
		}
		if jsonName == "" {
			jsonName = field.Name()
		}
		tsType := m.convert(field.Type())
		opt := ""
		if omitempty || isPointer(field.Type()) {
			opt = "?"
		}
		parts = append(parts, fmt.Sprintf("%s%s: %s", jsonName, opt, tsType))
	}
	if len(parts) == 0 {
		return "Record<string, never>"
	}
	return "{ " + strings.Join(parts, "; ") + " }"
}

func parseJSONTag(rawTag string) (name string, omitempty bool, skip bool) {
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
