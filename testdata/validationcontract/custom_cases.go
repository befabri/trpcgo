package validationcontract

import (
	"github.com/befabri/trpcgo/zodconfig"
)

// CustomValidation pairs developer-supplied TypeScript predicates with the Go
// registrations in the example-server oracle. Neither side is derived from
// the other implementation.
func CustomValidation() zodconfig.Config {
	return zodconfig.Config{
		TagName: "check", Strict: true,
		Aliases: map[string]string{"bounded": "required,range", "range": "min=2,max=4", "matching": "eqfield=First", "key": "required,alpha", "nonempty": "min=1"},
		Rules: map[string]zodconfig.Rule{
			"arraylength": {Predicate: "(value, parameter) => value.length === Number(parameter)", GoKinds: []string{"array"}},
			"dynamic":     {Predicate: "checks.dynamic", GoKinds: []string{"int"}},
			"nilmap":      {Predicate: "value => value === null", GoKinds: []string{"map"}},
			"zero":        {Predicate: "value => value === 0n", GoKinds: []string{"int64"}},
			"available":   {ServerOnly: true},
			"even":        {Predicate: "(value, parameter) => value % Number(parameter) === 0", GoKinds: []string{"int"}, Message: "Must be divisible by the parameter"},
			"suffix":      {Predicate: "(value, parameter) => value.endsWith(parameter)", GoKinds: []string{"string"}},
			"rounded":     {Predicate: "value => value === 16777216", GoKinds: []string{"float32"}},
			"large":       {Predicate: "value => value === 9223372036854775807n", GoKinds: []string{"int64"}},
			"truth":       {Predicate: "value => value === true", GoKinds: []string{"bool"}},
			"replacement": {Predicate: `value => value === "\uFFFD"`, GoKinds: []string{"string"}},
			"safe":        {Predicate: "checks.safe", GoKinds: []string{"string"}},
		},
		Imports: map[string]string{"checks": "./custom-validators"},
		StructRules: map[string][]zodconfig.StructRule{
			"CustomStructInput": {{Predicate: "data => data.end >= data.start", Path: []string{"end"}, Message: "End must follow start"}},
		},
	}
}

const CustomValidatorsTS = `export function safe(value: string): unknown {
  if (value === 'throw') throw new Error('Application predicate failed');
  if (value === 'promise') return Promise.resolve(false);
  if (value === 'reject') return Promise.reject(new Error('Async application predicate failed'));
  if (value === 'truthy') return 'yes';
  return value === 'ok';
}
let minimum = 1;
export function setMinimum(value: number): void { minimum = value; }
export function dynamic(value: number): boolean { return value >= minimum; }
`

