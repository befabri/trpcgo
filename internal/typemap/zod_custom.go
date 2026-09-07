package typemap

import "strings"

// ZodCustomPredicate isolates synchronous developer predicates. A thrown error
// or an accidental Promise is a failed check, never a truthy validation result.
func ZodCustomPredicate(predicate string, arguments ...string) string {
	return "(() => { try { const check: (...args: any[]) => unknown = (" + predicate + "); const result = check(" + strings.Join(arguments, ", ") + "); if (result !== null && (typeof result === 'object' || typeof result === 'function') && 'then' in result && typeof result.then === 'function') { void Promise.resolve(result).catch(() => {}); return false; } return result === true; } catch { return false; } })()"
}

// HasCustomZodRule reports whether a scope contains a client predicate.
func HasCustomZodRule(rules []ValidateRule) bool {
	for _, rule := range rules {
		if rule.Custom != nil && !rule.Custom.ServerOnly || HasCustomZodRule(rule.Alternatives) {
			return true
		}
	}
	return false
}

func zodMissingRulePredicate(f Field, rule ValidateRule, zero string) (string, bool) {
	if len(rule.Alternatives) > 0 {
		var alternatives []string
		for _, branch := range rule.Alternatives {
			predicate, ok := zodMissingRulePredicate(f, branch, zero)
			if !ok {
				return "", false
			}
			alternatives = append(alternatives, "("+predicate+")")
		}
		return "(" + strings.Join(alternatives, " || ") + ")", true
	}
	// Built-in collection checks use an empty representation to measure a nil
	// collection. Explicit predicates receive null so they can distinguish nil
	// from an allocated empty collection, as the Go callback can.
	if rule.Custom != nil && (f.GoKind == "map" || f.GoKind == "slice" || f.GoKind == "[]byte") {
		zero = "null"
	}
	return ZodRulePredicate(f, rule, zero)
}
