package typemap

import (
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

var zodGoKindBases = map[string]string{
	"time.Time": "z.string().check(z.refine((value) => " + ZodGoTimeParts("value") + " !== null))",
	// encoding/json uses base64.StdEncoding, which ignores CR and LF and
	// accepts empty data. Keep the wire spelling instead of transforming it.
	"[]byte":      `z.string().check(z.refine((value) => /^(?:[A-Za-z0-9+\/]{4})*(?:[A-Za-z0-9+\/]{2}==|[A-Za-z0-9+\/]{3}=)?$(?![\s\S])/.test(value.replace(/[\r\n]/g, ""))))`,
	"json.Number": "z.number()",
	"int":         "z.int()",
	"int8":        "z.number().check(z.refine(Number.isInteger), z.gte(-128), z.lte(127))",
	"int16":       "z.number().check(z.refine(Number.isInteger), z.gte(-32768), z.lte(32767))",
	"int32":       "z.int32()",
	"int64":       "z.number().check(z.refine((value) => Number.isInteger(value) && value >= -9223372036854775808 && value < 9223372036854775808))",
	"uint":        "z.number().check(z.refine((value) => Number.isInteger(value) && value >= 0 && value < 18446744073709551616))",
	"uint8":       "z.number().check(z.refine(Number.isInteger), z.gte(0), z.lte(255))",
	"uint16":      "z.number().check(z.refine(Number.isInteger), z.gte(0), z.lte(65535))",
	"uint32":      "z.uint32()",
	"uint64":      "z.number().check(z.refine((value) => Number.isInteger(value) && value >= 0 && value < 18446744073709551616))",
	// Decode as Go float32 before checking overflow, while retaining the wire
	// number. Values slightly above MaxFloat32 can still round to a finite value.
	"float32": "z.number().check(z.refine((value) => Number.isFinite(Math.fround(value))))",
	"float64": "z.float64()",
	"string":  "z.string()",
	"bool":    "z.boolean()",
}

var zodTSBases = map[string]string{
	"string":  "z.string()",
	"number":  "z.number()",
	"boolean": "z.boolean()",
	"unknown": "z.unknown()",
}

// ZodStyle controls the output format for Zod schema generation.
type ZodStyle int

const (
	ZodStandard ZodStyle = iota // z.string().min(5).max(100).optional()
	ZodMini                     // z.optional(z.string().check(z.minLength(5), z.maxLength(100)))
)

// ZodType converts a field to a schema while preserving its JSON wire value.
func ZodType(f Field, style ZodStyle) string {
	base := ZodBaseForTSType(f.Type, f.GoKind)
	if base == "" {
		return ""
	}
	result := ApplyZodRules(base, f, style)
	if f.JSONString {
		result = zodJSONString(f, result, style)
	}
	if ZodFieldOptional(f) {
		result = ZodOptionalField(result, f, style)
	}
	return result
}

// ApplyZodOptional applies field optionality without letting an absent property
// bypass validator rules on the Go zero value produced by JSON decoding.
func ApplyZodOptional(base string, f Field, style ZodStyle) string {
	if !ZodFieldOptional(f) {
		return base
	}
	return ZodOptionalField(base, f, style)
}

func ZodOptionalField(base string, f Field, style ZodStyle) string {
	result := ZodOptional(base, style)
	if len(f.WhenAnyPresent) > 0 {
		return result
	}
	predicate := ZodMissingValuePredicate(f)
	if predicate != "true" {
		if HasCustomZodRule(f.Validate) {
			// Application predicates run at parse time, including for a missing
			// field. Evaluating them while constructing the module caches a
			// potentially stateful result and invokes user code before parsing.
			return result + ".check(z.refine((value) => value !== undefined || (" + predicate + ")))"
		}
		return "((" + predicate + ") ? " + result + " : " + base + ")"
	}
	return result
}

// ZodMissingValuePredicate evaluates the ordered field rules against the Go
// zero value of an absent JSON property. It intentionally ignores contextual
// embedded-pointer presence; object emitters apply that context around it.
func ZodMissingValuePredicate(f Field) string {
	if f.Required {
		return "false"
	}
	zero := ""
	switch {
	case f.IsPointer:
		zero = "null"
	case f.GoKind == "string" || f.GoKind == "[]byte" || f.GoKind == "json.Number":
		zero = `""`
	case isNumericField(f):
		zero = "0"
	case f.GoKind == "bool":
		zero = "false"
	case f.GoKind == "map":
		zero = "{}"
	case f.GoKind == "slice":
		zero = "[]"
	case f.GoKind == "time.Time":
		zero = `"0001-01-01T00:00:00Z"`
	}
	if zero == "" {
		return "true"
	}
	var clauses []string
	for _, rule := range f.Validate {
		if rule.Tag == "omitempty" || rule.Tag == "omitzero" {
			break
		}
		if rule.Tag == "dive" {
			// A nil pointer fails on dive itself; a nil slice or map has no
			// elements to validate.
			if f.IsPointer {
				clauses = append(clauses, "false")
			}
			break
		}
		if rule.Tag == "omitnil" {
			if f.IsPointer || f.GoKind == "map" || f.GoKind == "slice" || f.GoKind == "[]byte" {
				break
			}
			// A missing scalar decodes to zero, which is not nil. Its following
			// validators still run, even when json omitempty made the field optional.
			continue
		}
		if _, cross := CrossFieldOp(rule.Tag); cross {
			continue
		}
		if len(UnsupportedZodRules([]ValidateRule{rule})) > 0 || invalidZodRule(rule, f.GoKind) {
			continue
		}
		if f.IsPointer {
			clauses = append(clauses, "false")
			break
		}
		if rule.Tag == "required" && (f.GoKind == "map" || f.GoKind == "slice" || f.GoKind == "[]byte") {
			clauses = append(clauses, "false")
			break
		}
		predicate, ok := zodMissingRulePredicate(f, rule, "("+zero+")")
		if ok && predicate != "true" {
			clauses = append(clauses, "("+predicate+")")
		}
	}
	if f.IsPointer && f.ElementValidate != nil && !zodRulesOmitBeforeDive(f.Validate) {
		// Legacy metadata keeps dive implicit at the ElementValidate boundary.
		clauses = append(clauses, "false")
	}
	if len(clauses) == 0 {
		return "true"
	}
	return strings.Join(clauses, " && ")
}

func zodRulesOmitBeforeDive(rules []ValidateRule) bool {
	for _, rule := range rules {
		if rule.Tag == "dive" {
			return false
		}
		if OmissionZodTag(rule.Tag) {
			return true
		}
	}
	return false
}

// ZodOptional wraps a schema using the selected Zod API.
func ZodOptional(base string, style ZodStyle) string {
	if style == ZodMini {
		return "z.optional(" + base + ")"
	}
	return base + ".optional()"
}

// ZodBaseForTSType converts a TypeScript type string to its Zod 4 base type.
// Used for fields without validate tags or when the field type is a reference.
func ZodBaseForTSType(tsType, goKind string) string {
	return zodBaseFromKindAndType(tsType, goKind, nil)
}

func zodBaseFromKindAndType(tsType, goKind string, rules []ValidateRule) string {

	// z.enum only takes strings in Zod 4; a numeric oneof becomes a union of
	// literals. A json.Number keeps its numeric base: validator compares its
	// decimal text, which the predicate derives from the wire number.
	for _, rule := range rules {
		if rule.Tag == "oneof" && rule.Param != "" && goKind != "json.Number" && !invalidZodRule(rule, goKind) {
			values := parseOneofValues(rule.Param)
			if len(values) == 0 {
				continue
			}
			if isNumericKind(goKind) {
				lits, ok := zodNumericOneofLiterals(values, goKind)
				if !ok {
					continue
				}
				if len(lits) == 1 {
					return lits[0]
				}
				return fmt.Sprintf("z.union([%s])", strings.Join(lits, ", "))
			}
			quoted := make([]string, len(values))
			// A replacement rune can also arrive as a lone JSON surrogate. Its
			// preserved wire value is a string, not the literal type of the rune.
			if slicesContainReplacementRune(values) {
				continue
			}
			for i, v := range values {
				quoted[i] = ZodStringLiteral(v)
			}
			return fmt.Sprintf("z.enum([%s])", strings.Join(quoted, ", "))
		}
	}

	if goKind == "string" || (goKind == "" && tsType == "string") {
		for _, rule := range rules {
			if base := zodFormatBases[rule.Tag]; base != "" {
				return base
			}
		}
	}

	if base := zodGoKindBases[goKind]; base != "" {
		return base
	}

	if base := zodTSBases[tsType]; base != "" {
		return base
	}

	// Arrays, records, and named types are composed by the caller.
	return ""
}

func zodConstraintNumberLiteral(param, goKind string, isStr bool) (string, bool) {
	if isStr || isLengthKind(goKind) {
		return zodLengthLiteral(param)
	}
	switch {
	case isSignedIntegerKind(goKind):
		return zodSignedIntegerLiteral(param)
	case isUnsignedIntegerKind(goKind):
		return zodUnsignedIntegerLiteral(param)
	case goKind == "float32":
		return zodFloatLiteral(param, 32)
	case goKind == "float64":
		return zodFloatLiteral(param, 64)
	default:
		return ZodNumberLiteral(param)
	}
}

func zodLengthLiteralAtLeast(param string, min int64) bool {
	n, ok := zodLengthValue(param)
	return ok && n >= min
}

func zodLengthLiteral(param string) (string, bool) {
	n, ok := zodLengthValue(param)
	if !ok {
		return "", false
	}
	return strconv.FormatInt(n, 10), true
}

func zodLengthValue(param string) (int64, bool) {
	if param == "" || strings.TrimSpace(param) != param {
		return 0, false
	}
	n, err := strconv.ParseInt(param, 0, 64)
	return n, err == nil
}

func zodSignedIntegerLiteral(param string) (string, bool) {
	if param == "" || strings.TrimSpace(param) != param {
		return "", false
	}
	n, err := strconv.ParseInt(param, 0, 64)
	if err != nil {
		return "", false
	}
	return strconv.FormatInt(n, 10), true
}

func zodUnsignedIntegerLiteral(param string) (string, bool) {
	if param == "" || strings.TrimSpace(param) != param {
		return "", false
	}
	n, err := strconv.ParseUint(param, 0, 64)
	if err != nil {
		return "", false
	}
	return strconv.FormatUint(n, 10), true
}

func zodFloatLiteral(param string, bitSize int) (string, bool) {
	if param == "" || strings.TrimSpace(param) != param {
		return "", false
	}
	n, err := strconv.ParseFloat(param, bitSize)
	if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
		return "", false
	}
	// JavaScript parses literals as float64. Preserve the exact rounded float32
	// value rather than printing a shorter spelling that requires float32 parsing.
	return strconv.FormatFloat(n, 'g', -1, 64), true
}

