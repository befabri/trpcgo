package typemap

import (
	"strings"
	"testing"
)

func TestZodBaseFromKindAndTypeCoversFormatsOneOfAndFallbacks(t *testing.T) {
	tests := []struct {
		name   string
		tsType string
		goKind string
		rules  []ValidateRule
		want   string
	}{
		{"email format", "string", "string", []ValidateRule{{Tag: "email"}}, "z.email()"},
		{"url format", "string", "string", []ValidateRule{{Tag: "url"}}, "z.url()"},
		{"uuid format", "string", "string", []ValidateRule{{Tag: "uuid"}}, "z.uuidv4()"},
		{"e164 format", "string", "string", []ValidateRule{{Tag: "e164"}}, "z.e164()"},
		{"jwt format", "string", "string", []ValidateRule{{Tag: "jwt"}}, "z.jwt()"},
		{"base64 format", "string", "string", []ValidateRule{{Tag: "base64"}}, "z.base64()"},
		{"lowercase format", "string", "string", []ValidateRule{{Tag: "lowercase"}}, "z.lowercase()"},
		{"ipv4 format", "string", "string", []ValidateRule{{Tag: "ipv4"}}, "z.ipv4()"},
		{"ipv6 format", "string", "string", []ValidateRule{{Tag: "ipv6"}}, "z.ipv6()"},
		{"hostname format", "string", "string", []ValidateRule{{Tag: "hostname_rfc1123"}}, "z.hostname()"},
		{"base64url format", "string", "string", []ValidateRule{{Tag: "base64url"}}, "z.base64url()"},
		{"hex format", "string", "string", []ValidateRule{{Tag: "hexadecimal"}}, "z.hex()"},
		{"ulid format", "string", "string", []ValidateRule{{Tag: "ulid"}}, "z.ulid()"},
		{"mac format", "string", "string", []ValidateRule{{Tag: "mac"}}, "z.mac()"},
		{"cidrv4 format", "string", "string", []ValidateRule{{Tag: "cidrv4"}}, "z.cidrv4()"},
		{"cidrv6 format", "string", "string", []ValidateRule{{Tag: "cidrv6"}}, "z.cidrv6()"},
		{"uppercase format", "string", "string", []ValidateRule{{Tag: "uppercase"}}, "z.uppercase()"},
		{"numeric oneof", "number", "int", []ValidateRule{{Tag: "oneof", Param: "1 2 3"}}, "z.union([z.literal(1), z.literal(2), z.literal(3)])"},
		{"single numeric oneof", "number", "int", []ValidateRule{{Tag: "oneof", Param: "1"}}, "z.literal(1)"},
		{"string oneof", "string", "string", []ValidateRule{{Tag: "oneof", Param: "a b"}}, `z.enum(["a", "b"])`},
		{"time", "string", "time.Time", nil, "z.iso.datetime()"},
		{"bytes", "string", "[]byte", nil, "z.base64()"},
		{"int", "number", "int", nil, "z.int()"},
		{"int32", "number", "int32", nil, "z.int32()"},
		{"int64", "number", "int64", nil, "z.number()"},
		{"uint32", "number", "uint32", nil, "z.uint32()"},
		{"uint64", "number", "uint64", nil, "z.number()"},
		{"float32", "number", "float32", nil, "z.float32()"},
		{"float64", "number", "float64", nil, "z.float64()"},
		{"small int", "number", "int8", nil, "z.number()"},
		{"string kind", "string", "string", nil, "z.string()"},
		{"bool kind", "boolean", "bool", nil, "z.boolean()"},
		{"ts string", "string", "", nil, "z.string()"},
		{"ts number", "number", "", nil, "z.number()"},
		{"ts boolean", "boolean", "", nil, "z.boolean()"},
		{"ts unknown", "unknown", "", nil, "z.unknown()"},
		{"named type", "User", "struct", nil, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := zodBaseFromKindAndType(tt.tsType, tt.goKind, tt.rules); got != tt.want {
				t.Errorf("zodBaseFromKindAndType() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestZodConstraintsAndMiniCoverConstraintSyntax(t *testing.T) {
	f := Field{Type: "string", GoKind: "string", Validate: []ValidateRule{
		{Tag: "min", Param: "2"},
		{Tag: "max", Param: "8"},
		{Tag: "len", Param: "4"},
		{Tag: "alphanum"},
		{Tag: "alpha"},
		{Tag: "numeric"},
		{Tag: "startswith", Param: "A"},
		{Tag: "endswith", Param: "Z"},
		{Tag: "contains", Param: "mid"},
	}}
	constraints := zodConstraints(f, "z.string()")
	for _, want := range []string{".min(2)", ".max(8)", ".length(4)", `.regex(/^[a-zA-Z0-9]*$/)`, `.regex(/^[a-zA-Z]*$/)`, `.regex(/^[0-9]*$/)`, `.startsWith("A")`, `.endsWith("Z")`, `.includes("mid")`} {
		if !strings.Contains(constraints, want) {
			t.Errorf("zodConstraints missing %q in %q", want, constraints)
		}
	}

	mini := zodMini("z.string()", constraints, true, `z.literal("")`)
	for _, want := range []string{"z.optional(", "z.string().check(", "z.minLength(2)", "z.maxLength(8)", "z.length(4)", "z.regex(/^[a-zA-Z0-9]*$/)", "z.startsWith(\"A\")", `.or(z.literal(""))`} {
		if !strings.Contains(mini, want) {
			t.Errorf("zodMini missing %q in %q", want, mini)
		}
	}

	numberConstraints := zodConstraints(Field{Validate: []ValidateRule{{Tag: "min", Param: "1"}, {Tag: "max", Param: "9"}, {Tag: "gt", Param: "0"}, {Tag: "gte", Param: "1"}, {Tag: "lt", Param: "10"}, {Tag: "lte", Param: "9"}}}, "z.int()")
	if numberConstraints != ".gte(1).lte(9).gt(0).gte(1).lt(10).lte(9)" {
		t.Errorf("number constraints = %q", numberConstraints)
	}
	if got := zodMini("z.int()", numberConstraints, false, ""); !strings.Contains(got, "z.gte(1)") || !strings.Contains(got, "z.lt(10)") {
		t.Errorf("number zodMini = %q", got)
	}
}

func TestZodTypeString(t *testing.T) {
	tests := []struct {
		name  string
		field Field
		want  string
	}{
		{
			name: "email validate produces z.email()",
			field: Field{
				Name:     "email",
				Type:     "string",
				GoKind:   "string",
				Validate: []ValidateRule{{Tag: "email"}},
			},
			want: "z.email()",
		},
		{
			name: "min and max on string",
			field: Field{
				Name:   "name",
				Type:   "string",
				GoKind: "string",
				Validate: []ValidateRule{
					{Tag: "min", Param: "3"},
					{Tag: "max", Param: "50"},
				},
			},
			want: "z.string().min(3).max(50)",
		},
		{
			name: "uuid validate produces z.uuidv4()",
			field: Field{
				Name:     "id",
				Type:     "string",
				GoKind:   "string",
				Validate: []ValidateRule{{Tag: "uuid"}},
			},
			want: "z.uuidv4()",
		},
		{
			name: "optional string field",
			field: Field{
				Name:     "nickname",
				Type:     "string",
				GoKind:   "string",
				Optional: true,
			},
			want: "z.string().optional()",
		},
		// --- new format tags ---
		{
			name:  "hostname format",
			field: Field{Name: "host", Type: "string", GoKind: "string", Validate: []ValidateRule{{Tag: "hostname"}}},
			want:  "z.hostname()",
		},
		{
			name:  "hostname_rfc1123 maps to same z.hostname()",
			field: Field{Name: "host", Type: "string", GoKind: "string", Validate: []ValidateRule{{Tag: "hostname_rfc1123"}}},
			want:  "z.hostname()",
		},
		{
			name:  "base64url format",
			field: Field{Name: "tok", Type: "string", GoKind: "string", Validate: []ValidateRule{{Tag: "base64url"}}},
			want:  "z.base64url()",
		},
		{
			name:  "hexadecimal format",
			field: Field{Name: "hex", Type: "string", GoKind: "string", Validate: []ValidateRule{{Tag: "hexadecimal"}}},
			want:  "z.hex()",
		},
		{
			name:  "ulid format",
			field: Field{Name: "id", Type: "string", GoKind: "string", Validate: []ValidateRule{{Tag: "ulid"}}},
			want:  "z.ulid()",
		},
		{
			name:  "mac format",
			field: Field{Name: "addr", Type: "string", GoKind: "string", Validate: []ValidateRule{{Tag: "mac"}}},
			want:  "z.mac()",
		},
		{
			name:  "cidrv4 format",
			field: Field{Name: "subnet", Type: "string", GoKind: "string", Validate: []ValidateRule{{Tag: "cidrv4"}}},
			want:  "z.cidrv4()",
		},
		{
			name:  "cidrv6 format",
			field: Field{Name: "subnet6", Type: "string", GoKind: "string", Validate: []ValidateRule{{Tag: "cidrv6"}}},
			want:  "z.cidrv6()",
		},
		{
			name:  "uppercase format",
			field: Field{Name: "code", Type: "string", GoKind: "string", Validate: []ValidateRule{{Tag: "uppercase"}}},
			want:  "z.uppercase()",
		},
		// --- format + constraint combo (isStringBase interaction) ---
		{
			name: "hostname + min uses string .min() not numeric .gte()",
			field: Field{Name: "host", Type: "string", GoKind: "string", Validate: []ValidateRule{
				{Tag: "hostname"},
				{Tag: "min", Param: "5"},
			}},
			want: "z.hostname().min(5)",
		},
		{
			name: "ulid + max uses string .max()",
			field: Field{Name: "id", Type: "string", GoKind: "string", Validate: []ValidateRule{
				{Tag: "ulid"},
				{Tag: "max", Param: "26"},
			}},
			want: "z.ulid().max(26)",
		},
		// --- format + omitempty (zero-value .or() wrapping) ---
		{
			name: "hostname + omitempty allows empty string",
			field: Field{
				Name: "host", Type: "string", GoKind: "string",
				ValidateOmitempty: true,
				Validate:          []ValidateRule{{Tag: "omitempty"}, {Tag: "hostname"}},
			},
			want: `z.hostname().or(z.literal(""))`,
		},
		{
			name: "mac + omitempty allows empty string",
			field: Field{
				Name: "addr", Type: "string", GoKind: "string",
				ValidateOmitempty: true,
				Validate:          []ValidateRule{{Tag: "omitempty"}, {Tag: "mac"}},
			},
			want: `z.mac().or(z.literal(""))`,
		},
		// --- format + optional ---
		{
			name: "cidrv4 + optional",
			field: Field{
				Name: "subnet", Type: "string", GoKind: "string",
				Optional: true,
				Validate: []ValidateRule{{Tag: "cidrv4"}},
			},
			want: "z.cidrv4().optional()",
		},
		// --- new constraint tags ---
		{
			name: "startswith constraint",
			field: Field{Name: "url", Type: "string", GoKind: "string", Validate: []ValidateRule{
				{Tag: "startswith", Param: "https://"},
			}},
			want: `z.string().startsWith("https://")`,
		},
		{
			name: "endswith constraint",
			field: Field{Name: "file", Type: "string", GoKind: "string", Validate: []ValidateRule{
				{Tag: "endswith", Param: ".go"},
			}},
			want: `z.string().endsWith(".go")`,
		},
		{
			name: "contains constraint",
			field: Field{Name: "path", Type: "string", GoKind: "string", Validate: []ValidateRule{
				{Tag: "contains", Param: "/api/"},
			}},
			want: `z.string().includes("/api/")`,
		},
		// --- constraint combos ---
		{
			name: "startswith + min + max combined",
			field: Field{Name: "url", Type: "string", GoKind: "string", Validate: []ValidateRule{
				{Tag: "startswith", Param: "https://"},
				{Tag: "min", Param: "10"},
				{Tag: "max", Param: "200"},
			}},
			want: `z.string().startsWith("https://").min(10).max(200)`,
		},
		{
			name: "contains + endswith combined",
			field: Field{Name: "path", Type: "string", GoKind: "string", Validate: []ValidateRule{
				{Tag: "contains", Param: "api"},
				{Tag: "endswith", Param: ".json"},
			}},
			want: `z.string().includes("api").endsWith(".json")`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ZodType(tt.field, ZodStandard)
			if got != tt.want {
				t.Errorf("ZodType(%+v, ZodStandard) = %q, want %q", tt.field, got, tt.want)
			}
		})
	}
}

func TestZodTypeNumeric(t *testing.T) {
	tests := []struct {
		name  string
		field Field
		want  string
	}{
		{
			name: "int32 field",
			field: Field{
				Name:   "age",
				Type:   "number",
				GoKind: "int32",
			},
			want: "z.int32()",
		},
		{
			name: "float64 with min=0",
			field: Field{
				Name:   "price",
				Type:   "number",
				GoKind: "float64",
				Validate: []ValidateRule{
					{Tag: "min", Param: "0"},
				},
			},
			want: "z.float64().gte(0)",
		},
		{
			name: "int with gte and lte",
			field: Field{
				Name:   "score",
				Type:   "number",
				GoKind: "int",
				Validate: []ValidateRule{
					{Tag: "gte", Param: "1"},
					{Tag: "lte", Param: "100"},
				},
			},
			want: "z.int().gte(1).lte(100)",
		},
		{
			name:  "int64 maps to z.number() not z.int64()",
			field: Field{Name: "big", Type: "number", GoKind: "int64"},
			want:  "z.number()",
		},
		{
			name:  "uint64 maps to z.number() not z.uint64()",
			field: Field{Name: "ubig", Type: "number", GoKind: "uint64"},
			want:  "z.number()",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ZodType(tt.field, ZodStandard)
			if got != tt.want {
				t.Errorf("ZodType(%+v, ZodStandard) = %q, want %q", tt.field, got, tt.want)
			}
		})
	}
}

func TestZodOneofEnum(t *testing.T) {
	tests := []struct {
		name  string
		field Field
		want  string
	}{
		{
			name: "string oneof → z.enum",
			field: Field{
				Name:   "role",
				Type:   "string",
				GoKind: "string",
				Validate: []ValidateRule{
					{Tag: "oneof", Param: "admin editor viewer"},
				},
			},
			want: `z.enum(["admin", "editor", "viewer"])`,
		},
		{
			name: "string oneof with quoted spaces → z.enum",
			field: Field{
				Name:   "role",
				Type:   "string",
				GoKind: "string",
				Validate: []ValidateRule{
					{Tag: "oneof", Param: "'red green' blue"},
				},
			},
			want: `z.enum(["red green", "blue"])`,
		},
		{
			name: "int oneof → z.union of literals",
			field: Field{
				Name:   "status",
				Type:   "number",
				GoKind: "int",
				Validate: []ValidateRule{
					{Tag: "oneof", Param: "1 2 3"},
				},
			},
			want: "z.union([z.literal(1), z.literal(2), z.literal(3)])",
		},
		{
			name: "int32 oneof → z.union of literals",
			field: Field{
				Name:   "code",
				Type:   "number",
				GoKind: "int32",
				Validate: []ValidateRule{
					{Tag: "oneof", Param: "100 200 404"},
				},
			},
			want: "z.union([z.literal(100), z.literal(200), z.literal(404)])",
		},
		{
			name: "uint oneof → z.union of literals",
			field: Field{
				Name:   "priority",
				Type:   "number",
				GoKind: "uint",
				Validate: []ValidateRule{
					{Tag: "oneof", Param: "0 1 2"},
				},
			},
			want: "z.union([z.literal(0), z.literal(1), z.literal(2)])",
		},
		{
			name: "float64 oneof is not emitted because validator oneof does not support floats",
			field: Field{
				Name:   "ratio",
				Type:   "number",
				GoKind: "float64",
				Validate: []ValidateRule{
					{Tag: "oneof", Param: "0.5 1.0 2.0"},
				},
			},
			want: "z.float64()",
		},
		{
			name: "int oneof rejects non-canonical values that server will not match",
			field: Field{
				Name:   "status",
				Type:   "number",
				GoKind: "int",
				Validate: []ValidateRule{
					{Tag: "oneof", Param: "01 2"},
				},
			},
			want: "z.int()",
		},
		{
			name: "string oneof optional",
			field: Field{
				Name:     "level",
				Type:     "string",
				GoKind:   "string",
				Optional: true,
				Validate: []ValidateRule{
					{Tag: "oneof", Param: "low high"},
				},
			},
			want: `z.enum(["low", "high"]).optional()`,
		},
		{
			name: "int oneof optional",
			field: Field{
				Name:     "tier",
				Type:     "number",
				GoKind:   "int",
				Optional: true,
				Validate: []ValidateRule{
					{Tag: "oneof", Param: "1 2 3"},
				},
			},
			want: "z.union([z.literal(1), z.literal(2), z.literal(3)]).optional()",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ZodType(tt.field, ZodStandard)
			if got != tt.want {
				t.Errorf("ZodType = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestZodOneofEnumMini(t *testing.T) {
	tests := []struct {
		name  string
		field Field
		want  string
	}{
		{
			name: "string oneof mini",
			field: Field{
				Name:   "role",
				Type:   "string",
				GoKind: "string",
				Validate: []ValidateRule{
					{Tag: "oneof", Param: "admin editor"},
				},
			},
			want: `z.enum(["admin", "editor"])`,
		},
		{
			name: "int oneof mini",
			field: Field{
				Name:   "status",
				Type:   "number",
				GoKind: "int",
				Validate: []ValidateRule{
					{Tag: "oneof", Param: "1 2 3"},
				},
			},
			want: "z.union([z.literal(1), z.literal(2), z.literal(3)])",
		},
		{
			name: "int oneof optional mini",
			field: Field{
				Name:     "tier",
				Type:     "number",
				GoKind:   "int",
				Optional: true,
				Validate: []ValidateRule{
					{Tag: "oneof", Param: "1 2 3"},
				},
			},
			want: "z.optional(z.union([z.literal(1), z.literal(2), z.literal(3)]))",
		},
		{
			name: "string oneof optional mini",
			field: Field{
				Name:     "level",
				Type:     "string",
				GoKind:   "string",
				Optional: true,
				Validate: []ValidateRule{
					{Tag: "oneof", Param: "low high"},
				},
			},
			want: `z.optional(z.enum(["low", "high"]))`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ZodType(tt.field, ZodMini)
			if got != tt.want {
				t.Errorf("ZodType(ZodMini) = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestZodRequiredStringEmitsMin1(t *testing.T) {
	f := Field{
		Name:   "name",
		Type:   "string",
		GoKind: "string",
		Validate: []ValidateRule{
			{Tag: "required"},
			{Tag: "max", Param: "100"},
		},
	}

	got := ZodType(f, ZodStandard)
	if got != "z.string().min(1).max(100)" {
		t.Errorf("ZodType = %q, want %q", got, "z.string().min(1).max(100)")
	}

	gotMini := ZodType(f, ZodMini)
	if gotMini != "z.string().check(z.minLength(1), z.maxLength(100))" {
		t.Errorf("ZodType mini = %q", gotMini)
	}
}

func TestZodRequiredPointerStringDoesNotEmitMin1(t *testing.T) {
	f := Field{
		Name:      "name",
		Type:      "string",
		GoKind:    "string",
		IsPointer: true,
		Validate: []ValidateRule{
			{Tag: "required"},
			{Tag: "max", Param: "100"},
		},
	}

	got := ZodType(f, ZodStandard)
	if got != "z.string().max(100)" {
		t.Errorf("ZodType = %q, want %q", got, "z.string().max(100)")
	}

	gotMini := ZodType(f, ZodMini)
	if gotMini != "z.string().check(z.maxLength(100))" {
		t.Errorf("ZodType mini = %q", gotMini)
	}
}

func TestZodRequiredStringDoesNotWeakenExistingMin(t *testing.T) {
	f := Field{
		Name:   "password",
		Type:   "string",
		GoKind: "string",
		Validate: []ValidateRule{
			{Tag: "required"},
			{Tag: "min", Param: "8"},
			{Tag: "max", Param: "128"},
		},
	}

	got := ZodType(f, ZodStandard)
	if got != "z.string().min(8).max(128)" {
		t.Errorf("ZodType = %q, want %q", got, "z.string().min(8).max(128)")
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

func TestZodNumericParamsFollowValidatorParsing(t *testing.T) {
	tests := []struct {
		name  string
		field Field
		want  string
	}{
		{
			name: "string length accepts base zero integer and normalizes",
			field: Field{Type: "string", GoKind: "string", Validate: []ValidateRule{
				{Tag: "min", Param: "0x10"},
			}},
			want: "z.string().min(16)",
		},
		{
			name: "int accepts base zero integer and normalizes",
			field: Field{Type: "number", GoKind: "int", Validate: []ValidateRule{
				{Tag: "min", Param: "0x10"},
			}},
			want: "z.int().gte(16)",
		},
		{
			name: "int rejects float exponent param",
			field: Field{Type: "number", GoKind: "int", Validate: []ValidateRule{
				{Tag: "min", Param: "1e3"},
			}},
			want: "z.int()",
		},
		{
			name: "uint rejects negative param",
			field: Field{Type: "number", GoKind: "uint", Validate: []ValidateRule{
				{Tag: "min", Param: "-1"},
			}},
			want: "z.number()",
		},
		{
			name: "float accepts exponent param",
			field: Field{Type: "number", GoKind: "float64", Validate: []ValidateRule{
				{Tag: "min", Param: "1e3"},
			}},
			want: "z.float64().gte(1000)",
		},
		{
			name: "int len emits equality range",
			field: Field{Type: "number", GoKind: "int", Validate: []ValidateRule{
				{Tag: "len", Param: "0x10"},
			}},
			want: "z.int().gte(16).lte(16)",
		},
		{
			name: "float len emits equality range",
			field: Field{Type: "number", GoKind: "float64", Validate: []ValidateRule{
				{Tag: "len", Param: "1.5"},
			}},
			want: "z.float64().gte(1.5).lte(1.5)",
		},
		{
			name: "bool numeric constraint is skipped",
			field: Field{Type: "boolean", GoKind: "bool", Validate: []ValidateRule{
				{Tag: "min", Param: "1"},
			}},
			want: "z.boolean()",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ZodType(tt.field, ZodStandard)
			if got != tt.want {
				t.Fatalf("ZodType = %q, want %q", got, tt.want)
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

func TestZodTypeOmitempty(t *testing.T) {
	tests := []struct {
		name  string
		field Field
		style ZodStyle
		want  string
	}{
		{
			name: "string omitempty+len standard — allows empty string",
			field: Field{
				Name:              "code",
				Type:              "string",
				GoKind:            "string",
				ValidateOmitempty: true,
				Validate:          []ValidateRule{{Tag: "omitempty"}, {Tag: "len", Param: "6"}},
			},
			style: ZodStandard,
			want:  `z.string().length(6).or(z.literal(""))`,
		},
		{
			name: "string omitempty+len mini — allows empty string",
			field: Field{
				Name:              "code",
				Type:              "string",
				GoKind:            "string",
				ValidateOmitempty: true,
				Validate:          []ValidateRule{{Tag: "omitempty"}, {Tag: "len", Param: "6"}},
			},
			style: ZodMini,
			want:  `z.string().check(z.length(6)).or(z.literal(""))`,
		},
		{
			name: "string omitempty+email format — allows empty string",
			field: Field{
				Name:              "email",
				Type:              "string",
				GoKind:            "string",
				ValidateOmitempty: true,
				Validate:          []ValidateRule{{Tag: "omitempty"}, {Tag: "email"}},
			},
			style: ZodStandard,
			want:  `z.email().or(z.literal(""))`,
		},
		{
			name: "string omitempty only — no wrapping needed",
			field: Field{
				Name:              "nickname",
				Type:              "string",
				GoKind:            "string",
				ValidateOmitempty: true,
				Validate:          []ValidateRule{{Tag: "omitempty"}},
			},
			style: ZodStandard,
			want:  "z.string()",
		},
		{
			name: "int omitempty+gt — allows zero",
			field: Field{
				Name:              "priority",
				Type:              "number",
				GoKind:            "int",
				ValidateOmitempty: true,
				Validate:          []ValidateRule{{Tag: "omitempty"}, {Tag: "gt", Param: "0"}},
			},
			style: ZodStandard,
			want:  "z.int().gt(0).or(z.literal(0))",
		},
		{
			name: "int omitempty+gte mini — allows zero",
			field: Field{
				Name:              "score",
				Type:              "number",
				GoKind:            "int",
				ValidateOmitempty: true,
				Validate:          []ValidateRule{{Tag: "omitempty"}, {Tag: "gte", Param: "1"}},
			},
			style: ZodMini,
			want:  "z.int().check(z.gte(1)).or(z.literal(0))",
		},
		{
			name: "omitempty+optional standard — both .or() and .optional()",
			field: Field{
				Name:              "code",
				Type:              "string",
				GoKind:            "string",
				Optional:          true,
				ValidateOmitempty: true,
				Validate:          []ValidateRule{{Tag: "omitempty"}, {Tag: "len", Param: "6"}},
			},
			style: ZodStandard,
			want:  `z.string().length(6).or(z.literal("")).optional()`,
		},
		{
			name: "omitempty+optional mini — .or() inside z.optional()",
			field: Field{
				Name:              "code",
				Type:              "string",
				GoKind:            "string",
				Optional:          true,
				ValidateOmitempty: true,
				Validate:          []ValidateRule{{Tag: "omitempty"}, {Tag: "len", Param: "6"}},
			},
			style: ZodMini,
			want:  `z.optional(z.string().check(z.length(6)).or(z.literal("")))`,
		},
		{
			name: "omitempty+email mini — allows empty string",
			field: Field{
				Name:              "email",
				Type:              "string",
				GoKind:            "string",
				ValidateOmitempty: true,
				Validate:          []ValidateRule{{Tag: "omitempty"}, {Tag: "email"}},
			},
			style: ZodMini,
			want:  `z.email().or(z.literal(""))`,
		},
		{
			name: "string omitempty+min+max standard",
			field: Field{
				Name:              "name",
				Type:              "string",
				GoKind:            "string",
				ValidateOmitempty: true,
				Validate: []ValidateRule{
					{Tag: "omitempty"},
					{Tag: "min", Param: "3"},
					{Tag: "max", Param: "50"},
				},
			},
			style: ZodStandard,
			want:  `z.string().min(3).max(50).or(z.literal(""))`,
		},
		{
			name: "string omitempty+uuid format — allows empty",
			field: Field{
				Name:              "ref_id",
				Type:              "string",
				GoKind:            "string",
				ValidateOmitempty: true,
				Validate:          []ValidateRule{{Tag: "omitempty"}, {Tag: "uuid"}},
			},
			style: ZodStandard,
			want:  `z.uuidv4().or(z.literal(""))`,
		},
		{
			name: "no omitempty — unchanged",
			field: Field{
				Name:   "code",
				Type:   "string",
				GoKind: "string",
				Validate: []ValidateRule{
					{Tag: "required"},
					{Tag: "len", Param: "6"},
				},
			},
			style: ZodStandard,
			want:  "z.string().length(6)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ZodType(tt.field, tt.style)
			if got != tt.want {
				t.Errorf("ZodType() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestZodMiniStyle(t *testing.T) {
	tests := []struct {
		name  string
		field Field
		want  string
	}{
		{
			name: "string with min and max in mini style",
			field: Field{
				Name:   "title",
				Type:   "string",
				GoKind: "string",
				Validate: []ValidateRule{
					{Tag: "min", Param: "5"},
					{Tag: "max", Param: "100"},
				},
			},
			want: "z.string().check(z.minLength(5), z.maxLength(100))",
		},
		{
			name: "optional string in mini style",
			field: Field{
				Name:     "bio",
				Type:     "string",
				GoKind:   "string",
				Optional: true,
			},
			want: "z.optional(z.string())",
		},
		{
			name: "startsWith in mini style",
			field: Field{
				Name:     "url",
				Type:     "string",
				GoKind:   "string",
				Validate: []ValidateRule{{Tag: "startswith", Param: "https://"}},
			},
			want: `z.string().check(z.startsWith("https://"))`,
		},
		{
			name: "endsWith in mini style",
			field: Field{
				Name:     "file",
				Type:     "string",
				GoKind:   "string",
				Validate: []ValidateRule{{Tag: "endswith", Param: ".ts"}},
			},
			want: `z.string().check(z.endsWith(".ts"))`,
		},
		{
			name: "includes in mini style",
			field: Field{
				Name:     "path",
				Type:     "string",
				GoKind:   "string",
				Validate: []ValidateRule{{Tag: "contains", Param: "/api/"}},
			},
			want: `z.string().check(z.includes("/api/"))`,
		},
		{
			name: "startsWith + min combined in mini style",
			field: Field{
				Name:   "url",
				Type:   "string",
				GoKind: "string",
				Validate: []ValidateRule{
					{Tag: "startswith", Param: "https://"},
					{Tag: "min", Param: "10"},
				},
			},
			want: `z.string().check(z.startsWith("https://"), z.minLength(10))`,
		},
		{
			name: "hostname + min in mini uses minLength",
			field: Field{
				Name:   "host",
				Type:   "string",
				GoKind: "string",
				Validate: []ValidateRule{
					{Tag: "hostname"},
					{Tag: "min", Param: "4"},
				},
			},
			want: "z.hostname().check(z.minLength(4))",
		},
		{
			name: "hostname + optional in mini",
			field: Field{
				Name:     "host",
				Type:     "string",
				GoKind:   "string",
				Optional: true,
				Validate: []ValidateRule{{Tag: "hostname"}},
			},
			want: "z.optional(z.hostname())",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ZodType(tt.field, ZodMini)
			if got != tt.want {
				t.Errorf("ZodType(%+v, ZodMini) = %q, want %q", tt.field, got, tt.want)
			}
		})
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

// go-playground/validator interprets gt/gte/lt/lte on a string as bounds on its
// length, not numeric comparisons. ZodString exposes only inclusive .min()/.max()
// (and z.minLength/z.maxLength in mini) — it has no .gt/.gte/.lt/.lte — so these
// tags must be translated to length checks, with a ±1 offset for the strict forms.
// Emitting z.string().gte(n) produces a runtime "x.gte is not a function" in the
// generated client.
func TestZodStringLengthComparisonTagsEmitValidZod(t *testing.T) {
	tests := []struct {
		name     string
		tag      string
		param    string
		want     string
		wantMini string
	}{
		{"gte is inclusive min length", "gte", "3", "z.string().min(3)", "z.string().check(z.minLength(3))"},
		{"gt is exclusive min length", "gt", "3", "z.string().min(4)", "z.string().check(z.minLength(4))"},
		{"lte is inclusive max length", "lte", "3", "z.string().max(3)", "z.string().check(z.maxLength(3))"},
		{"lt is exclusive max length", "lt", "3", "z.string().max(2)", "z.string().check(z.maxLength(2))"},
		{"gte respects base-zero parsing", "gte", "0x10", "z.string().min(16)", "z.string().check(z.minLength(16))"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := Field{Name: "code", Type: "string", GoKind: "string",
				Validate: []ValidateRule{{Tag: tt.tag, Param: tt.param}}}

			if got := ZodType(f, ZodStandard); got != tt.want {
				t.Errorf("ZodType standard = %q, want %q", got, tt.want)
			}
			if got := ZodType(f, ZodMini); got != tt.wantMini {
				t.Errorf("ZodType mini = %q, want %q", got, tt.wantMini)
			}

			// Regression guard: numeric comparators must never appear on a string schema.
			for _, style := range []ZodStyle{ZodStandard, ZodMini} {
				out := ZodType(f, style)
				for _, bad := range []string{".gte(", ".gt(", ".lte(", ".lt(", "z.gte(", "z.gt(", "z.lte(", "z.lt("} {
					if strings.Contains(out, bad) {
						t.Errorf("emitted numeric comparator %q on a string schema: %q", bad, out)
					}
				}
			}
		})
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