var CustomCases = []Case{
	{Name: "custom/alias-valid", Type: "CustomAliasInput", JSON: `{"first":"ab","again":"ab"}`, Valid: true},
	{Name: "custom/alias-short", Type: "CustomAliasInput", JSON: `{"first":"a","again":"a"}`},
	{Name: "custom/alias-long", Type: "CustomAliasInput", JSON: `{"first":"abcde","again":"abcde"}`},
	{Name: "custom/alias-go-field-binding", Type: "CustomAliasInput", JSON: `{"first":"ab","again":"ac"}`},
	{Name: "custom/dive-valid", Type: "CustomDiveInput", JSON: `{"values":{"alpha":[2,4],"b":[0]}}`, Valid: true},
	{Name: "custom/dive-key", Type: "CustomDiveInput", JSON: `{"values":{"1":[2]}}`},
	{Name: "custom/dive-empty", Type: "CustomDiveInput", JSON: `{"values":{"a":[]}}`},
	{Name: "custom/dive-element", Type: "CustomDiveInput", JSON: `{"values":{"a":[2,3]}}`},
	{Name: "custom/scalar-decoding", Type: "CustomScalarInput", JSON: `{"text":"x,|","float":16777217,"quoted":"9223372036854775807","bool":"true","string":"\"\ud800\""}`, Valid: true},
	{Name: "custom/scalar-float", Type: "CustomScalarInput", JSON: `{"text":"x,|","float":16777218,"quoted":"9223372036854775807","bool":"true","string":"\"\ud800\""}`},
	{Name: "custom/scalar-bigint", Type: "CustomScalarInput", JSON: `{"text":"x,|","float":16777217,"quoted":"9223372036854775806","bool":"true","string":"\"\ud800\""}`},
	{Name: "custom/scalar-parameter", Type: "CustomScalarInput", JSON: `{"text":"x,","float":16777217,"quoted":"9223372036854775807","bool":"true","string":"\"\ud800\""}`},
	{Name: "custom/scalar-bool", Type: "CustomScalarInput", JSON: `{"text":"x,|","float":16777217,"quoted":"9223372036854775807","bool":"false","string":"\"\ud800\""}`},
	{Name: "custom/omit-zero", Type: "CustomOmitInput", JSON: `{}`, Valid: true},
	{Name: "custom/omit-even", Type: "CustomOmitInput", JSON: `{"value":2,"other":4}`, Valid: true},
	{Name: "custom/omit-odd", Type: "CustomOmitInput", JSON: `{"value":3}`},
	{Name: "custom/struct-valid", Type: "CustomStructInput", JSON: `{"start":3,"end":4}`, Valid: true},
	{Name: "custom/struct-invalid", Type: "CustomStructInput", JSON: `{"start":4,"end":3}`},
	{Name: "custom/nested-valid", Type: "CustomNestedInput", JSON: `{"window":{"start":3,"end":4}}`, Valid: true},
	{Name: "custom/nested-invalid", Type: "CustomNestedInput", JSON: `{"window":{"start":4,"end":3}}`},
	{Name: "custom/import-valid", Type: "CustomSafeInput", JSON: `{"value":"ok"}`, Valid: true},
	{Name: "custom/import-false", Type: "CustomSafeInput", JSON: `{"value":"no"}`},
	{Name: "custom/import-throw", Type: "CustomSafeInput", JSON: `{"value":"throw"}`},
	{Name: "custom/import-promise", Type: "CustomSafeInput", JSON: `{"value":"promise"}`},
	{Name: "custom/import-rejected-promise", Type: "CustomSafeInput", JSON: `{"value":"reject"}`},
	{Name: "custom/import-truthy", Type: "CustomSafeInput", JSON: `{"value":"truthy"}`},
	{Name: "custom/or-first", Type: "CustomORInput", JSON: `{"value":2}`, Valid: true},
	{Name: "custom/or-second", Type: "CustomORInput", JSON: `{"value":3}`, Valid: true},
	{Name: "custom/or-neither", Type: "CustomORInput", JSON: `{"value":5}`},
	{Name: "custom/cross-or-first", Type: "CustomCrossORInput", JSON: `{"first":1,"value":2}`, Valid: true},
	{Name: "custom/cross-or-second", Type: "CustomCrossORInput", JSON: `{"first":3,"value":3}`, Valid: true},
	{Name: "custom/cross-or-neither", Type: "CustomCrossORInput", JSON: `{"first":1,"value":3}`},
	{Name: "custom/server-only-or", Type: "CustomServerInput", JSON: `{"first":1,"value":2,"other":2,"quoted":"2"}`, Valid: true},
	{Name: "custom/server-only-or-wire", Type: "CustomServerInput", JSON: `{"first":1,"value":2,"other":2,"quoted":"wrong"}`},
	{Name: "custom/dynamic-missing", Type: "CustomDynamicInput", JSON: `{}`},
	{Name: "custom/dynamic-present", Type: "CustomDynamicInput", JSON: `{"value":1}`, Valid: true},
	{Name: "custom/nil-missing", Type: "CustomNilInput", JSON: `{}`, Valid: true},
	{Name: "custom/nil-empty", Type: "CustomNilInput", JSON: `{"values":{}}`},
	{Name: "custom/nil-nonempty", Type: "CustomNilInput", JSON: `{"values":{"a":1}}`},
	{Name: "custom/nil-or-missing", Type: "CustomNilORInput", JSON: `{}`, Valid: true},
	{Name: "custom/nil-or-empty", Type: "CustomNilORInput", JSON: `{"values":{}}`, Valid: true},
	{Name: "custom/nil-or-nonempty", Type: "CustomNilORInput", JSON: `{"values":{"a":1}}`},
	{Name: "custom/quoted-zero-missing", Type: "CustomZeroQuotedInput", JSON: `{}`, Valid: true},
	{Name: "custom/quoted-zero-present", Type: "CustomZeroQuotedInput", JSON: `{"value":"0"}`, Valid: true},
	{Name: "custom/quoted-zero-nonzero", Type: "CustomZeroQuotedInput", JSON: `{"value":"1"}`},
	{Name: "custom/array-zero-missing", Type: "CustomOptionalArrayInput", JSON: `{}`, Valid: true},
	{Name: "custom/array-zero-padded", Type: "CustomOptionalArrayInput", JSON: `{"value":[]}`, Valid: true},
	{Name: "custom/array-zero-truncated", Type: "CustomOptionalArrayInput", JSON: `{"value":[1,2,3]}`, Valid: true},
	{Name: "custom/array-zero-missing-invalid", Type: "CustomOptionalArrayInvalid", JSON: `{}`},
	{Name: "custom/array-zero-present-invalid", Type: "CustomOptionalArrayInvalid", JSON: `{"value":[1,2,3]}`},
}

func init() {
	defineFixtures(
		inputFixture[CustomAliasInput](),
		inputFixture[CustomCrossORInput](),
		inputFixture[CustomDiveInput](),
		inputFixture[CustomDynamicInput](),
		inputFixture[CustomNestedInput](),
		inputFixture[CustomNilInput](),
		inputFixture[CustomNilORInput](),
		inputFixture[CustomORInput](),
		inputFixture[CustomOmitInput](),
		inputFixture[CustomOptionalArrayInput](),
		inputFixture[CustomOptionalArrayInvalid](),
		inputFixture[CustomSafeInput](),
		inputFixture[CustomScalarInput](),
		inputFixture[CustomServerInput](),
		inputFixture[CustomStructInput](),
		inputFixture[CustomZeroQuotedInput](),
	)
}
