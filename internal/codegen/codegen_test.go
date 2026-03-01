package codegen_test

import (
	"bytes"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/trpcgo/trpcgo/internal/analysis"
	"github.com/trpcgo/trpcgo/internal/codegen"
)

func testdataDir(name string) string {
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(thisFile), "..", "analysis", "testdata", name)
}

func generateFromFixture(t *testing.T, name string) string {
	t.Helper()
	dir := testdataDir(name)
	result, err := analysis.Analyze([]string{"."}, dir)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := codegen.Generate(&buf, result, result.TypeMetas); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

// containsLine checks that at least one line in output contains substr after trimming.
func containsLine(output, substr string) bool {
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(strings.TrimSpace(line), substr) {
			return true
		}
	}
	return false
}

func TestGenerateFromBasicFixture(t *testing.T) {
	output := generateFromFixture(t, "basic")

	checks := []string{
		"export interface User {",
		"export interface GetUserByIdInput {",
		"export interface CreateUserInput {",
		"export type AppRouter =",
		"$Query<GetUserByIdInput, User>",
		"$Query<void, User[]>",
		"$Mutation<CreateUserInput, User>",
		"$Subscription<void, User>",
		"user: {",
	}
	for _, c := range checks {
		if !strings.Contains(output, c) {
			t.Errorf("output missing %q", c)
		}
	}
}

