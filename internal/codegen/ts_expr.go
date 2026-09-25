package codegen

import (
	"strconv"
	"strings"

	"github.com/befabri/trpcgo/internal/typemap"
)

// splitTopLevel splits a string by sep, respecting angle brackets.
func splitTopLevel(s string, sep byte) []string {
	var parts []string
	depth, start := 0, 0
	var quote byte
	escaped := false
	for i := range len(s) {
		c := s[i]
		if quote != 0 {
			if escaped {
				escaped = false
			} else if c == '\\' {
				escaped = true
			} else if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '"', '\'', '`':
			quote = c
		case '<', '(', '{', '[':
			depth++
		case '>', ')', '}', ']':
			depth--
		default:
			if c == sep && depth == 0 {
				parts = append(parts, s[start:i])
				start = i + 1
			}
		}
	}
	return append(parts, s[start:])
}

// stripGenericArgs removes generic type arguments: "Foo<Bar>" → "Foo"
func stripGenericArgs(s string) string {
	if before, _, ok := strings.Cut(s, "<"); ok {
		return before
	}
	return s
}

func unwrapZodParentheses(ts string) string {
	if strings.HasPrefix(ts, "(") && strings.HasSuffix(ts, ")") {
		return ts[1 : len(ts)-1]
	}
	return ts
}

func zodRecordTypes(ts string) (key, value string, ok bool) {
	if strings.HasPrefix(ts, "Partial<") && strings.HasSuffix(ts, ">") {
		ts = ts[len("Partial<") : len(ts)-1]
	}
	if !strings.HasPrefix(ts, "Record<") || !strings.HasSuffix(ts, ">") {
		return "", "", false
	}
	parts := splitTopLevel(ts[len("Record<"):len(ts)-1], ',')
	if len(parts) != 2 {
		return "", "", false
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), true
}

// extractTypeRefs pulls named type references from a TS type string.
// "Foo[]" → ["Foo"], "Record<string, Bar>" → ["Bar"], "Foo" → ["Foo"]
func extractTypeRefs(tsType string) []string {
	var refs []string
	mapTypeNames(tsType, func(name string) string { refs = append(refs, name); return name })
	return refs
}

// TypeScript permits recursive object literals but rejects a type alias that
// immediately instantiates Record with itself. Emit the equivalent direct
// object type at declaration boundaries, preserving named references in the IR.
func directRecordType(ts string) string {
	partial := strings.HasPrefix(ts, "Partial<Record<")
	original := ts
	if partial {
		ts = ts[len("Partial<") : len(ts)-1]
	}
	if !strings.HasPrefix(ts, "Record<") || !strings.HasSuffix(ts, ">") {
		return original
	}
	parts := splitTopLevel(ts[len("Record<"):len(ts)-1], ',')
	if len(parts) != 2 {
		return original
	}
	key, value := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	if !partial && (key == "string" || key == "number") {
		return "{ [key: " + key + "]: " + value + " }"
	}
	optional := ""
	if partial {
		optional = "?"
	}
	return "{ [key in " + key + "]" + optional + ": " + value + " }"
}

func inlineTypeFields(ts string) ([]typemap.Field, bool) {
	if !strings.HasPrefix(ts, "{") || !strings.HasSuffix(ts, "}") {
		return nil, false
	}
	var fields []typemap.Field
	for _, prop := range splitTopLevel(ts[1:len(ts)-1], ';') {
		prop = strings.TrimSpace(prop)
		if prop == "" {
			continue
		}
		pair := splitTopLevel(prop, ':')
		if len(pair) != 2 {
			return nil, false
		}
		name, readonly := strings.CutPrefix(strings.TrimSpace(pair[0]), "readonly ")
		name, optional := strings.CutSuffix(name, "?")
		if decoded, err := strconv.Unquote(name); err == nil {
			name = decoded
		}
		fields = append(fields, typemap.Field{Name: name, Type: strings.TrimSpace(pair[1]), Readonly: readonly, Optional: optional})
	}
	return fields, true
}

// mapTypeNames visits type references without treating object property names
// or string literal contents as references.
func mapTypeNames(ts string, visit func(string) string) string {
	ts = strings.TrimSpace(ts)
	if parts := splitTopLevel(ts, '|'); len(parts) > 1 {
		for i := range parts {
			parts[i] = mapTypeNames(parts[i], visit)
		}
		return strings.Join(parts, " | ")
	}
	if strings.HasSuffix(ts, "[]") {
		return mapTypeNames(ts[:len(ts)-2], visit) + "[]"
	}
	if strings.HasPrefix(ts, "(") && strings.HasSuffix(ts, ")") {
		return "(" + mapTypeNames(ts[1:len(ts)-1], visit) + ")"
	}
	if fields, ok := inlineTypeFields(ts); ok {
		for i := range fields {
			fields[i].Type = mapTypeNames(fields[i].Type, visit)
		}
		return typemap.InlineObjectType(fields)
	}
	if i := strings.IndexByte(ts, '<'); i >= 0 && strings.HasSuffix(ts, ">") {
		base := ts[:i]
		if base != "Record" && base != "Partial" {
			base = visit(base)
		}
		args := splitTopLevel(ts[i+1:len(ts)-1], ',')
		for i := range args {
			args[i] = mapTypeNames(args[i], visit)
		}
		return base + "<" + strings.Join(args, ", ") + ">"
	}
	switch ts {
	case "string", "number", "boolean", "unknown", "void", "null", "undefined", "never", "true", "false":
		return ts
	}
	if strings.HasPrefix(ts, `"`) || strings.HasPrefix(ts, "'") {
		return ts
	}
	if _, err := strconv.ParseFloat(ts, 64); err == nil {
		return ts
	}
	return visit(ts)
}

func rewriteTypeExpressions(ts string, rewrite func(string) (string, bool)) string {
	ts = strings.TrimSpace(ts)
	if replacement, ok := rewrite(ts); ok {
		return replacement
	}
	if parts := splitTopLevel(ts, '|'); len(parts) > 1 {
		for i := range parts {
			parts[i] = rewriteTypeExpressions(parts[i], rewrite)
		}
		return strings.Join(parts, " | ")
	}
	if before, ok := strings.CutSuffix(ts, "[]"); ok {
		return rewriteTypeExpressions(before, rewrite) + "[]"
	}
	if strings.HasPrefix(ts, "(") && strings.HasSuffix(ts, ")") {
		return "(" + rewriteTypeExpressions(ts[1:len(ts)-1], rewrite) + ")"
	}
	if fields, ok := inlineTypeFields(ts); ok {
		for i := range fields {
			fields[i].Type = rewriteTypeExpressions(fields[i].Type, rewrite)
		}
		return typemap.InlineObjectType(fields)
	}
	if i := strings.IndexByte(ts, '<'); i >= 0 && strings.HasSuffix(ts, ">") {
		args := splitTopLevel(ts[i+1:len(ts)-1], ',')
		for i := range args {
			args[i] = rewriteTypeExpressions(args[i], rewrite)
		}
		return ts[:i] + "<" + strings.Join(args, ", ") + ">"
	}
	return ts
}

// mapTypeExpressions replaces whole concrete generic expressions while
// preserving properties and string literal contents in inline TypeScript types.
func mapTypeExpressions(ts string, replacements map[string]string) string {
	return rewriteTypeExpressions(ts, func(expression string) (string, bool) {
		replacement, ok := replacements[expression]
		return replacement, ok
	})
}
