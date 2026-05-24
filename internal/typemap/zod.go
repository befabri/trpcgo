package typemap

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

var zodFormatBases = map[string]string{
	"email":            "z.email()",
	"url":              "z.url()",
	"uuid":             "z.uuidv4()",
	"e164":             "z.e164()",
	"jwt":              "z.jwt()",
	"base64":           "z.base64()",
	"lowercase":        "z.lowercase()",
	"ip":               "z.ipv4()",
	"ipv4":             "z.ipv4()",
	"ipv6":             "z.ipv6()",
	"hostname":         "z.hostname()",
	"hostname_rfc1123": "z.hostname()",
	"base64url":        "z.base64url()",
	"hexadecimal":      "z.hex()",
	"ulid":             "z.ulid()",
	"mac":              "z.mac()",
	"cidrv4":           "z.cidrv4()",
	"cidrv6":           "z.cidrv6()",
	"uppercase":        "z.uppercase()",
}

var zodGoKindBases = map[string]string{
	"time.Time": "z.iso.datetime()",
	"[]byte":    "z.base64()",
	"int":       "z.int()",
	"int32":     "z.int32()",
	"int64":     "z.number()",
	"uint32":    "z.uint32()",
	"uint64":    "z.number()",
	"float32":   "z.float32()",
	"float64":   "z.float64()",
	"int8":      "z.number()",
	"int16":     "z.number()",
	"uint":      "z.number()",
	"uint8":     "z.number()",
	"uint16":    "z.number()",
	"string":    "z.string()",
	"bool":      "z.boolean()",
}

var zodTSBases = map[string]string{
	"string":  "z.string()",
	"number":  "z.number()",
	"boolean": "z.boolean()",
	"unknown": "z.unknown()",
}

var zodStringBases = map[string]bool{
	"z.string()":    true,
	"z.email()":     true,
	"z.url()":       true,
	"z.uuidv4()":    true,
	"z.e164()":      true,
	"z.jwt()":       true,
	"z.base64()":    true,
	"z.base64url()": true,
	"z.lowercase()": true,
	"z.uppercase()": true,
	"z.ipv4()":      true,
	"z.ipv6()":      true,
	"z.hostname()":  true,
	"z.hex()":       true,
	"z.ulid()":      true,
	"z.mac()":       true,
	"z.cidrv4()":    true,
	"z.cidrv6()":    true,
}

var zodZeroLiterals = map[string]string{
	"z.int()":     "z.literal(0)",
	"z.int32()":   "z.literal(0)",
	"z.uint32()":  "z.literal(0)",
	"z.float32()": "z.literal(0)",
	"z.float64()": "z.literal(0)",
	"z.number()":  "z.literal(0)",
	"z.boolean()": "z.literal(false)",
}

var zodMiniChecks = map[string]string{
	"min":        "minLength",
	"max":        "maxLength",
	"length":     "length",
	"gte":        "gte",
	"lte":        "lte",
	"gt":         "gt",
	"lt":         "lt",
	"regex":      "regex",
	"startsWith": "startsWith",
	"endsWith":   "endsWith",
	"includes":   "includes",
}

// ZodStyle controls the output format for Zod schema generation.
type ZodStyle int

const (
	ZodStandard ZodStyle = iota // z.string().min(5).max(100).optional()
	ZodMini                     // z.optional(z.string().check(z.minLength(5), z.maxLength(100)))
)

// ZodType converts a Field to its Zod 4 representation.
func ZodType(f Field, style ZodStyle) string {
	base := zodBaseType(f)
	constraints := zodConstraints(f, base)

	// validate:"omitempty" means "skip validation when zero value".
	// Zod equivalent: .or(z.literal(<zero>)) to accept the zero value
	// alongside the constrained type.
	omitemptyLit := zodOmitemptyLiteral(f, base, constraints)

	if style == ZodMini {
		return zodMini(base, constraints, f.Optional, omitemptyLit)
	}

	result := base + constraints
	if omitemptyLit != "" {
		result += ".or(" + omitemptyLit + ")"
	}
	if f.Optional {
		result += ".optional()"
	}
	return result
}

// zodOmitemptyLiteral returns the zero-value literal (e.g. z.literal("")) for
// .or() wrapping when validate:"omitempty" is set, or "" if no wrapping needed.
// Note: omitempty and optional are orthogonal — optional handles undefined (nil
// pointer), .or() handles the Go zero value ("" for strings, 0 for ints).
func zodOmitemptyLiteral(f Field, base, constraints string) string {
	if !f.ValidateOmitempty {
		return ""
	}
	// Only needed when constraints reject the zero value.
	// Plain z.string() already accepts ""; plain z.int() already accepts 0.
	// Format bases (z.email(), z.url(), etc.) inherently reject empty strings.
	if constraints == "" && !isFormatBase(base) {
		return ""
	}
	return zodZeroLiteral(base)
}

// isFormatBase returns true if the Zod base type is a format constructor
// that inherently rejects empty/zero values (e.g. z.email() rejects "").
func isFormatBase(base string) bool {
	if isStringBase(base) && base != "z.string()" {
		return true
	}
	return strings.HasPrefix(base, "z.enum(")
}

