package codegen_test

import (
	"errors"
	"fmt"
	"io"
	"testing"

	"github.com/befabri/trpcgo/internal/codegen"
	"github.com/befabri/trpcgo/internal/typemap"
)

type countingWriter struct {
	writes int
}

func (w *countingWriter) Write(p []byte) (int, error) {
	w.writes++
	return len(p), nil
}

type failNthWriter struct {
	failAt int
	writes int
	err    error
}

func (w *failNthWriter) Write(p []byte) (int, error) {
	if w.writes == w.failAt {
		w.writes++
		return 0, w.err
	}
	w.writes++
	return len(p), nil
}

func requireEveryWriteErrorPropagates(t *testing.T, write func(io.Writer) error) {
	t.Helper()

	counter := new(countingWriter)
	if err := write(counter); err != nil {
		t.Fatalf("counting successful writes: %v", err)
	}
	if counter.writes == 0 {
		t.Fatal("generator performed no writes")
	}

	writeErr := errors.New("injected write failure")
	for failAt := range counter.writes {
		t.Run(fmt.Sprintf("write_%d_of_%d", failAt+1, counter.writes), func(t *testing.T) {
			writer := &failNthWriter{failAt: failAt, err: writeErr}
			err := write(writer)
			if !errors.Is(err, writeErr) {
				t.Fatalf("generator error = %v, want wrapping %v", err, writeErr)
			}
		})
	}
}

func TestWriteAppRouterPropagatesEveryWriteError(t *testing.T) {
	procs := []codegen.ProcEntry{
		{Path: "user", ProcType: "query", InputTS: "void", OutputTS: "User"},
		{Path: "user.get", ProcType: "query", InputTS: "GetUserInput", OutputTS: "User"},
		{Path: "user.create", ProcType: "mutation", InputTS: "CreateUserInput", OutputTS: "User"},
		{Path: "user.events", ProcType: "subscription", InputTS: "void", OutputTS: "User"},
	}
	defs := []typemap.TypeDef{
		{
			Name:    "User",
			Kind:    typemap.TypeDefInterface,
			Comment: "A user.\nIncludes profile data.",
			Fields: []typemap.Field{
				{Name: "id", Type: "string", Readonly: true, Comment: "Stable ID.\nNever reused."},
			},
		},
		{Name: "Role", Kind: typemap.TypeDefUnion, UnionMembers: []string{`"admin"`, `"user"`}},
		{Name: "UserID", Kind: typemap.TypeDefAlias, AliasOf: "string"},
	}

	requireEveryWriteErrorPropagates(t, func(w io.Writer) error {
		return codegen.WriteAppRouter(w, procs, defs)
	})
}

func TestWriteZodSchemasPropagatesEveryWriteError(t *testing.T) {
	procs := []codegen.ProcEntry{
		{Path: "base", InputTS: "Base"},
		{Path: "profile", InputTS: "Profile"},
		{Path: "node", InputTS: "Node"},
		{Path: "status", InputTS: "Status"},
		{Path: "level", InputTS: "Level"},
		{Path: "alias", InputTS: "Alias"},
	}
	defs := []typemap.TypeDef{
		{
			Name: "Base",
			Kind: typemap.TypeDefInterface,
			Fields: []typemap.Field{
				{Name: "id", Type: "string", GoKind: "string"},
			},
		},
		{
			Name:    "Profile",
			Kind:    typemap.TypeDefInterface,
			Extends: []string{"Base"},
			Fields: []typemap.Field{
				{Name: "start", Type: "number", GoKind: "int", Comment: "Start value."},
				{Name: "end", Type: "number", GoKind: "int"},
			},
			Refinements: []typemap.Refinement{
				{Field: "end", Op: ">", OtherField: "start"},
			},
		},
		{
			Name: "Node",
			Kind: typemap.TypeDefInterface,
			Fields: []typemap.Field{
				{Name: "next", Type: "Node", Optional: true},
			},
		},
		{Name: "Status", Kind: typemap.TypeDefUnion, UnionMembers: []string{`"active"`, `"disabled"`}},
		{Name: "Level", Kind: typemap.TypeDefUnion, UnionMembers: []string{"1", "2"}},
		{Name: "Alias", Kind: typemap.TypeDefAlias, AliasOf: "string"},
	}

	styles := []struct {
		name  string
		style typemap.ZodStyle
	}{
		{name: "standard", style: typemap.ZodStandard},
		{name: "mini", style: typemap.ZodMini},
	}
	for _, tt := range styles {
		t.Run(tt.name, func(t *testing.T) {
			requireEveryWriteErrorPropagates(t, func(w io.Writer) error {
				return codegen.WriteZodSchemas(w, procs, defs, tt.style)
			})
		})
	}
}

func TestWriteEnumsPropagatesEveryWriteError(t *testing.T) {
	defs := []typemap.TypeDef{
		{
			Name:         "Role",
			Kind:         typemap.TypeDefUnion,
			Comment:      "A role.\nControls permissions.",
			UnionMembers: []string{`"admin"`, `"user"`},
		},
	}

	requireEveryWriteErrorPropagates(t, func(w io.Writer) error {
		return codegen.WriteEnums(w, defs)
	})
}
