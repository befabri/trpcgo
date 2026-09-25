package typemap

import (
	"fmt"
	"strconv"
	"strings"
)

// HoistZodRuntimeHelpers extracts generated runtime helpers from a complete
// schema body. Scalar expressions stay self-contained for standalone callers;
// the file emitter can share their implementations without parsing JavaScript.
// Only the exact generated function expression is replaced, and the reserved
// '$' prefix cannot collide with names originating from Go identifiers.
func HoistZodRuntimeHelpers(source string) (declarations, body string) {
	body = source
	// Outer helpers precede helpers embedded inside them. Both accumulated
	// declarations and the body are rewritten so dependencies are shared too.
	for _, helper := range []struct{ name, expression string }{
		{"$trpcgoIssue", goIssueCheck},
		{"$trpcgoValid", goSchemaCheck},
		{"$trpcgoURL", goURLValidator},
		{"$trpcgoEmail", goEmailValidator},
		{"$trpcgoTimeParts", goTimeParts},
		{"$trpcgoIPBytes", goIPBytes},
		{"$trpcgoQuotedFloat", goQuotedFloatDecoder},
		{"$trpcgoUnicodeLetter", goUnicodeLetter},
		{"$trpcgoUnicodeNumber", goUnicodeNumber},
		{"$trpcgoUnicodeLowerChanges", goUnicodeLowerChanges},
		{"$trpcgoUnicodeUpperChanges", goUnicodeUpperChanges},
	} {
		expression := "(" + helper.expression + ")"
		if !strings.Contains(body, expression) && !strings.Contains(declarations, expression) {
			continue
		}
		body = strings.ReplaceAll(body, expression, helper.name)
		declarations = strings.ReplaceAll(declarations, expression, helper.name)
		declarations += "const " + helper.name + " = " + helper.expression + ";\n"
	}
	return declarations, body
}

// ZodGoStringValue evaluates a JavaScript string as encoding/json decodes it in
// Go. The Unicode flag keeps valid surrogate pairs intact and replaces only
// unpaired surrogates. This expression never transforms the schema's output.
func ZodGoStringValue(value string) string {
	return "String(" + value + `).replace(/\p{Surrogate}/gu, "\uFFFD")`
}

// ZodQuotedScalar evaluates a validated json:",string" payload for a
// comparison. encoding/json treats the quoted null spelling as a no-op on a
// scalar, whose initial value is zero. Pointer presence is checked separately.
//
// throws reports that evaluating the expression can raise. Zod may run an
// object refinement after a field check has failed, so the payload may still
// be malformed: BigInt and JSON.parse throw on it, while the float decoder
// checks the syntax itself and returns NaN. A caller that embeds a throwing
// expression in a predicate must catch, so safeParse reports an issue.
func ZodQuotedScalar(value, goKind string) (expr string, throws bool) {
	if goKind == "float32" || goKind == "float64" {
		return ZodQuotedFloatValue(value, goKind), false
	}
	if goKind == "json.Number" {
		return ZodQuotedNumberValue(value), true
	}
	text := "(" + value + " === \"null\" ? " + ZodStringLiteral(zodQuotedZero(goKind)) + " : " + value + ")"
	if isSignedIntegerKind(goKind) || isUnsignedIntegerKind(goKind) {
		return "BigInt(" + text + ")", true
	}
	return "JSON.parse(" + text + ")", true
}

// ZodQuotedScalarValue is ZodQuotedScalar for callers that catch exceptions
// around the whole expression themselves.
func ZodQuotedScalarValue(value, goKind string) string {
	expr, _ := ZodQuotedScalar(value, goKind)
	return expr
}

func zodQuotedZero(goKind string) string {
	switch goKind {
	case "string", "json.Number":
		return `""`
	case "bool":
		return "false"
	default:
		return "0"
	}
}

// ZodQuotedNumberValue evaluates the payload of a json:",string" json.Number
// as encoding/json stores it: the quoted null spelling leaves the zero Number,
// a nested string literal is unquoted, and any other payload is the Number's
// text. The schema has already rejected the payloads Go's decoder rejects.
func ZodQuotedNumberValue(value string) string {
	return `((text: string) => text === "null" ? "" : text.startsWith('"') ? JSON.parse(text) as string : text)(` + value + ")"
}

// ZodQuotedFloatValue evaluates the payload of a json:",string" floating-point
// field. Invalid syntax and overflow produce NaN; explicit negative infinity
// and the scalar null spelling follow encoding/json. Callers handle pointer
// null presence separately. The same decoder serves scalar and field rules.
func ZodQuotedFloatValue(value, goKind string) string {
	bits := "64"
	if goKind == "float32" {
		bits = "32"
	}
	return "(" + goQuotedFloatDecoder + ")(" + value + ", " + bits + ")"
}

