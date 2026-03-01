package typemap

import (
	"reflect"
	"strings"
)

// TypeMeta carries AST-level metadata for a named Go type
// that cannot be obtained from types.Type alone.
// Populated by the static analysis path; the reflect path leaves these empty.
type TypeMeta struct {
	Comment       string         // doc comment on the type declaration
	FieldComments map[int]string // struct field index → doc comment
	ConstValues   []string       // TS-formatted const literal values (for union types)
	IsAlias       bool           // true for Go type aliases or defined basic types
}

// TSTypeTag holds the parsed result of a `tstype` struct tag.
type TSTypeTag struct {
	Type     string // overrides the generated TS type (empty = no override)
	Readonly bool
	Required bool
}

// ParseTSTypeTag parses a raw struct tag string for a `tstype` tag.
// Returns ok=false if no tstype tag is present.
func ParseTSTypeTag(rawTag string) (TSTypeTag, bool) {
	tag := reflect.StructTag(rawTag)
	tstype, ok := tag.Lookup("tstype")
	if !ok {
		return TSTypeTag{}, false
	}
	if tstype == "-" {
		return TSTypeTag{Type: "-"}, true
	}
	// Split on commas, but reassemble non-option parts back into the type.
	// This handles TS types with commas like "Record<string, unknown>".
	parts := strings.Split(tstype, ",")
	var result TSTypeTag
	var typeParts []string
	for _, p := range parts {
		switch strings.TrimSpace(p) {
		case "readonly":
			result.Readonly = true
		case "required":
			result.Required = true
		default:
			typeParts = append(typeParts, p)
		}
	}
	result.Type = strings.TrimSpace(strings.Join(typeParts, ","))
	return result, true
}
