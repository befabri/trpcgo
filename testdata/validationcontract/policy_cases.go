package validationcontract

var PolicyCases = []Case{
	{Name: "policy/omit-embedded-absent", Type: "PolicyOmitEmbedded", JSON: `{}`, Valid: true},
	{Name: "policy/omit-embedded-hidden-only", Type: "PolicyOmitEmbedded", JSON: `{"hidden":"provided"}`, Valid: false},
	{Name: "policy/omit-embedded-hidden-null", Type: "PolicyOmitEmbedded", JSON: `{"hidden":null}`, Valid: false},
	{Name: "policy/omit-embedded-complete", Type: "PolicyOmitEmbedded", JSON: `{"hidden":"provided","value":"ok"}`, Valid: true},
	{Name: "policy/omit-embedded-value-only", Type: "PolicyOmitEmbedded", JSON: `{"value":"ok"}`, Valid: true},
	{Name: "policy/omit-embedded-empty-value", Type: "PolicyOmitEmbedded", JSON: `{"value":""}`, Valid: false},
	{Name: "policy/omit-extended-absent", Type: "PolicyOmitExtended", JSON: `{}`, Valid: true},
	{Name: "policy/omit-extended-hidden-only", Type: "PolicyOmitExtended", JSON: `{"hidden":"provided"}`, Valid: false},
	{Name: "policy/omit-extended-hidden-null", Type: "PolicyOmitExtended", JSON: `{"hidden":null}`, Valid: false},
	{Name: "policy/omit-extended-complete", Type: "PolicyOmitExtended", JSON: `{"hidden":"provided","value":"ok"}`, Valid: true},
	{Name: "policy/omit-extended-value-only", Type: "PolicyOmitExtended", JSON: `{"value":"ok"}`, Valid: true},
	{Name: "policy/omit-extended-empty-value", Type: "PolicyOmitExtended", JSON: `{"value":""}`, Valid: false},
	{Name: "policy/omit-provided", Type: "PolicyOmitInput", JSON: `{"name":"ok","id":"supplied","keys":{}}`, Valid: true},
	{Name: "policy/omit-absent", Type: "PolicyOmitInput", JSON: `{"name":"ok","keys":{}}`, Valid: true},
	{Name: "policy/omit-extra", Type: "PolicyOmitInput", JSON: `{"name":"ok","id":"supplied","keys":{},"extra":true}`},

	{Name: "policy/enum-known", Type: "PolicyEnumInput", JSON: `{"role":"known","status":1,"keys":{"known":"value"}}`, Valid: true},
	{Name: "policy/enum-unlisted", Type: "PolicyEnumInput", JSON: `{"role":"custom","status":2,"keys":{"other":"value"}}`, Valid: true},
	{Name: "policy/enum-zero", Type: "PolicyEnumInput", JSON: `{"role":"","status":0,"keys":{}}`, Valid: true},
	{Name: "policy/enum-negative", Type: "PolicyEnumInput", JSON: `{"role":"custom","status":-128,"keys":{}}`, Valid: true},
	{Name: "policy/enum-overflow", Type: "PolicyEnumInput", JSON: `{"role":"custom","status":128,"keys":{}}`},
	{Name: "policy/enum-oneof-known", Type: "PolicyEnumExplicit", JSON: `{"role":"known","status":1}`, Valid: true},
	{Name: "policy/enum-oneof-unlisted-role", Type: "PolicyEnumExplicit", JSON: `{"role":"other","status":1}`},
	{Name: "policy/enum-oneof-unlisted-status", Type: "PolicyEnumExplicit", JSON: `{"role":"known","status":2}`},
	{Name: "policy/enum-quoted-unlisted", Type: "PolicyEnumQuoted", JSON: `{"role":"\"other\"","status":"2"}`, Valid: true},
	{Name: "policy/strict-known", Type: "PolicyStrictInput", JSON: `{"nested":{"value":1},"values":{}}`, Valid: true},
	{Name: "policy/strict-root-unknown", Type: "PolicyStrictInput", JSON: `{"nested":{"value":1},"values":{},"extra":true}`},
	{Name: "policy/strict-nested-unknown", Type: "PolicyStrictInput", JSON: `{"nested":{"value":1,"extra":true},"values":{}}`},
	{Name: "policy/strict-overwritten-unknown", Type: "PolicyStrictInput", JSON: `{"nested":{"extra":true},"nested":{"value":1},"values":{}}`},
	{Name: "policy/strict-overwritten-map-unknown", Type: "PolicyStrictInput", JSON: `{"nested":{"value":1},"values":{"01":{"extra":true},"1":{"value":1}}}`},
}

func init() {
	defineCases(PolicyCases,
		inputFixture[PolicyEnumExplicit](),
		inputFixture[PolicyEnumInput](),
		inputFixture[PolicyEnumQuoted](),
		inputFixture[PolicyOmitEmbedded](),
		inputFixture[PolicyOmitExtended](),
		inputFixture[PolicyOmitInput](),
		inputFixture[PolicyStrictInput](),
	)
}
