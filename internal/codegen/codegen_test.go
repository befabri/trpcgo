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

func TestRouterInputsOutputs(t *testing.T) {
	output := generateFromFixture(t, "basic")

	t.Run("RouterInputs is present", func(t *testing.T) {
		if !strings.Contains(output, "export type RouterInputs = {") {
			t.Fatalf("output missing RouterInputs declaration.\nOutput:\n%s", output)
		}
	})

	t.Run("RouterOutputs is present", func(t *testing.T) {
		if !strings.Contains(output, "export type RouterOutputs = {") {
			t.Fatalf("output missing RouterOutputs declaration.\nOutput:\n%s", output)
		}
	})

	t.Run("RouterInputs has user namespace", func(t *testing.T) {
		// Extract the RouterInputs block.
		idx := strings.Index(output, "export type RouterInputs = {")
		if idx < 0 {
			t.Fatal("RouterInputs not found")
		}
		// Get from declaration to end of output (safe slice).
		chunk := output[idx:]
		if !strings.Contains(chunk, "user:") {
			t.Errorf("RouterInputs missing user namespace.\nChunk:\n%s", chunk)
		}
	})

	t.Run("RouterInputs contains procedure input types", func(t *testing.T) {
		// Extract the section between RouterInputs and RouterOutputs.
		inputIdx := strings.Index(output, "export type RouterInputs = {")
		outputIdx := strings.Index(output, "export type RouterOutputs = {")
		if inputIdx < 0 {
			t.Fatal("RouterInputs not found")
		}
		end := len(output)
		if outputIdx > inputIdx {
			end = outputIdx
		}
		chunk := output[inputIdx:end]
		// user.getById has input GetUserByIdInput.
		if !strings.Contains(chunk, "getById: GetUserByIdInput") {
			t.Errorf("RouterInputs missing getById input.\nChunk:\n%s", chunk)
		}
		// user.createUser has input CreateUserInput.
		if !strings.Contains(chunk, "createUser: CreateUserInput") {
			t.Errorf("RouterInputs missing createUser input.\nChunk:\n%s", chunk)
		}
		// user.listUsers has void input.
		if !strings.Contains(chunk, "listUsers: void") {
			t.Errorf("RouterInputs missing listUsers void input.\nChunk:\n%s", chunk)
		}
	})

	t.Run("RouterOutputs contains procedure output types", func(t *testing.T) {
		idx := strings.Index(output, "export type RouterOutputs = {")
		if idx < 0 {
			t.Fatal("RouterOutputs not found")
		}
		chunk := output[idx:]
		// user.getById returns User.
		if !strings.Contains(chunk, "getById: User") {
			t.Errorf("RouterOutputs missing getById output.\nChunk:\n%s", chunk)
		}
		// user.listUsers returns User[].
		if !strings.Contains(chunk, "listUsers: User[]") {
			t.Errorf("RouterOutputs missing listUsers output.\nChunk:\n%s", chunk)
		}
		// user.createUser returns User.
		if !strings.Contains(chunk, "createUser: User") {
			t.Errorf("RouterOutputs missing createUser output.\nChunk:\n%s", chunk)
		}
	})
}

func TestWriteAppRouterNoProcs(t *testing.T) {
	var buf bytes.Buffer
	if err := codegen.WriteAppRouter(&buf, nil, nil); err != nil {
		t.Fatal(err)
	}
	output := buf.String()

	if strings.Contains(output, "$Procedure") {
		t.Errorf("$Procedure should not be emitted with no procedures.\nOutput:\n%s", output)
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

	if !strings.Contains(output, "$Procedure") {
		t.Errorf("$Procedure should be emitted when procs exist.\nOutput:\n%s", output)
	}
	if !strings.Contains(output, "$Query") {
		t.Errorf("$Query should be emitted for query proc.\nOutput:\n%s", output)
	}
	if strings.Contains(output, "$Mutation") {
		t.Errorf("$Mutation should not be emitted when no mutation procs exist.\nOutput:\n%s", output)
	}
	if strings.Contains(output, "$Subscription") {
		t.Errorf("$Subscription should not be emitted when no subscription procs exist.\nOutput:\n%s", output)
	}
}
