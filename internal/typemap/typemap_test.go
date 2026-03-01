package typemap

import (
	"go/types"
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
	m := NewMapper(map[string]TypeMeta{
		"Status": {ConstValues: []string{`"active"`, `"pending"`}},
	})

	pkg := types.NewPackage("main", "main")
	statusObj := types.NewTypeName(0, pkg, "Status", nil)
	statusType := types.NewNamed(statusObj, types.Typ[types.String], nil)

	got := m.Convert(statusType)
	if got != "Status" {
		t.Errorf("Convert(Status) = %q, want %q", got, "Status")
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
	m := NewMapper(map[string]TypeMeta{
		"Status": {ConstValues: []string{`"a"`, `"b"`}},
	})

	pkg := types.NewPackage("main", "main")
	obj := types.NewTypeName(0, pkg, "Status", nil)
	named := types.NewNamed(obj, types.Typ[types.String], nil)

	// Convert twice — should only produce one def.
	m.Convert(named)
	m.Convert(named)

	if len(m.Defs()) != 1 {
		t.Errorf("got %d defs after double Convert, want 1", len(m.Defs()))
	}
}

func TestAliasType(t *testing.T) {
	m := NewMapper(map[string]TypeMeta{
		"UserRole": {IsAlias: true},
	})

	pkg := types.NewPackage("main", "main")
	obj := types.NewTypeName(0, pkg, "UserRole", nil)
	alias := types.NewAlias(obj, types.Typ[types.String])

	got := m.Convert(alias)
	if got != "UserRole" {
		t.Errorf("Convert(UserRole) = %q, want %q", got, "UserRole")
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
