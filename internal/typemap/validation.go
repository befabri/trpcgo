package typemap

import (
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/befabri/trpcgo/zodconfig"
)

// ValidationProgram expands aliases before field references and dive scopes
// are bound, so custom configuration uses the same rule compiler as Go tags.
type ValidationProgram struct {
	Config  zodconfig.Config
	aliases map[string][]ValidateRule
}

func CompileValidation(c zodconfig.Config) (*ValidationProgram, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	p := &ValidationProgram{Config: c.Clone(), aliases: make(map[string][]ValidateRule)}
	active := make(map[string]bool)
	var expand func(string) ([]ValidateRule, error)
	expand = func(name string) ([]ValidateRule, error) {
		if rules, ok := p.aliases[name]; ok {
			return cloneValidationRules(rules), nil
		}
		if active[name] {
			return nil, fmt.Errorf("validation alias cycle at %q", name)
		}
		active[name] = true
		defer delete(active, name)
		var rules []ValidateRule
		for _, token := range strings.Split(c.Aliases[name], ",") {
			if _, ok := c.Aliases[token]; ok {
				inner, err := expand(token)
				if err != nil {
					return nil, err
				}
				rules = append(rules, inner...)
			} else {
				rules = append(rules, parseConfiguredToken(token)...)
			}
		}
		if err := p.annotate(rules, false); err != nil {
			return nil, fmt.Errorf("validation alias %q: %w", name, err)
		}
		if _, err := ParseValidationScope(rules); err != nil {
			return nil, fmt.Errorf("validation alias %q: %w", name, err)
		}
		p.aliases[name] = cloneValidationRules(rules)
		return rules, nil
	}
	for name := range c.Rules {
		if supportedZodTags[name] != 0 || validationStructuralTag(name) {
			return nil, fmt.Errorf("custom validation %q shadows a built-in rule; use a distinct name", name)
		}
	}
	for name := range c.Aliases {
		if supportedZodTags[name] != 0 || validationStructuralTag(name) {
			return nil, fmt.Errorf("validation alias %q shadows a built-in rule; use a distinct name", name)
		}
		if _, err := expand(name); err != nil {
			return nil, err
		}
	}
	return p, nil
}

func validationStructuralTag(name string) bool {
	switch name {
	case "-", "dive", "keys", "endkeys", "omitempty", "omitnil", "omitzero", "structonly", "nostructlevel":
		return true
	}
	return false
}

func (p *ValidationProgram) annotate(rules []ValidateRule, alternative bool) error {
	for i := range rules {
		rule := &rules[i]
		if len(rule.Alternatives) > 0 {
			if err := p.annotate(rule.Alternatives, true); err != nil {
				return err
			}
			continue
		}
		if _, alias := p.Config.Aliases[rule.Tag]; alias {
			// Validator resolves an alias only when it is a complete comma token.
			if alternative || rule.HasParam {
				return fmt.Errorf("alias %q must be a complete rule, without parameters or an OR branch", rule.Tag)
			}
		}
		if custom, ok := p.Config.Rules[rule.Tag]; ok {
			custom.GoKinds = slices.Clone(custom.GoKinds)
			rule.Custom = &custom
		}
	}
	return nil
}

func (p *ValidationProgram) rules(tag string) ([]ValidateRule, error) {
	if p == nil {
		return ParseValidateTag(tag), nil
	}
	name := p.Config.TagName
	if name == "" {
		name = "validate"
	}
	body := reflect.StructTag(tag).Get(name)
	if body == "" || body == "-" {
		return nil, nil
	}
	var rules []ValidateRule
	for _, token := range strings.Split(body, ",") {
		if alias, ok := p.aliases[token]; ok {
			rules = append(rules, cloneValidationRules(alias)...)
		} else {
			rules = append(rules, parseConfiguredToken(token)...)
		}
	}
	if err := p.annotate(rules, false); err != nil {
		return nil, err
	}
	return rules, nil
}

