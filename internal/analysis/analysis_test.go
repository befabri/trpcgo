package analysis_test

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/trpcgo/trpcgo/internal/analysis"
)

func testdataDir() string {
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(thisFile), "testdata", "basic")
}

func TestAnalyzeBasic(t *testing.T) {
	dir := testdataDir()
	procs, err := analysis.Analyze([]string{"."}, dir)
	if err != nil {
		t.Fatal(err)
	}

	if len(procs) != 4 {
		t.Fatalf("got %d procedures, want 4", len(procs))
	}

	// Build a map for easier lookup.
	byPath := make(map[string]analysis.Procedure)
	for _, p := range procs {
		byPath[p.Path] = p
	}

	// Query with input.
	q := byPath["user.getById"]
	if q.Type != "query" {
		t.Errorf("user.getById type = %q, want query", q.Type)
	}
	if q.InputType == nil {
		t.Error("user.getById should have an input type")
	}
	if q.OutputType == nil {
		t.Error("user.getById should have an output type")
	}

	// Void query.
	vq := byPath["user.listUsers"]
	if vq.Type != "query" {
		t.Errorf("user.listUsers type = %q, want query", vq.Type)
	}
	if vq.InputType != nil {
		t.Error("user.listUsers should have nil input type (void)")
	}

	// Mutation.
	m := byPath["user.createUser"]
	if m.Type != "mutation" {
		t.Errorf("user.createUser type = %q, want mutation", m.Type)
	}

	// Subscription.
	s := byPath["user.onCreated"]
	if s.Type != "subscription" {
		t.Errorf("user.onCreated type = %q, want subscription", s.Type)
	}
	if s.InputType != nil {
		t.Error("user.onCreated should have nil input type (void)")
	}
}
