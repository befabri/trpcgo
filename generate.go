package trpcgo

import (
	"bytes"
	"cmp"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"

	"github.com/befabri/trpcgo/internal/codegen"
	"github.com/befabri/trpcgo/internal/fsutil"
	"github.com/befabri/trpcgo/internal/typemap"
)

// GenerateTS writes TypeScript type definitions for all registered procedures.
// Procedures must be registered via the top-level functions (Query, Mutation, etc.)
// to have type information available.
func (r *Router) GenerateTS(outputPath string) error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	dir := filepath.Dir(outputPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}

	// Convert registered procedures (reflect types) to codegen entries.
	program, err := typemap.CompileValidation(r.opts.zodValidation)
	if err != nil {
		return fmt.Errorf("compiling Zod validation: %w", err)
	}
	procs, defs := r.convertProcedures(program)

	if err := fsutil.AtomicWriteFile(outputPath, 0o644, func(w io.Writer) error {
		return codegen.WriteAppRouter(w, procs, defs)
	}); err != nil {
		return fmt.Errorf("writing TypeScript output: %w", err)
	}
	return nil
}

// GenerateZod writes Zod validation schemas for all registered procedure
// input types. Uses the same reflect-based type information as GenerateTS,
// enriched with Go kind and validate tag metadata.
//
// If no procedures have typed inputs (all void), no file is written and
// nil is returned. Use WithZodMini to switch to zod/mini functional syntax.
func (r *Router) GenerateZod(outputPath string) error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	dir := filepath.Dir(outputPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}

	program, err := typemap.CompileValidation(r.opts.zodValidation)
	if err != nil {
		return fmt.Errorf("compiling Zod validation: %w", err)
	}
	procs, defs := r.convertProcedures(program)

	style := typemap.ZodStandard
	if r.opts.zodMini {
		style = typemap.ZodMini
	}

	var buf bytes.Buffer
	if err := codegen.WriteZodSchemas(&buf, procs, defs, style, codegen.ZodOptions{AllowUnknownFields: !r.opts.strictInput, Validation: r.opts.zodValidation}); err != nil {
		return fmt.Errorf("generating Zod schemas: %w", err)
	}

	// No typed inputs → remove stale file if it exists.
	if buf.Len() == 0 {
		if err := os.Remove(outputPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("removing stale Zod file: %w", err)
		}
		return nil
	}

	if err := fsutil.AtomicWriteFile(outputPath, 0o644, func(w io.Writer) error {
		_, err := buf.WriteTo(w)
		return err
	}); err != nil {
		return fmt.Errorf("writing Zod output: %w", err)
	}
	return nil
}

// convertProcedures converts reflect-based procedure registrations to
// codegen ProcEntry and typemap TypeDef slices.
func (r *Router) convertProcedures(validation ...*typemap.ValidationProgram) ([]codegen.ProcEntry, []typemap.TypeDef) {
	type procInfo struct {
		path       string
		typ        ProcedureType
		inputType  reflect.Type
		outputType reflect.Type
	}
	var procs []procInfo
	for path, proc := range r.procedures {
		if proc.outputType == nil {
			continue
		}
		procs = append(procs, procInfo{path, proc.typ, proc.inputType, proc.outputType})
	}
	slices.SortFunc(procs, func(a, b procInfo) int { return cmp.Compare(a.path, b.path) })

	defs := map[string]*reflectDef{}

	var entries []codegen.ProcEntry
	for _, p := range procs {
		inputTS := "void"
		if p.inputType != nil {
			inputTS = goTypeToTS(p.inputType, defs, validation...)
		}
		outputTS := reflectProcedureOutputTS(p.typ, p.outputType, defs, validation...)
		entries = append(entries, codegen.ProcEntry{
			Path:     p.path,
			ProcType: string(p.typ),
			InputTS:  inputTS,
			OutputTS: outputTS,
		})
	}

	// Resolve type name tokens. goTypeToTS embeds §key§ tokens for
	// named types. Resolve them to display names, disambiguating collisions
	// by prefixing with the title-cased package name (e.g., NpcListInput).
	display := resolveDisplayNames(defs)

	for i := range entries {
		entries[i].InputTS = typemap.ResolveTokens(entries[i].InputTS, display)
		entries[i].OutputTS = typemap.ResolveTokens(entries[i].OutputTS, display)
	}

	// Convert reflect defs to typemap.TypeDef for the shared writer.
	type defWithKey struct {
		key string
		def *reflectDef
	}
	sortedDefs := make([]defWithKey, 0, len(defs))
	for key, d := range defs {
		sortedDefs = append(sortedDefs, defWithKey{key, d})
	}
	slices.SortFunc(sortedDefs, func(a, b defWithKey) int {
		return cmp.Compare(display[a.key], display[b.key])
	})

	typeDefs := make([]typemap.TypeDef, len(sortedDefs))
	for i, dk := range sortedDefs {
		d := dk.def
		resolvedName := cmp.Or(display[dk.key], d.name)
		typeDefs[i] = typemap.TypeDef{
			ID:          dk.key,
			PkgPath:     d.pkgPath,
			Name:        resolvedName,
			Kind:        d.kind,
			AliasOf:     typemap.ResolveTokens(d.aliasOf, display),
			Underlying:  d.underlying,
			Extends:     d.extends,
			ExtendsAt:   d.extendsAt,
			Refinements: d.refinements,
			Fields:      d.fields,
		}
		typeDefs[i] = typemap.ResolveTypeDef(typeDefs[i], display)
	}

	return entries, typeDefs
}