// ZodNumberLiteral returns param when it is safe to emit as a TypeScript
// numeric literal inside generated Zod code.
func ZodNumberLiteral(param string) (string, bool) {
	return zodFloatLiteral(param, 64)
}

// ZodLengthLiteral returns param normalized as the integer literal semantics
// used by go-playground/validator for string, array, slice, and map lengths.
func ZodLengthLiteral(param string) (string, bool) {
	return zodLengthLiteral(param)
}

func zodNumericOneofLiterals(values []string, goKind string) ([]string, bool) {
	if len(values) == 0 {
		return nil, false
	}
	lits := make([]string, len(values))
	for i, v := range values {
		param, ok := zodNumericOneofLiteral(v, goKind)
		if !ok {
			return nil, false
		}
		lits[i] = "z.literal(" + param + ")"
	}
	return lits, true
}

func zodNumericOneofLiteral(value, goKind string) (string, bool) {
	if value == "" || strings.TrimSpace(value) != value {
		return "", false
	}
	if isSignedIntegerKind(goKind) {
		n, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return "", false
		}
		if value != strconv.FormatInt(n, 10) {
			return "", false
		}
		return value, true
	}
	if isUnsignedIntegerKind(goKind) {
		n, err := strconv.ParseUint(value, 10, 64)
		if err != nil {
			return "", false
		}
		if value != strconv.FormatUint(n, 10) {
			return "", false
		}
		return value, true
	}
	return "", false
}

