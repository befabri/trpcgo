package typemap

import (
	"maps"
	"slices"
	"strings"
	"testing"
)

func TestZodTypedChecksRenderBothStyles(t *testing.T) {
	f := Field{Type: "string", GoKind: "string", Validate: []ValidateRule{{Tag: "min", Param: "2"}, {Tag: "max", Param: "8"}, {Tag: "contains", Param: "a)b"}}}
	for _, style := range []ZodStyle{ZodStandard, ZodMini} {
		got := ZodType(f, style)
		if !strings.Contains(got, `"a)b"`) {
			t.Errorf("literal parameter lost: %s", got)
		}
	}
}

func TestZodNumericParamsRejectUnsafeLiterals(t *testing.T) {
	tests := []struct {
		name  string
		field Field
		want  string
	}{
		{
			name: "numeric constraint injection",
			field: Field{Type: "number", GoKind: "int", Validate: []ValidateRule{
				{Tag: "min", Param: `1); evil()`},
			}},
			want: "z.int()",
		},
		{
			name: "numeric oneof injection",
			field: Field{Type: "number", GoKind: "int", Validate: []ValidateRule{
				{Tag: "oneof", Param: `1 2); evil()`},
			}},
			want: "z.int()",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ZodType(tt.field, ZodStandard)
			if got != tt.want {
				t.Fatalf("ZodType = %q, want %q", got, tt.want)
			}
			if strings.Contains(got, "evil") {
				t.Fatalf("unsafe validate tag parameter leaked into Zod output: %q", got)
			}
		})
	}
}

func TestZodNumericLenMiniEmitsEqualityRange(t *testing.T) {
	f := Field{Type: "number", GoKind: "int", Validate: []ValidateRule{
		{Tag: "len", Param: "5"},
	}}

	got := ZodType(f, ZodMini)
	if got != "z.int().check(z.gte(5), z.lte(5))" {
		t.Fatalf("ZodType mini = %q, want equality range", got)
	}
}

func TestZodNumberAndLengthLiteralsNormalizeSafeForms(t *testing.T) {
	for _, tt := range []struct {
		name  string
		param string
		want  string
	}{
		{name: "decimal", param: "1.0", want: "1"},
		{name: "exponent", param: "1e3", want: "1000"},
		{name: "hex float", param: "0x1p2", want: "4"},
		{name: "plus sign", param: "+1", want: "1"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ZodNumberLiteral(tt.param)
			if !ok || got != tt.want {
				t.Fatalf("ZodNumberLiteral(%q) = %q, %v; want %q, true", tt.param, got, ok, tt.want)
			}
		})
	}

	for _, tt := range []struct {
		name  string
		param string
		want  string
	}{
		{name: "hex integer", param: "0x10", want: "16"},
		{name: "underscore integer", param: "1_000", want: "1000"},
		{name: "octal integer", param: "010", want: "8"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ZodLengthLiteral(tt.param)
			if !ok || got != tt.want {
				t.Fatalf("ZodLengthLiteral(%q) = %q, %v; want %q, true", tt.param, got, ok, tt.want)
			}
		})
	}

	for _, param := range []string{"NaN", "Inf", "1); evil()", "1e3"} {
		t.Run("invalid length "+param, func(t *testing.T) {
			if got, ok := ZodLengthLiteral(param); ok {
				t.Fatalf("ZodLengthLiteral(%q) = %q, true; want false", param, got)
			}
		})
	}
}

func TestZodMiniStringConstraintsWithClosingParen(t *testing.T) {
	f := Field{Type: "string", GoKind: "string", Validate: []ValidateRule{
		{Tag: "startswith", Param: ")"},
		{Tag: "contains", Param: "a)b"},
	}}

	got := ZodType(f, ZodMini)
	for _, want := range []string{`z.startsWith(")")`, `z.includes("a)b")`} {
		if !strings.Contains(got, want) {
			t.Fatalf("ZodType mini missing %q in %q", want, got)
		}
	}
}

func TestParseOneofValuesMatchesValidatorQuotedValues(t *testing.T) {
	got := parseOneofValues("'red green' blue can't")
	want := []string{"red green", "blue", "cant"}
	if len(got) != len(want) {
		t.Fatalf("parseOneofValues length = %d, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("parseOneofValues[%d] = %q, want %q; all values: %v", i, got[i], want[i], got)
		}
	}
}

