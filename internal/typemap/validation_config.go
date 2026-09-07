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
		if supportedZodTags[name] || validationStructuralTag(name) {
			return nil, fmt.Errorf("custom validation %q shadows a built-in rule; use a distinct name", name)
		}
	}
	for name := range c.Aliases {
		if supportedZodTags[name] || validationStructuralTag(name) {
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
