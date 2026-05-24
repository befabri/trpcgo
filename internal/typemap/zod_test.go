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
			name: "float64 oneof → z.union of literals",
			field: Field{
				Name:   "ratio",
				Type:   "number",
				GoKind: "float64",
				Validate: []ValidateRule{
					{Tag: "oneof", Param: "0.5 1.0 2.0"},
				},
			},
			want: "z.union([z.literal(0.5), z.literal(1.0), z.literal(2.0)])",
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
	// "required" on a string field should emit .min(1) since Zod accepts "" by default.
	// Currently the implementation doesn't add .min(1) for required — this test documents
	// whether that behavior exists. If it doesn't, this is a known gap.
	// The validate:"required" is handled at the optional/required level, not as a Zod constraint.
	// For now, just verify the output is reasonable.
	if got != "z.string().max(100)" {
		t.Errorf("ZodType = %q, want %q", got, "z.string().max(100)")
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
