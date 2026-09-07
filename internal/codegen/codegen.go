package codegen

import (
	"cmp"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"

	"github.com/befabri/trpcgo/internal/analysis"
	"github.com/befabri/trpcgo/internal/typemap"
)

// ProcEntry is a procedure with pre-converted TypeScript type strings.
type ProcEntry struct {
	Path      string
	ProcType  string
	InputTS   string
	InputZod  string // Concrete schema identity when Go types erase to the same TypeScript type.
	OutputTS  string
	OutputZod string // Concrete Go output identity when public generic arguments erase information.
}

// GenerateResult holds the converted procedures and type definitions of a
// generation pass. The same data feeds the AppRouter, Zod, and enum writers.
type GenerateResult struct {
	Procs []ProcEntry
	Defs  []typemap.TypeDef

	// Roots names the definitions seeded by exported types rather than by a
	// procedure signature. Pass it as [ZodOptions.Roots] so schemas cover
	// types no procedure mentions.
	Roots []string

	// Skipped records exported types left out of part of the output, and why.
	// Nothing else would say so.
	Skipped []SkippedType
}

// SkippedType is an exported type that reached the output partially or not at all.
type SkippedType struct {
	Name   string
	Reason string
}

// Prepare converts static analysis results into codegen entries: procedures
// with TypeScript type strings and the type definitions they reference.
// It performs no I/O; pass the result to [WriteAppRouter], [WriteZodSchemas],
// or [WriteEnums].
func Prepare(result *analysis.Result, metas map[string]typemap.TypeMeta, validation ...*typemap.ValidationProgram) *GenerateResult {
	mapper := typemap.NewMapper(metas)
	if len(validation) > 0 {
		mapper.SetValidation(validation[0])
	}

	var procs []ProcEntry
	for _, p := range result.Procedures {
		inputTS, inputZod := "void", "void"
		if p.InputType != nil {
			inputTS = mapper.Convert(p.InputType)
			inputZod = mapper.ConvertZod(p.InputType)
		}
		var outputTS, outputZod string
		if p.Type == "subscription" {
			outputTS = mapper.ConvertSubscriptionOutput(p.OutputType)
			outputZod = mapper.ConvertSubscriptionOutputZod(p.OutputType)
		} else {
			outputTS = mapper.Convert(p.OutputType)
			outputZod = mapper.ConvertZod(p.OutputType)
		}
		if outputZod == outputTS {
			outputZod = ""
		}

		procs = append(procs, ProcEntry{
			Path:      p.Path,
			ProcType:  p.Type,
			InputTS:   inputTS,
			InputZod:  inputZod,
			OutputTS:  outputTS,
			OutputZod: outputZod,
		})
	}

	// Exported types convert after the procedures so that definitions a
	// procedure reaches keep the order, and the names, they already had.
	// Kept by index: two packages may export the same name.
	rootTS := make([]string, len(result.ExportedTypes))
	for i, root := range result.ExportedTypes {
		rootTS[i] = mapper.Convert(root.Type)
		mapper.ConvertZod(root.Type)
	}

	// Resolve type tokens in proc types (handles collision renaming).
	for i := range procs {
		procs[i].InputTS = mapper.Resolve(procs[i].InputTS)
		procs[i].InputZod = mapper.Resolve(procs[i].InputZod)
		procs[i].OutputTS = mapper.Resolve(procs[i].OutputTS)
		procs[i].OutputZod = mapper.Resolve(procs[i].OutputZod)
	}

	defs := mapper.Defs()
	declared := make(map[string]typemap.TypeDef, len(defs))
	for _, def := range defs {
		declared[def.Name] = def
	}

	var roots []string
	var skipped []SkippedType
	for i, root := range result.ExportedTypes {
		name := stripGenericArgs(mapper.Resolve(rootTS[i]))
		def, ok := declared[name]
		switch {
		case !ok:
			// The type reached the output as nothing at all: an interface, a
			// func, a channel. Naming it beats silence.
			skipped = append(skipped, SkippedType{root.Name, "no TypeScript representation"})
		case len(def.TypeParams) > 0:
			// A type parameter has no wire shape, so the declaration itself has
			// no schema. Its instantiations do, wherever a field names one.
			skipped = append(skipped, SkippedType{root.Name, "generic declaration: a type, but only its instantiations get schemas"})
		default:
			roots = append(roots, name)
		}
	}

	return &GenerateResult{Procs: procs, Defs: defs, Roots: roots, Skipped: skipped}
}

// Generate writes the TypeScript AppRouter definition to w using static analysis results.
func Generate(w io.Writer, result *analysis.Result, metas map[string]typemap.TypeMeta, validation ...*typemap.ValidationProgram) (*GenerateResult, error) {
	gen := Prepare(result, metas, validation...)
	if err := WriteAppRouter(w, gen.Procs, gen.Defs); err != nil {
		return nil, err
	}
	return gen, nil
}

