package gojson

import (
	"encoding/json"
	"testing"
)

// Each variable is probed with one payload; these share its decoder path, so
// a release that changes the path only in part fails here first.
func TestVariablesDescribeDecoder(t *testing.T) {
	type (
		quotedInt struct {
			V int64 `json:"v,string"`
		}
		quotedUint struct {
			V uint64 `json:"v,string"`
		}
		quotedFloat struct {
			V float32 `json:"v,string"`
		}
		quotedNumber struct {
			V *json.Number `json:"v,string"`
		}
		quotedString struct {
			V string `json:"v,string"`
		}
	)
	for _, tc := range []struct {
		name  string
		json  string
		value any
		want  bool
	}{
		{"int plus sign", `{"v":"+1"}`, new(quotedInt), !QuotedNumberPrefix},
		{"int minus sign", `{"v":"-1"}`, new(quotedInt), true},
		{"int leading zero", `{"v":"01"}`, new(quotedInt), true},
		{"uint plus sign", `{"v":"+1"}`, new(quotedUint), false},
		{"float leading point", `{"v":".5"}`, new(quotedFloat), !QuotedNumberPrefix},
		{"float infinity", `{"v":"Inf"}`, new(quotedFloat), !QuotedNumberPrefix},
		{"float negative infinity", `{"v":"-infinity"}`, new(quotedFloat), true},
		{"float NaN", `{"v":"nan"}`, new(quotedFloat), !QuotedNumberPrefix},
		{"float signed NaN", `{"v":"-NaN"}`, new(quotedFloat), false},
		{"float overflow", `{"v":"1e39"}`, new(quotedFloat), false},
		{"number trailing text", `{"v":"1x"}`, new(quotedNumber), LenientQuotedNumber},
		{"number leading zero", `{"v":"01"}`, new(quotedNumber), LenientQuotedNumber},
		{"number trailing space", `{"v":"1 "}`, new(quotedNumber), LenientQuotedNumber},
		{"number nested literal", `{"v":"\"1\""}`, new(quotedNumber), LenientQuotedNumber},
		{"number quoted null", `{"v":"null"}`, new(quotedNumber), LenientQuotedNumber},
		{"number leading space", `{"v":" 1"}`, new(quotedNumber), false},
		{"number literal", `{"v":"-1.5e3"}`, new(quotedNumber), true},
		{"string high surrogate", `{"v":"\"\\ud800\""}`, new(quotedString), ReplacesQuotedSurrogates},
		{"string low surrogate", `{"v":"\"a\\udc00\""}`, new(quotedString), ReplacesQuotedSurrogates},
		{"string surrogate pair", `{"v":"\"\\ud83d\\ude00\""}`, new(quotedString), true},
		{"string quoted null", `{"v":"null"}`, new(quotedString), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := json.Unmarshal([]byte(tc.json), tc.value)
			if got := err == nil; got != tc.want {
				t.Fatalf("decoding %s: accepted = %v, want %v (error: %v)", tc.json, got, tc.want, err)
			}
		})
	}
}
