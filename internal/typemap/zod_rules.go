package typemap

import (
	"slices"
	"strconv"
	"strings"

	"github.com/befabri/trpcgo/internal/gojson"
)

// zodCheck is a typed rendering instruction. Constraints are never serialized
// into JavaScript and subsequently parsed to convert between the Zod APIs.
// A check with an issue tests args, a predicate of value, and raises the issue,
// a JavaScript object literal, when the predicate fails.
type zodCheck struct {
	method   string
	function string
	args     string
	issue    string
}

func (c zodCheck) functional() string {
	if c.issue != "" {
		return "(" + goIssueCheck + ")((value) => " + c.args + ", " + c.issue + ")"
	}
	return "z." + c.function + "(" + c.args + ")"
}

// goIssueCheck raises a Zod issue when a validator predicate fails. Issues keep
// the codes and fields of Zod's own checks, so Zod words their messages in the
// application's locale.
const goIssueCheck = `(valid: (value: any) => boolean, issue: Record<string, unknown>) => z.superRefine((value: any, ctx) => { if (!valid(value)) ctx.addIssue({ ...issue, input: value } as z.core.$ZodRawIssue); })`

// goSchemaCheck returns a predicate that tests values against a schema, and
// whose issues method returns the issues Zod reports for a value. The schema is
// built on the first call and kept: written inside a check, it would be built
// again on every parse. Building it lazily also lets it reference a schema the
// module declares later.
const goSchemaCheck = `(schema: () => z.core.$ZodType) => { let built: z.core.$ZodType | undefined; const parse = (value: unknown) => { if (built === undefined) built = schema(); return z.safeParse(built, value); }; return Object.assign((value: unknown): boolean => parse(value).success, { issues: (value: unknown): readonly object[] => parse(value).error?.issues ?? [] }); }`

// ZodSchemaCheck returns an expression whose value is a predicate testing a
// value against schema. Evaluate it once, where the enclosing schema or module
// is built, and call the predicate from checks. Its issues method explains a
// failure with the issues of the schema.
func ZodSchemaCheck(schema string) string {
	return "(" + goSchemaCheck + ")(() => " + schema + ")"
}

// zodBoundCheck returns a refine predicate over a string whose body tests a
// decoded value with valid, a predicate bound to schema once.
func zodBoundCheck(schema, body string) string {
	return "((valid: (value: unknown) => boolean) => (value: string) => { " + body + " })(" + ZodSchemaCheck(schema) + ")"
}

func zodIssueCheck(predicate, issue string) zodCheck {
	return zodCheck{args: predicate, issue: issue}
}

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
// caller owns optionality and collection dive/key scopes. An omission skips
// only the rules after it, and only for the value it omits: those rules become
// checks that pass that value, so each keeps the issue for its own failure,
// while the rules before the omission run on every value.
func ApplyZodRules(base string, f Field, style ZodStyle) string {
	base, checks := zodProgramChecks(base, f, zodOmission{})
	return renderZodChecks(base, checks, style, true)
}

// ZodEnumKindBase returns the Go kind's base schema of a scalar field when its
// rules narrow that base to an enumeration, as a oneof rule does, or "" when
// they keep it. Passing the returned base to ApplyZodRules selects the
// enumeration; a named scalar's own schema would keep it a check.
func ZodEnumKindBase(f Field) string {
	kind := ZodBaseForTSType(f.Type, f.GoKind)
	if kind == "" {
		return ""
	}
	selected, _ := zodProgramChecks(kind, f, zodOmission{})
	if strings.HasPrefix(selected, "z.enum(") || strings.HasPrefix(selected, "z.literal(") {
		return kind
	}
	return ""
}

// zodOmission is what earlier omissions let skip the remaining rules: skip
// tests value, and zero is the single omitted scalar as a JavaScript literal.
type zodOmission struct {
	skip, zero string
}

// zodProgramChecks returns the checks for the ordered rules in f.
func zodProgramChecks(base string, f Field, omission zodOmission) (string, []zodCheck) {
	for i, rule := range f.Validate {
		if !OmissionZodTag(rule.Tag) {
			continue
		}
		prefix, rest := f, f
		prefix.Validate, rest.Validate = f.Validate[:i], f.Validate[i+1:]
		base, checks := zodSegmentChecks(base, prefix, omission)
		if skip, zero := zodOmittedValue(f, rule.Tag); skip != "" {
			if omission.skip != "" {
				// A second omission can skip more than one value.
				skip, zero = omission.skip+" || "+skip, ""
			}
			omission = zodOmission{skip: skip, zero: zero}
		}
		base, later := zodProgramChecks(base, rest, omission)
		return base, append(checks, later...)
	}
	return zodSegmentChecks(base, f, omission)
}