func TestGenerateEnhanced(t *testing.T) {
	output := generateFromFixture(t, "enhanced")

	t.Run("union type Status with all values", func(t *testing.T) {
		if !strings.Contains(output, "export type Status = ") {
			t.Fatalf("missing Status union type.\nOutput:\n%s", output)
		}
		// Verify it's a proper union with pipe separators.
		for _, line := range strings.Split(output, "\n") {
			if strings.Contains(line, "export type Status = ") {
				for _, val := range []string{`"active"`, `"pending"`, `"banned"`} {
					if !strings.Contains(line, val) {
						t.Errorf("Status union missing value %s in line: %s", val, line)
					}
				}
				if strings.Count(line, "|") != 2 {
					t.Errorf("Status union should have 2 pipe separators, got line: %s", line)
				}
				break
			}
		}
	})

	t.Run("integer union type Priority", func(t *testing.T) {
		if !strings.Contains(output, "export type Priority = ") {
			t.Fatalf("missing Priority union type.\nOutput:\n%s", output)
		}
		for _, line := range strings.Split(output, "\n") {
			if strings.Contains(line, "export type Priority = ") {
				for _, val := range []string{"1", "2", "3"} {
					if !strings.Contains(line, val) {
						t.Errorf("Priority union missing value %s in line: %s", val, line)
					}
				}
				break
			}
		}
	})

	t.Run("type alias UserRole", func(t *testing.T) {
		if !containsLine(output, "export type UserRole = string;") {
			t.Errorf("missing UserRole alias.\nOutput:\n%s", output)
		}
	})

	t.Run("JSDoc on type", func(t *testing.T) {
		// The comment should appear as JSDoc before the User interface.
		idx := strings.Index(output, "User represents a registered user.")
		if idx == -1 {
			t.Fatalf("missing JSDoc for User.\nOutput:\n%s", output)
		}
		// JSDoc should appear before the interface declaration.
		ifaceIdx := strings.Index(output, "export interface User {")
		if ifaceIdx == -1 || idx > ifaceIdx {
			t.Errorf("JSDoc should appear before interface declaration")
		}
	})

	t.Run("JSDoc on field", func(t *testing.T) {
		commentIdx := strings.Index(output, "The unique identifier.")
		if commentIdx == -1 {
			t.Fatalf("missing JSDoc for ID field.\nOutput:\n%s", output)
		}
		// The id field should appear shortly after the comment (within User interface).
		rest := output[commentIdx:]
		fieldIdx := strings.Index(rest, "id: string;")
		if fieldIdx == -1 {
			t.Errorf("id field not found after JSDoc comment.\nOutput:\n%s", output)
		}
	})

	t.Run("readonly from tstype tag", func(t *testing.T) {
		if !containsLine(output, "readonly createdAt: string;") {
			t.Errorf("missing readonly createdAt.\nOutput:\n%s", output)
		}
	})

	t.Run("tstype override with commas", func(t *testing.T) {
		// Record<string, unknown> has a comma — must not be split by parser.
		if !containsLine(output, "metadata: Record<string, unknown>;") {
			t.Errorf("missing tstype override for metadata.\nOutput:\n%s", output)
		}
	})

	t.Run("json skip field excluded", func(t *testing.T) {
		if strings.Contains(output, "secret") || strings.Contains(output, "Secret") {
			t.Errorf("json:\"-\" field should be excluded.\nOutput:\n%s", output)
		}
	})

	t.Run("tstype skip field excluded", func(t *testing.T) {
		if strings.Contains(output, "debug") || strings.Contains(output, "Debug") {
			t.Errorf("tstype:\"-\" field should be excluded.\nOutput:\n%s", output)
		}
	})

	t.Run("optional pointer field", func(t *testing.T) {
		if !containsLine(output, "bio?: string;") {
			t.Errorf("pointer field bio should be optional.\nOutput:\n%s", output)
		}
	})

	t.Run("required overrides optional on pointer", func(t *testing.T) {
		// avatar is *string but has tstype:",required" — should NOT be optional.
		if containsLine(output, "avatar?:") {
			t.Errorf("avatar should not be optional (tstype required overrides pointer).\nOutput:\n%s", output)
		}
		if !containsLine(output, "avatar: string;") {
			t.Errorf("avatar should be required string.\nOutput:\n%s", output)
		}
	})

	t.Run("generic interface with type param", func(t *testing.T) {
		if !strings.Contains(output, "export interface Paginated<T>") {
			t.Errorf("missing Paginated<T> generic interface.\nOutput:\n%s", output)
		}
		if !containsLine(output, "items: T[];") {
			t.Errorf("Paginated should have items: T[].\nOutput:\n%s", output)
		}
		if !containsLine(output, "total: number;") {
			t.Errorf("Paginated should have total: number.\nOutput:\n%s", output)
		}
	})

	t.Run("generic instantiation in procedure", func(t *testing.T) {
		if !strings.Contains(output, "Paginated<User>") {
			t.Errorf("missing Paginated<User> instantiation.\nOutput:\n%s", output)
		}
	})

	t.Run("named type used as field type", func(t *testing.T) {
		if !containsLine(output, "status: Status;") {
			t.Errorf("status should reference Status type.\nOutput:\n%s", output)
		}
		if !containsLine(output, "priority: Priority;") {
			t.Errorf("priority should reference Priority type.\nOutput:\n%s", output)
		}
	})

	t.Run("User interface emitted exactly once", func(t *testing.T) {
		// User is returned by both user.get and user.create — should only have one interface.
		count := strings.Count(output, "export interface User {")
		if count != 1 {
			t.Errorf("User interface emitted %d times, want 1.\nOutput:\n%s", count, output)
		}
	})

	t.Run("procedure types", func(t *testing.T) {
		if !strings.Contains(output, "$Query<GetUserInput, User>") {
			t.Errorf("missing query procedure.\nOutput:\n%s", output)
		}
		if !strings.Contains(output, "$Query<void, Paginated<User>>") {
			t.Errorf("missing void query with generic output.\nOutput:\n%s", output)
		}
		if !strings.Contains(output, "$Mutation<CreateUserInput, User>") {
			t.Errorf("missing mutation procedure.\nOutput:\n%s", output)
		}
	})

	t.Run("idempotent generation", func(t *testing.T) {
		second := generateFromFixture(t, "enhanced")
		if output != second {
			t.Error("generating twice should produce identical output")
		}
	})
}
