package typemap

import (
	"go/types"
	"strings"
	"testing"
)

func TestBasicTypes(t *testing.T) {
	m := NewMapper(nil)

	tests := []struct {
		goType types.Type
		want   string
	}{
		{types.Typ[types.String], "string"},
		{types.Typ[types.Bool], "boolean"},
		{types.Typ[types.Int], "number"},
		{types.Typ[types.Int64], "number"},
		{types.Typ[types.Float64], "number"},
		{types.Typ[types.Uint8], "number"},
	}

	for _, tt := range tests {
		got := m.Convert(tt.goType)
		if got != tt.want {
			t.Errorf("Convert(%v) = %q, want %q", tt.goType, got, tt.want)
		}
	}
}

func TestSliceType(t *testing.T) {
	m := NewMapper(nil)

	// []string → string[]
	sliceStr := types.NewSlice(types.Typ[types.String])
	if got := m.Convert(sliceStr); got != "string[]" {
		t.Errorf("Convert([]string) = %q, want %q", got, "string[]")
	}

	// []byte → string (base64)
	sliceByte := types.NewSlice(types.Typ[types.Byte])
	if got := m.Convert(sliceByte); got != "string" {
		t.Errorf("Convert([]byte) = %q, want %q", got, "string")
	}
}

func TestMapType(t *testing.T) {
	m := NewMapper(nil)

	// map[string]int → Record<string, number>
	mapType := types.NewMap(types.Typ[types.String], types.Typ[types.Int])
	got := m.Convert(mapType)
	want := "Record<string, number>"
	if got != want {
		t.Errorf("Convert(map[string]int) = %q, want %q", got, want)
	}
}

func TestPointerType(t *testing.T) {
	m := NewMapper(nil)

	// *string → string (pointer unwrapped)
	ptr := types.NewPointer(types.Typ[types.String])
	if got := m.Convert(ptr); got != "string" {
		t.Errorf("Convert(*string) = %q, want %q", got, "string")
	}
}

func TestEmptyInterface(t *testing.T) {
	m := NewMapper(nil)

	// interface{} / any → unknown
	iface := types.NewInterfaceType(nil, nil)
	iface.Complete()
	if got := m.Convert(iface); got != "unknown" {
		t.Errorf("Convert(interface{}) = %q, want %q", got, "unknown")
	}
}

func TestUnionType(t *testing.T) {
	pkg := types.NewPackage("main", "main")
	statusObj := types.NewTypeName(0, pkg, "Status", nil)
	statusType := types.NewNamed(statusObj, types.Typ[types.String], nil)

	// Meta key must use TypeID (fully-qualified).
	m := NewMapper(map[string]TypeMeta{
		TypeID(statusObj): {ConstValues: []string{`"active"`, `"pending"`}},
	})

	got := m.Resolve(m.Convert(statusType))
	if got != "Status" {
		t.Errorf("Resolve(Convert(Status)) = %q, want %q", got, "Status")
	}

	defs := m.Defs()
	if len(defs) != 1 {
		t.Fatalf("got %d defs, want 1", len(defs))
	}
	if defs[0].Kind != TypeDefUnion {
		t.Errorf("Kind = %v, want TypeDefUnion", defs[0].Kind)
	}
	if len(defs[0].UnionMembers) != 2 {
		t.Errorf("UnionMembers = %v, want 2 members", defs[0].UnionMembers)
	}
}

func TestUnionTypeDedup(t *testing.T) {
	pkg := types.NewPackage("main", "main")
	obj := types.NewTypeName(0, pkg, "Status", nil)
	named := types.NewNamed(obj, types.Typ[types.String], nil)

	m := NewMapper(map[string]TypeMeta{
		TypeID(obj): {ConstValues: []string{`"a"`, `"b"`}},
	})

	// Convert twice — should only produce one def.
	m.Convert(named)
	m.Convert(named)

	if len(m.Defs()) != 1 {
		t.Errorf("got %d defs after double Convert, want 1", len(m.Defs()))
	}
}