func TestUnsupportedZodRules(t *testing.T) {
	t.Run("all supported returns nil", func(t *testing.T) {
		rules := []ValidateRule{
			{Tag: "required"},
			{Tag: "min", Param: "3"},
			{Tag: "email"},
		}
		got := UnsupportedZodRules(rules)
		if len(got) != 0 {
			t.Errorf("expected nil, got %v", got)
		}
	})

	t.Run("unsupported tags returned", func(t *testing.T) {
		rules := []ValidateRule{
			{Tag: "required"},
			{Tag: "alphanum_underscore"},
			{Tag: "custom_check"},
		}
		got := UnsupportedZodRules(rules)
		if len(got) != 2 {
			t.Fatalf("expected 2 unsupported, got %d: %v", len(got), got)
		}
		if got[0].Tag != "alphanum_underscore" {
			t.Errorf("got[0] = %+v, want alphanum_underscore", got[0])
		}
		if got[1].Tag != "custom_check" {
			t.Errorf("got[1] = %+v, want custom_check", got[1])
		}
	})

	t.Run("cross-field tags are supported", func(t *testing.T) {
		rules := []ValidateRule{
			{Tag: "required"},
			{Tag: "gtefield", Param: "MinVal"},
			{Tag: "ltefield", Param: "MaxVal"},
		}
		got := UnsupportedZodRules(rules)
		if len(got) != 0 {
			t.Errorf("cross-field tags should be supported, got %v", got)
		}
	})

	t.Run("new format and constraint tags are supported", func(t *testing.T) {
		rules := []ValidateRule{
			{Tag: "hostname"},
			{Tag: "hostname_rfc1123"},
			{Tag: "base64url"},
			{Tag: "hexadecimal"},
			{Tag: "ulid"},
			{Tag: "mac"},
			{Tag: "cidrv4"},
			{Tag: "cidrv6"},
			{Tag: "uppercase"},
			{Tag: "startswith", Param: "https://"},
			{Tag: "endswith", Param: ".go"},
			{Tag: "contains", Param: "api"},
		}
		got := UnsupportedZodRules(rules)
		if len(got) != 0 {
			tags := make([]string, len(got))
			for i, r := range got {
				tags[i] = r.Tag
			}
			t.Errorf("expected all supported, but got unsupported: %v", tags)
		}
	})

	t.Run("nil input returns nil", func(t *testing.T) {
		got := UnsupportedZodRules(nil)
		if got != nil {
			t.Errorf("expected nil, got %v", got)
		}
	})
}

func TestInvalidZodRules(t *testing.T) {
	t.Run("invalid numeric constraints returned", func(t *testing.T) {
		rules := []ValidateRule{
			{Tag: "required"},
			{Tag: "min", Param: "1); evil()"},
			{Tag: "max", Param: "0x10"},
		}
		got := InvalidZodRules(rules, "int")
		if len(got) != 1 {
			t.Fatalf("expected 1 invalid rule, got %d: %v", len(got), got)
		}
		if got[0].Tag != "min" || got[0].Param != "1); evil()" {
			t.Fatalf("invalid rule = %+v", got[0])
		}
	})

	t.Run("integer constraints use validator base zero parsing", func(t *testing.T) {
		rules := []ValidateRule{{Tag: "min", Param: "0x10"}, {Tag: "max", Param: "1e3"}}
		got := InvalidZodRules(rules, "int")
		if len(got) != 1 || got[0].Param != "1e3" {
			t.Fatalf("expected only exponent int param to be invalid, got %v", got)
		}
	})

	t.Run("invalid numeric oneof returned for numeric fields only", func(t *testing.T) {
		rules := []ValidateRule{{Tag: "oneof", Param: "1 2); evil()"}}
		got := InvalidZodRules(rules, "int")
		if len(got) != 1 || got[0].Tag != "oneof" {
			t.Fatalf("numeric oneof should be invalid, got %v", got)
		}
		if got := InvalidZodRules(rules, "string"); len(got) != 0 {
			t.Fatalf("string oneof should not be invalid, got %v", got)
		}
	})

	t.Run("numeric oneof uses canonical server values", func(t *testing.T) {
		rules := []ValidateRule{{Tag: "oneof", Param: "01 2"}}
		got := InvalidZodRules(rules, "int")
		if len(got) != 1 || got[0].Tag != "oneof" {
			t.Fatalf("non-canonical int oneof should be invalid, got %v", got)
		}
		if got := InvalidZodRules([]ValidateRule{{Tag: "oneof", Param: "0.5"}}, "float64"); len(got) != 1 {
			t.Fatalf("float oneof should be invalid, got %v", got)
		}
	})

	t.Run("unsupported tags are not duplicated as invalid", func(t *testing.T) {
		rules := []ValidateRule{{Tag: "custom", Param: "1); evil()"}}
		if got := InvalidZodRules(rules, "int"); len(got) != 0 {
			t.Fatalf("unsupported tag should not be invalid too: %v", got)
		}
	})

	t.Run("missing required params are invalid", func(t *testing.T) {
		rules := []ValidateRule{{Tag: "min"}, {Tag: "oneof"}}
		got := InvalidZodRules(rules, "int")
		if len(got) != 2 {
			t.Fatalf("expected missing params to be invalid, got %v", got)
		}
	})

	t.Run("len on unsupported primitive kind is invalid", func(t *testing.T) {
		rules := []ValidateRule{{Tag: "len", Param: "1"}}
		got := InvalidZodRules(rules, "bool")
		if len(got) != 1 || got[0].Tag != "len" {
			t.Fatalf("bool len should be invalid, got %v", got)
		}
	})

	t.Run("min on unsupported primitive kind is invalid", func(t *testing.T) {
		rules := []ValidateRule{{Tag: "min", Param: "1"}}
		got := InvalidZodRules(rules, "bool")
		if len(got) != 1 || got[0].Tag != "min" {
			t.Fatalf("bool min should be invalid, got %v", got)
		}
	})
}

