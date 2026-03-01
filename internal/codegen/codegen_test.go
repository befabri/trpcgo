package codegen_test

import (
	"bytes"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/trpcgo/trpcgo/internal/analysis"
	"github.com/trpcgo/trpcgo/internal/codegen"
	"github.com/trpcgo/trpcgo/internal/typemap"
)

func testdataDir() string {
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(thisFile), "..", "analysis", "testdata", "basic")
}

func TestGenerateFromBasicFixture(t *testing.T) {
	dir := testdataDir()
	procs, err := analysis.Analyze([]string{"."}, dir)
	if err != nil {
		t.Fatal(err)
	}

	mapper := typemap.NewMapper()
	var buf bytes.Buffer
	if err := codegen.Generate(&buf, procs, mapper); err != nil {
		t.Fatal(err)
	}

	output := buf.String()

	// Check that interfaces are generated.
	checks := []string{
		"export interface User {",
		"export interface GetUserByIdInput {",
		"export interface CreateUserInput {",
		"export type AppRouter =",
		"$Query<GetUserByIdInput, User>",
		"$Query<void, User[]>",
		"$Mutation<CreateUserInput, User>",
		"$Subscription<void, User>",
	}
	for _, c := range checks {
		if !strings.Contains(output, c) {
			t.Errorf("output missing %q", c)
		}
	}

	// Check nested structure: user namespace.
	if !strings.Contains(output, "user: {") {
		t.Error("output missing user namespace")
	}
}