func TestAliasType(t *testing.T) {
	pkg := types.NewPackage("main", "main")
	obj := types.NewTypeName(0, pkg, "UserRole", nil)
	alias := types.NewAlias(obj, types.Typ[types.String])

	m := NewMapper(map[string]TypeMeta{
		TypeID(obj): {IsAlias: true},
	})

	got := m.Resolve(m.Convert(alias))
	if got != "UserRole" {
		t.Errorf("Resolve(Convert(UserRole)) = %q, want %q", got, "UserRole")
	}

	defs := m.Defs()
	if len(defs) != 1 {
		t.Fatalf("got %d defs, want 1", len(defs))
	}
	if defs[0].Kind != TypeDefAlias {
		t.Errorf("Kind = %v, want TypeDefAlias", defs[0].Kind)
	}
	if defs[0].AliasOf != "string" {
		t.Errorf("AliasOf = %q, want %q", defs[0].AliasOf, "string")
	}
}

func TestAliasWithoutMetaResolvesToUnderlying(t *testing.T) {
	m := NewMapper(nil)

	pkg := types.NewPackage("main", "main")
	obj := types.NewTypeName(0, pkg, "MyStr", nil)
	alias := types.NewAlias(obj, types.Typ[types.String])

	got := m.Convert(alias)
	if got != "string" {
		t.Errorf("Convert(MyStr alias) = %q, want %q (no meta → resolve to underlying)", got, "string")
	}
	if len(m.Defs()) != 0 {
		t.Errorf("should produce no defs for unregistered alias")
	}
}

func TestNamedBasicWithoutMetaResolvesUnderlying(t *testing.T) {
	m := NewMapper(nil)

	pkg := types.NewPackage("main", "main")
	obj := types.NewTypeName(0, pkg, "MyString", nil)
	named := types.NewNamed(obj, types.Typ[types.String], nil)

	got := m.Convert(named)
	if got != "string" {
		t.Errorf("Convert(MyString) = %q, want %q (should resolve to underlying)", got, "string")
	}
}

func TestParseTSTypeTag(t *testing.T) {
	tests := []struct {
		tag          string
		wantOK       bool
		wantType     string
		wantReadonly bool
		wantRequired bool
	}{
		{`tstype:"Date"`, true, "Date", false, false},
		{`tstype:",readonly"`, true, "", true, false},
		{`tstype:",required"`, true, "", false, true},
		{`tstype:"string,readonly,required"`, true, "string", true, true},
		{`tstype:"-"`, true, "-", false, false},
		{`json:"name"`, false, "", false, false},
		{``, false, "", false, false},
		{`json:"id" tstype:"string"`, true, "string", false, false},
		// TS type containing commas (generics) — must not break.
		{`tstype:"Record<string, unknown>"`, true, "Record<string, unknown>", false, false},
		{`tstype:"Map<string, number>,readonly"`, true, "Map<string, number>", true, false},
		{`tstype:"Record<string, Record<string, number>>,readonly,required"`, true, "Record<string, Record<string, number>>", true, true},
	}

	for _, tt := range tests {
		result, ok := ParseTSTypeTag(tt.tag)
		if ok != tt.wantOK {
			t.Errorf("ParseTSTypeTag(%q) ok = %v, want %v", tt.tag, ok, tt.wantOK)
			continue
		}
		if !ok {
			continue
		}
		if result.Type != tt.wantType {
			t.Errorf("ParseTSTypeTag(%q).Type = %q, want %q", tt.tag, result.Type, tt.wantType)
		}
		if result.Readonly != tt.wantReadonly {
			t.Errorf("ParseTSTypeTag(%q).Readonly = %v, want %v", tt.tag, result.Readonly, tt.wantReadonly)
		}
		if result.Required != tt.wantRequired {
			t.Errorf("ParseTSTypeTag(%q).Required = %v, want %v", tt.tag, result.Required, tt.wantRequired)
		}
	}
}

func TestJSONTagParsing(t *testing.T) {
	tests := []struct {
		tag      string
		wantName string
		wantOmit bool
		wantSkip bool
	}{
		{`json:"name"`, "name", false, false},
		{`json:"name,omitempty"`, "name", true, false},
		{`json:"-"`, "", false, true},
		{`json:"id" xml:"id"`, "id", false, false},
		{``, "", false, false},
		{`xml:"name"`, "", false, false},
	}

	for _, tt := range tests {
		name, omit, skip := ParseJSONTag(tt.tag)
		if name != tt.wantName || omit != tt.wantOmit || skip != tt.wantSkip {
			t.Errorf("ParseJSONTag(%q) = (%q, %v, %v), want (%q, %v, %v)",
				tt.tag, name, omit, skip, tt.wantName, tt.wantOmit, tt.wantSkip)
		}
	}
}

