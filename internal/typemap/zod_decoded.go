package typemap

import "strings"

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

// ZodQuotedScalarValue evaluates a validated json:",string" payload for a
// comparison. encoding/json treats the quoted null spelling as a no-op on a
// scalar, whose initial value is zero. Pointer presence is checked separately.
func ZodQuotedScalarValue(value, goKind string) string {
	if goKind == "float32" || goKind == "float64" {
		return ZodQuotedFloatValue(value, goKind)
	}
	text := "(" + value + " === \"null\" ? " + ZodStringLiteral(zodQuotedZero(goKind)) + " : " + value + ")"
	if isSignedIntegerKind(goKind) || isUnsignedIntegerKind(goKind) {
		return "BigInt(" + text + ")"
	}
	return "JSON.parse(" + text + ")"
}

func zodQuotedZero(goKind string) string {
	switch goKind {
	case "string":
		return `""`
	case "bool":
		return "false"
	default:
		return "0"
	}
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