func TestCrossFieldOp(t *testing.T) {
	tests := []struct {
		tag    string
		wantOp string
		wantOk bool
	}{
		{"gtefield", ">=", true},
		{"ltefield", "<=", true},
		{"gtfield", ">", true},
		{"ltfield", "<", true},
		{"eqfield", "===", true},
		{"nefield", "!==", true},
		{"min", "", false},
		{"required", "", false},
		{"custom", "", false},
	}
	for _, tc := range tests {
		op, ok := CrossFieldOp(tc.tag)
		if ok != tc.wantOk || op != tc.wantOp {
			t.Errorf("CrossFieldOp(%q) = (%q, %v), want (%q, %v)", tc.tag, op, ok, tc.wantOp, tc.wantOk)
		}
	}
}

func TestParseZodOmitTag(t *testing.T) {
	tests := []struct {
		tag  string
		want bool
	}{
		{`json:"id" zod_omit:"true"`, true},
		{`json:"name"`, false},
		{`json:"id" zod_omit:"false"`, false},
		{`zod_omit:"true"`, true},
		{``, false},
	}
	for _, tc := range tests {
		got := ParseZodOmitTag(tc.tag)
		if got != tc.want {
			t.Errorf("ParseZodOmitTag(%q) = %v, want %v", tc.tag, got, tc.want)
		}
	}
}

// An unparseable length parameter on a string comparison tag must be dropped
// (and flagged via InvalidZod), never emitted as code.
func TestZodStringLengthComparisonRejectsInvalidParam(t *testing.T) {
	f := Field{Name: "code", Type: "string", GoKind: "string",
		Validate: []ValidateRule{{Tag: "gt", Param: "1); evil()"}}}

	if got := ZodType(f, ZodStandard); got != "z.string()" {
		t.Errorf("ZodType = %q, want bare z.string() for unsafe param", got)
	}
	if invalid := InvalidZodRules(f.Validate, "string"); len(invalid) != 1 || invalid[0].Tag != "gt" {
		t.Errorf("InvalidZodRules = %+v, want the gt rule flagged", invalid)
	}
}

// Named types, arrays and records are composed by the schema writer.
func TestZodBaseLeavesComposedTypesToTheWriter(t *testing.T) {
	for _, tsType := range []string{"User", "string[]", "Record<string, number>"} {
		if got := zodBaseFromKindAndType(tsType, "struct", nil); got != "" {
			t.Errorf("zodBaseFromKindAndType(%q) = %q, want no base", tsType, got)
		}
	}
}

// Every rendering table must stay a subset of the supported set. Otherwise a
// tag could produce Zod output while being reported as unsupported, or the
// contract coverage gate could miss it.
func TestSupportedZodTagsAreConsistent(t *testing.T) {
	supported := SupportedZodTags()
	if !slices.IsSorted(supported) || len(supported) != len(supportedZodTags) {
		t.Fatalf("SupportedZodTags() = %v, want the sorted keys of supportedZodTags", supported)
	}
	tables := map[string][]string{
		"zodFormats":        slices.Collect(maps.Keys(zodFormats)),
		"zodStructuralTags": slices.Collect(maps.Keys(zodStructuralTags)),
		"crossFieldOps":     slices.Collect(maps.Keys(crossFieldOps)),
	}
	for name, tags := range tables {
		for _, tag := range tags {
			if !supportedZodTags[tag] {
				t.Errorf("%s contains %q, which supportedZodTags does not list", name, tag)
			}
		}
	}
	for tag := range zodStructuralTags {
		if _, cross := CrossFieldOp(tag); cross || zodFormats[tag].predicate != nil {
			t.Errorf("%q is structural and cannot also be a rule", tag)
		}
	}
}