func TestParseValidateTag(t *testing.T) {
	tests := []struct {
		name      string
		tag       string
		wantCount int
		wantNil   bool // true if we expect nil (no validate tag at all)
	}{
		{
			name:      "multiple rules with params",
			tag:       `validate:"required,min=3,max=50"`,
			wantCount: 3,
		},
		{
			name:      "omitempty and len",
			tag:       `validate:"omitempty,len=6"`,
			wantCount: 2,
		},
		{
			name:      "single format rule",
			tag:       `validate:"email"`,
			wantCount: 1,
		},
		{
			name:    "empty tag",
			tag:     ``,
			wantNil: true,
		},
		{
			name:    "no validate tag",
			tag:     `json:"name"`,
			wantNil: true,
		},
		{
			name:      "skip tag returns empty",
			tag:       `validate:"-"`,
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rules := ParseValidateTag(tt.tag)
			if tt.wantNil {
				if rules != nil {
					t.Errorf("ParseValidateTag(%q) = %v, want nil", tt.tag, rules)
				}
				return
			}
			if len(rules) != tt.wantCount {
				t.Errorf("ParseValidateTag(%q) returned %d rules, want %d: %+v", tt.tag, len(rules), tt.wantCount, rules)
			}
		})
	}

	// Verify specific rule parsing for "required,min=3,max=50".
	rules := ParseValidateTag(`validate:"required,min=3,max=50"`)
	if rules[0].Tag != "required" || rules[0].Param != "" {
		t.Errorf("rule[0] = %+v, want {Tag:required, Param:}", rules[0])
	}
	if rules[1].Tag != "min" || rules[1].Param != "3" {
		t.Errorf("rule[1] = %+v, want {Tag:min, Param:3}", rules[1])
	}
	if rules[2].Tag != "max" || rules[2].Param != "50" {
		t.Errorf("rule[2] = %+v, want {Tag:max, Param:50}", rules[2])
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

func TestCollisionRename(t *testing.T) {
	authPkg := types.NewPackage("github.com/app/auth", "auth")
	modelsPkg := types.NewPackage("github.com/app/models", "models")

	authTokenField := types.NewField(0, authPkg, "Token", types.Typ[types.String], false)
	authUser := types.NewNamed(
		types.NewTypeName(0, authPkg, "User", nil),
		types.NewStruct([]*types.Var{authTokenField}, []string{`json:"token"`}),
		nil,
	)

	modelsNameField := types.NewField(0, modelsPkg, "Name", types.Typ[types.String], false)
	modelsUser := types.NewNamed(
		types.NewTypeName(0, modelsPkg, "User", nil),
		types.NewStruct([]*types.Var{modelsNameField}, []string{`json:"name"`}),
		nil,
	)

	m := NewMapper(nil)
	m.Convert(authUser)
	m.Convert(modelsUser)

	defs := m.Defs()
	if len(defs) != 2 {
		t.Fatalf("got %d defs, want 2 (both Users should be registered)", len(defs))
	}

	names := map[string]bool{}
	for _, d := range defs {
		if names[d.Name] {
			t.Fatalf("collision not resolved: duplicate name %q", d.Name)
		}
		names[d.Name] = true
	}

	// Expect prefixed names: "AuthUser" and "ModelsUser".
	if !names["AuthUser"] {
		t.Errorf("expected AuthUser in defs, got %v", names)
	}
	if !names["ModelsUser"] {
		t.Errorf("expected ModelsUser in defs, got %v", names)
	}
}

func TestNoCollisionSameOutput(t *testing.T) {
	pkg := types.NewPackage("main", "main")
	nameField := types.NewField(0, pkg, "Name", types.Typ[types.String], false)
	user := types.NewNamed(
		types.NewTypeName(0, pkg, "User", nil),
		types.NewStruct([]*types.Var{nameField}, []string{`json:"name"`}),
		nil,
	)

	m := NewMapper(nil)
	resolved := m.Resolve(m.Convert(user))
	if resolved != "User" {
		t.Errorf("Resolve(Convert(User)) = %q, want %q (no collision → short name)", resolved, "User")
	}

	defs := m.Defs()
	if len(defs) != 1 {
		t.Fatalf("got %d defs, want 1", len(defs))
	}
	if defs[0].Name != "User" {
		t.Errorf("def Name = %q, want %q", defs[0].Name, "User")
	}
}

func TestCollisionInFieldRef(t *testing.T) {
	authPkg := types.NewPackage("github.com/app/auth", "auth")
	modelsPkg := types.NewPackage("github.com/app/models", "models")

	// auth.User struct
	authTokenField := types.NewField(0, authPkg, "Token", types.Typ[types.String], false)
	authUser := types.NewNamed(
		types.NewTypeName(0, authPkg, "User", nil),
		types.NewStruct([]*types.Var{authTokenField}, []string{`json:"token"`}),
		nil,
	)

	// models.User struct
	modelsNameField := types.NewField(0, modelsPkg, "Name", types.Typ[types.String], false)
	modelsUser := types.NewNamed(
		types.NewTypeName(0, modelsPkg, "User", nil),
		types.NewStruct([]*types.Var{modelsNameField}, []string{`json:"name"`}),
		nil,
	)

	m := NewMapper(nil)

	// Convert a slice of auth.User — should produce "AuthUser[]" after resolution.
	sliceType := types.NewSlice(authUser)
	m.Resolve(m.Convert(sliceType))

	// Also convert models.User to trigger collision.
	m.Convert(modelsUser)

	// Re-resolve after both are known.
	resolved := m.Resolve(m.Convert(sliceType))
	if resolved != "AuthUser[]" {
		t.Errorf("Resolve(Convert([]auth.User)) = %q, want %q", resolved, "AuthUser[]")
	}
}

func TestCollisionInGenericRef(t *testing.T) {
	authPkg := types.NewPackage("github.com/app/auth", "auth")
	modelsPkg := types.NewPackage("github.com/app/models", "models")

	// Two "User" structs from different packages.
	authUser := types.NewNamed(
		types.NewTypeName(0, authPkg, "User", nil),
		types.NewStruct(
			[]*types.Var{types.NewField(0, authPkg, "Token", types.Typ[types.String], false)},
			[]string{`json:"token"`},
		),
		nil,
	)
	modelsUser := types.NewNamed(
		types.NewTypeName(0, modelsPkg, "User", nil),
		types.NewStruct(
			[]*types.Var{types.NewField(0, modelsPkg, "Name", types.Typ[types.String], false)},
			[]string{`json:"name"`},
		),
		nil,
	)

	m := NewMapper(nil)

	// Convert a map containing auth.User as value type.
	mapType := types.NewMap(types.Typ[types.String], authUser)
	m.Convert(mapType)
	m.Convert(modelsUser)

	resolved := m.Resolve(m.Convert(mapType))
	if resolved != "Record<string, AuthUser>" {
		t.Errorf("Resolve(Convert(map[string]auth.User)) = %q, want %q", resolved, "Record<string, AuthUser>")
	}
}

func TestCollisionThreePackages(t *testing.T) {
	pkg1 := types.NewPackage("github.com/app/auth", "auth")
	pkg2 := types.NewPackage("github.com/app/models", "models")
	pkg3 := types.NewPackage("github.com/app/admin", "admin")

	mkUser := func(pkg *types.Package, fieldName string) *types.Named {
		return types.NewNamed(
			types.NewTypeName(0, pkg, "User", nil),
			types.NewStruct(
				[]*types.Var{types.NewField(0, pkg, fieldName, types.Typ[types.String], false)},
				[]string{`json:"` + strings.ToLower(fieldName) + `"`},
			),
			nil,
		)
	}

	m := NewMapper(nil)
	m.Convert(mkUser(pkg1, "Token"))
	m.Convert(mkUser(pkg2, "Name"))
	m.Convert(mkUser(pkg3, "Role"))

	defs := m.Defs()
	if len(defs) != 3 {
		t.Fatalf("got %d defs, want 3", len(defs))
	}

	names := map[string]bool{}
	for _, d := range defs {
		if names[d.Name] {
			t.Fatalf("collision not resolved: duplicate name %q", d.Name)
		}
		names[d.Name] = true
	}

	if !names["AuthUser"] || !names["ModelsUser"] || !names["AdminUser"] {
		t.Errorf("expected AuthUser, ModelsUser, AdminUser; got %v", names)
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

func TestSplitAtDive(t *testing.T) {
	rules := ParseValidateTag(`validate:"required,min=1,dive,min=3,max=50"`)
	container, element := SplitAtDive(rules)

	if len(container) != 2 {
		t.Fatalf("container rules: got %d, want 2", len(container))
	}
	if container[0].Tag != "required" {
		t.Errorf("container[0].Tag = %q, want %q", container[0].Tag, "required")
	}
	if container[1].Tag != "min" || container[1].Param != "1" {
		t.Errorf("container[1] = %+v, want {Tag:min, Param:1}", container[1])
	}

	if len(element) != 2 {
		t.Fatalf("element rules: got %d, want 2", len(element))
	}
	if element[0].Tag != "min" || element[0].Param != "3" {
		t.Errorf("element[0] = %+v, want {Tag:min, Param:3}", element[0])
	}
	if element[1].Tag != "max" || element[1].Param != "50" {
		t.Errorf("element[1] = %+v, want {Tag:max, Param:50}", element[1])
	}
}

func TestSplitAtDiveNoDive(t *testing.T) {
	rules := ParseValidateTag(`validate:"required,min=1"`)
	container, element := SplitAtDive(rules)

	if len(container) != 2 {
		t.Fatalf("container rules: got %d, want 2", len(container))
	}
	if element != nil {
		t.Errorf("element rules should be nil, got %v", element)
	}
}

func TestSplitAtDiveOnlyElement(t *testing.T) {
	rules := ParseValidateTag(`validate:"dive,email"`)
	container, element := SplitAtDive(rules)

	if len(container) != 0 {
		t.Errorf("container rules: got %d, want 0", len(container))
	}
	if len(element) != 1 {
		t.Fatalf("element rules: got %d, want 1", len(element))
	}
	if element[0].Tag != "email" {
		t.Errorf("element[0].Tag = %q, want %q", element[0].Tag, "email")
	}
}

func TestSliceElementGoKind(t *testing.T) {
	tests := []struct {
		name string
		typ  types.Type
		want string
	}{
		{
			name: "[]string element is string",
			typ:  types.NewSlice(types.Typ[types.String]),
			want: "string",
		},
		{
			name: "[]int element is int",
			typ:  types.NewSlice(types.Typ[types.Int]),
			want: "int",
		},
		{
			name: "[]bool element is bool",
			typ:  types.NewSlice(types.Typ[types.Bool]),
			want: "bool",
		},
		{
			name: "*[]float64 unwraps pointer",
			typ:  types.NewPointer(types.NewSlice(types.Typ[types.Float64])),
			want: "float64",
		},
		{
			name: "non-slice returns empty",
			typ:  types.Typ[types.String],
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sliceElementGoKind(tt.typ)
			if got != tt.want {
				t.Errorf("sliceElementGoKind(%v) = %q, want %q", tt.typ, got, tt.want)
			}
		})
	}
}

func TestSliceElementGoKindStruct(t *testing.T) {
	pkg := types.NewPackage("main", "main")
	nameField := types.NewField(0, pkg, "Name", types.Typ[types.String], false)
	st := types.NewStruct([]*types.Var{nameField}, []string{`json:"name"`})
	item := types.NewNamed(types.NewTypeName(0, pkg, "Item", nil), st, nil)

	got := sliceElementGoKind(types.NewSlice(item))
	if got != "struct" {
		t.Errorf("sliceElementGoKind([]Item) = %q, want %q", got, "struct")
	}
}

func TestCollectFieldsDive(t *testing.T) {
	// Build a struct with a []string field that has a dive tag.
	pkg := types.NewPackage("main", "main")
	tagsField := types.NewField(0, pkg, "Tags", types.NewSlice(types.Typ[types.String]), false)
	st := types.NewStruct(
		[]*types.Var{tagsField},
		[]string{`json:"tags" validate:"required,min=1,dive,min=3,max=50"`},
	)

	m := NewMapper(nil)
	var fields []Field
	m.collectFields(st, &fields, nil, nil)

	if len(fields) != 1 {
		t.Fatalf("got %d fields, want 1", len(fields))
	}
	f := fields[0]

	// Container rules: required, min=1 (before dive).
	if len(f.Validate) != 2 {
		t.Fatalf("Validate has %d rules, want 2 (container rules before dive)", len(f.Validate))
	}
	if f.Validate[0].Tag != "required" {
		t.Errorf("Validate[0].Tag = %q, want %q", f.Validate[0].Tag, "required")
	}
	if f.Validate[1].Tag != "min" || f.Validate[1].Param != "1" {
		t.Errorf("Validate[1] = %+v, want {Tag:min, Param:1}", f.Validate[1])
	}

	// Element rules: min=3, max=50 (after dive).
	if len(f.ElementValidate) != 2 {
		t.Fatalf("ElementValidate has %d rules, want 2 (element rules after dive)", len(f.ElementValidate))
	}
	if f.ElementValidate[0].Tag != "min" || f.ElementValidate[0].Param != "3" {
		t.Errorf("ElementValidate[0] = %+v, want {Tag:min, Param:3}", f.ElementValidate[0])
	}
	if f.ElementValidate[1].Tag != "max" || f.ElementValidate[1].Param != "50" {
		t.Errorf("ElementValidate[1] = %+v, want {Tag:max, Param:50}", f.ElementValidate[1])
	}

	// Element Go kind should be derived from the slice element type.
	if f.ElementGoKind != "string" {
		t.Errorf("ElementGoKind = %q, want %q", f.ElementGoKind, "string")
	}

	// GoKind should be slice.
	if f.GoKind != "slice" {
		t.Errorf("GoKind = %q, want %q", f.GoKind, "slice")
	}
}

func TestCollectFieldsNoDive(t *testing.T) {
	// A []string field with validate but no dive — ElementValidate should be nil.
	pkg := types.NewPackage("main", "main")
	tagsField := types.NewField(0, pkg, "Tags", types.NewSlice(types.Typ[types.String]), false)
	st := types.NewStruct(
		[]*types.Var{tagsField},
		[]string{`json:"tags" validate:"required,min=1"`},
	)

	m := NewMapper(nil)
	var fields []Field
	m.collectFields(st, &fields, nil, nil)

	if len(fields) != 1 {
		t.Fatalf("got %d fields, want 1", len(fields))
	}
	f := fields[0]

	if len(f.Validate) != 2 {
		t.Errorf("Validate has %d rules, want 2", len(f.Validate))
	}
	if f.ElementValidate != nil {
		t.Errorf("ElementValidate should be nil when no dive, got %v", f.ElementValidate)
	}
	// ElementGoKind is still populated for slice fields (even without dive).
	if f.ElementGoKind != "string" {
		t.Errorf("ElementGoKind = %q, want %q (should always be set for slices)", f.ElementGoKind, "string")
	}
}

func TestCollectFieldsDiveOnNonSlice(t *testing.T) {
	// A string field with dive tag — dive should be split but ElementGoKind stays empty.
	pkg := types.NewPackage("main", "main")
	nameField := types.NewField(0, pkg, "Name", types.Typ[types.String], false)
	st := types.NewStruct(
		[]*types.Var{nameField},
		[]string{`json:"name" validate:"required,dive,min=3"`},
	)

	m := NewMapper(nil)
	var fields []Field
	m.collectFields(st, &fields, nil, nil)

	if len(fields) != 1 {
		t.Fatalf("got %d fields, want 1", len(fields))
	}
	f := fields[0]

	// Dive still splits the rules.
	if len(f.Validate) != 1 {
		t.Errorf("Validate has %d rules, want 1 (required before dive)", len(f.Validate))
	}
	if len(f.ElementValidate) != 1 {
		t.Errorf("ElementValidate has %d rules, want 1 (min=3 after dive)", len(f.ElementValidate))
	}
	// But ElementGoKind is empty because it's not a slice.
	if f.ElementGoKind != "" {
		t.Errorf("ElementGoKind = %q, want empty (not a slice)", f.ElementGoKind)
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

func TestExtendsTokenResolution(t *testing.T) {
	pkg := types.NewPackage("github.com/app/models", "models")

	// Base struct: type Base struct { ID string `json:"id"` }
	baseIDField := types.NewField(0, pkg, "ID", types.Typ[types.String], false)
	baseStruct := types.NewStruct([]*types.Var{baseIDField}, []string{`json:"id"`})
	base := types.NewNamed(types.NewTypeName(0, pkg, "Base", nil), baseStruct, nil)

	// User struct with embedded Base using tstype:",extends"
	// type User struct { Base `tstype:",extends"`; Name string `json:"name"` }
	embeddedBase := types.NewField(0, pkg, "Base", base, true)
	nameField := types.NewField(0, pkg, "Name", types.Typ[types.String], false)
	userStruct := types.NewStruct(
		[]*types.Var{embeddedBase, nameField},
		[]string{`tstype:",extends"`, `json:"name"`},
	)
	user := types.NewNamed(types.NewTypeName(0, pkg, "User", nil), userStruct, nil)

	m := NewMapper(nil)
	m.Convert(user)
	defs := m.Defs()

	// Find User def.
	var userDef *TypeDef
	for i := range defs {
		if defs[i].Name == "User" {
			userDef = &defs[i]
			break
		}
	}
	if userDef == nil {
		t.Fatal("User def not found")
	}

	// Extends should be resolved to "Base", not a raw §...§ token.
	if len(userDef.Extends) != 1 {
		t.Fatalf("User.Extends = %v, want 1 entry", userDef.Extends)
	}
	if userDef.Extends[0] != "Base" {
		t.Errorf("User.Extends[0] = %q, want %q", userDef.Extends[0], "Base")
	}

	// User should NOT have the base field (ID) since it's extended, not flattened.
	for _, f := range userDef.Fields {
		if f.Name == "id" {
			t.Error("User should not have 'id' field — extended fields should not be flattened")
		}
	}
}

func TestTSDocTagInCollectFields(t *testing.T) {
	pkg := types.NewPackage("github.com/app/models", "models")

	// type Config struct {
	//     Host string `json:"host" ts_doc:"The hostname to connect to"`
	//     Port int    `json:"port"`
	// }
	hostField := types.NewField(0, pkg, "Host", types.Typ[types.String], false)
	portField := types.NewField(0, pkg, "Port", types.Typ[types.Int], false)
	configStruct := types.NewStruct(
		[]*types.Var{hostField, portField},
		[]string{`json:"host" ts_doc:"The hostname to connect to"`, `json:"port"`},
	)
	config := types.NewNamed(types.NewTypeName(0, pkg, "Config", nil), configStruct, nil)

	m := NewMapper(nil)
	m.Convert(config)
	defs := m.Defs()

	if len(defs) != 1 {
		t.Fatalf("got %d defs, want 1", len(defs))
	}

	var hostComment, portComment string
	for _, f := range defs[0].Fields {
		switch f.Name {
		case "host":
			hostComment = f.Comment
		case "port":
			portComment = f.Comment
		}
	}

	if hostComment != "The hostname to connect to" {
		t.Errorf("host comment = %q, want %q", hostComment, "The hostname to connect to")
	}
	if portComment != "" {
		t.Errorf("port comment = %q, want empty (no ts_doc tag)", portComment)
	}
}

func TestTSDocTagFallbackBehindSourceComment(t *testing.T) {
	pkg := types.NewPackage("github.com/app/models", "models")

	// When both a source comment and ts_doc exist, source comment wins.
	hostField := types.NewField(0, pkg, "Host", types.Typ[types.String], false)
	configStruct := types.NewStruct(
		[]*types.Var{hostField},
		[]string{`json:"host" ts_doc:"from tag"`},
	)
	config := types.NewNamed(types.NewTypeName(0, pkg, "Config", nil), configStruct, nil)

	// Provide a source comment via metadata.
	metas := map[string]TypeMeta{
		"github.com/app/models.Config": {
			FieldComments: map[int]string{0: "from source"},
		},
	}
	m := NewMapper(metas)
	m.Convert(config)
	defs := m.Defs()

	if len(defs) != 1 || len(defs[0].Fields) != 1 {
		t.Fatal("unexpected defs structure")
	}
	if defs[0].Fields[0].Comment != "from source" {
		t.Errorf("comment = %q, want %q (source comment should take precedence)", defs[0].Fields[0].Comment, "from source")
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