// zodZeroLiteral returns the Zod literal for the zero value of the given base type.
func zodZeroLiteral(base string) string {
	if isStringBase(base) || strings.HasPrefix(base, "z.enum(") {
		return `z.literal("")`
	}
	return zodZeroLiterals[base]
}

// ZodBaseForTSType converts a TypeScript type string to its Zod 4 base type.
// Used for fields without validate tags or when the field type is a reference.
func ZodBaseForTSType(tsType, goKind string) string {
	return zodBaseFromKindAndType(tsType, goKind, nil)
}

// zodBaseType determines the Zod base type for a field, checking validate
// format tags first (they replace z.string() entirely in Zod 4).
func zodBaseType(f Field) string {
	return zodBaseFromKindAndType(f.Type, f.GoKind, f.Validate)
}

func zodBaseFromKindAndType(tsType, goKind string, rules []ValidateRule) string {
	// Check format tags FIRST — in Zod 4 they are top-level constructors.
	for _, rule := range rules {
		if base := zodFormatBases[rule.Tag]; base != "" {
			return base
		}
	}

	// Check for oneof — becomes z.enum() for strings, z.union([z.literal()]) for numbers.
	// z.enum() is string-only in Zod 4; numeric oneofs need z.literal() inside z.union().
	for _, rule := range rules {
		if rule.Tag == "oneof" && rule.Param != "" {
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
			for i, v := range values {
				quoted[i] = fmt.Sprintf("%q", v)
			}
			return fmt.Sprintf("z.enum([%s])", strings.Join(quoted, ", "))
		}
	}

	// Fall back to Go kind / TS type based mapping.
	if base := zodGoKindBases[goKind]; base != "" {
		return base
	}

	// TS type fallback for complex types.
	if base := zodTSBases[tsType]; base != "" {
		return base
	}

	// Array types: "Foo[]" → handled by caller as z.array(FooSchema)
	// Record types: "Record<K, V>" → handled by caller
	// Named types: "Foo" → handled by caller as FooSchema reference

	return ""
}

// zodConstraints builds the chained constraint methods for a field.
// The base parameter is needed to determine if we're dealing with a string
// or number schema (min/max have different Zod methods).
func zodConstraints(f Field, base string) string {
	if len(f.Validate) == 0 {
		return ""
	}

	isStr := isStringBase(base)
	var parts []string
	if shouldRequireNonEmptyString(f, isStr) {
		parts = append(parts, `.min(1)`)
	}

	for _, rule := range f.Validate {
		if part := zodConstraint(rule, f, isStr); part != "" {
			parts = append(parts, part)
		}
	}

	return strings.Join(parts, "")
}

func zodConstraint(rule ValidateRule, f Field, isStr bool) string {
	if rule.Tag == "alphanum" {
		return `.regex(/^[a-zA-Z0-9]*$/)`
	}
	if rule.Tag == "alpha" {
		return `.regex(/^[a-zA-Z]*$/)`
	}
	if rule.Tag == "numeric" {
		return `.regex(/^[0-9]*$/)`
	}
	if rule.Param == "" {
		return ""
	}
	method := zodConstraintMethod(rule.Tag, isStr)
	if method == "" {
		return ""
	}
	if method == "startsWith" || method == "endsWith" || method == "includes" {
		return fmt.Sprintf(".%s(%q)", method, rule.Param)
	}
	// ZodString has no .gt/.gte/.lt/.lte; go-playground/validator treats these on
	// a string as bounds on its length, so translate them to .min()/.max().
	if isStr {
		switch rule.Tag {
		case "gt", "gte", "lt", "lte":
			part, ok := zodStringLengthConstraint(rule.Tag, rule.Param)
			if !ok {
				return ""
			}
			return part
		}
	}
	if f.GoKind != "" && !isStr && !isLengthKind(f.GoKind) && !isNumericField(f) {
		return ""
	}
	param, ok := zodConstraintNumberLiteral(rule.Param, f.GoKind, isStr)
	if !ok {
		return ""
	}
	if rule.Tag == "len" && !isStr {
		if !isNumericField(f) {
			return ""
		}
		return fmt.Sprintf(".gte(%s).lte(%s)", param, param)
	}
	return fmt.Sprintf(".%s(%s)", method, param)
}

// zodStringLengthConstraint translates a numeric-comparison validator tag on a
// string field into the equivalent Zod string-length method. ZodString only
// exposes inclusive .min()/.max(), so the strict forms gain a ±1 offset:
// gte=n→.min(n), gt=n→.min(n+1), lte=n→.max(n), lt=n→.max(n-1). Returns false
// when the parameter is not a safe length literal so the caller drops it.
func zodStringLengthConstraint(tag, param string) (string, bool) {
	n, ok := zodLengthValue(param)
	if !ok {
		return "", false
	}
	switch tag {
	case "gte":
		return fmt.Sprintf(".min(%d)", n), true
	case "gt":
		if n == math.MaxInt64 {
			return "", false
		}
		return fmt.Sprintf(".min(%d)", n+1), true
	case "lte":
		return fmt.Sprintf(".max(%d)", n), true
	case "lt":
		if n == math.MinInt64 {
			return "", false
		}
		return fmt.Sprintf(".max(%d)", n-1), true
	}
	return "", false
}