// ApplyValidation binds configured rules while original Go field lookup is
// still available. It records errors for the schema writer's path diagnostics.
func ApplyValidation(f *Field, tag string, p *ValidationProgram) {
	rules, err := p.rules(tag)
	if err != nil {
		f.ValidationError = err.Error()
		return
	}
	applyValidateRules(f, rules)
}

func (m *Mapper) SetValidation(p *ValidationProgram) { m.validation = p }

// A whole-tag empty string or '-' has special meaning, but a comma token does
// not. Preserve those tokens for grammar diagnostics after alias expansion.
func parseConfiguredToken(token string) []ValidateRule {
	if token == "" || token == "-" {
		return []ValidateRule{{Tag: token}}
	}
	return ParseValidateTag("validate:" + strconv.Quote(token))
}

// ValidationScope retains container, key and element rules separately. Rules
// remain ordered within each scope, including OR and omission boundaries.
type ValidationScope struct {
	Rules   []ValidateRule
	Keys    *ValidationScope
	Element *ValidationScope
}

// ParseValidationScope compiles the structural part of validator's grammar.
// Invalid key boundaries are errors instead of being applied to map values.
func ParseValidationScope(rules []ValidateRule) (ValidationScope, error) {
	if err := validateScopeGrammar(rules, validateTagText(rules), false); err != nil {
		return ValidationScope{}, err
	}
	return parseValidationScope(rules)
}

// validateTagText writes rules back in validate tag syntax for diagnostics.
func validateTagText(rules []ValidateRule) string {
	parts := make([]string, len(rules))
	for i, rule := range rules {
		parts[i] = ValidateRuleText(rule)
	}
	return strings.Join(parts, ",")
}

// Structural directives are handled before validator parses OR branches. They
// cannot be alternatives or take parameters: otherwise validator treats their
// names as validation functions, which do not exist. validator splits a tag
// on commas and pipes alone, so a doubled or trailing separator leaves an
// empty rule and a space after a comma stays in the rule name; it panics on
// both at its first use, and text names the tag so the report shows them.
func validateScopeGrammar(rules []ValidateRule, text string, alternative bool) error {
	for _, rule := range rules {
		if len(rule.Alternatives) > 0 {
			if err := validateScopeGrammar(rule.Alternatives, text, true); err != nil {
				return err
			}
			continue
		}
		if rule.Tag == "" {
			return fmt.Errorf("empty validation rule in %q: validator reports an invalid validation tag", text)
		}
		if strings.TrimSpace(rule.Tag) != rule.Tag {
			return fmt.Errorf("rule %q in %q keeps its surrounding whitespace: validator reports an undefined validation function", rule.Tag, text)
		}
		switch rule.Tag {
		case "-":
			// ParseValidateTag consumes a whole-tag '-', so any remaining
			// occurrence is an invalid combination with other rules.
			return fmt.Errorf("- must be the entire validation tag")
		case "dive", "keys", "endkeys", "omitempty", "omitnil", "omitzero", "structonly", "nostructlevel":
			if alternative {
				return fmt.Errorf("%s cannot be used in an OR group", rule.Tag)
			}
			if rule.HasParam || rule.Param != "" {
				return fmt.Errorf("%s does not accept a parameter", rule.Tag)
			}
		}
	}
	return nil
}

func parseValidationScope(rules []ValidateRule) (ValidationScope, error) {
	prefix, rest := SplitAtDive(rules)
	scope := ValidationScope{Rules: prefix}
	for _, rule := range prefix {
		if rule.Tag == "keys" || rule.Tag == "endkeys" {
			return scope, fmt.Errorf("%s must belong to a keys/endkeys block immediately after dive", rule.Tag)
		}
	}
	if rest == nil {
		return scope, nil
	}
	if len(rest) > 0 && rest[0].Tag == "keys" {
		depth, end := 1, -1
		for i := 1; i < len(rest); i++ {
			switch rest[i].Tag {
			case "keys":
				depth++
			case "endkeys":
				depth--
			}
			if depth == 0 {
				end = i
				break
			}
		}
		if end < 0 {
			return scope, fmt.Errorf("keys requires a matching endkeys")
		}
		keys, err := parseValidationScope(rest[1:end])
		if err != nil {
			return scope, fmt.Errorf("map keys: %w", err)
		}
		scope.Keys = &keys
		rest = rest[end+1:]
	}
	element, err := parseValidationScope(rest)
	if err != nil {
		return scope, fmt.Errorf("container elements: %w", err)
	}
	scope.Element = &element
	return scope, nil
}

