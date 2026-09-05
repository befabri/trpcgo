package codegen

import (
	"strings"

	"github.com/befabri/trpcgo/internal/typemap"
)

func zodRefinementPredicate(ref typemap.Refinement, fields map[string]typemap.Field) string {
	left, leftGuard := zodRefinementOperand(ref.Field, fields[ref.Field])
	right, rightGuard := zodRefinementOperand(ref.OtherField, fields[ref.OtherField])
	var conditions []string
	if guard := zodPresence(ref.OtherWhenAnyPresent, fields); guard != "" {
		conditions = append(conditions, guard)
	}
	if leftGuard != "" {
		conditions = append(conditions, leftGuard)
	}
	if rightGuard != "" {
		conditions = append(conditions, rightGuard)
	}
	conditions = append(conditions, left+" "+ref.Op+" "+right)
	result := strings.Join(conditions, " && ")
	if field := fields[ref.Field]; field.ValidateOmitempty {
		zero := zodGoZero(field)
		if zero == "" {
			zero = "undefined"
		}
		result = "(" + left + " === " + zero + " || (" + result + "))"
	}
	if guard := zodPresence(ref.WhenAnyPresent, fields); guard != "" {
		result = "!(" + guard + ") || (" + result + ")"
	}
	return result
}

func zodRefinementOperand(name string, field typemap.Field) (value, guard string) {
	value = zodDataAccess(name)
	if field.Optional {
		if zero := zodGoZero(field); zero != "" {
			// Go decodes a missing property to its zero value and validates
			// that, so compare against it without altering the parsed output.
			value = "(" + value + " ?? " + zero + ")"
		} else {
			guard = value + " !== undefined"
		}
	}
	return value, guard
}

func zodPresence(names []string, fields map[string]typemap.Field) string {
	var checks []string
	for _, name := range names {
		if field, ok := fields[name]; ok && !field.ZodOmit {
			checks = append(checks, zodDataAccess(name)+" !== undefined")
		}
	}
	if len(checks) == 0 {
		return ""
	}
	return "(" + strings.Join(checks, " || ") + ")"
}

func zodGoZero(field typemap.Field) string {
	if field.IsPointer {
		return ""
	}
	kind := field.GoKind
	if kind == "" {
		kind = field.Type
	}
	switch kind {
	case "number", "int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64", "uintptr", "float32", "float64":
		return "0"
	case "string":
		return `""`
	case "bool", "boolean":
		return "false"
	default:
		return ""
	}
}
