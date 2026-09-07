package trpcgo_test

import (
	"bytes"
	"encoding/json"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/befabri/trpcgo"
	"github.com/befabri/trpcgo/internal/codegen"
	"github.com/befabri/trpcgo/internal/typemap"
	"github.com/befabri/trpcgo/testdata/typegraph"
	"github.com/befabri/trpcgo/testdata/validationcontract"
	"github.com/befabri/trpcgo/zodconfig"
)

// zodContract runs a fixture corpus through generated schemas and compares
// every case with its Go acceptance expectation.
type zodContract struct {
	cases  []validationcontract.Case
	config zodconfig.Config
	// files are extra module sources written beside schemas.ts.
	files map[string]string
	// assertions are appended to the validation script. They can use the
	// generated module as schemas and Assert and Equal from ./assertions.
	assertions []string
	// reflectionOnly skips the static mapper when the property under test is
	// decided in the schema writer, which both mappers share.
	reflectionOnly bool
}

func testZodValidationCases(t *testing.T, cases []validationcontract.Case, assertions ...string) {
	t.Helper()
	runZodContract(t, zodContract{cases: cases, assertions: assertions})
}

func runZodContract(t *testing.T, c zodContract) {
	t.Helper()
	if len(c.cases) == 0 {
		t.Fatal("validation contract has no cases")
	}
	t.Logf("%d independent Go/Zod cases", len(c.cases))
	seen := map[string]bool{}
	for _, tc := range c.cases {
		if seen[tc.Name] || tc.Name == "" {
			t.Fatalf("duplicate or empty contract case name: %q", tc.Name)
		}
		seen[tc.Name] = true
		if !json.Valid([]byte(tc.JSON)) || validationcontract.NewInput(tc.Type) == nil {
			t.Fatalf("%s: invalid fixture JSON or unregistered Go type %q", tc.Name, tc.Type)
		}
	}

	encoded, err := json.Marshal(c.cases)
	if err != nil {
		t.Fatal(err)
	}
	script := `import * as schemas from './schemas';
import type { Assert, Equal } from './assertions';
type Schema = { safeParse(input: unknown): { success: true; data: unknown } | { success: false } };
const exports: Record<string, Schema | ((raw: string) => unknown)> = schemas;
const parse = exports.parseGoJSON;
const parseJSON = typeof parse === 'function' ? parse : JSON.parse;
const cases = ` + string(encoded) + `;
const results = cases.map(test => {
  const schema = exports[test.type + 'Schema'];
  if (!schema || typeof schema === 'function') throw new Error('Missing schema for ' + test.type);
  try {
    const result = schema.safeParse(parseJSON(test.json));
    return { success: result.success, data: result.success ? result.data : null };
  } catch (error) {
    // Catch per input so one thrown refinement cannot hide the other cases.
    // The Go assertions below always fail for a thrown exception.
    return { success: false, data: null, exception: String(error) };
  }
});
` + strings.Join(c.assertions, "\n") + "\nconsole.log(JSON.stringify(results));\n"

	mappers := []bool{false, true}
	var staticPkg *types.Package
	if c.reflectionOnly {
		mappers = mappers[:1]
	} else {
		staticPkg = validationContractTypes(t)
	}
	for _, mini := range []bool{false, true} {
		for _, static := range mappers {
			t.Run(zodStyleName(mini)+"/"+mapperName(static), func(t *testing.T) {
				dir := t.TempDir()
				symlinkNodeModules(t, dir)
				copyTypeScriptAssertions(t, dir)
				writeTestSources(t, dir, c.files)
				output := filepath.Join(dir, "schemas.ts")
				if static {
					writeStaticValidationContract(t, staticPkg, c.cases, mini, output, codegen.ZodOptions{Validation: c.config})
				} else {
					r := trpcgo.NewRouter(trpcgo.WithZodMini(mini), trpcgo.WithZodValidation(c.config))
					t.Cleanup(func() { _ = r.Close() })
					validationcontract.RegisterCases(r, c.cases)
					if err := r.GenerateZod(output); err != nil {
						t.Fatal(err)
					}
				}
				writeTestSources(t, dir, map[string]string{"validate.ts": script})
				// Check both independently: a compile failure should not hide a
				// runtime exception or acceptance mismatch in the same module.
				t.Run("compile", func(t *testing.T) {
					assertTypeScriptCompiles(t, dir, "schemas.ts", "validate.ts")
				})
				t.Run("runtime", func(t *testing.T) {
					data := runTypeScript(t, dir, "validate.ts")
					var results []struct {
						Success   bool            `json:"success"`
						Data      json.RawMessage `json:"data"`
						Exception *string         `json:"exception"`
					}
					if err := json.Unmarshal(data, &results); err != nil {
						t.Fatalf("invalid contract results: %v\n%s", err, data)
					}
					if len(results) != len(c.cases) {
						t.Fatalf("executed %d cases, want %d", len(results), len(c.cases))
					}
					for i, tc := range c.cases {
						t.Run(tc.Name, func(t *testing.T) {
							if results[i].Exception != nil {
								t.Fatalf("%s %s: safeParse threw: %s", tc.Type, tc.JSON, *results[i].Exception)
							}
							if results[i].Success != tc.Valid {
								t.Fatalf("%s %s: Zod accepted=%v, Go contract=%v", tc.Type, tc.JSON, results[i].Success, tc.Valid)
							}
							if tc.Valid && results[i].Success {
								before, after := validationcontract.NewInput(tc.Type), validationcontract.NewInput(tc.Type)
								if err := json.Unmarshal([]byte(tc.JSON), before); err != nil {
									t.Fatal(err)
								}
								if err := json.Unmarshal(results[i].Data, after); err != nil {
									t.Fatalf("Zod parsed output cannot be decoded by Go: %v; output=%s", err, results[i].Data)
								}
								if !reflect.DeepEqual(before, after) {
									t.Fatalf("Zod changed Go field values: before=%#v, after=%#v; input=%s, output=%s", before, after, tc.JSON, results[i].Data)
								}
							}
						})
					}
				})
			})
		}
	}
}

