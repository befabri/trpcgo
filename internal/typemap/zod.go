package typemap

import (
	"fmt"
	"strings"
)

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

	if style == ZodMini {
		return zodMini(base, constraints, f.Optional)
	}

	result := base + constraints
	if f.Optional {
		result += ".optional()"
	}
	return result
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
		switch rule.Tag {
		case "email":
			return "z.email()"
		case "url":
			return "z.url()"
		case "uuid":
			return "z.uuidv4()"
		case "e164":
			return "z.e164()"
		case "jwt":
			return "z.jwt()"
		case "base64":
			return "z.base64()"
		case "lowercase":
			return "z.lowercase()"
		case "ip", "ipv4":
			return "z.ipv4()"
		case "ipv6":
			return "z.ipv6()"
		}
	}

	// Check for oneof — becomes z.enum() replacing the base entirely.
	for _, rule := range rules {
		if rule.Tag == "oneof" && rule.Param != "" {
			values := strings.Fields(rule.Param)
			quoted := make([]string, len(values))
			for i, v := range values {
				quoted[i] = fmt.Sprintf("%q", v)
			}
			return fmt.Sprintf("z.enum([%s])", strings.Join(quoted, ", "))
		}
	}

	// Fall back to Go kind / TS type based mapping.
	switch goKind {
	case "time.Time":
		return "z.iso.datetime()"
	case "[]byte":
		return "z.base64()"
	case "int":
		return "z.int()"
	case "int32":
		return "z.int32()"
	case "int64":
		return "z.int64()"
	case "uint32":
		return "z.uint32()"
	case "uint64":
		return "z.uint64()"
	case "float32":
		return "z.float32()"
	case "float64":
		return "z.float64()"
	case "int8", "int16", "uint", "uint8", "uint16":
		return "z.number()"
	case "string":
		return "z.string()"
	case "bool":
		return "z.boolean()"
	}

	// TS type fallback for complex types.
	switch tsType {
	case "string":
		return "z.string()"
	case "number":
		return "z.number()"
	case "boolean":
		return "z.boolean()"
	case "unknown":
		return "z.unknown()"
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
		switch rule.Tag {
		case "min":
			if rule.Param != "" {
				if isStr {
					parts = append(parts, fmt.Sprintf(".min(%s)", rule.Param))
				} else {
					parts = append(parts, fmt.Sprintf(".gte(%s)", rule.Param))
				}
			}
		case "max":
			if rule.Param != "" {
				if isStr {
					parts = append(parts, fmt.Sprintf(".max(%s)", rule.Param))
				} else {
					parts = append(parts, fmt.Sprintf(".lte(%s)", rule.Param))
				}
			}
		case "len":
			if rule.Param != "" {
				parts = append(parts, fmt.Sprintf(".length(%s)", rule.Param))
			}
		case "gt":
			if rule.Param != "" {
				parts = append(parts, fmt.Sprintf(".gt(%s)", rule.Param))
			}
		case "gte":
			if rule.Param != "" {
				parts = append(parts, fmt.Sprintf(".gte(%s)", rule.Param))
			}
		case "lt":
			if rule.Param != "" {
				parts = append(parts, fmt.Sprintf(".lt(%s)", rule.Param))
			}
		case "lte":
			if rule.Param != "" {
				parts = append(parts, fmt.Sprintf(".lte(%s)", rule.Param))
			}
		case "alphanum":
			parts = append(parts, `.regex(/^[a-zA-Z0-9]*$/)`)
		case "alpha":
			parts = append(parts, `.regex(/^[a-zA-Z]*$/)`)
		case "numeric":
			parts = append(parts, `.regex(/^[0-9]*$/)`)
		// Format tags (email, url, uuid, etc.) are handled in zodBaseType.
		// required, omitempty are handled at the field level.
		}
	}

	return strings.Join(parts, "")
}

// zodMini generates Zod Mini functional syntax.
func zodMini(base string, constraints string, optional bool) string {
	if constraints == "" && !optional {
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

			// Map method to Zod Mini check function.
			switch method {
			case "min":
				checks = append(checks, fmt.Sprintf("z.minLength(%s)", args))
			case "max":
				checks = append(checks, fmt.Sprintf("z.maxLength(%s)", args))
			case "length":
				checks = append(checks, fmt.Sprintf("z.length(%s)", args))
			case "gte":
				checks = append(checks, fmt.Sprintf("z.gte(%s)", args))
			case "lte":
				checks = append(checks, fmt.Sprintf("z.lte(%s)", args))
			case "gt":
				checks = append(checks, fmt.Sprintf("z.gt(%s)", args))
			case "lt":
				checks = append(checks, fmt.Sprintf("z.lt(%s)", args))
			case "regex":
				checks = append(checks, fmt.Sprintf("z.regex(%s)", args))
			}
		}
	}

	inner := base
	if len(checks) > 0 {
		inner = fmt.Sprintf("%s.check(%s)", base, strings.Join(checks, ", "))
	}

	if optional {
		return fmt.Sprintf("z.optional(%s)", inner)
	}
	return inner
}

// isStringBase returns true if the Zod base type is string-like
// (determines whether min/max mean length vs numeric bound).
func isStringBase(base string) bool {
	return strings.HasPrefix(base, "z.string()") ||
		base == "z.email()" ||
		base == "z.url()" ||
		base == "z.uuidv4()" ||
		base == "z.e164()" ||
		base == "z.jwt()" ||
		base == "z.base64()" ||
		base == "z.lowercase()" ||
		base == "z.ipv4()" ||
		base == "z.ipv6()"
}
