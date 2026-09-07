package typemap

import (
	"reflect"
	"strings"
	"testing"
)

func TestOrderedRuleGrammar(t *testing.T) {
	got := ParseValidateTag(`validate:"min=2,contains=0x2C|contains=0x7C,omitempty,max=5"`)
	want := []ValidateRule{{Tag: "min", Param: "2", HasParam: true}, {Alternatives: []ValidateRule{{Tag: "contains", Param: ",", HasParam: true}, {Tag: "contains", Param: "|", HasParam: true}}}, {Tag: "omitempty"}, {Tag: "max", Param: "5", HasParam: true}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("rule tree = %#v; want %#v", got, want)
	}
	for _, raw := range []string{`validate:"min=1,,max=2"`, `validate:"required,-"`, `validate:"required, email"`} {
		if len(UnsupportedZodRules(ParseValidateTag(raw))) == 0 {
			t.Errorf("malformed grammar %s was silently normalized", raw)
		}
	}
}

func TestOneofUsesValidatorWhitespaceGrammar(t *testing.T) {
	// Go regexp's \S includes vertical tab: it belongs to the value, unlike a
	// space, tab, newline, carriage return or form feed.
	got := parseOneofValues("a\vb 'c d' e\tf")
	want := []string{"a\vb", "c d", "e", "f"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("values=%q; want %q", got, want)
	}
}

func TestPresenceFollowsOmissionOrder(t *testing.T) {
	for _, tc := range []struct {
		tag  string
		want bool
	}{
		{`validate:"required,omitempty"`, true},
		{`validate:"omitempty,required"`, false},
		{`validate:"omitnil,required"`, false},
		{`validate:"dive,required"`, false},
		{`validate:"required|startswith=a"`, false},
	} {
		if got := ValidationRequiresPresence(ParseValidateTag(tc.tag)); got != tc.want {
			t.Errorf("%s presence=%v", tc.tag, got)
		}
	}
}

func TestRulesAreTypeChecked(t *testing.T) {
	for _, tc := range []struct{ kind, tag, param string }{
		{"int", "email", ""}, {"int", "alpha", ""}, {"bool", "oneof", "true false"},
		{"bool", "min", "1"}, {"map", "lowercase", ""}, {"string", "unique", ""},
		{"bool", "eq", "perhaps"}, {"string", "min", "0); malicious()"},
	} {
		rule := ValidateRule{Tag: tc.tag, Param: tc.param}
		if len(InvalidZodRules([]ValidateRule{rule}, tc.kind)) != 1 {
			t.Errorf("%s on %s not diagnosed", tc.tag, tc.kind)
		}
	}
	for _, kind := range []string{"int8", "int16", "int32", "int64", "uint8", "uint16", "uint32", "uint64", "float32", "float64"} {
		rules := []ValidateRule{{Tag: "numeric"}}
		if len(InvalidZodRules(rules, kind)) != 0 {
			t.Errorf("numeric on %s diagnosed as invalid", kind)
		}
		f := Field{Type: "number", GoKind: kind, Validate: rules}
		if strings.Contains(ZodType(f, ZodStandard), "regex") {
			t.Errorf("numeric on %s emitted a regex", kind)
		}
	}
}

func TestJavaScriptStringLiteralRoundTripsControlCharacters(t *testing.T) {
	if got := ZodStringLiteral("\a\U0001f600\u2028"); got != `"\u0007😀\u2028"` {
		t.Fatalf("literal=%s", got)
	}
}

func TestUniqueRequiresComparableScalarMetadata(t *testing.T) {
	for _, kind := range []string{"struct", "array", "slice", "map", "time.Time", "json.Number", ""} {
		field := Field{GoKind: "slice", Element: &ElementType{GoKind: kind}, Validate: []ValidateRule{{Tag: "unique"}}}
		if err := ValidateZodFieldRules(field); err == nil {
			t.Errorf("unique on %s element needs a diagnostic", kind)
		}
		if _, ok := ZodRulePredicate(field, field.Validate[0], "value"); ok {
			t.Errorf("unique on %s must not emit incorrect identity comparison", kind)
		}
	}
	for _, kind := range []string{"string", "int", "uint64", "float64", "bool"} {
		for _, pointer := range []bool{false, true} {
			field := Field{GoKind: "slice", Element: &ElementType{GoKind: kind, IsPointer: pointer}, Validate: []ValidateRule{{Tag: "unique"}}}
			if err := ValidateZodFieldRules(field); err != nil {
				t.Errorf("unique on %s pointer=%v: %v", kind, pointer, err)
			}
		}
	}
}
