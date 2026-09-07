package typemap

import (
	"strconv"
	"strings"
)

// zodCheck is a typed rendering instruction. Constraints are never serialized
// into JavaScript and subsequently parsed to convert between the Zod APIs.
type zodCheck struct {
	method   string
	function string
	args     string
}

func (c zodCheck) functional() string { return "z." + c.function + "(" + c.args + ")" }

// renderZodChecks appends checks to base. Only a chainable concrete string or
// number schema takes the fluent methods; a union built by ApplyZodRules is
// never chainable even when it starts with one of those constructors.
func renderZodChecks(base string, checks []zodCheck, style ZodStyle, chainable bool) string {
	if len(checks) == 0 {
		return base
	}
	// Only concrete string and number schemas have the fluent methods. References,
	// unions, enums and records use checks, retaining their inferred output type.
	fluent := chainable && style == ZodStandard && (strings.HasPrefix(base, "z.string()") || strings.HasPrefix(base, "z.number()") || strings.HasPrefix(base, "z.int()") || strings.HasPrefix(base, "z.int32()") || strings.HasPrefix(base, "z.uint32()") || strings.HasPrefix(base, "z.float32()") || strings.HasPrefix(base, "z.float64()"))
	if fluent {
		for _, c := range checks {
			if c.method != "" {
				base += "." + c.method + "(" + c.args + ")"
			} else {
				base += ".check(" + c.functional() + ")"
			}
		}
		return base
	}
	funcs := make([]string, len(checks))
	for i, c := range checks {
		funcs[i] = c.functional()
	}
	return base + ".check(" + strings.Join(funcs, ", ") + ")"
}

// ApplyZodRules applies the ordered rules in f to an already-built schema. The
// caller owns optionality and collection dive/key scopes. Omission only skips
// rules after its position; it never weakens the checks before it, so those
// checks wrap the omission union instead of being chained onto it.
func ApplyZodRules(base string, f Field, style ZodStyle) string {
	for i, rule := range f.Validate {
		if !OmissionZodTag(rule.Tag) {
			continue
		}
		rest := f
		rest.Validate = f.Validate[i+1:]
		result := ApplyZodRules(base, rest, style)
		chainable := true
		if result != base {
			if predicate := zodOmitZeroPredicate(f, rule.Tag); predicate != "" {
				// Collections keep their own schema type. The omitted rules
				// still run on a non-empty value.
				result = base + ".check(z.refine((value) => " + predicate + " || " + result + ".safeParse(value).success))"
				chainable = false
			} else if zero := zodOmissionZero(f, rule.Tag); zero != "" {
				chainable = false
				if style == ZodMini {
					result = "z.union([" + result + ", " + zero + "])"
				} else {
					result += ".or(" + zero + ")"
				}
			}
		}
		prefix := f
		prefix.Validate = f.Validate[:i]
		return applyPlainZodRules(result, prefix, style, false, chainable)
	}
	return applyPlainZodRules(base, f, style, true, true)
}

// OmissionZodTag reports whether tag skips the rules after it for some value:
// omitempty and omitzero for a Go zero value, omitnil for a nil value.
func OmissionZodTag(tag string) bool {
	return tag == "omitempty" || tag == "omitzero" || tag == "omitnil"
}

// zodOmissionZero returns the schema for the values that an omission tag lets
// bypass the rules after it. validator's omitempty dereferences a pointer and
// then tests it as a value, so a pointer to zero is never empty, while omitzero
// tests the dereferenced value itself.
func zodOmissionZero(f Field, tag string) string {
	switch tag {
	case "omitempty":
		if f.IsPointer {
			return ""
		}
	case "omitzero":
	default:
		return ""
	}
	return zodFieldZero(f)
}

// zodOmitZeroPredicate tests the collection values omitzero treats as zero: an
// empty slice or map, which omitempty still validates as a non-nil value.
func zodOmitZeroPredicate(f Field, tag string) string {
	if tag != "omitzero" {
		return ""
	}
	switch f.GoKind {
	case "slice":
		return "value.length === 0"
	case "map":
		return "Object.keys(value).length === 0"
	}
	return ""
}

func zodFieldZero(f Field) string {
	if f.GoKind == "float32" {
		return "z.number().check(z.refine((value) => Math.fround(value) === 0))"
	}
	if f.GoKind == "string" || f.GoKind == "[]byte" || f.GoKind == "" && f.Type == "string" {
		return `z.literal("")`
	}
	if isNumericField(f) {
		return "z.literal(0)"
	}
	if f.GoKind == "bool" || f.Type == "boolean" {
		return "z.literal(false)"
	}
	return ""
}