var oneofValuePattern = regexp.MustCompile(`'[^']*'|\S+`)

func parseOneofValues(param string) []string {
	values := oneofValuePattern.FindAllString(param, -1)
	for i, value := range values {
		values[i] = strings.ReplaceAll(value, "'", "")
	}
	return values
}

// supportedZodTags is the complete set of validate tags that produce Zod output.
// Tags not in this set are flagged as unsupported in generated schemas.
var supportedZodTags = map[string]bool{
	// Structural (consumed before Zod generation).
	"required":  true,
	"omitempty": true,
	"omitzero":  true,
	"dive":      true,
	"omitnil":   true, "keys": true, "endkeys": true, "structonly": true, "nostructlevel": true,
	"eq": true, "ne": true, "unique": true, "excludes": true, "containsany": true, "excludesall": true, "startsnotwith": true, "endsnotwith": true, "ascii": true, "printascii": true, "number": true, "alphaunicode": true, "alphanumunicode": true, "base64rawurl": true,
	// Format tags (become Zod base types).
	"email":            true,
	"url":              true,
	"uuid":             true,
	"e164":             true,
	"jwt":              true,
	"base64":           true,
	"base64url":        true,
	"lowercase":        true,
	"uppercase":        true,
	"ip":               true,
	"ipv4":             true,
	"ipv6":             true,
	"hostname":         true,
	"hostname_rfc1123": true,
	"hexadecimal":      true,
	"ulid":             true,
	"mac":              true,
	"cidrv4":           true,
	"cidrv6":           true,
	// Enum.
	"oneof": true,
	// Constraints.
	"min": true,
	"max": true,
	"len": true,
	"gt":  true,
	"gte": true,
	"lt":  true,
	"lte": true,
	// Regex patterns.
	"alphanum": true,
	"alpha":    true,
	"numeric":  true,
	// String constraints.
	"startswith": true,
	"endswith":   true,
	"contains":   true,
	// Cross-field (emitted as .refine() at object level).
	"gtefield": true,
	"ltefield": true,
	"gtfield":  true,
	"ltfield":  true,
	"eqfield":  true,
	"nefield":  true,
}