// zodSegmentChecks checks rules without an omission among them. Unless an
// earlier omission can skip them, they use native checks and may select the
// schema itself; otherwise each is an issue check that passes the skipped
// value, and a oneof adds that value to the schema's enumeration.
func zodSegmentChecks(base string, f Field, omission zodOmission) (string, []zodCheck) {
	if omission.skip == "" {
		return zodPlainChecks(base, f)
	}
	oneof := -1
	var checks []zodCheck
	if omission.zero != "" && base == ZodBaseForTSType(f.Type, f.GoKind) {
		for i, rule := range f.Validate {
			if rule.Tag != "oneof" || f.GoKind == "json.Number" {
				continue
			}
			if enum := zodOneofBase(f.Type, f.GoKind, rule, omission.zero); enum != "" {
				base, oneof = enum, i
				checks = zodEnumWireChecks(base, f)
			}
			break
		}
	}
	for i, rule := range f.Validate {
		if i == oneof || zodRuleWithoutCheck(f, rule) || zodRequiredImplied(f, rule) {
			continue
		}
		for _, check := range zodRuleIssueChecks(f, rule) {
			check.args = "(" + omission.skip + ") || (" + check.args + ")"
			checks = append(checks, check)
		}
	}
	return base, checks
}

// OmissionZodTag reports whether tag skips the rules after it for some value:
// omitempty and omitzero for a Go zero value, omitnil for a nil value.
func OmissionZodTag(tag string) bool {
	return tag == "omitempty" || tag == "omitzero" || tag == "omitnil"
}

// zodOmittedValue tests value for the value an omission tag skips the later
// rules for. validator's omitempty dereferences a pointer and then tests it as
// a value, so a pointer to zero is never empty, while omitzero tests the
// dereferenced value itself and also treats an empty slice or map as zero,
// which omitempty still validates as a non-nil value. A []byte is a slice of
// its decoded bytes: encoding/json decodes every JSON string, even "", into a
// non-nil slice, so omitempty never skips one, while omitzero skips a decoded
// length of zero. A nil value is handled by the caller's optionality, so
// omitnil adds nothing here. Both test a time.Time with reflect.Value.IsZero,
// so only the zero instant spelled with the Z designator is omitted.
func zodOmittedValue(f Field, tag string) (skip, zero string) {
	switch tag {
	case "omitempty":
		if f.IsPointer || f.GoKind == "[]byte" {
			return "", ""
		}
	case "omitzero":
		switch f.GoKind {
		case "slice":
			return "value.length === 0", ""
		case "map":
			return "Object.keys(value).length === 0", ""
		case "[]byte":
			// Line breaks alone decode to no bytes, as "" does.
			return zodPreparedOperand(f, "value").compare("===", "0"), ""
		}
	default:
		return "", ""
	}
	switch {
	case f.GoKind == "float32":
		return "Math.fround(value) === 0", "0"
	case f.GoKind == "string" || f.GoKind == "" && f.Type == "string":
		return `value === ""`, `""`
	case isNumericField(f):
		return "value === 0", "0"
	case f.GoKind == "bool" || f.Type == "boolean":
		return "value === false", "false"
	case f.GoKind == "time.Time":
		return ZodGoTimeIsZero("value"), ""
	}
	return "", ""
}