func applyPlainZodRules(base string, f Field, style ZodStyle, selectBase, chainable bool) string {
	selected := ""
	if selectBase && base == ZodBaseForTSType(f.Type, f.GoKind) {
		candidate := zodBaseFromKindAndType(f.Type, f.GoKind, f.Validate)
		if candidate != "" && candidate != base {
			base = candidate
			for _, rule := range f.Validate {
				if one := zodBaseFromKindAndType(f.Type, f.GoKind, []ValidateRule{rule}); one == base {
					selected = rule.Tag
					break
				}
			}
		}
	}
	var checks []zodCheck
	// An enum narrows values but must not erase the Go wire bounds.
	if isNumericField(f) && (strings.HasPrefix(base, "z.literal(") || strings.HasPrefix(base, "z.union(")) {
		original := ZodBaseForTSType(f.Type, f.GoKind)
		for _, rule := range f.Validate {
			if rule.Tag != "oneof" {
				continue
			}
			for _, value := range parseOneofValues(rule.Param) {
				if !numericOneofInRange(value, f.GoKind) {
					checks = append(checks, zodCheck{function: "refine", args: "(value) => " + original + ".safeParse(value).success"})
					break
				}
			}
		}
	}
	for _, rule := range f.Validate {
		if rule.Tag != "" && rule.Tag == selected {
			selected = ""
			continue
		}
		if rule.Tag == "required" && (f.GoKind == "string" || f.GoKind == "" && f.Type == "string") && zodRulesRequireNonempty(f.Validate) {
			continue
		}
		checks = append(checks, zodRuleChecks(f, rule)...)
	}
	return renderZodChecks(base, checks, style, chainable)
}

func zodRulesRequireNonempty(rules []ValidateRule) bool {
	for _, rule := range rules {
		if (rule.Tag == "min" || rule.Tag == "len") && zodLengthLiteralAtLeast(rule.Param, 1) {
			return true
		}
		if rule.Tag == "oneof" {
			nonempty := true
			for _, value := range parseOneofValues(rule.Param) {
				nonempty = nonempty && value != ""
			}
			if nonempty && rule.Param != "" {
				return true
			}
		}
		if zodFormatBases[rule.Tag] != "" {
			return true
		}
	}
	return false
}

func zodRuleChecks(f Field, rule ValidateRule) []zodCheck {
	if invalidZodRule(rule, f.GoKind) {
		return nil
	}
	isStr := f.GoKind == "string" || f.GoKind == "" && f.Type == "string"
	// Native numeric checks compare the original JS number. Float32 rules must
	// use predicates over the decoded Go value, including scalar OR branches.
	if len(rule.Alternatives) == 0 && f.GoKind != "float32" {
		switch rule.Tag {
		case "numeric", "number":
			if isNumericField(f) {
				return nil
			}
		case "required":
			if f.IsPointer || f.GoKind == "slice" || f.GoKind == "map" {
				return nil
			}
			if isStr {
				return []zodCheck{{"min", "minLength", "1"}}
			}
		case "startswith", "endswith", "contains":
			if isStr && !strings.ContainsRune(rule.Param, '\uFFFD') {
				name := map[string]string{"startswith": "startsWith", "endswith": "endsWith", "contains": "includes"}[rule.Tag]
				return []zodCheck{{name, name, ZodStringLiteral(rule.Param)}}
			}
		case "min", "max", "len", "gt", "gte", "lt", "lte":
			param, ok := zodConstraintNumberLiteral(rule.Param, f.GoKind, isStr)
			if !ok {
				return nil
			}
			// String lengths stay predicates: validator counts runes, whereas
			// Zod's native length checks count UTF-16 code units. Only a lower
			// bound of at most one code point agrees in both encodings.
			if isStr {
				n, _ := strconv.ParseInt(param, 10, 64)
				switch {
				case (rule.Tag == "min" || rule.Tag == "gte") && n >= 0 && n <= 1:
					return []zodCheck{{"min", "minLength", strconv.FormatInt(n, 10)}}
				case rule.Tag == "gt" && n == 0:
					return []zodCheck{{"min", "minLength", "1"}}
				}
			} else if f.GoKind == "slice" || f.GoKind == "array" {
				// Negative bounds remain predicates: Zod's native checks reject negative
				// lengths during construction, whereas Go evaluates them as comparisons.
				n, _ := strconv.ParseInt(param, 10, 64)
				if n >= 0 && (rule.Tag != "lt" || n > 0) && (rule.Tag != "gt" || n < 9007199254740991) {
					method := map[string]string{"min": "min", "max": "max", "len": "length", "gte": "min", "gt": "min", "lte": "max", "lt": "max"}[rule.Tag]
					if rule.Tag == "gt" {
						n++
					}
					if rule.Tag == "lt" {
						n--
					}
					function := map[string]string{"min": "minLength", "max": "maxLength", "length": "length"}[method]
					// Arrays may be unions or named references, so always use checks there.
					return []zodCheck{{function: function, args: strconv.FormatInt(n, 10)}}
				}
			} else if isNumericField(f) {
				method := map[string]string{"min": "gte", "max": "lte", "gt": "gt", "gte": "gte", "lt": "lt", "lte": "lte"}[rule.Tag]
				if rule.Tag == "len" {
					return []zodCheck{{"gte", "gte", param}, {"lte", "lte", param}}
				}
				return []zodCheck{{method, method, param}}
			}
		}
	}
	predicate, ok := ZodRulePredicate(f, rule, "value")
	if !ok || predicate == "true" {
		return nil
	}
	args := "(value) => " + predicate
	if rule.Custom != nil && rule.Custom.Message != "" {
		args += ", { message: " + ZodStringLiteral(rule.Custom.Message) + " }"
	}
	return []zodCheck{{function: "refine", args: args}}
}