// SupportedZodTags lists every validate tag the generator translates, sorted.
// The validation contract corpus uses it to prove that each supported tag has
// Go-versus-Zod coverage, so adding a tag here without fixtures fails a test.
func SupportedZodTags() []string {
	return slices.Sorted(maps.Keys(supportedZodTags))
}

// zodStructuralTags never reject a value by themselves. They decide which
// later rules run or which container scope those rules apply to, so the Go
// validator never reports them as a failing tag.
var zodStructuralTags = map[string]bool{
	"omitempty":     true,
	"omitzero":      true,
	"omitnil":       true,
	"dive":          true,
	"keys":          true,
	"endkeys":       true,
	"structonly":    true,
	"nostructlevel": true,
}

// StructuralZodTag reports whether tag is a supported directive rather than a
// rule that can reject a value.
func StructuralZodTag(tag string) bool {
	return zodStructuralTags[tag]
}

// crossFieldOps maps cross-field validate tags to their JavaScript comparison operator.
var crossFieldOps = map[string]string{
	"gtefield": ">=",
	"ltefield": "<=",
	"gtfield":  ">",
	"ltfield":  "<",
	"eqfield":  "===",
	"nefield":  "!==",
}

// CrossFieldOp returns the JavaScript comparison operator for a cross-field
// validate tag. Returns ("", false) for non-cross-field tags.
func CrossFieldOp(tag string) (string, bool) {
	op, ok := crossFieldOps[tag]
	return op, ok
}

// UnsupportedZodRules returns validate rules that have no Zod equivalent.
func UnsupportedZodRules(rules []ValidateRule) []ValidateRule {
	var unsupported []ValidateRule
	for _, r := range rules {
		if len(r.Alternatives) > 0 {
			unsupported = append(unsupported, UnsupportedZodRules(r.Alternatives)...)
			continue
		}
		if r.Custom != nil && !r.Custom.ServerOnly {
			continue
		}
		if !supportedZodTags[r.Tag] {
			unsupported = append(unsupported, r)
		}
	}
	return unsupported
}