// WriteAppRouter writes the complete TypeScript AppRouter file given
// pre-converted procedure entries and interface definitions.
// Used by both static analysis (Generate) and runtime reflection (GenerateTS).
//
// Without procedures there is no router to describe, so the file holds the type
// definitions alone: no AppRouter, and no import of @trpc/server for a package
// that may not depend on tRPC at all.
func WriteAppRouter(w io.Writer, procs []ProcEntry, defs []typemap.TypeDef) error {
	procs, defs = publicGoArrayTypes(procs, defs)
	if err := uniqueDefinitionNames(defs); err != nil {
		return err
	}
	ew := newErrWriter(w)

	ew.println("// Code generated by trpcgo. DO NOT EDIT.")
	ew.println("")

	if len(procs) == 0 {
		writeTypeDefs(ew, defs)
		return ew.err
	}

	// Determine which procedure types are used.
	usedTypes := map[string]bool{}
	for _, p := range procs {
		usedTypes[p.ProcType] = true
	}

	// Import from @trpc/server (only include types actually used).
	imports := []string{"TRPCRouterDef", "TRPCRouterCaller"}
	if usedTypes["query"] {
		imports = append(imports, "TRPCQueryProcedure")
	}
	if usedTypes["mutation"] {
		imports = append(imports, "TRPCMutationProcedure")
	}
	if usedTypes["subscription"] {
		imports = append(imports, "TRPCSubscriptionProcedure")
	}
	imports = append(imports, "inferRouterInputs", "inferRouterOutputs")
	slices.Sort(imports)

	ew.println("import type {")
	for _, name := range imports {
		ew.printf("  %s,\n", name)
	}
	ew.println("} from '@trpc/server';")
	ew.println("")

	// Type definitions (interfaces, unions, aliases).
	writeTypeDefs(ew, defs)

	// Procedure type aliases (only emit types that are used).
	if usedTypes["query"] {
		ew.println(`type $Query<TInput, TOutput> = TRPCQueryProcedure<{ input: TInput; output: TOutput; meta: object }>;`)
	}
	if usedTypes["mutation"] {
		ew.println(`type $Mutation<TInput, TOutput> = TRPCMutationProcedure<{ input: TInput; output: TOutput; meta: object }>;`)
	}
	if usedTypes["subscription"] {
		ew.println(`type $Subscription<TInput, TOutput> = TRPCSubscriptionProcedure<{ input: TInput; output: TOutput; meta: object }>;`)
	}
	ew.println("")

	// Build procedure tree from dot-separated paths.
	tree := buildProcTree(procs, func(p ProcEntry) procLeaf {
		return procLeaf{procType: p.ProcType, inputTS: p.InputTS, outputTS: p.OutputTS}
	})

	ew.print("type AppRouterRecord = ")
	writeTreeNodes(ew, tree, 0, func(l procLeaf) string {
		helper := "$Query"
		switch l.procType {
		case "mutation":
			helper = "$Mutation"
		case "subscription":
			helper = "$Subscription"
		}
		return fmt.Sprintf("%s<%s, %s>", helper, l.inputTS, l.outputTS)
	})
	ew.println(";")
	ew.println("")

	// AppRouter using @trpc/server types.
	ew.print(`type $ErrorShape = { code: number; message: string; data: { code: string; httpStatus: number; path?: string } };
type $RootTypes = { ctx: object; meta: object; errorShape: $ErrorShape; transformer: false };

export type AppRouter = {
  _def: TRPCRouterDef<$RootTypes, AppRouterRecord>;
  createCaller: TRPCRouterCaller<$RootTypes, AppRouterRecord>;
};
`)
	ew.println("")

	// RouterInputs / RouterOutputs via tRPC inference.
	ew.println("export type RouterInputs = inferRouterInputs<AppRouter>;")
	ew.println("")
	ew.println("export type RouterOutputs = inferRouterOutputs<AppRouter>;")

	return ew.err
}

func writeTypeDefs(ew *errWriter, defs []typemap.TypeDef) {
	for _, def := range defs {
		writeTypeDef(ew, def)
	}
}

func writeTypeDef(ew *errWriter, def typemap.TypeDef) {
	writeJSDoc(ew, def.Comment, "")
	switch def.Kind {
	case typemap.TypeDefInterface:
		writeInterface(ew, def)
	case typemap.TypeDefUnion:
		writeUnion(ew, def)
	case typemap.TypeDefAlias:
		writeAlias(ew, def)
	}
	ew.println("")
}