type reflectDef struct {
	kind        typemap.TypeDefKind
	aliasOf     string
	underlying  *typemap.Field
	name        string
	pkgPath     string
	extends     []string
	extendsAt   []int
	refinements []typemap.Refinement
	fields      []typemap.Field
}

func reflectProcedureOutputTS(typ ProcedureType, output reflect.Type, defs map[string]*reflectDef, validation ...*typemap.ValidationProgram) string {
	if typ == ProcedureSubscription {
		t := output
		if t.Kind() == reflect.Pointer {
			t = t.Elem()
		}
		// httpSubscriptionLink wraps only the top-level stream item as { id, data }.
		if t.PkgPath() == "github.com/befabri/trpcgo" && strings.HasPrefix(t.Name(), "TrackedEvent[") {
			if field, ok := t.FieldByName("Data"); ok {
				return "{ id: string; data: " + goTypeToTS(field.Type, defs, validation...) + " }"
			}
		}
	}
	return goTypeToTS(output, defs, validation...)
}

var reflectKindTS = map[reflect.Kind]string{
	reflect.String:  "string",
	reflect.Bool:    "boolean",
	reflect.Int:     "number",
	reflect.Int8:    "number",
	reflect.Int16:   "number",
	reflect.Int32:   "number",
	reflect.Int64:   "number",
	reflect.Uint:    "number",
	reflect.Uint8:   "number",
	reflect.Uint16:  "number",
	reflect.Uint32:  "number",
	reflect.Uint64:  "number",
	reflect.Float32: "number",
	reflect.Float64: "number",
}

var reflectKindNames = map[reflect.Kind]string{
	reflect.String:  "string",
	reflect.Bool:    "bool",
	reflect.Int:     "int",
	reflect.Int8:    "int8",
	reflect.Int16:   "int16",
	reflect.Int32:   "int32",
	reflect.Int64:   "int64",
	reflect.Uint:    "uint",
	reflect.Uint8:   "uint8",
	reflect.Uint16:  "uint16",
	reflect.Uint32:  "uint32",
	reflect.Uint64:  "uint64",
	reflect.Float32: "float32",
	reflect.Float64: "float64",
}

