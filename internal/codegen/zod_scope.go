package codegen

import (
	"fmt"
	"strings"

	"github.com/befabri/trpcgo/internal/typemap"
)

// validateZodScopes runs before writing the module, so malformed structural
// rules produce a field-specific generation error and never a partial schema.
func validateZodScopes(order []string, defs map[string]typemap.TypeDef, strict ...bool) error {
	var checkField func(typemap.Field, typemap.ValidationScope, string) error
	var checkDefinition func(typemap.TypeDef, string) error
	activeElements := make(map[*typemap.ElementType]bool)
	activeInline := make(map[*typemap.TypeDef]bool)
	checkDefinition = func(def typemap.TypeDef, path string) error {
		for _, field := range def.Fields {
			if field.ZodOmit {
				continue
			}
			scope, err := typemap.FieldValidationScope(field)
			if err != nil {
				return fmt.Errorf("zod validation %s.%s: %w", path, field.Name, err)
			}
			if err := checkField(field, scope, path+"."+field.Name); err != nil {
				return err
			}
		}
		if def.Underlying != nil {
			scope, err := typemap.FieldValidationScope(*def.Underlying)
			if err != nil {
				return fmt.Errorf("zod validation %s: %w", path, err)
			}
			return checkField(*def.Underlying, scope, path)
		}
		if def.Kind == typemap.TypeDefAlias {
			return checkField(typemap.Field{Type: def.AliasOf}, typemap.ValidationScope{}, path)
		}
		return nil
	}
	checkField = func(field typemap.Field, scope typemap.ValidationScope, path string) error {
		if field.ValidationError != "" {
			return fmt.Errorf("zod validation %s: %s", path, field.ValidationError)
		}
		field.Validate = scope.Rules
		if err := validateZodConfiguredRules(field, len(strict) > 0 && strict[0]); err != nil {
			return fmt.Errorf("zod validation %s: %w", path, err)
		}
		if err := typemap.ValidateZodFieldRules(field); err != nil {
			return fmt.Errorf("zod validation %s: %w", path, err)
		}
		if err := validateZodArrayRules(field); err != nil {
			return fmt.Errorf("zod validation %s: %w", path, err)
		}
		if err := validateZodReferencePaths(scope.Rules); err != nil {
			return fmt.Errorf("zod validation %s: %w", path, err)
		}
		ts := zodFieldType(field)
		key, value, record := zodRecordTypes(ts)
		kind := field.GoKind
		if record {
			kind = "map"
			if field.Key != nil && (field.Key.IsPointer || field.Key.GoKind != "" && field.Key.GoKind != "string" && !zodIntegerKind(field.Key.GoKind)) {
				return fmt.Errorf("zod validation %s{key}: Go map key kind %s requires a custom JSON decoder; only string and integer keys are supported", path, field.Key.GoKind)
			}
		} else if strings.HasSuffix(ts, "[]") && kind == "" {
			kind = "slice"
		}
		if scope.Keys != nil && kind != "map" {
			return fmt.Errorf("zod validation %s: keys/endkeys requires a map, got %s", path, ts)
		}
		if scope.Element != nil && kind != "map" && kind != "slice" && kind != "array" && kind != "[]byte" {
			return fmt.Errorf("zod validation %s: dive requires an array, slice or map, got %s", path, ts)
		}
		if field.Inline == nil && !record && !strings.HasSuffix(ts, "[]") && strings.Contains(ts, "<") {
			return fmt.Errorf("zod validation %s: generic type %s requires concrete Go type metadata", path, ts)
		}
		if field.Inline != nil && !activeInline[field.Inline] {
			activeInline[field.Inline] = true
			defer delete(activeInline, field.Inline)
			if err := checkDefinition(*field.Inline, path); err != nil {
				return err
			}
		}
		if scope.Keys != nil || (record || field.Key != nil) && (field.Key == nil || !activeElements[field.Key]) {
			keyScope := typemap.ValidationScope{}
			if scope.Keys != nil {
				keyScope = *scope.Keys
			}
			if field.Key != nil && !activeElements[field.Key] {
				activeElements[field.Key] = true
				defer delete(activeElements, field.Key)
			}
			if err := checkField(zodElementField(key, field.Key), keyScope, path+"{key}"); err != nil {
				return err
			}
		}
		if scope.Element != nil || (record || strings.HasSuffix(ts, "[]") || field.Element != nil) && (field.Element == nil || !activeElements[field.Element]) {
			if !record {
				value = unwrapZodParentheses(strings.TrimSuffix(ts, "[]"))
			}
			elementScope := typemap.ValidationScope{}
			if scope.Element != nil {
				elementScope = *scope.Element
			}
			if field.Element != nil && !activeElements[field.Element] {
				activeElements[field.Element] = true
				defer delete(activeElements, field.Element)
			}
			return checkField(zodElementField(value, field.Element), elementScope, path+"[]")
		}
		return nil
	}
	for _, name := range order {
		if err := checkDefinition(defs[name], name); err != nil {
			return err
		}
	}
	return nil
}

func validateZodReferencePaths(rules []typemap.ValidateRule) error {
	for _, rule := range rules {
		if rule.Tag == "" && len(rule.Alternatives) == 0 {
			return fmt.Errorf("empty validation rule")
		}
		if _, crossField := typemap.CrossFieldOp(rule.Tag); crossField && strings.ContainsAny(rule.Param, ".[") {
			return fmt.Errorf("%s=%s: nested field references are not supported", rule.Tag, rule.Param)
		}
		if err := validateZodReferencePaths(rule.Alternatives); err != nil {
			return err
		}
	}
	return nil
}
