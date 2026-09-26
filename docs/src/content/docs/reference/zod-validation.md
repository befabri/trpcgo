---
title: Zod Validation
description: How generated Zod schemas match go-playground/validator and encoding/json in edge cases.
---

Generated schemas accept and reject what the Go server does: validator's rules applied to the value `encoding/json` decodes. This page lists the cases where that behavior is not obvious. For everyday use, see [Zod Schemas](/zod-schemas/).

Format checks are tested against Go 1.26, Go 1.27, and validator v10.30.4. Email, URL, IP, and timestamp checks use parsers derived from Go's, and Unicode character classes use the generator's Go Unicode tables.

## Tags

- String `min`, `max`, `len`, `gt`, `gte`, `lt`, and `lte` count Unicode code points, as validator counts runes.
- `0x2C` and `0x7C` in a parameter stand for a literal comma and pipe.
- A format combined with another rule must satisfy both: `email,oneof=admin@example.com` accepts only that address.
- `numeric` on a numeric Go field keeps the numeric schema instead of applying a string pattern.
- Unsupported tags and invalid parameters become comments in the generated schema. Set `Strict` in [custom validation rules](/zod-schemas/#custom-validation-rules) to fail generation instead.
- Malformed `dive` or `keys` scopes fail generation.

## Required And Omission

- `validate:"omitempty"` does not make a key optional; only an optional TypeScript property does. See [Struct Tags](/struct-tags/#json-tags).
- Rules before an omission tag always run: `required,omitempty,min=3` rejects `""` through `required` and skips only `min`.
- `omitnil` skips nil pointers, maps, and slices, and still validates scalar zero values.
- `omitempty` on a map or slice skips only nil. A present empty collection still runs the later rules.
- `omitzero` looks through pointers: a pointer to `0` or `""` skips the later rules, and so does an empty map or slice.
- `encoding/json` decodes every JSON string, even `""`, into a non-nil `[]byte`. On a `[]byte` field, `omitempty` and `omitnil` never skip a supplied value and `required` accepts any value. `omitzero` skips a value whose decoded length is zero, such as `""` or a string of line breaks.
- `structonly` and `nostructlevel` end a field's rule list; rules after them never run in Go and are not generated.
- On a struct field, `structonly` and `nostructlevel` skip the nested struct's field rules, so only its JSON shape is checked. `structonly` still applies the struct's `StructRules`; `nostructlevel` skips them too.

## Containers

- A slice or map of structs validates its elements only after `dive`. Without it, elements are checked for JSON shape only. Struct fields and pointers to structs are always validated.
- The exported schema of a named slice, array, or map type validates every struct its elements reach, as `StructValidator` does for a procedure input. A struct field of that type still validates its elements only after `dive`.
- Integer map keys must fit the Go key type. `"01"`, `"+1"`, and `"1"` name the same entry: keys are normalized to decimal before length, `unique`, key rules, and `dive` run. Parsed output uses the normalized keys and leaves the input unchanged.
- Map keys of a named scalar type accept any value of that type. Declared constants neither require entries nor restrict keys.
- `[]byte` fields are Base64 in JSON. Size rules count decoded bytes, and element rules validate those bytes.
- `unique` works on comparable values, and `unique=Field` on struct elements.
- `unique` fails generation on noncomparable values, which panic in validator, and on nested pointer identity. `unique=Field` fails when the field is missing, unexported, or not in the JSON.
- `unique` over `json.Number` fails generation unless the field has `json:",string"`, which keeps the text Go compares.

## Fixed Arrays

- Go fixed arrays keep their declared length. Schemas pad a short array with Go zero values and ignore extra entries before validating.
- Padding can produce `null` for pointers, maps, and slices, including inside nested structs. Generated types allow `null` in those positions, so parsed data stays assignable to `RouterInputs` and `RouterOutputs`. Named struct declarations and `tstype` overrides are unchanged.
- `required` rejects an all-zero array. `omitempty` skips the later rules, including `dive`, for an all-zero array.
- A non-nil pointer or an empty map or slice inside an array counts as nonzero.
- A missing optional array is validated as the zero array Go creates.
- Element rules run only after `dive`. JSON type and range checks always apply.
- A `time.Time` element is zero only when it is Go's zero time in UTC, as `reflect.Value.IsZero` sees it. Fractional digits after the ninth are dropped, and any explicit offset, even `+00:00`, makes the value nonzero.
- Zero-value rules that depend on fields JSON cannot set, or on the zero values of `json.Number` and `json.RawMessage`, fail generation.

## Cross-Field Rules

- Numbers compare decoded values. Integers with `json:",string"` compare without losing precision.
- String `eqfield` and `nefield` compare contents. `gtfield`, `gtefield`, `ltfield`, and `ltefield` on strings compare UTF-8 byte lengths.
- Array, slice, and map `eqfield` and `nefield` compare lengths, not elements. Ordered rules on collections follow validator's reflection behavior; use size tags such as `min=2` or `gt=1` for size limits.
- `time.Time` compares instants across timezone offsets, to the nanosecond.
- Fields of different Go kinds compare as validator compares them, even when their TypeScript types match: `eqfield` between an `int` and an `int64` fails.
- A target that JSON never sets, such as an unexported field or one tagged `json:"-"`, compares as its Go zero value.
- A target field that does not exist fails the comparison, except for `nefield`, which passes.
- Refinements respect pointer presence, omission tags, and whether a field promoted from an embedded pointer exists.
- An OR group produces one check with one error path.
- Nested paths such as `eqfield=Address.Code` fail generation. `*csfield` rules are not supported.

## Numbers

- Integer schemas require whole numbers within the Go type's bounds where JavaScript can represent them. Go `int` uses Zod's safe-integer range, and larger `int64` and `uint64` values lose precision as JavaScript numbers.
- Generic schemas keep the Go kinds of their type arguments, so `Box[int8]` and `Box[int16]` have different bounds even though both are TypeScript numbers.
- `float32` values and numeric rule parameters are rounded to 32 bits before comparison: `1.00000001` compares as `1`. Parsed output keeps the original number.
- Declared constants do not restrict a named type: its schema accepts any value of the underlying type. Add `oneof` to restrict it.
- A `json.Number` field is a `number` in TypeScript and in its schema, so a quoted numeric string that Go accepts is rejected. Its rules compare the decimal text JavaScript prints, where validator compares the text Go stored.

## JSON String Fields

Use `json:",string"` when an integer must stay exact across its full range:

```go
type LedgerInput struct {
    Amount int64 `json:"amount,string" validate:"gte=0"`
}
```

The TypeScript property is a string, and the schema validates the integer with `BigInt`. Parsing returns the string unchanged.

- The option also works on float, bool, string, and `json.Number` fields, and keeps their string encoding.
- Quoted floats follow Go's grammar for the option, including hexadecimal floats and digit separators, and round to the field's float size. Cross-field comparisons use the same rounding.
- A quoted `json.Number` accepts what Go's decoder stores, and its rules see that text. See [Go Versions](#go-versions).
- The string `"null"` validates as a JSON `null` does in Go: pointers are nil and other fields keep their zero value. Parsing keeps `"null"`.

## Strings And Time

- String rules check the text Go decodes. Unpaired surrogate escapes become U+FFFD, as in Go; valid pairs are kept.
- `time.Time` accepts Go's JSON timestamp grammar, including offsets, comma fractions, and the other spellings Go accepts. Parsing keeps the original string, and comparisons use its offset and full nanosecond precision.
- Go's `time.Time.UnmarshalJSON` can tell escaped timestamp characters from literal ones. `JSON.parse` has already decoded them, so schemas cannot.

## decodeGoJSON

[`decodeGoJSON`](/zod-schemas/#validate-raw-json) decodes JSON text as Go does before validating it:

- Field names match case-insensitively, with Go's case folding.
- Repeated struct fields merge according to their Go types. Repeated map fields combine entries, and each map entry decodes into a fresh value.
- A value Go cannot decode fails at its path in the text, even if a later key overwrote it.
- Validator rules apply to the final value of each field or entry.
- Unknown keys are checked at every occurrence in the text.
- JSON nested more than 10,000 levels deep fails, as in Go.

`safeParse` on an object from `JSON.parse` cannot see overwritten keys, but it rejects keys that name the same Go entry, such as `"01"` and `"1"` in a `map[int]` field.

`decodeGoJSON` is not a complete Go decoder. It does not run custom `UnmarshalJSON` or `UnmarshalText` methods, and map key types it cannot decode fail generation.

## Go Versions

Schemas follow the Go toolchain that generates them: its Unicode tables and its `encoding/json` decoder. Go 1.27 moved to Unicode 17 and to `encoding/json/v2`. Building with `GOEXPERIMENT=nojsonv2` keeps the original decoder.

- `alphaunicode`, `alphanumunicode`, `lowercase`, `uppercase`, and field-name matching use the generating Go's Unicode tables.
- `url` follows the generating Go's `net/url`, including its `urlstrictcolons` GODEBUG setting. Go 1.27 rejects a bracket after the first character of a host and accepts extra host colons for every scheme but `http` and `https`.
- A tag naming a key Go rejects keeps the Go field name. Go 1.27's default decoder accepts symbols such as emoji, and ignores a field whose tag name holds a backslash or quote.
- Go 1.27's default decoder accepts a leading `+` in quoted integers, and any `strconv.ParseFloat` syntax in quoted floats, including `Inf` and `NaN`.
- A quoted `NaN` is nonzero for `required` and unequal to every value, itself included.
- Go 1.27's default decoder stores only a JSON number in a quoted `json.Number`. The original decoder also stores text that starts like a number, or a nested string literal holding one.
- Go 1.27's default decoder rejects an unpaired surrogate escape inside a quoted string's nested literal.

## Custom Rule Predicates

- Scalar predicates receive the value Go decodes: `float32` values are rounded, unpaired surrogates are replaced, and integer `json:",string"` fields and integer map keys arrive as `bigint`.
- Collection predicates and `StructRules` receive the parsed JSON value.
- The parameter is a string, with `0x2C` and `0x7C` decoded.
- Aliases expand before field names resolve and before `dive` and `keys` scopes compile.
- Alias cycles, malformed scopes, aliases used in OR groups or given parameters, and rules used outside their `GoKinds` fail generation.
- Built-in tags cannot be redefined. Set a different `TagName` to change their behavior.
- A `StructRules` key that matches no type, several types, or a non-struct fails generation.
- `StructRules` do not translate Go struct-level validation functions. Attach checks for fields promoted from embedded structs to the containing type.
- `Strict` accepts rules marked `ServerOnly`.

## Zod-Only Omit

- A `zod_omit` field becomes an optional property with no runtime check, typed as the field's TypeScript type, such as `z.custom<string>().optional()`. Strict schemas accept the key, and its value passes through unchanged, including through `decodeGoJSON`.
- Named types used only by omitted fields get type-only declarations and no schema, so their rules never run.

## Limits

- JSON `null` on a non-pointer field and a missing required key are rejected, although Go leaves the zero value for both. Clients of the generated types cannot send either, and `decodeGoJSON` rejects them too.
- Go validator functions and options are not read from `WithValidator`. Declare client counterparts with [custom validation rules](/zod-schemas/#custom-validation-rules).
- Conditional tags such as `required_if` and `excluded_with`, and nested field namespaces, are not supported.
- Values of a recursive type nested a few thousand levels deep can exceed the JavaScript stack, although Go decodes up to 10,000 levels.
- Objects reject unknown keys by default, matching strict input. `WithStrictInput(false)` or `--zod-allow-unknown-fields` generates objects that keep them.
- Nullability, coercion, defaults, and transformations are not inferred.