// validationContractTypes type-checks the fixture declarations alone. The
// *_cases.go files import the router and stay out of the static mapper's view.
func validationContractTypes(t *testing.T) *types.Package {
	t.Helper()
	paths, err := filepath.Glob("testdata/validationcontract/*_types.go")
	if err != nil || len(paths) == 0 {
		t.Fatalf("finding contract types: %v", err)
	}
	return parseFixture(t, "validationcontract", paths...)
}

func writeStaticValidationContract(t *testing.T, pkg *types.Package, cases []validationcontract.Case, mini bool, path string, options codegen.ZodOptions) {
	t.Helper()
	m := typemap.NewMapper(nil)
	program, err := typemap.CompileValidation(options.Validation)
	if err != nil {
		t.Fatal(err)
	}
	m.SetValidation(program)
	seen := map[string]bool{}
	var procs []codegen.ProcEntry
	for _, tc := range cases {
		if seen[tc.Type] {
			continue
		}
		seen[tc.Type] = true
		obj := pkg.Scope().Lookup(tc.Type)
		if obj == nil {
			t.Fatalf("missing static fixture type %q", tc.Type)
		}
		procs = append(procs, codegen.ProcEntry{Path: tc.Type, ProcType: "query", InputTS: m.Convert(obj.Type()), OutputTS: "string"})
	}
	for i := range procs {
		procs[i].InputTS = m.Resolve(procs[i].InputTS)
	}
	var out bytes.Buffer
	if err := codegen.WriteZodSchemas(&out, procs, m.Defs(), zodStyle(mini), options); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, out.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

// parseFixture type-checks testdata sources without loading the module, so a
// fixture package's router registration file never reaches the static mapper.
func parseFixture(t *testing.T, importPath string, paths ...string) *types.Package {
	t.Helper()
	fset := token.NewFileSet()
	var files []*ast.File
	for _, path := range paths {
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, file)
	}
	return typeCheck(t, importPath, fset, files)
}

func typeCheck(t *testing.T, importPath string, fset *token.FileSet, files []*ast.File) *types.Package {
	t.Helper()
	pkg, err := (&types.Config{Importer: importer.Default()}).Check(importPath, fset, files, nil)
	if err != nil {
		t.Fatal(err)
	}
	return pkg
}

func typeGraphPackage(t *testing.T) *types.Package {
	t.Helper()
	return parseFixture(t, "github.com/befabri/trpcgo/testdata/typegraph", "testdata/typegraph/types.go")
}