// InvalidZodRules returns validate rules that are recognized but cannot be
// emitted safely as Zod code, for example numeric constraints with non-numeric
// parameters.
func InvalidZodRules(rules []ValidateRule, goKind string) []ValidateRule {
	var invalid []ValidateRule
	for _, r := range rules {
		if len(r.Alternatives) > 0 {
			invalid = append(invalid, InvalidZodRules(r.Alternatives, goKind)...)
			continue
		}
		if invalidZodRule(r, goKind) {
			invalid = append(invalid, r)
		}
	}
	return invalid
}

func invalidZodRule(rule ValidateRule, goKind string) bool {
	if rule.Custom != nil {
		if len(rule.Custom.GoKinds) == 0 {
			return false
		}
		for _, allowed := range rule.Custom.GoKinds {
			if goKind == allowed {
				return false
			}
		}
		return true
	}
	if goKind == "json.Number" {
		goKind = "string"
	}
	if len(rule.Alternatives) > 0 {
		return len(InvalidZodRules(rule.Alternatives, goKind)) > 0 || len(UnsupportedZodRules(rule.Alternatives)) > 0
	}
	if (zodFormatBases[rule.Tag] != "" || zodStringRegexes[rule.Tag] != "") && goKind != "" && goKind != "string" && goKind != "json.Number" {
		return !((rule.Tag == "numeric" || rule.Tag == "number") && isNumericKind(goKind))
	}
	if !supportedZodTags[rule.Tag] {
		return false
	}
	switch rule.Tag {
	case "unique":
		return goKind != "slice" && goKind != "array" && goKind != "map"
	case "eq", "ne":
		if goKind == "string" || goKind == "" {
			return false
		}
		if goKind == "bool" {
			_, err := strconv.ParseBool(rule.Param)
			return err != nil
		}
		if !isNumericKind(goKind) {
			return true
		}
		_, ok := zodConstraintNumberLiteral(rule.Param, goKind, false)
		return !ok
	case "lowercase", "uppercase", "startswith", "endswith", "contains", "excludes", "startsnotwith", "endsnotwith", "containsany", "excludesall":
		return goKind != "" && goKind != "string" && goKind != "json.Number"
	case "len":
		if rule.Param == "" {
			return true
		}
		if goKind != "string" && !isLengthKind(goKind) && !isNumericKind(goKind) && goKind != "" {
			return true
		}
		_, ok := zodConstraintNumberLiteral(rule.Param, goKind, goKind == "string")
		return !ok
	case "min", "max", "gt", "gte", "lt", "lte":
		if rule.Param == "" {
			return true
		}
		if goKind != "string" && !isLengthKind(goKind) && !isNumericKind(goKind) && goKind != "" {
			return true
		}
		_, ok := zodConstraintNumberLiteral(rule.Param, goKind, goKind == "string")
		return !ok
	case "oneof":
		if rule.Param == "" {
			return true
		}
		if !isNumericKind(goKind) {
			return goKind != "string" && goKind != ""
		}
		_, ok := zodNumericOneofLiterals(parseOneofValues(rule.Param), goKind)
		return !ok
	default:
		return false
	}
}

func isLengthKind(goKind string) bool {
	switch goKind {
	case "string", "slice", "array", "map", "[]byte":
		return true
	}
	return false
}

func isSignedIntegerKind(goKind string) bool {
	switch goKind {
	case "int", "int8", "int16", "int32", "int64":
		return true
	}
	return false
}

func isUnsignedIntegerKind(goKind string) bool {
	switch goKind {
	case "uint", "uint8", "uint16", "uint32", "uint64":
		return true
	}
	return false
}

func isNumericField(f Field) bool {
	return isNumericKind(f.GoKind) || (f.GoKind == "" && f.Type == "number")
}

// isNumericKind reports whether a Go kind string represents a numeric type.
func isNumericKind(goKind string) bool {
	return isSignedIntegerKind(goKind) ||
		isUnsignedIntegerKind(goKind) ||
		goKind == "float32" ||
		goKind == "float64"
}

// ZodStringLiteral emits a JavaScript string literal. Go's %q uses escapes
// such as \a that JavaScript interprets differently, so use JSON's shared grammar.
func ZodStringLiteral(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}