func shouldRequireNonEmptyString(f Field, isStr bool) bool {
	if !isStr || f.IsPointer || f.ValidateOmitempty {
		return false
	}
	required := false
	for _, rule := range f.Validate {
		switch rule.Tag {
		case "required":
			required = true
		case "min", "len":
			if zodLengthLiteralAtLeast(rule.Param, 1) {
				return false
			}
		}
	}
	return required
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
	return strconv.FormatFloat(n, 'g', -1, bitSize), true
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

func parseOneofValues(param string) []string {
	var values []string
	for i := 0; i < len(param); {
		for i < len(param) && isASCIISpace(param[i]) {
			i++
		}
		if i >= len(param) {
			break
		}
		start := i
		if param[i] == '\'' {
			end := i + 1
			for end < len(param) && param[end] != '\'' {
				end++
			}
			if end < len(param) {
				values = append(values, strings.ReplaceAll(param[start:end+1], "'", ""))
				i = end + 1
				continue
			}
		}
		for i < len(param) && !isASCIISpace(param[i]) {
			i++
		}
		values = append(values, strings.ReplaceAll(param[start:i], "'", ""))
	}
	return values
}

func isASCIISpace(ch byte) bool {
	switch ch {
	case ' ', '\t', '\n', '\r', '\f', '\v':
		return true
	default:
		return false
	}
}

func zodConstraintMethod(tag string, isStr bool) string {
	if tag == "min" && !isStr {
		return "gte"
	}
	if tag == "max" && !isStr {
		return "lte"
	}
	methods := map[string]string{
		"min":        "min",
		"max":        "max",
		"len":        "length",
		"gt":         "gt",
		"gte":        "gte",
		"lt":         "lt",
		"lte":        "lte",
		"startswith": "startsWith",
		"endswith":   "endsWith",
		"contains":   "includes",
	}
	return methods[tag]
}

// zodMini generates Zod Mini functional syntax.
// omitemptyLit is the zero-value literal for .or() wrapping (empty string if not needed).
func zodMini(base string, constraints string, optional bool, omitemptyLit string) string {
	if constraints == "" && !optional && omitemptyLit == "" {
		return base
	}

	// Convert method chain constraints to z.check() style.
	// ".min(3).max(50)" → "z.minLength(3), z.maxLength(50)" for strings
	// ".gte(1).lte(20)" → "z.gte(1), z.lte(20)" for numbers
	var checks []string
	if constraints != "" {
		// Parse the chained methods.
		remaining := constraints
		for remaining != "" {
			if !strings.HasPrefix(remaining, ".") {
				break
			}
			remaining = remaining[1:]
			// Find end of method call.
			parenIdx := strings.IndexByte(remaining, '(')
			if parenIdx < 0 {
				break
			}
			method := remaining[:parenIdx]
			// Find matching close paren, respecting quoted string args.
			closeIdx := zodCallCloseIndex(remaining[parenIdx:])
			if closeIdx < 0 {
				break
			}
			args := remaining[parenIdx+1 : parenIdx+closeIdx]
			remaining = remaining[parenIdx+closeIdx+1:]

			if fn := zodMiniChecks[method]; fn != "" {
				checks = append(checks, fmt.Sprintf("z.%s(%s)", fn, args))
			}
		}
	}

	inner := base
	if len(checks) > 0 {
		inner = fmt.Sprintf("%s.check(%s)", base, strings.Join(checks, ", "))
	}

	if omitemptyLit != "" {
		inner += ".or(" + omitemptyLit + ")"
	}

	if optional {
		return fmt.Sprintf("z.optional(%s)", inner)
	}
	return inner
}

func zodCallCloseIndex(s string) int {
	depth := 0
	var quote byte
	escaped := false
	for i := range len(s) {
		ch := s[i]
		if quote != 0 {
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == quote {
				quote = 0
			}
			continue
		}

		switch ch {
		case '\'', '"', '`':
			quote = ch
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// supportedZodTags is the complete set of validate tags that produce Zod output.
// Tags not in this set are flagged as unsupported in generated schemas.
var supportedZodTags = map[string]bool{
	// Structural (consumed before Zod generation).
	"required":  true,
	"omitempty": true,
	"dive":      true,
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
		if invalidZodRule(r, goKind) {
			invalid = append(invalid, r)
		}
	}
	return invalid
}

func invalidZodRule(rule ValidateRule, goKind string) bool {
	if !supportedZodTags[rule.Tag] {
		return false
	}
	switch rule.Tag {
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
			return false
		}
		_, ok := zodNumericOneofLiterals(parseOneofValues(rule.Param), goKind)
		return !ok
	default:
		return false
	}
}

// isStringBase returns true if the Zod base type is string-like
// (determines whether min/max mean length vs numeric bound).
func isStringBase(base string) bool {
	return strings.HasPrefix(base, "z.string()") || zodStringBases[base]
}

func isLengthKind(goKind string) bool {
	switch goKind {
	case "string", "slice", "array", "map":
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
