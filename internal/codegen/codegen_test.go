package codegen_test

import (
	"bytes"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/befabri/trpcgo/internal/analysis"
	"github.com/befabri/trpcgo/internal/codegen"
	"github.com/befabri/trpcgo/internal/typemap"
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
	if _, err := codegen.Generate(&buf, result, result.TypeMetas); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

// containsLine checks that at least one line in output contains substr after trimming.
func containsLine(output, substr string) bool {
	for line := range strings.SplitSeq(output, "\n") {
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
		for line := range strings.SplitSeq(output, "\n") {
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
		for line := range strings.SplitSeq(output, "\n") {
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
		found := strings.Contains(rest, "id: string;")
		if !found {
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

func TestGenerateEnhancedDiveArray(t *testing.T) {
	output := generateFromFixture(t, "enhanced")

	// CreateUserInput should have tags field as string[].
	if !containsLine(output, "tags: string[];") {
		t.Errorf("missing tags field in CreateUserInput.\nOutput:\n%s", output)
	}

	// The TS interface output is type-level only — dive doesn't affect it.
	// Verify that validate tags don't leak into the interface output.
	if strings.Contains(output, "dive") {
		t.Errorf("validate tag 'dive' should not appear in TS output.\nOutput:\n%s", output)
	}
	if strings.Contains(output, "validate") {
		t.Errorf("validate tags should not appear in TS output.\nOutput:\n%s", output)
	}
}

func TestZodSchemaFromEnhanced(t *testing.T) {
	dir := testdataDir("enhanced")
	result, err := analysis.Analyze([]string{"."}, dir)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	genResult, err := codegen.Generate(&buf, result, result.TypeMetas)
	if err != nil {
		t.Fatal(err)
	}

	var zodBuf bytes.Buffer
	if err := codegen.WriteZodSchemas(&zodBuf, genResult.Procs, genResult.Defs, typemap.ZodStandard); err != nil {
		t.Fatal(err)
	}
	zodOutput := zodBuf.String()

	t.Run("import line", func(t *testing.T) {
		if !strings.Contains(zodOutput, `import { z } from "zod";`) {
			t.Errorf("missing Zod import line.\nOutput:\n%s", zodOutput)
		}
	})

	t.Run("CreateUserInputSchema present", func(t *testing.T) {
		if !strings.Contains(zodOutput, "export const CreateUserInputSchema = z.object(") {
			t.Errorf("missing CreateUserInputSchema.\nOutput:\n%s", zodOutput)
		}
	})

	t.Run("dive array element constraints", func(t *testing.T) {
		// tags field: validate:"required,min=1,dive,min=1,max=50"
		// Expected: z.array(z.string().min(1).max(50)).min(1)
		if !strings.Contains(zodOutput, "z.array(z.string().min(1).max(50)).min(1)") {
			t.Errorf("missing dive array with element constraints.\nOutput:\n%s", zodOutput)
		}
	})

	t.Run("email field as z.email()", func(t *testing.T) {
		if !strings.Contains(zodOutput, "z.email()") {
			t.Errorf("email field should use z.email() top-level format.\nOutput:\n%s", zodOutput)
		}
	})
}

func TestZodMiniOutput(t *testing.T) {
	dir := testdataDir("enhanced")
	result, err := analysis.Analyze([]string{"."}, dir)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	genResult, err := codegen.Generate(&buf, result, result.TypeMetas)
	if err != nil {
		t.Fatal(err)
	}

	var zodBuf bytes.Buffer
	if err := codegen.WriteZodSchemas(&zodBuf, genResult.Procs, genResult.Defs, typemap.ZodMini); err != nil {
		t.Fatal(err)
	}
	zodOutput := zodBuf.String()

	t.Run("mini import line", func(t *testing.T) {
		if !strings.Contains(zodOutput, `import * as z from "zod/mini";`) {
			t.Errorf("missing Zod Mini import line.\nOutput:\n%s", zodOutput)
		}
	})

	t.Run("standard import absent", func(t *testing.T) {
		if strings.Contains(zodOutput, `import { z } from "zod";`) {
			t.Errorf("standard Zod import should not appear in Mini output.\nOutput:\n%s", zodOutput)
		}
	})

	t.Run("schemas still present", func(t *testing.T) {
		if !strings.Contains(zodOutput, "CreateUserInputSchema") {
			t.Errorf("missing CreateUserInputSchema in Mini output.\nOutput:\n%s", zodOutput)
		}
	})

	// Compare with standard output to verify they differ.
	var stdBuf bytes.Buffer
	if err := codegen.WriteZodSchemas(&stdBuf, genResult.Procs, genResult.Defs, typemap.ZodStandard); err != nil {
		t.Fatal(err)
	}
	stdOutput := stdBuf.String()

	t.Run("mini differs from standard", func(t *testing.T) {
		if zodOutput == stdOutput {
			t.Error("Mini output should differ from Standard output")
		}
	})
}

func TestZodDiveArrayDirect(t *testing.T) {
	// Test dive array Zod output directly by constructing a minimal proc + def set.
	// This tests the codegen fieldToZod path without depending on the full analysis pipeline.
	procs := []codegen.ProcEntry{
		{Path: "items.create", ProcType: "mutation", InputTS: "CreateItemInput", OutputTS: "Item"},
	}
	defs := []typemap.TypeDef{
		{
			Name: "CreateItemInput",
			Kind: typemap.TypeDefInterface,
			Fields: []typemap.Field{
				{
					Name:   "name",
					Type:   "string",
					GoKind: "string",
					Validate: []typemap.ValidateRule{
						{Tag: "required"},
						{Tag: "min", Param: "1"},
						{Tag: "max", Param: "100"},
					},
				},
				{
					Name:   "tags",
					Type:   "string[]",
					GoKind: "slice",
					Validate: []typemap.ValidateRule{
						{Tag: "required"},
						{Tag: "min", Param: "1"},
						{Tag: "max", Param: "10"},
					},
					ElementValidate: []typemap.ValidateRule{
						{Tag: "min", Param: "2"},
						{Tag: "max", Param: "64"},
					},
					ElementGoKind: "string",
				},
			},
		},
	}

	var buf bytes.Buffer
	if err := codegen.WriteZodSchemas(&buf, procs, defs, typemap.ZodStandard); err != nil {
		t.Fatal(err)
	}
	output := buf.String()

	// Element constraints (min=2, max=64) applied inside z.array().
	// Container constraints (min=1, max=10) applied after z.array().
	want := "z.array(z.string().min(2).max(64)).min(1).max(10)"
	if !strings.Contains(output, want) {
		t.Errorf("missing dive array output.\nwant substring: %s\nOutput:\n%s", want, output)
	}

	// Name field should have standard string constraints.
	if !strings.Contains(output, "z.string().min(1).max(100)") {
		t.Errorf("missing name field constraints.\nOutput:\n%s", output)
	}
}

func TestZodDiveArrayStructElement(t *testing.T) {
	// When dive target is a struct, element constraints don't apply — just use schema ref.
	procs := []codegen.ProcEntry{
		{Path: "orders.create", ProcType: "mutation", InputTS: "CreateOrderInput", OutputTS: "Order"},
	}
	defs := []typemap.TypeDef{
		{
			Name: "CreateOrderInput",
			Kind: typemap.TypeDefInterface,
			Fields: []typemap.Field{
				{
					Name:   "items",
					Type:   "ItemInput[]",
					GoKind: "slice",
					Validate: []typemap.ValidateRule{
						{Tag: "required"},
						{Tag: "min", Param: "1"},
					},
					ElementValidate: []typemap.ValidateRule{},
					ElementGoKind:   "struct",
				},
			},
		},
		{
			Name: "ItemInput",
			Kind: typemap.TypeDefInterface,
			Fields: []typemap.Field{
				{Name: "sku", Type: "string", GoKind: "string"},
			},
		},
	}

	var buf bytes.Buffer
	if err := codegen.WriteZodSchemas(&buf, procs, defs, typemap.ZodStandard); err != nil {
		t.Fatal(err)
	}
	output := buf.String()

	// Struct elements use schema reference, not inline constraints.
	if !strings.Contains(output, "z.array(ItemInputSchema).min(1)") {
		t.Errorf("missing struct element array.\nOutput:\n%s", output)
	}

	// ItemInputSchema should be emitted before CreateOrderInputSchema (topo sort).
	itemIdx := strings.Index(output, "ItemInputSchema = z.object")
	createIdx := strings.Index(output, "CreateOrderInputSchema = z.object")
	if itemIdx < 0 || createIdx < 0 {
		t.Fatalf("missing schemas.\nOutput:\n%s", output)
	}
	if itemIdx > createIdx {
		t.Errorf("ItemInputSchema should be emitted before CreateOrderInputSchema (dependency order)")
	}
}

func TestZodDiveArrayNoElementRules(t *testing.T) {
	// Array with dive but no element rules — should use plain element ref.
	procs := []codegen.ProcEntry{
		{Path: "items.create", ProcType: "mutation", InputTS: "Input", OutputTS: "Output"},
	}
	defs := []typemap.TypeDef{
		{
			Name: "Input",
			Kind: typemap.TypeDefInterface,
			Fields: []typemap.Field{
				{
					Name:            "ids",
					Type:            "string[]",
					GoKind:          "slice",
					Validate:        []typemap.ValidateRule{{Tag: "required"}},
					ElementValidate: nil,
					ElementGoKind:   "string",
				},
			},
		},
	}

	var buf bytes.Buffer
	if err := codegen.WriteZodSchemas(&buf, procs, defs, typemap.ZodStandard); err != nil {
		t.Fatal(err)
	}
	output := buf.String()

	// No element rules → z.array(z.string()), no element-level constraints.
	if !strings.Contains(output, "z.array(z.string())") {
		t.Errorf("missing plain array without element constraints.\nOutput:\n%s", output)
	}
}

func TestZodMiniDiveArray(t *testing.T) {
	// Verify Mini style produces different output for dive arrays.
	procs := []codegen.ProcEntry{
		{Path: "items.create", ProcType: "mutation", InputTS: "Input", OutputTS: "Output"},
	}
	defs := []typemap.TypeDef{
		{
			Name: "Input",
			Kind: typemap.TypeDefInterface,
			Fields: []typemap.Field{
				{
					Name:   "tags",
					Type:   "string[]",
					GoKind: "slice",
					Validate: []typemap.ValidateRule{
						{Tag: "min", Param: "1"},
					},
					ElementValidate: []typemap.ValidateRule{
						{Tag: "min", Param: "2"},
						{Tag: "max", Param: "50"},
					},
					ElementGoKind: "string",
				},
			},
		},
	}

	var stdBuf bytes.Buffer
	if err := codegen.WriteZodSchemas(&stdBuf, procs, defs, typemap.ZodStandard); err != nil {
		t.Fatal(err)
	}
	var miniBuf bytes.Buffer
	if err := codegen.WriteZodSchemas(&miniBuf, procs, defs, typemap.ZodMini); err != nil {
		t.Fatal(err)
	}

	stdOutput := stdBuf.String()
	miniOutput := miniBuf.String()

	// Standard: element constraints use method chains.
	if !strings.Contains(stdOutput, "z.array(z.string().min(2).max(50)).min(1)") {
		t.Errorf("standard output wrong.\nOutput:\n%s", stdOutput)
	}

	// Mini: element constraints use .check() syntax.
	if !strings.Contains(miniOutput, "z.string().check(z.minLength(2), z.maxLength(50))") {
		t.Errorf("mini output missing element .check() syntax.\nOutput:\n%s", miniOutput)
	}

	// Mini import.
	if !strings.Contains(miniOutput, `"zod/mini"`) {
		t.Errorf("mini output missing zod/mini import.\nOutput:\n%s", miniOutput)
	}
}

func TestZodIntegerUnion(t *testing.T) {
	// Integer unions must use z.union([z.literal(N), ...]) not z.enum().
	procs := []codegen.ProcEntry{
		{Path: "task.create", ProcType: "mutation", InputTS: "TaskInput", OutputTS: "void"},
	}
	defs := []typemap.TypeDef{
		{
			Name: "TaskInput",
			Kind: typemap.TypeDefInterface,
			Fields: []typemap.Field{
				{Name: "priority", Type: "Priority", GoKind: ""},
			},
		},
		{
			Name:         "Priority",
			Kind:         typemap.TypeDefUnion,
			UnionMembers: []string{"1", "2", "3"},
		},
	}

	var buf bytes.Buffer
	if err := codegen.WriteZodSchemas(&buf, procs, defs, typemap.ZodStandard); err != nil {
		t.Fatal(err)
	}
	output := buf.String()

	// Must use z.union + z.literal for integers.
	want := "z.union([z.literal(1), z.literal(2), z.literal(3)])"
	if !strings.Contains(output, want) {
		t.Errorf("integer union should use z.union([z.literal(N), ...]).\nwant: %s\nOutput:\n%s", want, output)
	}

	// Must NOT use z.enum for integers.
	if strings.Contains(output, "z.enum(") {
		t.Errorf("integer union should not use z.enum().\nOutput:\n%s", output)
	}
}

func TestZodStringUnionUnchanged(t *testing.T) {
	// String unions should still use z.enum().
	procs := []codegen.ProcEntry{
		{Path: "user.create", ProcType: "mutation", InputTS: "UserInput", OutputTS: "void"},
	}
	defs := []typemap.TypeDef{
		{
			Name: "UserInput",
			Kind: typemap.TypeDefInterface,
			Fields: []typemap.Field{
				{Name: "status", Type: "Status", GoKind: ""},
			},
		},
		{
			Name:         "Status",
			Kind:         typemap.TypeDefUnion,
			UnionMembers: []string{`"active"`, `"pending"`, `"banned"`},
		},
	}

	var buf bytes.Buffer
	if err := codegen.WriteZodSchemas(&buf, procs, defs, typemap.ZodStandard); err != nil {
		t.Fatal(err)
	}
	output := buf.String()

	want := `z.enum(["active", "pending", "banned"])`
	if !strings.Contains(output, want) {
		t.Errorf("string union should use z.enum().\nwant: %s\nOutput:\n%s", want, output)
	}
}

func TestZodCyclicLazy(t *testing.T) {
	// Cyclic types must use z.lazy() to avoid referencing the schema before it's defined.
	procs := []codegen.ProcEntry{
		{Path: "tree.create", ProcType: "mutation", InputTS: "TreeNode", OutputTS: "void"},
	}
	defs := []typemap.TypeDef{
		{
			Name: "TreeNode",
			Kind: typemap.TypeDefInterface,
			Fields: []typemap.Field{
				{Name: "label", Type: "string", GoKind: "string"},
				{Name: "children", Type: "TreeNode[]", GoKind: "slice"},
			},
		},
	}

	var buf bytes.Buffer
	if err := codegen.WriteZodSchemas(&buf, procs, defs, typemap.ZodStandard); err != nil {
		t.Fatal(err)
	}
	output := buf.String()

	// Must use z.lazy() wrapper.
	if !strings.Contains(output, "z.lazy(() => z.object(") {
		t.Errorf("cyclic type should use z.lazy().\nOutput:\n%s", output)
	}

	// Must have type annotation for type inference.
	if !strings.Contains(output, "z.ZodType<TreeNode>") {
		t.Errorf("cyclic type should have z.ZodType<T> annotation.\nOutput:\n%s", output)
	}

	// Must close lazy wrapper with })).
	if !strings.Contains(output, "}))") {
		t.Errorf("cyclic type should close z.lazy.\nOutput:\n%s", output)
	}

	// Self-reference should just use TreeNodeSchema.
	if !strings.Contains(output, "z.array(TreeNodeSchema)") {
		t.Errorf("cyclic field should reference TreeNodeSchema.\nOutput:\n%s", output)
	}
}

func TestZodNonCyclicUnchanged(t *testing.T) {
	// Non-cyclic types should NOT use z.lazy().
	procs := []codegen.ProcEntry{
		{Path: "user.create", ProcType: "mutation", InputTS: "UserInput", OutputTS: "void"},
	}
	defs := []typemap.TypeDef{
		{
			Name: "UserInput",
			Kind: typemap.TypeDefInterface,
			Fields: []typemap.Field{
				{Name: "name", Type: "string", GoKind: "string"},
			},
		},
	}

	var buf bytes.Buffer
	if err := codegen.WriteZodSchemas(&buf, procs, defs, typemap.ZodStandard); err != nil {
		t.Fatal(err)
	}
	output := buf.String()

	if strings.Contains(output, "z.lazy") {
		t.Errorf("non-cyclic type should not use z.lazy().\nOutput:\n%s", output)
	}
	if !strings.Contains(output, "export const UserInputSchema = z.object(") {
		t.Errorf("non-cyclic type should use plain z.object().\nOutput:\n%s", output)
	}
}

func TestZodMutualCycle(t *testing.T) {
	// Mutual recursion: A references B, B references A.
	procs := []codegen.ProcEntry{
		{Path: "item.create", ProcType: "mutation", InputTS: "NodeA", OutputTS: "void"},
	}
	defs := []typemap.TypeDef{
		{
			Name: "NodeA",
			Kind: typemap.TypeDefInterface,
			Fields: []typemap.Field{
				{Name: "b", Type: "NodeB", GoKind: ""},
			},
		},
		{
			Name: "NodeB",
			Kind: typemap.TypeDefInterface,
			Fields: []typemap.Field{
				{Name: "a", Type: "NodeA", GoKind: ""},
			},
		},
	}

	var buf bytes.Buffer
	if err := codegen.WriteZodSchemas(&buf, procs, defs, typemap.ZodStandard); err != nil {
		t.Fatal(err)
	}
	output := buf.String()

	// DFS visits NodeA first (sorted), discovers back-edge NodeB→NodeA,
	// so NodeB gets z.lazy. NodeA is emitted second as plain z.object.
	if !strings.Contains(output, "NodeBSchema: z.ZodType<NodeB> = z.lazy(") {
		t.Errorf("NodeB (back-edge) should use z.lazy with type annotation.\nOutput:\n%s", output)
	}
	if !strings.Contains(output, "NodeASchema = z.object(") {
		t.Errorf("NodeA should be plain z.object.\nOutput:\n%s", output)
	}

	// Exactly one z.lazy and one plain z.object.
	lazyCount := strings.Count(output, "z.lazy(")
	objectCount := strings.Count(output, "= z.object(")
	if lazyCount != 1 || objectCount != 1 {
		t.Errorf("expected 1 z.lazy + 1 z.object, got %d + %d.\nOutput:\n%s", lazyCount, objectCount, output)
	}
}

func TestZodOmitemptyE2E(t *testing.T) {
	// End-to-end: Go source → analysis → codegen → Zod output.
	// Uses the omitempty fixture with validate:"omitempty,len=6" and validate:"omitempty,email".
	dir := testdataDir("omitempty")
	result, err := analysis.Analyze([]string{"."}, dir)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	genResult, err := codegen.Generate(&buf, result, result.TypeMetas)
	if err != nil {
		t.Fatal(err)
	}

	var zodBuf bytes.Buffer
	if err := codegen.WriteZodSchemas(&zodBuf, genResult.Procs, genResult.Defs, typemap.ZodStandard); err != nil {
		t.Fatal(err)
	}
	zodOutput := zodBuf.String()

	t.Run("ConfirmTOTPInputSchema present", func(t *testing.T) {
		if !strings.Contains(zodOutput, "ConfirmTOTPInputSchema") {
			t.Fatalf("missing ConfirmTOTPInputSchema.\nOutput:\n%s", zodOutput)
		}
	})

	t.Run("omitempty+len fields accept empty strings", func(t *testing.T) {
		want := `z.string().length(6).or(z.literal(""))`
		count := strings.Count(zodOutput, want)
		if count != 2 {
			t.Errorf("expected %q to appear 2 times (code + current_code), got %d.\nOutput:\n%s", want, count, zodOutput)
		}
	})

	t.Run("required field unchanged", func(t *testing.T) {
		if strings.Contains(zodOutput, `name: z.string().min(1).or(`) {
			t.Errorf("required name field should not have .or() wrapping.\nOutput:\n%s", zodOutput)
		}
	})

	t.Run("omitempty+email allows empty string", func(t *testing.T) {
		if !strings.Contains(zodOutput, `z.email().or(z.literal(""))`) {
			t.Errorf("backup_email should have omitempty wrapping on format base.\nOutput:\n%s", zodOutput)
		}
	})

	t.Run("required+email unchanged", func(t *testing.T) {
		// primary_email has validate:"required,email" — no .or() wrapping.
		// Both emails use z.email(), but only backup_email has .or().
		orCount := strings.Count(zodOutput, `z.email().or(`)
		if orCount != 1 {
			t.Errorf("expected exactly 1 z.email().or() (backup_email), got %d.\nOutput:\n%s", orCount, zodOutput)
		}
	})

	t.Run("omitempty+optional pointer field", func(t *testing.T) {
		// nickname is *string with json:"nickname,omitempty" validate:"omitempty,min=3,max=30"
		// Should have both .or(z.literal("")) AND .optional().
		want := `z.string().min(3).max(30).or(z.literal("")).optional()`
		if !strings.Contains(zodOutput, want) {
			t.Errorf("nickname should have both .or() and .optional().\nwant: %s\nOutput:\n%s", want, zodOutput)
		}
	})
}

func TestZodOmitemptyDirect(t *testing.T) {
	// Simulates SetupTOTPRequest: code and current_code have validate:"omitempty,len=6".
	// Both should accept empty strings alongside 6-char strings.
	procs := []codegen.ProcEntry{
		{Path: "auth.setupTotp", ProcType: "mutation", InputTS: "SetupTOTPRequest", OutputTS: "void"},
	}
	defs := []typemap.TypeDef{
		{
			Name: "SetupTOTPRequest",
			Kind: typemap.TypeDefInterface,
			Fields: []typemap.Field{
				{
					Name:              "code",
					Type:              "string",
					GoKind:            "string",
					ValidateOmitempty: true,
					Validate: []typemap.ValidateRule{
						{Tag: "omitempty"},
						{Tag: "len", Param: "6"},
					},
				},
				{
					Name:              "current_code",
					Type:              "string",
					GoKind:            "string",
					ValidateOmitempty: true,
					Validate: []typemap.ValidateRule{
						{Tag: "omitempty"},
						{Tag: "len", Param: "6"},
					},
				},
				{
					Name:   "secret",
					Type:   "string",
					GoKind: "string",
					Validate: []typemap.ValidateRule{
						{Tag: "required"},
					},
				},
			},
		},
	}

	t.Run("standard", func(t *testing.T) {
		var buf bytes.Buffer
		if err := codegen.WriteZodSchemas(&buf, procs, defs, typemap.ZodStandard); err != nil {
			t.Fatal(err)
		}
		output := buf.String()

		// omitempty fields should accept empty strings.
		wantOmitempty := `z.string().length(6).or(z.literal(""))`
		count := strings.Count(output, wantOmitempty)
		if count != 2 {
			t.Errorf("expected %q to appear 2 times (code + current_code), got %d.\nOutput:\n%s", wantOmitempty, count, output)
		}

		// Non-omitempty field should NOT have .or().
		if strings.Contains(output, `secret: z.string().or(`) {
			t.Errorf("secret field should not have omitempty .or() wrapping.\nOutput:\n%s", output)
		}
	})

	t.Run("mini", func(t *testing.T) {
		var buf bytes.Buffer
		if err := codegen.WriteZodSchemas(&buf, procs, defs, typemap.ZodMini); err != nil {
			t.Fatal(err)
		}
		output := buf.String()

		wantOmitempty := `z.string().check(z.length(6)).or(z.literal(""))`
		count := strings.Count(output, wantOmitempty)
		if count != 2 {
			t.Errorf("expected %q to appear 2 times, got %d.\nOutput:\n%s", wantOmitempty, count, output)
		}
	})
}

func TestZodOmitemptyFormatBase(t *testing.T) {
	// Format bases (email, uuid) with omitempty should also allow empty strings.
	procs := []codegen.ProcEntry{
		{Path: "user.update", ProcType: "mutation", InputTS: "UpdateInput", OutputTS: "void"},
	}
	defs := []typemap.TypeDef{
		{
			Name: "UpdateInput",
			Kind: typemap.TypeDefInterface,
			Fields: []typemap.Field{
				{
					Name:              "backup_email",
					Type:              "string",
					GoKind:            "string",
					ValidateOmitempty: true,
					Validate: []typemap.ValidateRule{
						{Tag: "omitempty"},
						{Tag: "email"},
					},
				},
				{
					Name:   "primary_email",
					Type:   "string",
					GoKind: "string",
					Validate: []typemap.ValidateRule{
						{Tag: "required"},
						{Tag: "email"},
					},
				},
			},
		},
	}

	var buf bytes.Buffer
	if err := codegen.WriteZodSchemas(&buf, procs, defs, typemap.ZodStandard); err != nil {
		t.Fatal(err)
	}
	output := buf.String()

	// backup_email: omitempty + email → z.email().or(z.literal(""))
	if !strings.Contains(output, `z.email().or(z.literal(""))`) {
		t.Errorf("backup_email should have omitempty wrapping.\nOutput:\n%s", output)
	}

	// primary_email: required + email → z.email() (no .or())
	// Count: z.email() should appear twice (once with .or, once without).
	emailCount := strings.Count(output, "z.email()")
	if emailCount != 2 {
		t.Errorf("expected z.email() 2 times, got %d.\nOutput:\n%s", emailCount, output)
	}
}

func TestRouterInputsOutputs(t *testing.T) {
	output := generateFromFixture(t, "basic")

	t.Run("RouterInputs uses inferRouterInputs", func(t *testing.T) {
		if !strings.Contains(output, "export type RouterInputs = inferRouterInputs<AppRouter>;") {
			t.Fatalf("output missing RouterInputs declaration.\nOutput:\n%s", output)
		}
	})

	t.Run("RouterOutputs uses inferRouterOutputs", func(t *testing.T) {
		if !strings.Contains(output, "export type RouterOutputs = inferRouterOutputs<AppRouter>;") {
			t.Fatalf("output missing RouterOutputs declaration.\nOutput:\n%s", output)
		}
	})

	t.Run("AppRouterRecord nests procedures under namespace", func(t *testing.T) {
		// All basic fixture procs are under "user." — verify the tree structure
		// places them inside a `user: { ... }` namespace block, not at the top level.
		idx := strings.Index(output, "type AppRouterRecord = {")
		if idx == -1 {
			t.Fatalf("missing AppRouterRecord.\nOutput:\n%s", output)
		}
		record := output[idx:]

		// "user:" should appear as a namespace key.
		if !strings.Contains(record, "user: {") {
			t.Errorf("procedures should be nested under user namespace.\nRecord:\n%s", record)
		}

		// Procedure names should appear as keys inside the namespace, not at top level.
		for _, proc := range []string{"getById:", "listUsers:", "createUser:", "onCreated:"} {
			if !strings.Contains(record, proc) {
				t.Errorf("missing procedure key %s in AppRouterRecord.\nRecord:\n%s", proc, record)
			}
		}
	})
}

func TestAppRouterImportStatement(t *testing.T) {
	output := generateFromFixture(t, "basic")

	t.Run("import from @trpc/server", func(t *testing.T) {
		if !strings.Contains(output, "from '@trpc/server';") {
			t.Fatalf("output missing @trpc/server import.\nOutput:\n%s", output)
		}
	})

	t.Run("imports all used procedure types", func(t *testing.T) {
		// The basic fixture uses query, mutation, and subscription.
		for _, typ := range []string{"TRPCQueryProcedure", "TRPCMutationProcedure", "TRPCSubscriptionProcedure"} {
			if !strings.Contains(output, typ) {
				t.Errorf("import missing %s.\nOutput:\n%s", typ, output)
			}
		}
	})

	t.Run("imports router plumbing types", func(t *testing.T) {
		for _, typ := range []string{"TRPCRouterDef", "TRPCRouterCaller", "inferRouterInputs", "inferRouterOutputs"} {
			if !strings.Contains(output, typ) {
				t.Errorf("import missing %s.\nOutput:\n%s", typ, output)
			}
		}
	})

	t.Run("import is type-only", func(t *testing.T) {
		if !strings.Contains(output, "import type {") {
			t.Errorf("import should be type-only.\nOutput:\n%s", output)
		}
	})
}

func TestAppRouterHelperTypes(t *testing.T) {
	output := generateFromFixture(t, "basic")

	t.Run("$ErrorShape emitted", func(t *testing.T) {
		want := "type $ErrorShape = { code: number; message: string; data: { code: string; httpStatus: number; path?: string } };"
		if !strings.Contains(output, want) {
			t.Errorf("output missing $ErrorShape.\nOutput:\n%s", output)
		}
	})

	t.Run("$RootTypes emitted", func(t *testing.T) {
		want := "type $RootTypes = { ctx: object; meta: object; errorShape: $ErrorShape; transformer: false };"
		if !strings.Contains(output, want) {
			t.Errorf("output missing $RootTypes.\nOutput:\n%s", output)
		}
	})

	t.Run("AppRouter uses TRPCRouterDef", func(t *testing.T) {
		if !strings.Contains(output, "TRPCRouterDef<$RootTypes, AppRouterRecord>") {
			t.Errorf("AppRouter should use TRPCRouterDef.\nOutput:\n%s", output)
		}
	})

	t.Run("AppRouter uses TRPCRouterCaller", func(t *testing.T) {
		if !strings.Contains(output, "TRPCRouterCaller<$RootTypes, AppRouterRecord>") {
			t.Errorf("AppRouter should use TRPCRouterCaller.\nOutput:\n%s", output)
		}
	})

	t.Run("no inline any or unknown in plumbing", func(t *testing.T) {
		// The whole point of this refactor: no any/unknown in the structural types.
		// Scan everything from AppRouterRecord onwards (procedure tree, $ErrorShape,
		// $RootTypes, AppRouter block, RouterInputs/Outputs).
		marker := "type AppRouterRecord = "
		idx := strings.Index(output, marker)
		if idx == -1 {
			t.Fatal("missing AppRouterRecord")
		}
		plumbing := output[idx:]
		for line := range strings.SplitSeq(plumbing, "\n") {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" || trimmed == "};" || trimmed == "}" {
				continue
			}
			if strings.Contains(trimmed, " any") || strings.HasSuffix(trimmed, ":any;") {
				t.Errorf("plumbing contains 'any': %s", trimmed)
			}
			if strings.Contains(trimmed, "unknown") {
				t.Errorf("plumbing contains 'unknown': %s", trimmed)
			}
		}
	})
}

func TestWriteAppRouterSubscriptionOnly(t *testing.T) {
	procs := []codegen.ProcEntry{
		{Path: "events.onMessage", ProcType: "subscription", InputTS: "void", OutputTS: "Message"},
	}
	var buf bytes.Buffer
	if err := codegen.WriteAppRouter(&buf, procs, nil); err != nil {
		t.Fatal(err)
	}
	output := buf.String()

	// Should import TRPCSubscriptionProcedure but NOT query/mutation.
	if !strings.Contains(output, "TRPCSubscriptionProcedure") {
		t.Errorf("should import TRPCSubscriptionProcedure.\nOutput:\n%s", output)
	}
	if strings.Contains(output, "TRPCQueryProcedure") {
		t.Errorf("should not import TRPCQueryProcedure for subscription-only router.\nOutput:\n%s", output)
	}
	if strings.Contains(output, "TRPCMutationProcedure") {
		t.Errorf("should not import TRPCMutationProcedure for subscription-only router.\nOutput:\n%s", output)
	}

	// Should emit $Subscription but not $Query/$Mutation.
	if !strings.Contains(output, "$Subscription<void, Message>") {
		t.Errorf("should emit $Subscription.\nOutput:\n%s", output)
	}
	if strings.Contains(output, "$Query") {
		t.Errorf("should not emit $Query for subscription-only router.\nOutput:\n%s", output)
	}
	if strings.Contains(output, "$Mutation") {
		t.Errorf("should not emit $Mutation for subscription-only router.\nOutput:\n%s", output)
	}
}

func TestWriteAppRouterNoProcs(t *testing.T) {
	var buf bytes.Buffer
	if err := codegen.WriteAppRouter(&buf, nil, nil); err != nil {
		t.Fatal(err)
	}
	output := buf.String()

	if strings.Contains(output, "TRPCQueryProcedure") {
		t.Errorf("TRPCQueryProcedure should not be imported with no procedures.\nOutput:\n%s", output)
	}
	if strings.Contains(output, "$Query") {
		t.Errorf("$Query should not be emitted with no procedures.\nOutput:\n%s", output)
	}
	if strings.Contains(output, "RouterInputs") {
		t.Errorf("RouterInputs should not be emitted with no procedures.\nOutput:\n%s", output)
	}
	if strings.Contains(output, "RouterOutputs") {
		t.Errorf("RouterOutputs should not be emitted with no procedures.\nOutput:\n%s", output)
	}
	// AppRouter should still be emitted.
	if !strings.Contains(output, "export type AppRouter =") {
		t.Errorf("AppRouter should always be emitted.\nOutput:\n%s", output)
	}
}

func TestWriteAppRouterOnlyEmitsUsedProcTypes(t *testing.T) {
	procs := []codegen.ProcEntry{
		{Path: "user.get", ProcType: "query", InputTS: "string", OutputTS: "User"},
	}
	var buf bytes.Buffer
	if err := codegen.WriteAppRouter(&buf, procs, nil); err != nil {
		t.Fatal(err)
	}
	output := buf.String()

	if !strings.Contains(output, "TRPCQueryProcedure") {
		t.Errorf("TRPCQueryProcedure should be imported when query procs exist.\nOutput:\n%s", output)
	}
	if !strings.Contains(output, "$Query") {
		t.Errorf("$Query should be emitted for query proc.\nOutput:\n%s", output)
	}
	if strings.Contains(output, "TRPCMutationProcedure") {
		t.Errorf("TRPCMutationProcedure should not be imported when no mutation procs exist.\nOutput:\n%s", output)
	}
	if strings.Contains(output, "$Mutation") {
		t.Errorf("$Mutation should not be emitted when no mutation procs exist.\nOutput:\n%s", output)
	}
	if strings.Contains(output, "$Subscription") {
		t.Errorf("$Subscription should not be emitted when no subscription procs exist.\nOutput:\n%s", output)
	}
}

func TestPropertyQuotingInInterface(t *testing.T) {
	procs := []codegen.ProcEntry{
		{Path: "item.get", ProcType: "query", InputTS: "void", OutputTS: "Item"},
	}
	defs := []typemap.TypeDef{
		{
			Name: "Item",
			Kind: typemap.TypeDefInterface,
			Fields: []typemap.Field{
				{Name: "normal_name", Type: "string", GoKind: "string"},
				{Name: "hyphen-name", Type: "string", GoKind: "string"},
				{Name: "123start", Type: "number", GoKind: "int"},
				{Name: "has space", Type: "string", GoKind: "string"},
			},
		},
	}

	var buf bytes.Buffer
	if err := codegen.WriteAppRouter(&buf, procs, defs); err != nil {
		t.Fatal(err)
	}
	output := buf.String()

	// Normal identifier — no quotes.
	if !containsLine(output, "normal_name: string;") {
		t.Errorf("normal identifier should not be quoted.\nOutput:\n%s", output)
	}

	// Hyphenated name — must be quoted.
	if !containsLine(output, `"hyphen-name": string;`) {
		t.Errorf("hyphenated name should be quoted.\nOutput:\n%s", output)
	}

	// Starts with digit — must be quoted.
	if !containsLine(output, `"123start": number;`) {
		t.Errorf("digit-starting name should be quoted.\nOutput:\n%s", output)
	}

	// Contains space — must be quoted.
	if !containsLine(output, `"has space": string;`) {
		t.Errorf("name with space should be quoted.\nOutput:\n%s", output)
	}
}

func TestPropertyQuotingInTreeNodes(t *testing.T) {
	procs := []codegen.ProcEntry{
		{Path: "my-ns.get", ProcType: "query", InputTS: "void", OutputTS: "string"},
	}

	var buf bytes.Buffer
	if err := codegen.WriteAppRouter(&buf, procs, nil); err != nil {
		t.Fatal(err)
	}
	output := buf.String()

	// Namespace key with hyphen must be quoted in AppRouterRecord.
	if !strings.Contains(output, `"my-ns"`) {
		t.Errorf("hyphenated namespace should be quoted in tree.\nOutput:\n%s", output)
	}
}

func TestLeafAndNamespaceCollision(t *testing.T) {
	// "user" is both a leaf procedure and a namespace containing "user.get".
	procs := []codegen.ProcEntry{
		{Path: "user", ProcType: "mutation", InputTS: "string", OutputTS: "string"},
		{Path: "user.get", ProcType: "query", InputTS: "void", OutputTS: "User"},
	}

	var buf bytes.Buffer
	if err := codegen.WriteAppRouter(&buf, procs, nil); err != nil {
		t.Fatal(err)
	}
	output := buf.String()

	// The "user" node should be an intersection of the leaf type and the namespace.
	if !strings.Contains(output, "$Mutation<string, string> & {") {
		t.Errorf("leaf+namespace should emit intersection type.\nOutput:\n%s", output)
	}

	// The child procedure should still appear.
	if !strings.Contains(output, "$Query<void, User>") {
		t.Errorf("child procedure should be emitted within namespace.\nOutput:\n%s", output)
	}
}

func TestZodUnsupportedComment(t *testing.T) {
	procs := []codegen.ProcEntry{
		{Path: "create", ProcType: "mutation", InputTS: "RangeInput", OutputTS: "string"},
	}
	defs := []typemap.TypeDef{
		{
			Name: "RangeInput",
			Kind: typemap.TypeDefInterface,
			Fields: []typemap.Field{
				{Name: "minVal", Type: "number", GoKind: "float64",
					Validate: []typemap.ValidateRule{{Tag: "required"}, {Tag: "gte", Param: "0"}}},
				{Name: "maxVal", Type: "number", GoKind: "float64",
					Validate: []typemap.ValidateRule{{Tag: "required"}, {Tag: "gte", Param: "0"}}},
				{Name: "label", Type: "string", GoKind: "string",
					Validate: []typemap.ValidateRule{{Tag: "required"}, {Tag: "custom_check"}},
					UnsupportedZod: []typemap.ValidateRule{{Tag: "custom_check"}}},
			},
			Refinements: []typemap.Refinement{
				{Field: "maxVal", Op: ">=", OtherField: "minVal", Tag: "gtefield"},
			},
		},
	}

	var buf bytes.Buffer
	if err := codegen.WriteZodSchemas(&buf, procs, defs, typemap.ZodStandard); err != nil {
		t.Fatal(err)
	}
	output := buf.String()
	t.Log(output)

	// minVal has no unsupported tags — no comment.
	if strings.Contains(output, "minVal: z.float64().gte(0), /*") {
		t.Error("minVal should not have unsupported comment")
	}

	// maxVal should NOT have unsupported comment — gtefield now generates .refine().
	if strings.Contains(output, "/* unsupported: gtefield") {
		t.Error("gtefield should not appear as unsupported")
	}

	// Should have .refine() for gtefield.
	if !strings.Contains(output, ".refine(") {
		t.Errorf("expected .refine() for cross-field validation.\nOutput:\n%s", output)
	}
	if !strings.Contains(output, "data.maxVal >= data.minVal") {
		t.Errorf("expected refine callback with correct field names.\nOutput:\n%s", output)
	}

	// label should have the custom_check comment.
	if !strings.Contains(output, "/* unsupported: custom_check */") {
		t.Errorf("label should have unsupported comment.\nOutput:\n%s", output)
	}
}

func TestZodOmitField(t *testing.T) {
	procs := []codegen.ProcEntry{
		{Path: "update", ProcType: "mutation", InputTS: "UpdateInput", OutputTS: "string"},
	}
	defs := []typemap.TypeDef{
		{
			Name: "UpdateInput",
			Kind: typemap.TypeDefInterface,
			Fields: []typemap.Field{
				{Name: "id", Type: "string", GoKind: "string", ZodOmit: true},
				{Name: "name", Type: "string", GoKind: "string"},
				{Name: "active", Type: "boolean", GoKind: "bool"},
			},
		},
	}

	var buf bytes.Buffer
	if err := codegen.WriteZodSchemas(&buf, procs, defs, typemap.ZodStandard); err != nil {
		t.Fatal(err)
	}
	output := buf.String()
	t.Log(output)

	// id should NOT appear in the schema.
	if strings.Contains(output, "id: z.") {
		t.Errorf("omitted field 'id' should not appear in Zod schema.\nOutput:\n%s", output)
	}

	// name and active should appear.
	if !strings.Contains(output, "name: z.string(),") {
		t.Errorf("non-omitted field 'name' should appear.\nOutput:\n%s", output)
	}
	if !strings.Contains(output, "active: z.boolean(),") {
		t.Errorf("non-omitted field 'active' should appear.\nOutput:\n%s", output)
	}
}

func TestZodOmitSkipsRefinement(t *testing.T) {
	procs := []codegen.ProcEntry{
		{Path: "update", ProcType: "mutation", InputTS: "RangeInput", OutputTS: "string"},
	}
	defs := []typemap.TypeDef{
		{
			Name: "RangeInput",
			Kind: typemap.TypeDefInterface,
			Fields: []typemap.Field{
				{Name: "id", Type: "string", GoKind: "string", ZodOmit: true},
				{Name: "min_val", Type: "number", GoKind: "int32"},
				{Name: "max_val", Type: "number", GoKind: "int32"},
			},
			Refinements: []typemap.Refinement{
				// This one references a non-omitted field pair → should be emitted.
				{Field: "max_val", Op: ">=", OtherField: "min_val", Tag: "gtefield"},
				// This one references the omitted "id" field → should be skipped.
				{Field: "min_val", Op: ">=", OtherField: "id", Tag: "gtefield"},
			},
		},
	}

	var buf bytes.Buffer
	if err := codegen.WriteZodSchemas(&buf, procs, defs, typemap.ZodStandard); err != nil {
		t.Fatal(err)
	}
	output := buf.String()
	t.Log(output)

	// Refinement for max_val >= min_val should exist.
	if !strings.Contains(output, "data.max_val >= data.min_val") {
		t.Errorf("expected refinement for non-omitted fields.\nOutput:\n%s", output)
	}

	// Refinement referencing omitted "id" should be skipped.
	if strings.Count(output, ".refine(") != 1 {
		t.Errorf("expected exactly 1 .refine() (skipping one that references omitted field), got %d.\nOutput:\n%s",
			strings.Count(output, ".refine("), output)
	}
}

func TestZodOmitE2E(t *testing.T) {
	// End-to-end: Go source → analysis → codegen → Zod output.
	// Verifies zod_omit tag is picked up through the AST path.
	dir := testdataDir("zodomit")
	result, err := analysis.Analyze([]string{"."}, dir)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	genResult, err := codegen.Generate(&buf, result, result.TypeMetas)
	if err != nil {
		t.Fatal(err)
	}

	var zodBuf bytes.Buffer
	if err := codegen.WriteZodSchemas(&zodBuf, genResult.Procs, genResult.Defs, typemap.ZodStandard); err != nil {
		t.Fatal(err)
	}
	zodOutput := zodBuf.String()
	t.Log(zodOutput)

	t.Run("omitted field excluded", func(t *testing.T) {
		if strings.Contains(zodOutput, "id: z.") {
			t.Errorf("zod_omit field 'id' should not appear in Zod schema.\nOutput:\n%s", zodOutput)
		}
	})

	t.Run("non-omitted fields present", func(t *testing.T) {
		if !strings.Contains(zodOutput, "name: z.string().min(1),") {
			t.Errorf("field 'name' should appear with constraints.\nOutput:\n%s", zodOutput)
		}
		if !strings.Contains(zodOutput, "active: z.boolean(),") {
			t.Errorf("field 'active' should appear.\nOutput:\n%s", zodOutput)
		}
	})

	t.Run("refinement referencing omitted field skipped", func(t *testing.T) {
		// UpdateWithRefine has gtefield=ID (omitted) and gtefield=MinVal (kept).
		if strings.Count(zodOutput, ".refine(") != 1 {
			t.Errorf("expected 1 .refine() (skipping one referencing omitted 'id'), got %d.\nOutput:\n%s",
				strings.Count(zodOutput, ".refine("), zodOutput)
		}
		if !strings.Contains(zodOutput, "data.max_val >= data.min_val") {
			t.Errorf("expected refinement for non-omitted fields.\nOutput:\n%s", zodOutput)
		}
	})

	t.Run("TS interface still has omitted field", func(t *testing.T) {
		tsOutput := buf.String()
		if !strings.Contains(tsOutput, "id: string") {
			t.Errorf("TS interface should still include omitted field.\nOutput:\n%s", tsOutput)
		}
	})
}

func generateORPCFromFixture(t *testing.T, name string) string {
	t.Helper()
	dir := testdataDir(name)
	result, err := analysis.Analyze([]string{"."}, dir)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if _, err := codegen.GenerateORPC(&buf, result, result.TypeMetas); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func TestGenerateORPCFromBasicFixture(t *testing.T) {
	output := generateORPCFromFixture(t, "basic")

	checks := []string{
		"@orpc/client",
		"export interface User {",
		"export interface GetUserByIdInput {",
		"export interface CreateUserInput {",
		"export type RouterClient =",
		"$Proc<GetUserByIdInput, User>",
		"$Proc<void, User[]>",
		"$Proc<CreateUserInput, User>",
		"$Sub<void, User>",
		"user: {",
	}
	for _, c := range checks {
		if !strings.Contains(output, c) {
			t.Errorf("output missing %q\nOutput:\n%s", c, output)
		}
	}

	// Must NOT contain tRPC-specific types.
	trpcOnly := []string{
		"@trpc/server",
		"TRPCQueryProcedure",
		"TRPCMutationProcedure",
		"AppRouter",
		"TRPCRouterDef",
	}
	for _, c := range trpcOnly {
		if strings.Contains(output, c) {
			t.Errorf("oRPC output should NOT contain tRPC type %q", c)
		}
	}
}