// ZodRulePredicate returns a pure JavaScript predicate for a rule, suitable for
// OR branches and caller-owned refinements. Unsupported/invalid rules return
// false so diagnostics can identify them rather than emit malformed code.
func ZodRulePredicate(f Field, rule ValidateRule, value string) (string, bool) {
	if rule.Custom != nil {
		if rule.Custom.ServerOnly || invalidZodRule(rule, f.GoKind) {
			return "", false
		}
		if f.JSONString && (isSignedIntegerKind(f.GoKind) || isUnsignedIntegerKind(f.GoKind)) {
			value = "BigInt(" + value + ")"
		}
		switch f.GoKind {
		case "string", "json.Number":
			value = ZodGoStringValue(value)
		case "float32":
			value = "Math.fround(" + value + ")"
		}
		return ZodCustomPredicate(rule.Custom.Predicate, value, ZodStringLiteral(rule.Param)), true
	}
	if f.GoKind == "json.Number" {
		f.GoKind = "string"
		value = "String(" + value + ")"
	}
	if len(rule.Alternatives) > 0 {
		branches := make([]string, 0, len(rule.Alternatives))
		for _, branch := range rule.Alternatives {
			predicate, ok := ZodRulePredicate(f, branch, value)
			if !ok {
				return "", false
			}
			branches = append(branches, "("+predicate+")")
		}
		return "(" + strings.Join(branches, " || ") + ")", true
	}
	if f.GoKind == "float32" {
		value = "Math.fround(" + value + ")"
	}
	if invalidZodRule(rule, f.GoKind) {
		return "", false
	}
	isStr := f.GoKind == "string" || f.GoKind == "" && f.Type == "string"
	// A lone surrogate is one code point in JavaScript and one replacement
	// rune in Go, so lengths agree on the raw value.
	wire := value
	if isStr {
		value = ZodGoStringValue(value)
	}
	if format := zodFormatBases[rule.Tag]; format != "" && isStr {
		return format + ".safeParse(" + value + ").success", true
	}
	if regex := zodStringRegexes[rule.Tag]; regex != "" && isStr {
		return regex + ".test(" + value + ")", true
	}
	param := ZodStringLiteral(rule.Param)
	switch rule.Tag {
	case "required":
		if f.IsPointer || f.GoKind == "map" || f.GoKind == "slice" {
			return value + " != null", true
		}
		if isStr {
			return value + `.length > 0`, true
		}
		if isNumericField(f) {
			return "Number(" + value + ") !== 0", true
		}
		if f.GoKind == "bool" || f.Type == "boolean" {
			return "Boolean(" + value + ")", true
		}
		if f.GoKind == "time.Time" {
			return "!" + ZodGoTimeIsZero(value), true
		}
		return "", false
	case "numeric", "number":
		if isNumericField(f) {
			return "true", true
		}
	case "lowercase", "uppercase":
		if isStr {
			return zodGoUnicodePredicate(rule.Tag, value), true
		}
	case "oneof":
		values := parseOneofValues(rule.Param)
		lits := make([]string, len(values))
		for i, v := range values {
			if isNumericField(f) {
				lit, ok := zodNumericOneofLiteral(v, f.GoKind)
				if !ok {
					return "", false
				}
				lits[i] = lit
			} else {
				lits[i] = ZodStringLiteral(v)
			}
		}
		return "([" + strings.Join(lits, ", ") + "] as readonly unknown[]).includes(" + value + ")", true
	case "min", "max", "len", "gt", "gte", "lt", "lte", "eq", "ne":
		if (rule.Tag == "eq" || rule.Tag == "ne") && f.GoKind == "bool" {
			target, err := strconv.ParseBool(rule.Param)
			if err != nil {
				return "", false
			}
			op := " === "
			if rule.Tag == "ne" {
				op = " !== "
			}
			return "Boolean(" + value + ")" + op + strconv.FormatBool(target), true
		}
		if (rule.Tag == "eq" || rule.Tag == "ne") && isStr {
			op := " === "
			if rule.Tag == "ne" {
				op = " !== "
			}
			return value + op + param, true
		}
		n, ok := zodConstraintNumberLiteral(rule.Param, f.GoKind, isStr)
		if !ok {
			return "", false
		}
		left := value
		switch {
		case isStr:
			left = "Array.from(" + wire + ").length"
		case f.GoKind == "[]byte":
			left = "atob(" + value + ").length"
		case f.GoKind == "map":
			left = "Object.keys(" + value + ").length"
		case f.GoKind == "slice" || f.GoKind == "array":
			left = value + ".length"
		case !isNumericField(f):
			return "", false
		}
		op := map[string]string{"min": ">=", "max": "<=", "len": "===", "gt": ">", "gte": ">=", "lt": "<", "lte": "<=", "eq": "===", "ne": "!=="}[rule.Tag]
		if f.GoKind == "[]byte" {
			// Zod may continue refinements after the base format check fails.
			// A malformed Base64 payload must produce a validation issue, never
			// escape safeParse as an exception from the browser's decoder.
			return "(() => { try { return " + left + " " + op + " " + n + "; } catch { return false; } })()", true
		}
		return left + " " + op + " " + n, true
	case "startswith", "endswith", "contains", "startsnotwith", "endsnotwith", "excludes":
		if !isStr {
			return "", false
		}
		method := map[string]string{"startswith": "startsWith", "startsnotwith": "startsWith", "endswith": "endsWith", "endsnotwith": "endsWith", "contains": "includes", "excludes": "includes"}[rule.Tag]
		prefix := ""
		if rule.Tag == "startsnotwith" || rule.Tag == "endsnotwith" || rule.Tag == "excludes" {
			prefix = "!"
		}
		return prefix + value + "." + method + "(" + param + ")", true
	case "containsany", "excludesall":
		if !isStr {
			return "", false
		}
		prefix := ""
		if rule.Tag == "excludesall" {
			prefix = "!"
		}
		return prefix + "Array.from(" + param + ").some((rune) => " + value + ".includes(rune))", true
	case "unique":
		return zodUniquePredicate(f, rule.Param, value)
	}
	return "", false
}