// runTypeGraphContract generates the named typegraph fixtures with both
// mappers and both Zod styles, then compiles and executes script against them.
func runTypeGraphContract(t *testing.T, names []string, script string) {
	t.Helper()
	forEachGeneration(t, func(t *testing.T, mini, static bool) {
		generateTypeGraphContract(t, mini, static, names, script)
	})
}

func generateTypeGraphContract(t *testing.T, mini, static bool, names []string, script string) {
	t.Helper()
	dir := t.TempDir()
	symlinkNodeModules(t, dir)
	if static {
		pkg := typeGraphPackage(t)
		mapper := typemap.NewMapper(nil)
		var procs []codegen.ProcEntry
		for _, name := range names {
			typ := pkg.Scope().Lookup(name).Type()
			procs = append(procs, codegen.ProcEntry{Path: name, ProcType: "query", InputTS: mapper.Convert(typ), InputZod: mapper.ConvertZod(typ), OutputTS: "string"})
		}
		for i := range procs {
			procs[i].InputTS = mapper.Resolve(procs[i].InputTS)
			procs[i].InputZod = mapper.Resolve(procs[i].InputZod)
		}
		writeStaticContract(t, dir, mini, procs, mapper.Defs())
	} else {
		r := trpcgo.NewRouter(trpcgo.WithZodMini(mini))
		t.Cleanup(func() { _ = r.Close() })
		typegraph.Register(r, names...)
		if err := r.GenerateZod(filepath.Join(dir, "schemas.ts")); err != nil {
			t.Fatal(err)
		}
		if err := r.GenerateTS(filepath.Join(dir, "trpc.ts")); err != nil {
			t.Fatal(err)
		}
	}
	writeTestSources(t, dir, map[string]string{"validate.ts": script})
	assertTypeScriptCompiles(t, dir, "trpc.ts", "schemas.ts", "validate.ts")
	runTypeScript(t, dir, "validate.ts")
}

// forEachGeneration runs fn for both Zod styles and both mappers. Subtests are
// named style/mapper, so -run 'mini/static' selects one combination.
func forEachGeneration(t *testing.T, fn func(t *testing.T, mini, static bool)) {
	t.Helper()
	for _, mini := range []bool{false, true} {
		for _, static := range []bool{false, true} {
			t.Run(zodStyleName(mini)+"/"+mapperName(static), func(t *testing.T) { fn(t, mini, static) })
		}
	}
}

func forEachStyle(t *testing.T, fn func(t *testing.T, mini bool)) {
	t.Helper()
	for _, mini := range []bool{false, true} {
		t.Run(zodStyleName(mini), func(t *testing.T) { fn(t, mini) })
	}
}

func zodStyleName(mini bool) string {
	if mini {
		return "mini"
	}
	return "standard"
}

func zodStyle(mini bool) typemap.ZodStyle {
	if mini {
		return typemap.ZodMini
	}
	return typemap.ZodStandard
}

func mapperName(static bool) string {
	if static {
		return "static"
	}
	return "reflection"
}

// typeScriptCompiler keeps compiler checks independent of the example app's
// compiler version. TRPCGO_TSC can point at another compiler, including a build
// from .reference/typescript-go, without changing either package's dependencies.
func typeScriptCompiler(t *testing.T) string {
	t.Helper()
	compiler := os.Getenv("TRPCGO_TSC")
	if compiler == "" {
		compiler = filepath.Join("testdata", "zodruntime", "node_modules", "typescript", "bin", "tsc")
	}
	path, err := filepath.Abs(compiler)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		if os.Getenv("TRPCGO_TSC") != "" || os.Getenv("CI") != "" || !os.IsNotExist(err) {
			t.Fatalf("TypeScript compiler unavailable at %s: %v", path, err)
		}
		t.Skip("TypeScript compiler not installed; run npm ci --prefix testdata/zodruntime")
	}
	return path
}

func assertTypeScriptCompiles(t *testing.T, dir string, files ...string) {
	t.Helper()
	args := append([]string{
		"--noEmit", "--strict", "--skipLibCheck", "--target", "ES2022",
		"--allowImportingTsExtensions",
		"--module", "ES2022", "--moduleResolution", "bundler",
	}, files...)
	cmd := exec.CommandContext(t.Context(), typeScriptCompiler(t), args...)
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated TypeScript does not compile: %v\n%s", err, output)
	}
}