// Decimal and hexadecimal significands are parsed exactly. Rounding the exact
// rational once to the destination format avoids float32 double rounding and
// hexadecimal overflow/underflow introduced by intermediate JS numbers.
const goQuotedFloatDecoder = `(input: string, bits: 32 | 64): number => {
  if (input === "null") return 0;
  if (!/^[-0-9]/.test(input)) return NaN;
  if (/^-inf(?:inity)?$(?![\s\S])/i.test(input)) return -Infinity;
  const decimal = /^-?(?:[0-9](?:_?[0-9])*(?:\.(?:[0-9](?:_?[0-9])*)?)?|\.[0-9](?:_?[0-9])*)(?:[eE][+-]?[0-9](?:_?[0-9])*)?$(?![\s\S])/;
  const hexadecimal = /^-?0[xX](?:_?[0-9a-fA-F](?:_?[0-9a-fA-F])*(?:\.(?:[0-9a-fA-F](?:_?[0-9a-fA-F])*)?)?|\.[0-9a-fA-F](?:_?[0-9a-fA-F])*)[pP][+-]?[0-9](?:_?[0-9])*$(?![\s\S])/;
  const hex = hexadecimal.test(input);
  if (!hex && !decimal.test(input)) return NaN;
  const negative = input.startsWith("-");
  const text = input.replace(/_/g, "").replace(/^-/, "");
  const parts = text.split(hex ? /[pP]/ : /[eE]/);
  const mantissa = parts[0]!.replace(/^0[xX]/, "");
  const point = mantissa.indexOf(".");
  const fraction = point < 0 ? 0 : mantissa.length - point - 1;
  const digits = mantissa.replace(".", "").replace(/^0+/, "");
  if (digits === "") return negative ? -0 : 0;
  const exponent = parts[1] === undefined ? 0 : Number(parts[1]);
  const scale = exponent - fraction * (hex ? 4 : 1);
  const order = (digits.length - 1) * (hex ? 4 : 1) + scale;
  // Bounds are intentionally outside both IEEE formats; they also prevent
  // huge exponents from allocating enormous powers of ten or bit shifts.
  if (order > (hex ? 1030 : 310)) return NaN;
  if (order < (hex ? -1080 : -330)) return negative ? -0 : 0;
  let numerator = BigInt((hex ? "0x" : "") + digits);
  let denominator = 1n;
  const binaryScale = hex ? scale : 0;
  if (!hex) {
    if (scale >= 0) numerator *= 10n ** BigInt(scale);
    else denominator = 10n ** BigInt(-scale);
  }
  let exponent2 = numerator.toString(2).length - denominator.toString(2).length;
  if (exponent2 >= 0 ? numerator < (denominator << BigInt(exponent2)) : (numerator << BigInt(-exponent2)) < denominator) exponent2--;
  exponent2 += binaryScale;
  const precision = bits === 32 ? 24 : 53;
  const minimum = bits === 32 ? -126 : -1022;
  const maximum = bits === 32 ? 127 : 1023;
  if (exponent2 > maximum) return NaN;
  if (exponent2 < minimum - precision) return negative ? -0 : 0;
  const quantum = Math.max(exponent2, minimum) - precision + 1;
  const shift = binaryScale - quantum;
  if (shift >= 0) numerator <<= BigInt(shift);
  else denominator <<= BigInt(-shift);
  let rounded = numerator / denominator;
  const remainder = numerator % denominator;
  if (remainder * 2n > denominator || (remainder * 2n === denominator && (rounded & 1n) !== 0n)) rounded++;
  const result = Number(rounded) * 2 ** quantum;
  if (!Number.isFinite(result) || (bits === 32 && !Number.isFinite(Math.fround(result)))) return NaN;
  return negative ? -result : result;
}`

// ValidateZodFieldRules checks restrictions that require preserved field
// metadata rather than just its top-level Go kind. Callers add their complete
// field/key/element path when reporting the error.
func ValidateZodFieldRules(f Field) error { return validateZodTypedRules(f, f.Validate) }

func validateZodTypedRules(f Field, rules []ValidateRule) error {
	for _, rule := range rules {
		if len(rule.Alternatives) > 0 {
			if err := validateZodTypedRules(f, rule.Alternatives); err != nil {
				return err
			}
			continue
		}
		if rule.Tag == "unique" {
			if _, _, err := zodUniqueType(f, rule.Param); err != nil {
				return fmt.Errorf("unique%s: %w", uniqueParam(rule.Param), err)
			}
		}
	}
	return nil
}

func uniqueParam(param string) string {
	if param == "" {
		return ""
	}
	return "=" + param
}

func zodUniqueType(f Field, param string) (*GoEqualityType, *GoEqualityField, error) {
	if f.GoKind != "slice" && f.GoKind != "array" && f.GoKind != "map" {
		return nil, nil, fmt.Errorf("requires an array, slice or map")
	}
	if f.Element == nil {
		return nil, nil, fmt.Errorf("requires concrete Go comparable-value metadata")
	}
	d := f.Element.Equality
	if d == nil {
		// Compatibility for manually supplied scalar metadata.
		if f.Element.GoKind == "string" || f.Element.GoKind == "bool" || isNumericKind(f.Element.GoKind) {
			d = &GoEqualityType{Kind: f.Element.GoKind, Pointer: f.Element.IsPointer}
		} else {
			return nil, nil, fmt.Errorf("requires concrete Go comparable-value metadata for %s", f.Element.GoKind)
		}
	}
	// The backend ignores the parameter on map values.
	if param != "" && f.GoKind != "map" {
		if d.Kind != "struct" {
			return nil, nil, fmt.Errorf("field selection requires Go struct elements")
		}
		var selected *GoEqualityField
		for index := range d.Fields {
			if d.Fields[index].GoName == param && d.Fields[index].Selectable {
				if selected != nil {
					return nil, nil, fmt.Errorf("field %s is ambiguous in the JSON schema", param)
				}
				selected = &d.Fields[index]
			}
		}
		if selected == nil {
			return nil, nil, fmt.Errorf("field %s is missing, unexported, or unavailable in the JSON schema", param)
		}
		if selected.Type.Error != "" {
			return nil, nil, fmt.Errorf("%s", selected.Type.Error)
		}
		return d, selected, nil
	}
	if d.Error != "" {
		return nil, nil, fmt.Errorf("%s", d.Error)
	}
	return d, nil, nil
}

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
	case "json.Number":
		// validator keys its map by the Number's text, so a quoted Number's
		// stored text decides; a wire number prints one text per value.
		key = `["string", String(` + input + ` ?? "")]`
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
