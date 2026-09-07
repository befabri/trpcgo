package typemap

import (
	"reflect"
	"strings"

	"github.com/befabri/trpcgo/zodconfig"
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
	Custom   *zodconfig.Rule // Explicit client counterpart; never inferred from a Go callback.
	Tag      string          // "required", "min", "max", "len", "email", etc.
	Param    string          // "3", "50", etc. (empty for parameterless rules)
	HasParam bool            // An explicit '=' was present, including an empty parameter.
	// Alternatives is an ordered OR group. Tag and Param are empty on groups.
	// Splitting grammar before decoding escaped parameters keeps literal pipes
	// distinguishable from the validator's OR operator.
	Alternatives []ValidateRule
}

// ParseValidateTag parses a raw struct tag string for a `validate` tag.
// Returns nil if no validate tag is present.
//
// Format: validate:"required,min=3,max=50,alphanum"
func ParseValidateTag(rawTag string) []ValidateRule {
	tag := reflect.StructTag(rawTag)
	v, ok := tag.Lookup("validate")
	if !ok || v == "" || v == "-" {
		return nil
	}
	var rules []ValidateRule
	for p := range strings.SplitSeq(v, ",") {
		var alternatives []ValidateRule
		for branch := range strings.SplitSeq(p, "|") {
			name, param, hasParam := strings.Cut(branch, "=")
			param = strings.ReplaceAll(strings.ReplaceAll(param, "0x2C", ","), "0x7C", "|")
			alternatives = append(alternatives, ValidateRule{Tag: name, Param: param, HasParam: hasParam})
		}
		if len(alternatives) == 1 {
			rules = append(rules, alternatives[0])
		} else {
			rules = append(rules, ValidateRule{Alternatives: alternatives})
		}
	}
	return rules
}

// ValidationRequiresPresence reports whether required is reached before an
// omission directive can skip it. An OR group requires presence only when all
// its branches do. Container element rules do not affect container presence.
func ValidationRequiresPresence(rules []ValidateRule) bool {
	for _, rule := range rules {
		switch rule.Tag {
		case "omitempty", "omitzero", "omitnil", "dive", "structonly", "nostructlevel":
			return false
		case "required":
			return true
		}
		if len(rule.Alternatives) > 0 {
			all := true
			for _, branch := range rule.Alternatives {
				all = all && ValidationRequiresPresence([]ValidateRule{branch})
			}
			if all {
				return true
			}
		}
	}
	return false
}

// SplitAtDive splits validate rules at the "dive" boundary.
// Rules before "dive" apply to the container (slice/array), rules after apply to elements.
// If no dive tag is present, elementRules is nil.
func SplitAtDive(rules []ValidateRule) (containerRules []ValidateRule, elementRules []ValidateRule) {
	for i, r := range rules {
		if r.Tag == "dive" && !r.HasParam && r.Param == "" {
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
	Extends  bool // embedded field should use TypeScript extends (not flatten)
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
	// Non-option parts are rejoined so a type such as "Record<string, unknown>"
	// survives the split on commas.
	var result TSTypeTag
	var typeParts []string
	for p := range strings.SplitSeq(tstype, ",") {
		switch strings.TrimSpace(p) {
		case "readonly":
			result.Readonly = true
		case "required":
			result.Required = true
		case "extends":
			result.Extends = true
		default:
			typeParts = append(typeParts, p)
		}
	}
	result.Type = strings.TrimSpace(strings.Join(typeParts, ","))
	return result, true
}

// ParseTSDocTag parses a raw struct tag string for a `ts_doc` tag.
// Returns the JSDoc comment text and ok=true if present.
func ParseTSDocTag(rawTag string) (doc string, ok bool) {
	tag := reflect.StructTag(rawTag)
	doc, ok = tag.Lookup("ts_doc")
	return doc, ok
}

// ParseZodOmitTag reports whether the field has `zod_omit:"true"`.
// Fields with this tag are excluded from Zod schema generation
// but still appear in the TypeScript interface.
func ParseZodOmitTag(rawTag string) bool {
	tag := reflect.StructTag(rawTag)
	v, ok := tag.Lookup("zod_omit")
	return ok && v == "true"
}