// goTypeToTS converts a reflect.Type to its TypeScript representation.
func goTypeToTS(t reflect.Type, defs map[string]*reflectDef, validation ...*typemap.ValidationProgram) string {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}

	if ts := reflectWellKnownTS(t); ts != "" {
		return ts
	}

	if ts := reflectKindTS[t.Kind()]; ts != "" {
		return ts
	}
	if t.Name() != "" && (t.Kind() == reflect.Slice || t.Kind() == reflect.Array || t.Kind() == reflect.Map) {
		key := t.PkgPath() + "." + t.Name()
		if _, exists := defs[key]; !exists {
			d := &reflectDef{name: reflectTypeName(t), pkgPath: t.PkgPath(), kind: typemap.TypeDefAlias}
			defs[key] = d
			d.aliasOf = reflectContainerToTS(t, defs, validation...)
			d.underlying = reflectTypeField(t, defs, d.aliasOf, validation...)
		}
		return typemap.TokenDelim + key + typemap.TokenDelim
	}
	switch t.Kind() {
	case reflect.Slice:
		return reflectSliceToTS(t, defs, validation...)

	case reflect.Array:
		return goTypeToTS(t.Elem(), defs, validation...) + "[]"

	case reflect.Map:
		key := goTypeToTS(t.Key(), defs, validation...)
		val := goTypeToTS(t.Elem(), defs, validation...)
		return fmt.Sprintf("Record<%s, %s>", key, val)

	case reflect.Struct:
		return reflectStructToTS(t, defs, validation...)

	case reflect.Interface:
		return "unknown"

	default:
		return "unknown"
	}
}

// Well-known reflect types are compared by identity rather than by package
// path and name: since Go 1.27 json.RawMessage is an alias of jsontext.Value,
// so its reflect.Type reports the jsontext package. Identity comparison is
// correct on every toolchain and also covers direct jsontext.Value usage.
var (
	rawMessageType = reflect.TypeFor[json.RawMessage]()
	jsonNumberType = reflect.TypeFor[json.Number]()
)

func reflectWellKnownTS(t reflect.Type) string {
	if t == rawMessageType {
		return "unknown"
	}
	if t == jsonNumberType {
		return "number"
	}
	return ""
}

func reflectSliceToTS(t reflect.Type, defs map[string]*reflectDef, validation ...*typemap.ValidationProgram) string {
	if t.Elem().Kind() == reflect.Uint8 {
		return "string"
	}
	elem := goTypeToTS(t.Elem(), defs, validation...)
	if strings.Contains(elem, "|") {
		return "(" + elem + ")[]"
	}
	return elem + "[]"
}

func reflectStructToTS(t reflect.Type, defs map[string]*reflectDef, validation ...*typemap.ValidationProgram) string {
	name := t.Name()
	if t.PkgPath() == "time" && name == "Time" {
		return "string"
	}

	if name == "" {
		return inlineStructTS(t, defs, validation...)
	}
	key := t.PkgPath() + "." + name
	name = reflectTypeName(t)
	if _, ok := defs[key]; !ok {
		resolveStructDefTS(t, name, key, defs, validation...)
	}
	return typemap.TokenDelim + key + typemap.TokenDelim
}

// resolveStructDefTS registers a struct type as a TypeScript interface definition.
func resolveStructDefTS(t reflect.Type, name, key string, defs map[string]*reflectDef, validation ...*typemap.ValidationProgram) {
	defs[key] = &reflectDef{
		name:    name,
		pkgPath: t.PkgPath(),
	}

	fields, extends, extendsAt, refs := collectFieldsTS(t, defs, validation...)
	defs[key].fields = fields
	defs[key].extends = extends
	defs[key].extendsAt = extendsAt
	defs[key].refinements = refs
}

func reflectFieldAdapter(defs map[string]*reflectDef, validation ...*typemap.ValidationProgram) typemap.FieldAdapter[reflect.Type] {
	return typemap.FieldAdapter[reflect.Type]{
		Fields: func(t reflect.Type) []typemap.EmbeddedField[reflect.Type] {
			fields := make([]typemap.EmbeddedField[reflect.Type], t.NumField())
			for i := range fields {
				f := t.Field(i)
				fields[i] = typemap.EmbeddedField[reflect.Type]{Type: f.Type, Name: f.Name, Tag: string(f.Tag), Exported: f.IsExported(), Embedded: f.Anonymous}
			}
			return fields
		},
		Struct: func(t reflect.Type) (reflect.Type, bool, bool) {
			pointer := t.Kind() == reflect.Pointer
			if pointer {
				t = t.Elem()
			}
			return t, t.Kind() == reflect.Struct, pointer
		},
		TypeName: func(t reflect.Type) string { return goTypeToTS(t, defs, validation...) },
		Lookup:   func(t reflect.Type, name string) ([]int, bool) { f, ok := t.FieldByName(name); return f.Index, ok },
		Describe: func(t reflect.Type, index int) typemap.Field {
			f := t.Field(index)
			field := typemap.Field{GoKind: reflectGoKind(f.Type), IsPointer: f.Type.Kind() == reflect.Pointer, GoType: f.Type.String()}
			if f.Type.Kind() == reflect.Array {
				n := int64(f.Type.Len())
				field.ArrayLen = &n
			}
			return field
		},
		Map: func(t reflect.Type, index int, name string, omitted bool, tag typemap.TSTypeTag, hasTag bool) typemap.Field {
			return reflectMappedField(t.Field(index), defs, name, omitted, tag, hasTag, validation...)
		},
	}
}

