package typemap

import (
	"strconv"
	"strings"
)

func zodUniquePredicate(f Field, param, value string) (string, bool) {
	d, selected, err := zodUniqueType(f, param)
	if err != nil {
		return "", false
	}
	values := value
	if f.GoKind == "map" {
		values = "Object.values(" + value + ")"
	}
	var key string
	if selected != nil {
		key = zodEqualityKey(selected.Type, "(element as { [key: string]: unknown } | null | undefined)?.["+ZodStringLiteral(selected.JSONName)+"]", "index", selected.JSONString)
		if d.Pointer {
			key = `(element == null ? ["nil"] : ` + key + ")"
		}
	} else {
		key = zodEqualityKey(d, "element", "index", false)
	}
	return "new Set(" + values + ".map((element: unknown, index: number) => JSON.stringify(" + key + "))).size === " + values + ".length", true
}

// Structural keys contain only Go comparable fields. Normalization happens in
// the comparison, preserving the wire values returned by the schema. A NaN in
// any position makes a Go value unequal to every other value, including itself.
func zodEqualityKey(d *GoEqualityType, value, index string, quoted bool) string {
	input := "value"
	if quoted {
		input = ZodQuotedScalarValue("String(value ?? "+ZodStringLiteral(zodQuotedZero(d.Kind))+")", d.Kind)
	}
	key := ""
	switch d.Kind {
	case "string":
		key = `["string", ` + ZodGoStringValue("("+input+` ?? "")`) + "]"
	case "bool":
		key = `["bool", Boolean(` + input + ")]"
	case "struct":
		if d.EmptySentinel {
			key = `["nil"]`
			break
		}
		parts := []string{`"struct"`}
		for _, field := range d.Fields {
			parts = append(parts, zodEqualityKey(field.Type, `(value as { [key: string]: unknown } | null | undefined)?.[`+ZodStringLiteral(field.JSONName)+"]", index, field.JSONString))
		}
		key = "[" + strings.Join(parts, ", ") + "]"
	case "array":
		key = `["array", ...Array.from({ length: ` + strconv.FormatInt(d.Length, 10) + ` }, (_, position) => ` + zodEqualityKey(d.Element, `(value as readonly unknown[] | null | undefined)?.[position]`, index, false) + ")]"
	default:
		if quoted && (isSignedIntegerKind(d.Kind) || isUnsignedIntegerKind(d.Kind)) {
			key = `["number", String(` + input + ")]"
		} else {
			number := "Number(" + input + " ?? 0)"
			if d.Kind == "float32" {
				number = "Math.fround(" + number + ")"
			}
			key = `((number: number) => Number.isNaN(number) ? ["nan", ` + index + `] : ["number", String(number)])(` + number + ")"
		}
	}
	if d.Pointer {
		nilCheck := "value == null"
		if quoted {
			nilCheck += ` || value === "null"`
		}
		key = "(" + nilCheck + ` ? ["nil"] : ` + key + ")"
	}
	return "((value: unknown) => " + key + ")(" + value + ")"
}
