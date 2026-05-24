package typemap

import (
	"fmt"
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
			values := strings.Fields(rule.Param)
			if isNumericKind(goKind) {
				lits := make([]string, len(values))
				for i, v := range values {
					lits[i] = "z.literal(" + v + ")"
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

	for _, rule := range f.Validate {
		if part := zodConstraint(rule, isStr); part != "" {
			parts = append(parts, part)
		}
	}

	return strings.Join(parts, "")
}

func zodConstraint(rule ValidateRule, isStr bool) string {
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
	return fmt.Sprintf(".%s(%s)", method, rule.Param)
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
			// Find matching close paren.
			closeIdx := strings.IndexByte(remaining[parenIdx:], ')')
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

// isStringBase returns true if the Zod base type is string-like
// (determines whether min/max mean length vs numeric bound).
func isStringBase(base string) bool {
	return strings.HasPrefix(base, "z.string()") || zodStringBases[base]
}

// isNumericKind reports whether a Go kind string represents a numeric type.
func isNumericKind(goKind string) bool {
	switch goKind {
	case "int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64",
		"float32", "float64":
		return true
	}
	return false
}