// zodRuntime locates the one locked Node tree that every generated-code
// contract resolves against: zod, tsx, the TypeScript compiler, and the tRPC
// packages the generated router imports. The example app's dependencies never
// take part in library tests.
func zodRuntime(t *testing.T) (modules, tsx string) {
	t.Helper()
	modules, err := filepath.Abs("testdata/zodruntime/node_modules")
	if err != nil {
		t.Fatal(err)
	}
	tsx = filepath.Join(modules, ".bin", "tsx")
	if _, err := os.Stat(tsx); err != nil {
		if os.Getenv("CI") != "" || !os.IsNotExist(err) {
			t.Fatalf("Zod runtime unavailable: %v; run npm ci --prefix testdata/zodruntime", err)
		}
		t.Skip("run npm ci --prefix testdata/zodruntime to execute runtime contracts")
	}
	return modules, tsx
}

func symlinkNodeModules(t *testing.T, dir string) {
	t.Helper()
	modules, _ := zodRuntime(t)
	if err := os.Symlink(modules, filepath.Join(dir, "node_modules")); err != nil {
		t.Fatal(err)
	}
}

func runTypeScript(t *testing.T, dir, file string) []byte {
	t.Helper()
	_, tsx := zodRuntime(t)
	cmd := exec.CommandContext(t.Context(), tsx, file)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("TypeScript runtime contract: %v\n%s", err, output)
	}
	return output
}

// checkGeneratedZod compiles and runs script against the router's generated
// schemas. Compilation catches unresolved names; execution catches values that
// Zod's object type accepts but its parser rejects.
func checkGeneratedZod(t *testing.T, r *trpcgo.Router, script string) {
	t.Helper()
	checkGeneratedZodFile(t, r.GenerateZod, script)
}

func checkGeneratedZodFile(t *testing.T, generate func(string) error, script string) {
	t.Helper()
	dir := t.TempDir()
	symlinkNodeModules(t, dir)
	if err := generate(filepath.Join(dir, "schemas.ts")); err != nil {
		t.Fatal(err)
	}
	writeTestSources(t, dir, map[string]string{"validate.ts": script})
	assertTypeScriptCompiles(t, dir, "schemas.ts", "validate.ts")
	runTypeScript(t, dir, "validate.ts")
}

func checkGeneratedRouterContract(t *testing.T, r *trpcgo.Router, script string) {
	t.Helper()
	dir := t.TempDir()
	symlinkNodeModules(t, dir)
	if err := r.GenerateTS(filepath.Join(dir, "trpc.ts")); err != nil {
		t.Fatal(err)
	}
	writeTestSources(t, dir, map[string]string{"client.ts": script})
	assertTypeScriptCompiles(t, dir, "client.ts")
}

func writeTestSources(t *testing.T, dir string, sources map[string]string) {
	t.Helper()
	for name, body := range sources {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func copyTypeScriptAssertions(t *testing.T, dir string) {
	t.Helper()
	source, err := os.ReadFile("testdata/typecontracts/assertions.ts")
	if err != nil {
		t.Fatal(err)
	}
	writeTestSources(t, dir, map[string]string{"assertions.ts": string(source)})
}

func writeStaticContract(t *testing.T, dir string, mini bool, procs []codegen.ProcEntry, defs []typemap.TypeDef, options ...codegen.ZodOptions) {
	t.Helper()
	var schemas, router bytes.Buffer
	if err := codegen.WriteZodSchemas(&schemas, procs, defs, zodStyle(mini), options...); err != nil {
		t.Fatal(err)
	}
	if err := codegen.WriteAppRouter(&router, procs, defs); err != nil {
		t.Fatal(err)
	}
	writeTestSources(t, dir, map[string]string{"schemas.ts": schemas.String(), "trpc.ts": router.String()})
}

func generateTS(t *testing.T, r *trpcgo.Router) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "trpc.ts")
	if err := r.GenerateTS(out); err != nil {
		t.Fatalf("GenerateTS: %v", err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}
	return string(data)
}

func generateZod(t *testing.T, r *trpcgo.Router) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "zod.ts")
	if err := r.GenerateZod(out); err != nil {
		t.Fatalf("GenerateZod: %v", err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}
	return string(data)
}

func countPattern(s, pattern string) int {
	return len(regexp.MustCompile(pattern).FindAllStringIndex(s, -1))
}
