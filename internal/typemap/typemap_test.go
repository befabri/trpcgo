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

func TestQuotePropName(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{"", `""`},
		{"name", "name"},
		{"_name", "_name"},
		{"$name", "$name"},
		{"na9me", "na9me"},
		{"9name", `"9name"`},
		{"with-hyphen", `"with-hyphen"`},
		{"with space", `"with space"`},
	}

	for _, tt := range tests {
		if got := QuotePropName(tt.name); got != tt.want {
			t.Errorf("QuotePropName(%q) = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestGoKindCoversKnownAndCompositeKinds(t *testing.T) {
	pkg := types.NewPackage("example.com/app", "app")
	timePkg := types.NewPackage("time", "time")
	jsonPkg := types.NewPackage("encoding/json", "json")
	namedInt := types.NewNamed(types.NewTypeName(0, pkg, "Age", nil), types.Typ[types.Int], nil)
	timeType := types.NewNamed(types.NewTypeName(0, timePkg, "Time", nil), types.NewStruct(nil, nil), nil)
	rawMessage := types.NewNamed(types.NewTypeName(0, jsonPkg, "RawMessage", nil), types.NewSlice(types.Typ[types.Byte]), nil)
	iface := types.NewInterfaceType(nil, nil)
	iface.Complete()

	tests := []struct {
		name string
		typ  types.Type
		want string
	}{
		{"pointer named int", types.NewPointer(namedInt), "int"},
		{"time", timeType, "time.Time"},
		{"raw message", rawMessage, "json.RawMessage"},
		{"byte slice", types.NewSlice(types.Typ[types.Byte]), "[]byte"},
		{"string slice", types.NewSlice(types.Typ[types.String]), "slice"},
		{"array", types.NewArray(types.Typ[types.Int32], 2), "array"},
		{"map", types.NewMap(types.Typ[types.String], types.Typ[types.Bool]), "map"},
		{"struct", types.NewStruct(nil, nil), "struct"},
		{"interface", iface, "interface"},
		{"complex", types.Typ[types.Complex64], "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := goKind(tt.typ); got != tt.want {
				t.Errorf("goKind(%v) = %q, want %q", tt.typ, got, tt.want)
			}
		})
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
