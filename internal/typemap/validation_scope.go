package typemap

import "fmt"

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
	if err := validateScopeGrammar(rules, false); err != nil {
		return ValidationScope{}, err
	}
	return parseValidationScope(rules)
}

// Structural directives are handled before validator parses OR branches. They
// cannot be alternatives or take parameters: otherwise validator treats their
// names as validation functions, which do not exist.
func validateScopeGrammar(rules []ValidateRule, alternative bool) error {
	for _, rule := range rules {
		if len(rule.Alternatives) > 0 {
			if err := validateScopeGrammar(rule.Alternatives, true); err != nil {
				return err
			}
			continue
		}
		if rule.Tag == "" {
			return fmt.Errorf("empty validation rule")
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