func zodJSONString(f Field, inner string, style ZodStyle) string {
	// Normalize only the refinement's local parameter. The schema still
	// returns the original wire string, including the quoted null spelling.
	nullPrefix := ""
	if !f.MapKey {
		if f.IsPointer {
			nullPrefix = `if (value === "null") return ` + ZodMissingValuePredicate(f) + "; "
		} else {
			nullPrefix = `if (value === "null") value = ` + ZodStringLiteral(zodQuotedZero(f.GoKind)) + "; "
		}
	}
	guard := ""
	parsed := "JSON.parse(value)"
	switch {
	case isSignedIntegerKind(f.GoKind) || isUnsignedIntegerKind(f.GoKind):
		pattern := `/^-?[0-9]+$(?![\s\S])/`
		if f.MapKey {
			pattern = `/^[+-]?[0-9]+$(?![\s\S])/`
		}
		if isUnsignedIntegerKind(f.GoKind) {
			pattern = `/^[0-9]+$(?![\s\S])/`
		}
		lo, hi := "-9223372036854775808", "9223372036854775807"
		if isUnsignedIntegerKind(f.GoKind) {
			lo, hi = "0", "18446744073709551615"
		}
		switch f.GoKind {
		case "int8":
			lo, hi = "-128", "127"
		case "int16":
			lo, hi = "-32768", "32767"
		case "int32":
			lo, hi = "-2147483648", "2147483647"
		case "uint8":
			hi = "255"
		case "uint16":
			hi = "65535"
		case "uint32":
			hi = "4294967295"
		}
		guard = "if (!" + pattern + ".test(value)) return false; const integer = BigInt(value); if (integer < " + lo + "n || integer > " + hi + "n) return false; "
		return "z.string().check(z.refine((value) => { try { " + nullPrefix + guard + "return " + integerWireRules(f, f.Validate) + "; } catch { return false; } }))"
	case f.GoKind == "bool":
		guard = `if (value !== "true" && value !== "false") return false; `
	case f.GoKind == "string":
		guard = `if (!value.startsWith('"') || !value.endsWith('"')) return false; `
	case f.GoKind == "float32" || f.GoKind == "float64":
		// The wire parser enforces the destination's range. A quoted -Inf is
		// valid Go input, so scalar rules must not add z.number's finite policy.
		inner = ApplyZodRules("z.custom<number>()", f, style)
		guard = "const decoded = " + ZodQuotedFloatValue("value", f.GoKind) + "; if (Number.isNaN(decoded)) return false; "
		parsed = "decoded"
	}
	return "z.string().check(z.refine((value) => { try { " + nullPrefix + guard + "return " + inner + ".safeParse(" + parsed + ").success; } catch { return false; } }))"
}