// FieldValidationScope reconstructs the full rule program from legacy field
// boundaries while callers migrate to the structured scope representation.
func FieldValidationScope(f Field) (ValidationScope, error) {
	if f.ElementValidate == nil {
		return ParseValidationScope(f.Validate)
	}
	rules := make([]ValidateRule, 0, len(f.Validate)+len(f.ElementValidate)+1)
	rules = append(rules, f.Validate...)
	rules = append(rules, ValidateRule{Tag: "dive"})
	rules = append(rules, f.ElementValidate...)
	return ParseValidationScope(rules)
}

// ApplyValidateRules attaches the ordered rule program and its diagnostics to
// a field whose Go type metadata is already available. Both mappers use this
// path, so rules on map keys and nested elements are checked against the same
// Go kinds that the schema emitter will use.
func ApplyValidateRules(f *Field, tag string) {
	rules := ParseValidateTag(tag)
	applyValidateRules(f, rules)
}

func applyValidateRules(f *Field, rules []ValidateRule) {
	rules = TruncateAtStructOnly(rules)
	f.Validate, f.ElementValidate = SplitAtDive(rules)
	f.UnsupportedZod = UnsupportedZodRules(rules)
	f.InvalidZod = nil
	f.ValidateOmitempty = false
	if ValidationRequiresPresence(f.Validate) {
		f.Optional = false
	}
	for _, rule := range f.Validate {
		if rule.Tag == "omitempty" || rule.Tag == "omitzero" {
			f.ValidateOmitempty = true
		}
	}
	scope, err := ParseValidationScope(rules)
	if err != nil {
		// The writer reports the malformed tag at the field's path before
		// writing any output, so no scalar diagnostics are inferred from it.
		f.ValidationError = err.Error()
		return
	}
	root := &ElementType{GoKind: f.GoKind, Element: f.Element, Key: f.Key}
	var collect func(ValidationScope, *ElementType)
	collect = func(scope ValidationScope, typ *ElementType) {
		if typ == nil {
			typ = &ElementType{}
		}
		f.InvalidZod = append(f.InvalidZod, InvalidZodRules(scope.Rules, typ.GoKind)...)
		if scope.Keys != nil {
			collect(*scope.Keys, typ.Key)
		}
		if scope.Element != nil {
			element := typ.Element
			if typ.GoKind == "[]byte" {
				element = &ElementType{GoKind: "uint8"}
			}
			collect(*scope.Element, element)
		}
	}
	collect(scope, root)
}

// ZodFieldOptional centralizes the schema's structural optionality. A key is
// optional exactly when the TypeScript type lets a client omit it, so parsed
// values stay assignable to the procedure input; validate omitempty and
// omitzero only admit the zero value. An explicit tstype required override
// wins over omission tags. Promoted fields remain conditional on their
// embedded pointer; the object-level presence checks enforce the override once
// that parent exists.
func ZodFieldOptional(f Field) bool {
	return f.Optional && (!f.Required || len(f.WhenAnyPresent) > 0)
}

// TruncateAtStructOnly drops the rules after structonly or nostructlevel.
// validator stops traversing a field at either directive, so later rules,
// including dive, never run; the directive itself is kept so struct-typed
// fields can report that their nested validation is skipped.
func TruncateAtStructOnly(rules []ValidateRule) []ValidateRule {
	for i, rule := range rules {
		if StructOnlyZodTag(rule.Tag) {
			return rules[:i+1]
		}
	}
	return rules
}

// StructOnlyZodTag reports whether tag ends field validation in validator.
func StructOnlyZodTag(tag string) bool {
	return tag == "structonly" || tag == "nostructlevel"
}
