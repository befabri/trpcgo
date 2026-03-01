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

// ValidateRule represents a single parsed rule from a `validate` struct tag.
type ValidateRule struct {
	Tag   string // "required", "min", "max", "len", "email", etc.
	Param string // "3", "50", etc. (empty for parameterless rules)
}

// ParseValidateTag parses a raw struct tag string for a `validate` tag.
// Returns nil if no validate tag is present.
//
// Format: validate:"required,min=3,max=50,alphanum"
func ParseValidateTag(rawTag string) []ValidateRule {
	tag := reflect.StructTag(rawTag)
	v, ok := tag.Lookup("validate")
	if !ok || v == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	rules := make([]ValidateRule, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" || p == "-" {
			continue
		}
		rule := ValidateRule{}
		if idx := strings.IndexByte(p, '='); idx >= 0 {
			rule.Tag = p[:idx]
			rule.Param = p[idx+1:]
		} else {
			rule.Tag = p
		}
		rules = append(rules, rule)
	}
	return rules
}

// SplitAtDive splits validate rules at the "dive" boundary.
// Rules before "dive" apply to the container (slice/array), rules after apply to elements.
// If no dive tag is present, elementRules is nil.
func SplitAtDive(rules []ValidateRule) (containerRules []ValidateRule, elementRules []ValidateRule) {
	for i, r := range rules {
		if r.Tag == "dive" {
			return rules[:i], rules[i+1:]
		}
	}
	return rules, nil
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
