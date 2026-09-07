package codegen

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/befabri/trpcgo/internal/typemap"
	"github.com/befabri/trpcgo/zodconfig"
)

func validateZodConfiguredRules(f typemap.Field, strict bool) error {
	var check func([]typemap.ValidateRule) error
	check = func(rules []typemap.ValidateRule) error {
		for _, rule := range rules {
			if len(rule.Alternatives) > 0 {
				if err := check(rule.Alternatives); err != nil {
					return err
				}
				continue
			}
			if rule.Custom != nil && rule.Custom.ServerOnly {
				continue
			}
			if strict && len(typemap.UnsupportedZodRules([]typemap.ValidateRule{rule})) > 0 {
				return fmt.Errorf("rule %q has no client counterpart; configure a predicate or mark it serverOnly", rule.Tag)
			}
			if (strict || rule.Custom != nil) && len(typemap.InvalidZodRules([]typemap.ValidateRule{rule}, f.GoKind)) > 0 {
				return fmt.Errorf("rule %q with parameter %q cannot validate Go kind %s", rule.Tag, rule.Param, f.GoKind)
			}
		}
		return nil
	}
	return check(f.Validate)
}

// writeZodMissingValueChecks rejects an absent property whose Go zero value
// fails the field's rules. An object ignores the checks on an absent optional
// key, so the test belongs to the containing object rather than the field.
func writeZodMissingValueChecks(ew *errWriter, fields []typemap.Field) {
	for _, field := range fields {
		if field.ZodOmit || len(field.WhenAnyPresent) > 0 || !typemap.ZodFieldOptional(field) {
			continue
		}
		predicate := typemap.ZodMissingValuePredicate(field)
		if predicate != "true" {
			ew.printf(".check(z.refine((data) => %s !== undefined || (%s), { message: %s, path: [%s] }))", zodDataAccess(field.Name), predicate, typemap.ZodStringLiteral(field.Name+" must satisfy validation when absent"), typemap.ZodStringLiteral(field.Name))
		}
	}
}

// Resolve explicit struct registrations before writing any output. Never let a
// misspelled or ambiguous type silently lose an application validation rule.
func resolveZodStructRules(config zodconfig.Config, defs map[string]typemap.TypeDef, reachable map[string]bool) (map[string][]zodconfig.StructRule, error) {
	result := make(map[string][]zodconfig.StructRule)
	for _, key := range slices.Sorted(maps.Keys(config.StructRules)) {
		var matches []string
		for _, name := range slices.Sorted(maps.Keys(defs)) {
			def := defs[name]
			if key == def.Name || key == def.ID || def.PkgPath != "" && key == def.PkgPath+"."+def.Name {
				matches = append(matches, name)
			}
		}
		if len(matches) != 1 {
			return nil, fmt.Errorf("struct validation %q must identify one generated Go type (matched %d)", key, len(matches))
		}
		name := matches[0]
		if defs[name].Kind != typemap.TypeDefInterface {
			return nil, fmt.Errorf("struct validation %q refers to a non-struct type", key)
		}
		if !reachable[name] {
			return nil, fmt.Errorf("struct validation %q refers to %s, which is not a procedure input type or reachable from one", key, name)
		}
		result[name] = append(result[name], config.StructRules[key]...)
	}
	for alias := range config.Imports {
		if strings.HasSuffix(alias, "Schema") && reachable[strings.TrimSuffix(alias, "Schema")] {
			return nil, fmt.Errorf("validation import %q conflicts with a generated schema", alias)
		}
	}
	return result, nil
}