func slicesContainReplacementRune(values []string) bool {
	for _, value := range values {
		if strings.ContainsRune(value, '\uFFFD') {
			return true
		}
	}
	return false
}

// Integer JSON strings and map keys retain the full Go range. Running them
// through Number before validation loses exactly the precision this wire
// representation exists to preserve, so comparisons use BigInt throughout.
func integerWireRules(f Field, rules []ValidateRule) string {
	var clauses []string
	for i, rule := range rules {
		if rule.Tag == "omitempty" || rule.Tag == "omitzero" {
			if f.IsPointer && rule.Tag == "omitempty" {
				clauses = append(clauses, integerWireRules(f, rules[i+1:]))
			} else {
				clauses = append(clauses, "(integer === 0n || ("+integerWireRules(f, rules[i+1:])+"))")
			}
			break
		}
		if rule.Tag == "omitnil" {
			continue
		}
		if rule.Custom != nil {
			if !rule.Custom.ServerOnly && !invalidZodRule(rule, f.GoKind) {
				clauses = append(clauses, ZodCustomPredicate(rule.Custom.Predicate, "integer", ZodStringLiteral(rule.Param)))
			}
			continue
		}
		if len(rule.Alternatives) > 0 {
			var alternatives []string
			for _, branch := range rule.Alternatives {
				alternatives = append(alternatives, "("+integerWireRules(f, []ValidateRule{branch})+")")
			}
			clauses = append(clauses, "("+strings.Join(alternatives, " || ")+")")
			continue
		}
		switch rule.Tag {
		case "required":
			if !f.IsPointer {
				clauses = append(clauses, "integer !== 0n")
			}
		case "min", "max", "len", "gt", "gte", "lt", "lte", "eq", "ne":
			n, ok := zodConstraintNumberLiteral(rule.Param, f.GoKind, false)
			if !ok {
				continue
			}
			op := map[string]string{"min": ">=", "max": "<=", "len": "===", "gt": ">", "gte": ">=", "lt": "<", "lte": "<=", "eq": "===", "ne": "!=="}[rule.Tag]
			clauses = append(clauses, "integer "+op+" "+n+"n")
		case "oneof":
			var values []string
			for _, value := range parseOneofValues(rule.Param) {
				n, ok := zodNumericOneofLiteral(value, f.GoKind)
				if ok {
					values = append(values, n+"n")
				}
			}
			clauses = append(clauses, "["+strings.Join(values, ", ")+"].includes(integer)")
		}
	}
	if len(clauses) == 0 {
		return "true"
	}
	return strings.Join(clauses, " && ")
}

func numericOneofInRange(value, kind string) bool {
	bits := 64
	switch kind {
	case "int8", "uint8":
		bits = 8
	case "int16", "uint16":
		bits = 16
	case "int32", "uint32":
		bits = 32
	}
	if isSignedIntegerKind(kind) {
		_, err := strconv.ParseInt(value, 10, bits)
		return err == nil
	}
	if isUnsignedIntegerKind(kind) {
		_, err := strconv.ParseUint(value, 10, bits)
		return err == nil
	}
	return false
}
