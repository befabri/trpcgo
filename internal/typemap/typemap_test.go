package typemap

import (
	"go/types"
	"testing"
)

func TestBasicTypes(t *testing.T) {
	m := NewMapper()

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
	m := NewMapper()

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
	m := NewMapper()

	// map[string]int → Record<string, number>
	mapType := types.NewMap(types.Typ[types.String], types.Typ[types.Int])
	got := m.Convert(mapType)
	want := "Record<string, number>"
	if got != want {
		t.Errorf("Convert(map[string]int) = %q, want %q", got, want)
	}
}

func TestPointerType(t *testing.T) {
	m := NewMapper()

	// *string → string (pointer unwrapped)
	ptr := types.NewPointer(types.Typ[types.String])
	if got := m.Convert(ptr); got != "string" {
		t.Errorf("Convert(*string) = %q, want %q", got, "string")
	}
}

func TestEmptyInterface(t *testing.T) {
	m := NewMapper()

	// interface{} / any → unknown
	iface := types.NewInterfaceType(nil, nil)
	iface.Complete()
	if got := m.Convert(iface); got != "unknown" {
		t.Errorf("Convert(interface{}) = %q, want %q", got, "unknown")
	}
}

func TestJSONTagParsing(t *testing.T) {
	tests := []struct {
		tag       string
		wantName  string
		wantOmit  bool
		wantSkip  bool
	}{
		{`json:"name"`, "name", false, false},
		{`json:"name,omitempty"`, "name", true, false},
		{`json:"-"`, "", false, true},
		{`json:"id" xml:"id"`, "id", false, false},
		{``, "", false, false},
		{`xml:"name"`, "", false, false},
	}

	for _, tt := range tests {
		name, omit, skip := parseJSONTag(tt.tag)
		if name != tt.wantName || omit != tt.wantOmit || skip != tt.wantSkip {
			t.Errorf("parseJSONTag(%q) = (%q, %v, %v), want (%q, %v, %v)",
				tt.tag, name, omit, skip, tt.wantName, tt.wantOmit, tt.wantSkip)
		}
	}
}