func zodPlainChecks(base string, f Field) (string, []zodCheck) {
	selected := ""
	if base == ZodBaseForTSType(f.Type, f.GoKind) {
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
	checks := zodEnumWireChecks(base, f)
	for _, rule := range f.Validate {
		if rule.Tag != "" && rule.Tag == selected {
			selected = ""
			continue
		}
		if zodRequiredImplied(f, rule) {
			continue
		}
		checks = append(checks, zodRuleChecks(f, rule)...)
	}
	return base, checks
}

// zodEnumWireChecks keeps the Go wire bounds of a numeric kind whose oneof
// values leave its range: the enumeration alone would accept them.
func zodEnumWireChecks(base string, f Field) []zodCheck {
	if !isNumericField(f) || !strings.HasPrefix(base, "z.literal(") {
		return nil
	}
	original := ZodBaseForTSType(f.Type, f.GoKind)
	for _, rule := range f.Validate {
		if rule.Tag != "oneof" {
			continue
		}
		for _, value := range parseOneofValues(rule.Param) {
			if !numericOneofInRange(value, f.GoKind) {
				return []zodCheck{{function: "refine", args: ZodSchemaCheck(original)}}
			}
		}
	}
	return nil
}

// zodRequiredImplied reports a required rule that the other rules already
// enforce by rejecting the zero value, so it would only repeat their issue.
func zodRequiredImplied(f Field, rule ValidateRule) bool {
	if rule.Tag != "required" {
		return false
	}
	switch {
	case f.GoKind == "string" || f.GoKind == "" && f.Type == "string":
		return zodRulesRequireNonempty(f.Validate)
	case isNumericField(f) && !f.IsPointer:
		return zodRulesExcludeZero(f)
	}
	return false
}

// zodRulesExcludeZero reports whether a numeric rule rejects zero.
func zodRulesExcludeZero(f Field) bool {
	for _, rule := range f.Validate {
		if len(rule.Alternatives) > 0 || rule.Custom != nil {
			continue
		}
		if rule.Tag == "oneof" {
			if values, ok := zodOneofIssueValues(f, rule.Param); ok && !slices.Contains(values, "0") {
				return true
			}
			continue
		}
		literal, ok := zodConstraintNumberLiteral(rule.Param, f.GoKind, false)
		if !ok {
			continue
		}
		n, err := strconv.ParseFloat(literal, 64)
		if err != nil {
			continue
		}
		switch rule.Tag {
		case "min", "gte":
			if n > 0 {
				return true
			}
		case "gt":
			if n >= 0 {
				return true
			}
		case "max", "lte":
			if n < 0 {
				return true
			}
		case "lt":
			if n <= 0 {
				return true
			}
		case "len", "eq":
			if n != 0 {
				return true
			}
		}
	}
	return false
}

// zodRuleWithoutCheck reports rules that constrain nothing a schema check can
// see: numeric on a Go number, and required on a pointer, slice or map, whose
// presence the caller's optionality and null policy decide.
func zodRuleWithoutCheck(f Field, rule ValidateRule) bool {
	if len(rule.Alternatives) > 0 || f.GoKind == "float32" {
		return false
	}
	switch rule.Tag {
	case "numeric", "number":
		return isNumericField(f)
	case "required":
		// A decoded []byte is never nil, so required only tests presence.
		return f.IsPointer || f.GoKind == "slice" || f.GoKind == "map" || f.GoKind == "[]byte"
	}
	return false
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
		if zodFormats[rule.Tag].base {
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
		if zodRuleWithoutCheck(f, rule) {
			return nil
		}
		switch rule.Tag {
		case "required":
			if isStr {
				return []zodCheck{{method: "min", function: "minLength", args: "1"}}
			}
		case "startswith", "endswith", "contains":
			if isStr && !strings.ContainsRune(rule.Param, '�') {
				name := map[string]string{"startswith": "startsWith", "endswith": "endsWith", "contains": "includes"}[rule.Tag]
				return []zodCheck{{method: name, function: name, args: ZodStringLiteral(rule.Param)}}
			}
		case "min", "max", "len", "gt", "gte", "lt", "lte":
			param, ok := zodConstraintNumberLiteral(rule.Param, f.GoKind, isStr)
			if !ok {
				return nil
			}
			if isStr || f.GoKind == "slice" || f.GoKind == "array" {
				// Zod counts code points since 4.5, as validator counts runes, so
				// string lengths share the native checks. Arrays may be unions or
				// named references, so they always use checks.
				if checks := zodNativeLengthChecks(rule.Tag, param, isStr); checks != nil {
					return checks
				}
			} else if isNumericField(f) {
				method := map[string]string{"min": "gte", "max": "lte", "gt": "gt", "gte": "gte", "lt": "lt", "lte": "lte"}[rule.Tag]
				if rule.Tag == "len" {
					return []zodCheck{{method: "gte", function: "gte", args: param}, {method: "lte", function: "lte", args: param}}
				}
				return []zodCheck{{method: method, function: method, args: param}}
			}
		}
	}
	return zodRuleIssueChecks(f, rule)
}

// zodNativeLengthChecks expresses a validator length bound with Zod's length
// checks, or returns nil for a bound they cannot express: Zod rejects a
// negative length when the check is built, whereas validator compares it.
func zodNativeLengthChecks(tag, param string, fluent bool) []zodCheck {
	n, err := strconv.ParseInt(param, 10, 64)
	if err != nil || n < 0 || tag == "lt" && n == 0 || tag == "gt" && n >= 9007199254740991 {
		return nil
	}
	switch tag {
	case "gt":
		n++
	case "lt":
		n--
	}
	method := map[string]string{"min": "min", "gte": "min", "gt": "min", "max": "max", "lte": "max", "lt": "max", "len": "length"}[tag]
	check := zodCheck{function: map[string]string{"min": "minLength", "max": "maxLength", "length": "length"}[method], args: strconv.FormatInt(n, 10)}
	if fluent {
		check.method = method
	}
	return []zodCheck{check}
}

// zodRuleIssueChecks tests a rule with its predicate and raises the issue a
// Zod check for the same constraint would raise, so Zod words the message and
// applications can translate it. A rule with no Zod counterpart raises a
// custom issue that names the failed validator rule.
func zodRuleIssueChecks(f Field, rule ValidateRule) []zodCheck {
	predicate, ok := ZodRulePredicate(f, rule, "value")
	if !ok || predicate == "true" {
		return nil
	}
	custom := func(message string) []zodCheck {
		return []zodCheck{zodIssueCheck(predicate, `{ code: "custom", message: `+ZodStringLiteral(message)+`, params: { rule: `+ZodStringLiteral(ValidateRuleText(rule))+` } }`)}
	}
	if rule.Custom != nil {
		if rule.Custom.Message != "" {
			return custom(rule.Custom.Message)
		}
		return custom("Invalid input: fails " + ValidateRuleText(rule))
	}
	if len(rule.Alternatives) > 0 {
		branches := make([]string, len(rule.Alternatives))
		for i, branch := range rule.Alternatives {
			branches[i] = ValidateRuleText(branch)
		}
		return custom("Invalid input: must satisfy " + strings.Join(branches, " or "))
	}
	isStr := f.GoKind == "string" || f.GoKind == "json.Number" || f.GoKind == "" && f.Type == "string"
	quoted := ZodStringLiteral(rule.Param)
	if format, ok := zodFormats[rule.Tag]; ok && isStr {
		return []zodCheck{zodIssueCheck(predicate, `{ code: "invalid_format", format: `+ZodStringLiteral(format.issue)+` }`)}
	}
	switch rule.Tag {
	case "required":
		switch {
		case f.GoKind == "bool" || f.Type == "boolean":
			return []zodCheck{zodIssueCheck(predicate, `{ code: "invalid_value", values: [true] }`)}
		case f.GoKind == "string" || f.GoKind == "" && f.Type == "string":
			return []zodCheck{zodIssueCheck(predicate, `{ code: "too_small", origin: "string", minimum: 1, inclusive: true }`)}
		}
		return custom("Required")
	case "lowercase", "uppercase":
		return []zodCheck{zodIssueCheck(predicate, `{ code: "invalid_format", format: `+ZodStringLiteral(rule.Tag)+` }`)}
	case "startswith":
		return []zodCheck{zodIssueCheck(predicate, `{ code: "invalid_format", format: "starts_with", prefix: `+quoted+` }`)}
	case "endswith":
		return []zodCheck{zodIssueCheck(predicate, `{ code: "invalid_format", format: "ends_with", suffix: `+quoted+` }`)}
	case "contains":
		return []zodCheck{zodIssueCheck(predicate, `{ code: "invalid_format", format: "includes", includes: `+quoted+` }`)}
	case "startsnotwith":
		return custom("Invalid string: must not start with " + strconv.Quote(rule.Param))
	case "endsnotwith":
		return custom("Invalid string: must not end with " + strconv.Quote(rule.Param))
	case "excludes":
		return custom("Invalid string: must not include " + strconv.Quote(rule.Param))
	case "containsany":
		return custom("Invalid string: must include one of the characters " + strconv.Quote(rule.Param))
	case "excludesall":
		return custom("Invalid string: must not include any of the characters " + strconv.Quote(rule.Param))
	case "unique":
		if rule.Param != "" {
			return custom("Invalid input: each " + rule.Param + " must be unique")
		}
		return custom("Invalid input: values must be unique")
	case "oneof":
		if values, ok := zodOneofIssueValues(f, rule.Param); ok {
			return []zodCheck{zodIssueCheck(predicate, `{ code: "invalid_value", values: [`+strings.Join(values, ", ")+`] }`)}
		}
	case "eq", "ne", "min", "max", "len", "gt", "gte", "lt", "lte":
		if checks, ok := zodComparisonIssueChecks(f, rule, predicate); ok {
			return checks
		}
		if rule.Tag == "ne" {
			return custom("Invalid input: must not equal " + rule.Param)
		}
	}
	return custom("Invalid input: fails " + ValidateRuleText(rule))
}

// zodOneofIssueValues lists the accepted values for an invalid_value issue.
func zodOneofIssueValues(f Field, param string) ([]string, bool) {
	var values []string
	for _, value := range parseOneofValues(param) {
		if isNumericField(f) {
			literal, ok := zodNumericOneofLiteral(value, f.GoKind)
			if !ok {
				return nil, false
			}
			values = append(values, literal)
		} else {
			values = append(values, ZodStringLiteral(value))
		}
	}
	return values, len(values) > 0
}

// zodComparisonIssueChecks reports a comparison rule as Zod's size or value
// issue. An exact length becomes a lower and an upper bound, each raising the
// issue for its own direction.
func zodComparisonIssueChecks(f Field, rule ValidateRule, predicate string) ([]zodCheck, bool) {
	f, operand := zodRuleOperand(f, "value")
	switch {
	case rule.Tag == "ne":
		return nil, false
	case rule.Tag == "eq" && (f.GoKind == "bool" || f.Type == "boolean"):
		target, err := strconv.ParseBool(rule.Param)
		if err != nil {
			return nil, false
		}
		return []zodCheck{zodIssueCheck(predicate, `{ code: "invalid_value", values: [`+strconv.FormatBool(target)+`] }`)}, true
	case rule.Tag == "eq" && operand.origin == "string":
		// eq on a string compares the value itself, not its length.
		return []zodCheck{zodIssueCheck(predicate, `{ code: "invalid_value", values: [`+ZodStringLiteral(rule.Param)+`] }`)}, true
	case operand.left == "":
		return nil, false
	}
	n, ok := zodConstraintNumberLiteral(rule.Param, f.GoKind, operand.origin == "string")
	if !ok {
		return nil, false
	}
	bound := func(op, code, limit string, inclusive, exact bool) zodCheck {
		issue := `{ code: "` + code + `", origin: "` + operand.origin + `", ` + limit + `: ` + n + `, inclusive: ` + strconv.FormatBool(inclusive)
		if exact {
			issue += `, exact: true`
		}
		return zodIssueCheck(operand.compare(op, n), issue+` }`)
	}
	switch rule.Tag {
	case "min", "gte":
		return []zodCheck{bound(">=", "too_small", "minimum", true, false)}, true
	case "gt":
		return []zodCheck{bound(">", "too_small", "minimum", false, false)}, true
	case "max", "lte":
		return []zodCheck{bound("<=", "too_big", "maximum", true, false)}, true
	case "lt":
		return []zodCheck{bound("<", "too_big", "maximum", false, false)}, true
	}
	if operand.origin == "number" {
		// A number compared for equality has one accepted value.
		return []zodCheck{zodIssueCheck(predicate, `{ code: "invalid_value", values: [`+n+`] }`)}, true
	}
	return []zodCheck{bound(">=", "too_small", "minimum", true, true), bound("<=", "too_big", "maximum", true, true)}, true
}

// zodOperand is the value a rule tests, prepared as validator compares it: a
// string after Go's JSON decoding, with its length in runes; bytes by their
// decoded length; collections by size; and a float32 after rounding.
type zodOperand struct {
	value  string // Go-decoded value
	left   string // compared quantity; empty when size and ordering rules do not apply
	origin string // Zod issue origin of the compared quantity
	bytes  bool   // left decodes Base64, which can throw
}

// zodRuleOperand prepares value for a rule on f and returns f with the kind
// the rule sees: validator compares a json.Number as its decimal text.
func zodRuleOperand(f Field, value string) (Field, zodOperand) {
	if f.GoKind == "json.Number" {
		f.GoKind = "string"
		value = "String(" + value + ")"
	}
	if f.GoKind == "float32" {
		value = "Math.fround(" + value + ")"
	}
	return f, zodPreparedOperand(f, value)
}

func zodPreparedOperand(f Field, value string) zodOperand {
	switch {
	case f.GoKind == "string" || f.GoKind == "" && f.Type == "string":
		// A lone surrogate is one code point in JavaScript and one replacement
		// rune in Go, so lengths agree on the raw value.
		return zodOperand{value: ZodGoStringValue(value), left: "Array.from(" + value + ").length", origin: "string"}
	case f.GoKind == "[]byte":
		return zodOperand{value: value, left: "atob(" + value + ").length", origin: "file", bytes: true}
	case f.GoKind == "map":
		return zodOperand{value: value, left: "Object.keys(" + value + ").length", origin: "map"}
	case f.GoKind == "slice" || f.GoKind == "array":
		return zodOperand{value: value, left: value + ".length", origin: "array"}
	case isNumericField(f):
		return zodOperand{value: value, left: value, origin: "number"}
	}
	return zodOperand{value: value}
}

func (o zodOperand) compare(op, n string) string {
	if o.bytes {
		// Zod may continue checks after the base format check fails. A malformed
		// Base64 payload must produce a validation issue, never escape safeParse
		// as an exception from the browser's decoder.
		return "(() => { try { return " + o.left + " " + op + " " + n + "; } catch { return false; } })()"
	}
	return o.left + " " + op + " " + n
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
	operand := zodPreparedOperand(f, value)
	isStr := operand.origin == "string"
	value = operand.value
	if format, ok := zodFormats[rule.Tag]; ok && isStr {
		return format.predicate(value), true
	}
	param := ZodStringLiteral(rule.Param)
	switch rule.Tag {
	case "required":
		if f.IsPointer || f.GoKind == "map" || f.GoKind == "slice" || f.GoKind == "[]byte" {
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
		if !ok || operand.left == "" {
			return "", false
		}
		op := map[string]string{"min": ">=", "max": "<=", "len": "===", "gt": ">", "gte": ">=", "lt": "<", "lte": "<=", "eq": "===", "ne": "!=="}[rule.Tag]
		return operand.compare(op, n), true
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
	if !f.MapKey && ZodQuotedNull(f.GoKind) {
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
		// strconv, which decodes map keys, takes a leading plus sign. A quoted
		// field takes it too unless the decoder requires a minus sign or digit.
		pattern := `/^[+-]?[0-9]+$(?![\s\S])/`
		if !f.MapKey && gojson.QuotedNumberPrefix {
			pattern = `/^-?[0-9]+$(?![\s\S])/`
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
		if !gojson.ReplacesQuotedSurrogates {
			// Go replaces an unpaired surrogate in the outer string with
			// U+FFFD before reading the nested literal, so one that survives
			// parsing came from an escape in that literal, which is an error.
			guard += `if (/\p{Surrogate}/u.test(JSON.parse(value.replace(/\p{Surrogate}/gu, "\uFFFD")))) return false; `
		}
	case f.GoKind == "json.Number":
		inner = ApplyZodRules("z.string()", f, style)
		parsed = "value"
		if !gojson.LenientQuotedNumber {
			// The payload must be a JSON number; the quoted null spelling is not one.
			guard = `if (!/^-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?$(?![\s\S])/.test(value)) return false; `
			break
		}
		// A lenient decoder stores a payload that starts like a number as the
		// Number's text without checking the rest, unquotes a nested string
		// literal that must then be a valid JSON number, and leaves the quoted
		// null spelling as the zero Number. The rules see that text.
		if !f.IsPointer {
			nullPrefix = `if (value === "null") return valid(""); `
		}
		guard = `if (value.startsWith('"')) { if (!value.endsWith('"')) return false; value = JSON.parse(value); if (typeof value !== "string" || !/^-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?$(?![\s\S])/.test(value)) return false; } else if (!/^[-0-9]/.test(value)) return false; `
	case f.GoKind == "float32" || f.GoKind == "float64":
		// The decoder enforces the destination's range and throws on what Go
		// rejects. Quoted infinities, and NaN where the decoder takes strconv's
		// syntax, are valid Go input, so rules must not add z.number's finite
		// policy.
		inner = ApplyZodRules("z.custom<number>()", f, style)
		guard = "const decoded = " + ZodQuotedFloatValue("value", f.GoKind) + "; "
		parsed = "decoded"
	}
	return "z.string().check(z.refine(" + zodBoundCheck(inner, "try { "+nullPrefix+guard+"return valid("+parsed+"); } catch { return false; }") + "))"
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