func writeJSDoc(ew *errWriter, comment, indent string) {
	if comment == "" {
		return
	}
	if !strings.Contains(comment, "\n") {
		ew.printf("%s/** %s */\n", indent, comment)
		return
	}
	ew.printf("%s/**\n", indent)
	for line := range strings.SplitSeq(comment, "\n") {
		if line == "" {
			ew.printf("%s *\n", indent)
		} else {
			ew.printf("%s * %s\n", indent, line)
		}
	}
	ew.printf("%s */\n", indent)
}

func writeInterface(ew *errWriter, def typemap.TypeDef) {
	typeParams := ""
	if len(def.TypeParams) > 0 {
		typeParams = "<" + strings.Join(def.TypeParams, ", ") + ">"
	}
	extendsClause := ""
	if len(def.Extends) > 0 {
		extendsClause = " extends " + strings.Join(def.Extends, ", ")
	}
	ew.printf("export interface %s%s%s {\n", def.Name, typeParams, extendsClause)
	for _, f := range def.Fields {
		writeJSDoc(ew, f.Comment, "  ")
		ew.printf("  %s;\n", typemap.PropertyDeclaration(f))
	}
	ew.println("}")
}

func writeUnion(ew *errWriter, def typemap.TypeDef) {
	// Retain literal autocomplete while admitting every value allowed by Go.
	// Named constants are suggestions; validate:"oneof=..." provides closure.
	underlying := enumUnderlyingField(def).Type
	open := underlying
	if underlying == "string" {
		open = "(string & {})"
	}
	// A number intersection cannot serve as a numeric Record index signature.
	members := append(append([]string(nil), def.UnionMembers...), open)
	ew.printf("export type %s = %s;\n", def.Name, strings.Join(members, " | "))
}

func writeAlias(ew *errWriter, def typemap.TypeDef) {
	params := ""
	if len(def.TypeParams) > 0 {
		params = "<" + strings.Join(def.TypeParams, ", ") + ">"
	}
	ew.printf("export type %s%s = %s;\n", def.Name, params, directRecordType(def.AliasOf))
}

// procLeaf holds the data for a procedure tree leaf node.
type procLeaf struct {
	procType string
	inputTS  string
	outputTS string
}

// treeNode is a generic tree node parameterized by leaf data type.
type treeNode[L any] struct {
	children map[string]*treeNode[L]
	isLeaf   bool
	leaf     L
}

// buildProcTree builds a tree from dot-separated procedure paths,
// extracting leaf data from each ProcEntry via leafFn.
func buildProcTree[L any](procs []ProcEntry, leafFn func(ProcEntry) L) *treeNode[L] {
	root := &treeNode[L]{children: make(map[string]*treeNode[L])}
	for _, p := range procs {
		node := root
		for part := range strings.SplitSeq(p.Path, ".") {
			child, ok := node.children[part]
			if !ok {
				child = &treeNode[L]{children: make(map[string]*treeNode[L])}
				node.children[part] = child
			}
			node = child
		}
		node.isLeaf = true
		node.leaf = leafFn(p)
	}
	return root
}

// writeTreeNodes recursively writes a tree as TypeScript object notation,
// using renderLeaf to format each leaf node.
func writeTreeNodes[L any](ew *errWriter, node *treeNode[L], indent int, renderLeaf func(L) string) {
	keys := slices.Sorted(maps.Keys(node.children))

	prefix := strings.Repeat("  ", indent)
	ew.println("{")
	for _, key := range keys {
		child := node.children[key]
		qk := typemap.QuotePropName(key)
		switch {
		case child.isLeaf && len(child.children) > 0:
			// Path is both a leaf and a namespace — emit intersection type.
			ew.printf("%s  %s: %s & ", prefix, qk, renderLeaf(child.leaf))
			writeTreeNodes(ew, child, indent+1, renderLeaf)
			ew.println(";")
		case child.isLeaf:
			ew.printf("%s  %s: %s;\n", prefix, qk, renderLeaf(child.leaf))
		default:
			ew.printf("%s  %s: ", prefix, qk)
			writeTreeNodes(ew, child, indent+1, renderLeaf)
			ew.println(";")
		}
	}
	ew.printf("%s}", prefix)
}

// uniqueDefinitionNames rejects two definitions that would share a generated
// TypeScript name. TypeScript would merge the interfaces and the schema writer
// would keep only one of them, so neither output could be trusted.
func uniqueDefinitionNames(defs []typemap.TypeDef) error {
	seen := make(map[string]string, len(defs))
	for _, def := range defs {
		if other, ok := seen[def.Name]; ok {
			return fmt.Errorf("type name %s is generated for both %s and %s", def.Name, cmp.Or(other, "an earlier definition"), cmp.Or(def.ID, "a later definition"))
		}
		seen[def.Name] = def.ID
	}
	return nil
}