func collectFieldsTS(t reflect.Type, defs map[string]*reflectDef, validation ...*typemap.ValidationProgram) ([]typemap.Field, []string, []int, []typemap.Refinement) {
	return typemap.CollectJSONFields(t, reflectFieldAdapter(defs, validation...), true)
}

func reflectMappedField(f reflect.StructField, defs map[string]*reflectDef, jsonName string, omitempty bool, tstag typemap.TSTypeTag, hasTSTag bool, validation ...*typemap.ValidationProgram) typemap.Field {
	tsType := goTypeToTS(f.Type, defs, validation...)
	optional := omitempty || f.Type.Kind() == reflect.Pointer

	field := *reflectTypeField(f.Type, defs, tsType, validation...)
	field.Name, field.GoName, field.Optional = jsonName, f.Name, optional
	field.JSONString = typemap.JSONStringOption(string(f.Tag), field.GoKind)
	if field.JSONString {
		field.Type = "string"
	}

	field.GoKind = reflectGoKind(f.Type)

	var program *typemap.ValidationProgram
	if len(validation) > 0 {
		program = validation[0]
	}
	typemap.ApplyValidation(&field, string(f.Tag), program)

	if hasTSTag {
		if tstag.Type != "" {
			field.TypeOverride = true
			field.ZodType = field.Type
			field.Type = tstag.Type
		}
		field.Readonly = tstag.Readonly
		if tstag.Required {
			field.Required = true
			field.Optional = false
		}
	}

	if doc, ok := typemap.ParseTSDocTag(string(f.Tag)); ok {
		field.Comment = doc
	}

	field.ZodOmit = typemap.ParseZodOmitTag(string(f.Tag))

	return field
}

func inlineStructTS(t reflect.Type, defs map[string]*reflectDef, validation ...*typemap.ValidationProgram) string {
	fields, _, _, _ := typemap.CollectJSONFields(t, reflectFieldAdapter(defs, validation...), false)
	return typemap.InlineObjectType(fields)
}

// resolveDisplayNames computes a mapping from def key (pkgPath.Name) to
// display name. When no collisions exist, display names equal short names;
// otherwise package path segments are prefixed until the names are distinct,
// exactly as the static generator does.
func resolveDisplayNames(defs map[string]*reflectDef) map[string]string {
	return typemap.UniqueTypeNames(slices.Sorted(maps.Keys(defs)), func(key string) string { return defs[key].name }, func(key string) string { return defs[key].pkgPath })
}

// reflectGoKind returns a Go kind string for Zod type discrimination.
// This mirrors typemap.goKind (which uses go/types) but works with reflect.Type.
// SYNC: when adding well-known types here, update typemap.goKind too.
func reflectGoKind(t reflect.Type) string {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}

	// Well-known types.
	if t.PkgPath() == "time" && t.Name() == "Time" {
		return "time.Time"
	}
	if t == rawMessageType {
		return "json.RawMessage"
	}
	if t == jsonNumberType {
		return "json.Number"
	}

	if kind := reflectKindNames[t.Kind()]; kind != "" {
		return kind
	}
	switch t.Kind() {
	case reflect.Slice:
		if t.Elem().Kind() == reflect.Uint8 {
			return "[]byte"
		}
		return "slice"
	case reflect.Array:
		return "array"
	case reflect.Map:
		return "map"
	case reflect.Struct:
		return "struct"
	case reflect.Interface:
		return "interface"
	default:
		return "unknown"
	}
}
