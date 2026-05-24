package typemap

import (
	"go/types"
	"strings"
	"testing"
)

func TestApplyTSTypeTag(t *testing.T) {
	base := Field{Type: "string", Optional: true}
	applyTSTypeTag(&base, TSTypeTag{Type: "Record<string, unknown>", Readonly: true, Required: true}, true)
	if base.Type != "Record<string, unknown>" || !base.Readonly || !base.Required || base.Optional {
		t.Fatalf("applyTSTypeTag override = %#v", base)
	}

	unchanged := Field{Type: "number", Optional: true}
	applyTSTypeTag(&unchanged, TSTypeTag{Type: "boolean", Readonly: true, Required: true}, false)
	if unchanged.Type != "number" || unchanged.Readonly || unchanged.Required || !unchanged.Optional {
		t.Fatalf("applyTSTypeTag without tag changed field: %#v", unchanged)
	}

	readonlyOnly := Field{Type: "string", Optional: true}
	applyTSTypeTag(&readonlyOnly, TSTypeTag{Readonly: true}, true)
	if readonlyOnly.Type != "string" || !readonlyOnly.Readonly || readonlyOnly.Required || !readonlyOnly.Optional {
		t.Fatalf("applyTSTypeTag readonly-only = %#v", readonlyOnly)
	}
}

func TestFieldCommentFallsBackToTSDocForEmptyDocComment(t *testing.T) {
	comments := map[int]string{0: ""}
	if got := fieldComment(`ts_doc:"fallback doc"`, comments, 0); got != "fallback doc" {
		t.Fatalf("fieldComment empty doc fallback = %q, want fallback doc", got)
	}
	if got := fieldComment(`ts_doc:"fallback doc"`, map[int]string{0: "real doc"}, 0); got != "real doc" {
		t.Fatalf("fieldComment non-empty doc = %q, want real doc", got)
	}
}

func TestInlineStructCoversTagsAndSkippedFields(t *testing.T) {
	m := NewMapper(nil)
	pkg := types.NewPackage("example.com/app", "app")
	fields := []*types.Var{
		types.NewField(0, pkg, "ID", types.Typ[types.String], false),
		types.NewField(0, pkg, "Count", types.NewPointer(types.Typ[types.Int]), false),
		types.NewField(0, pkg, "Custom", types.Typ[types.String], false),
		types.NewField(0, pkg, "SkipJSON", types.Typ[types.String], false),
		types.NewField(0, pkg, "SkipTS", types.Typ[types.String], false),
		types.NewField(0, pkg, "hidden", types.Typ[types.String], false),
		types.NewField(0, pkg, "Required", types.NewPointer(types.Typ[types.String]), false),
	}
	tags := []string{
		`json:"id"`,
		`json:"count,omitempty"`,
		`json:"custom-name" tstype:"ReadonlyThing,readonly"`,
		`json:"-"`,
		`tstype:"-"`,
		`json:"hidden"`,
		`tstype:",required"`,
	}
	st := types.NewStruct(fields, tags)

	got := m.inlineStruct(st)
	checks := []string{
		"id: string",
		"count?: number",
		`readonly "custom-name": ReadonlyThing`,
		"Required: string",
	}
	for _, check := range checks {
		if !strings.Contains(got, check) {
			t.Errorf("inlineStruct missing %q in %q", check, got)
		}
	}
	for _, excluded := range []string{"SkipJSON", "SkipTS", "hidden"} {
		if strings.Contains(got, excluded) {
			t.Errorf("inlineStruct should exclude %q in %q", excluded, got)
		}
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

func TestCollectFieldsInvalidZodRules(t *testing.T) {
	pkg := types.NewPackage("main", "main")
	countField := types.NewField(0, pkg, "Count", types.Typ[types.Int], false)
	tagsField := types.NewField(0, pkg, "Tags", types.NewSlice(types.Typ[types.String]), false)
	st := types.NewStruct(
		[]*types.Var{countField, tagsField},
		[]string{
			`json:"count" validate:"min=1); evil()"`,
			`json:"tags" validate:"min=1,dive,max=2); evil()"`,
		},
	)

	m := NewMapper(nil)
	var fields []Field
	m.collectFields(st, &fields, nil, nil)

	if len(fields) != 2 {
		t.Fatalf("got %d fields, want 2", len(fields))
	}
	count := fields[0]
	if len(count.InvalidZod) != 1 || count.InvalidZod[0].Tag != "min" {
		t.Fatalf("count InvalidZod = %+v, want min rule", count.InvalidZod)
	}
	tags := fields[1]
	if len(tags.InvalidZod) != 1 || tags.InvalidZod[0].Tag != "max" {
		t.Fatalf("tags InvalidZod = %+v, want element max rule", tags.InvalidZod)
	}
}

func TestCollectFieldsTracksPointerSemantics(t *testing.T) {
	pkg := types.NewPackage("main", "main")
	nameField := types.NewField(0, pkg, "Name", types.NewPointer(types.Typ[types.String]), false)
	tagsField := types.NewField(0, pkg, "Tags", types.NewSlice(types.NewPointer(types.Typ[types.String])), false)
	st := types.NewStruct(
		[]*types.Var{nameField, tagsField},
		[]string{
			`json:"name" validate:"required"`,
			`json:"tags" validate:"dive,required"`,
		},
	)

	m := NewMapper(nil)
	var fields []Field
	m.collectFields(st, &fields, nil, nil)

	if len(fields) != 2 {
		t.Fatalf("got %d fields, want 2", len(fields))
	}
	if !fields[0].IsPointer {
		t.Fatalf("Name IsPointer = false, want true")
	}
	if !fields[1].ElementIsPointer {
		t.Fatalf("Tags ElementIsPointer = false, want true")
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
