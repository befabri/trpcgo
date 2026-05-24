package typemap

import (
	"go/types"
	"testing"
)

func TestInlineStructEmptyWhenNoExportedFields(t *testing.T) {
	m := NewMapper(nil)
	pkg := types.NewPackage("example.com/app", "app")
	st := types.NewStruct([]*types.Var{
		types.NewField(0, pkg, "hidden", types.Typ[types.String], false),
		types.NewField(0, pkg, "Skipped", types.Typ[types.String], false),
	}, []string{"", `json:"-"`})

	if got := m.inlineStruct(st); got != "Record<string, never>" {
		t.Errorf("inlineStruct(empty) = %q, want Record<string, never>", got)
	}
}
