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
	if err := codegen.WriteZodSchemas(&buf, procs, defs, style, codegen.ZodOptions{AllowUnknownFields: !r.opts.strictInput, Validation: program}); err != nil {
		return fmt.Errorf("generating Zod schemas: %w", err)
	}

	// No input needs a schema, so a stale module must not outlive the router.
	if buf.Len() == 0 {
		if _, err := fsutil.RemoveIfExists(outputPath); err != nil {
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
func (r *Router) convertProcedures(validation *typemap.ValidationProgram) ([]codegen.ProcEntry, []typemap.TypeDef) {
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

	defs := newReflectDefs(validation)

	var entries []codegen.ProcEntry
	for _, p := range procs {
		inputTS := "void"
		if p.inputType != nil {
			inputTS = goTypeToTS(p.inputType, defs)
		}
		outputTS := reflectProcedureOutputTS(p.typ, p.outputType, defs)
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
	sortedDefs := make([]defWithKey, 0, len(defs.byKey))
	for key, d := range defs.byKey {
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

// reflectDefs holds the named definitions collected in one generation run,
// keyed by package path and type name, and the types being converted or
// described within the definition currently being resolved.
type reflectDefs struct {
	byKey      map[string]*reflectDef
	converting map[reflect.Type]bool
	describing map[reflect.Type]bool
	validation *typemap.ValidationProgram // compiled Zod configuration, or nil
}

func newReflectDefs(validation *typemap.ValidationProgram) *reflectDefs {
	return &reflectDefs{byKey: make(map[string]*reflectDef), converting: make(map[reflect.Type]bool), describing: make(map[reflect.Type]bool), validation: validation}
}

// definitionScope gives a definition's fields a fresh record of the types in
// progress and returns the function that restores the enclosing record.
// Definitions are resolved once and cached, so their metadata must not
// depend on which reference happened to reach them first.
func (d *reflectDefs) definitionScope() (restore func()) {
	converting, describing := d.converting, d.describing
	d.converting, d.describing = make(map[reflect.Type]bool), make(map[reflect.Type]bool)
	return func() { d.converting, d.describing = converting, describing }
}

func reflectProcedureOutputTS(typ ProcedureType, output reflect.Type, defs *reflectDefs) string {
	if typ == ProcedureSubscription {
		t := output
		if t.Kind() == reflect.Pointer {
			t = t.Elem()
		}
		// httpSubscriptionLink wraps only the top-level stream item as { id, data }.
		if t.PkgPath() == "github.com/befabri/trpcgo" && strings.HasPrefix(t.Name(), "TrackedEvent[") {
			if field, ok := t.FieldByName("Data"); ok {
				return "{ id: string; data: " + goTypeToTS(field.Type, defs) + " }"
			}
		}
	}
	return goTypeToTS(output, defs)
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
func goTypeToTS(t reflect.Type, defs *reflectDefs) string {
	for t.Kind() == reflect.Pointer {
		// Only a named pointer can reach itself, through the anonymous struct
		// it points to. It has no definition to reference, so the recursive
		// occurrence is left untyped.
		if t.Name() != "" {
			if defs.converting[t] {
				return "unknown"
			}
			defs.converting[t] = true
			defer delete(defs.converting, t)
		}
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
		if _, exists := defs.byKey[key]; !exists {
			resolveAliasDefTS(t, key, defs)
		}
		return typemap.TokenDelim + key + typemap.TokenDelim
	}
	switch t.Kind() {
	case reflect.Slice:
		return reflectSliceToTS(t, defs)

	case reflect.Array:
		return goTypeToTS(t.Elem(), defs) + "[]"

	case reflect.Map:
		key := goTypeToTS(t.Key(), defs)
		val := goTypeToTS(t.Elem(), defs)
		return fmt.Sprintf("Record<%s, %s>", key, val)

	case reflect.Struct:
		return reflectStructToTS(t, defs)

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

func reflectSliceToTS(t reflect.Type, defs *reflectDefs) string {
	if t.Elem().Kind() == reflect.Uint8 {
		return "string"
	}
	elem := goTypeToTS(t.Elem(), defs)
	if strings.Contains(elem, "|") {
		return "(" + elem + ")[]"
	}
	return elem + "[]"
}

func reflectStructToTS(t reflect.Type, defs *reflectDefs) string {
	name := t.Name()
	if t.PkgPath() == "time" && name == "Time" {
		return "string"
	}

	if name == "" {
		return inlineStructTS(t, defs)
	}
	key := t.PkgPath() + "." + name
	name = reflectTypeName(t)
	if _, ok := defs.byKey[key]; !ok {
		resolveStructDefTS(t, name, key, defs)
	}
	return typemap.TokenDelim + key + typemap.TokenDelim
}

// resolveStructDefTS registers a struct type as a TypeScript interface definition.
func resolveStructDefTS(t reflect.Type, name, key string, defs *reflectDefs) {
	defs.byKey[key] = &reflectDef{
		name:    name,
		pkgPath: t.PkgPath(),
	}
	defer defs.definitionScope()()

	fields, extends, extendsAt, refs := collectFieldsTS(t, defs)
	defs.byKey[key].fields = fields
	defs.byKey[key].extends = extends
	defs.byKey[key].extendsAt = extendsAt
	defs.byKey[key].refinements = refs
}

// resolveAliasDefTS registers a named slice, array or map. The definition is
// registered before its body is expanded, so recursive references stay named.
func resolveAliasDefTS(t reflect.Type, key string, defs *reflectDefs) {
	d := &reflectDef{name: reflectTypeName(t), pkgPath: t.PkgPath(), kind: typemap.TypeDefAlias}
	defs.byKey[key] = d
	defer defs.definitionScope()()
	d.aliasOf = reflectContainerToTS(t, defs)
	d.underlying = reflectTypeField(t, defs, d.aliasOf)
}

func reflectFieldAdapter(defs *reflectDefs) typemap.FieldAdapter[reflect.Type] {
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
		TypeName: func(t reflect.Type) string { return goTypeToTS(t, defs) },
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
			return reflectMappedField(t.Field(index), defs, name, omitted, tag, hasTag)
		},
	}
}

func collectFieldsTS(t reflect.Type, defs *reflectDefs) ([]typemap.Field, []string, []int, []typemap.Refinement) {
	return typemap.CollectJSONFields(t, reflectFieldAdapter(defs), true)
}

func reflectMappedField(f reflect.StructField, defs *reflectDefs, jsonName string, omitempty bool, tstag typemap.TSTypeTag, hasTSTag bool) typemap.Field {
	// The descriptor already carries the field's TypeScript type and kind.
	descriptor := reflectDescribeType(f.Type, defs)
	field := *descriptorField(descriptor, descriptor.Type)
	field.Name, field.GoName, field.Optional = jsonName, f.Name, omitempty || f.Type.Kind() == reflect.Pointer
	field.JSONString = typemap.JSONStringOption(string(f.Tag), field.GoKind)
	if field.JSONString {
		field.Type = "string"
	}
	typemap.ApplyValidation(&field, string(f.Tag), defs.validation)

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

func inlineStructTS(t reflect.Type, defs *reflectDefs) string {
	fields, _, _, _ := typemap.CollectJSONFields(t, reflectFieldAdapter(defs), false)
	return typemap.InlineObjectType(fields)
}

// resolveDisplayNames computes a mapping from def key (pkgPath.Name) to
// display name. When no collisions exist, display names equal short names;
// otherwise package path segments are prefixed until the names are distinct,
// exactly as the static generator does.
func resolveDisplayNames(defs *reflectDefs) map[string]string {
	return typemap.UniqueTypeNames(slices.Sorted(maps.Keys(defs.byKey)), func(key string) string { return defs.byKey[key].name }, func(key string) string { return defs.byKey[key].pkgPath })
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

func reflectTypeName(t reflect.Type) string {
	name := t.Name()
	if i := strings.IndexByte(name, '['); i >= 0 {
		return typemap.SpecializationName(name[:i], t.PkgPath()+"."+name)
	}
	return name
}

// reflectContainerToTS expands exactly one named container layer. Definitions
// are registered before expansion, so recursive references remain named edges.
func reflectContainerToTS(t reflect.Type, defs *reflectDefs) string {
	switch t.Kind() {
	case reflect.Slice:
		return reflectSliceToTS(t, defs)
	case reflect.Array:
		return goTypeToTS(t.Elem(), defs) + "[]"
	case reflect.Map:
		return fmt.Sprintf("Record<%s, %s>", goTypeToTS(t.Key(), defs), goTypeToTS(t.Elem(), defs))
	default:
		panic("reflectContainerToTS called for a non-container type")
	}
}

func reflectTypeField(t reflect.Type, defs *reflectDefs, ts string) *typemap.Field {
	return descriptorField(reflectDescribeType(t, defs), ts)
}

// descriptorField starts a field from its type's descriptor, typed as ts.
func descriptorField(d *typemap.ElementType, ts string) *typemap.Field {
	return &typemap.Field{Type: ts, Equality: d.Equality, ArrayLen: d.ArrayLen, GoKind: d.GoKind, ValidatorKind: d.ValidatorKind, GoType: d.GoType, IsPointer: d.IsPointer, Inline: d.Inline, Element: d.Element, Key: d.Key}
}

// reflectDescribeType records the element chain and anonymous objects of t.
// A named type already being described within the current definition,
// reached again through an anonymous struct's field, keeps only its name,
// like a reference to a named struct. Every Go type cycle passes through a
// named type, and stopping there keeps the anonymous levels above it fully
// described.
func reflectDescribeType(t reflect.Type, defs *reflectDefs) *typemap.ElementType {
	d := &typemap.ElementType{Type: goTypeToTS(t, defs), Equality: typemap.DescribeReflectEquality(t), GoKind: reflectGoKind(t), ValidatorKind: typemap.ReflectValidatorKind(t), IsPointer: t.Kind() == reflect.Pointer}
	for {
		// Named pointers are dereferenced here, so they are tracked too.
		if t.Name() != "" {
			if defs.describing[t] {
				d.GoType = t.String()
				return d
			}
			defs.describing[t] = true
			defer delete(defs.describing, t)
		}
		if t.Kind() != reflect.Pointer {
			break
		}
		t = t.Elem()
	}
	d.GoType = t.String()
	switch t.Kind() {
	case reflect.Slice, reflect.Array:
		if t.Kind() == reflect.Array {
			length := int64(t.Len())
			d.ArrayLen = &length
		}
		if d.GoKind != "[]byte" {
			d.Element = reflectDescribeType(t.Elem(), defs)
		}
	case reflect.Map:
		d.Key = reflectDescribeType(t.Key(), defs)
		d.Element = reflectDescribeType(t.Elem(), defs)
	case reflect.Struct:
		if t.Name() == "" {
			fields, _, _, refs := typemap.CollectJSONFields(t, reflectFieldAdapter(defs), false)
			d.Inline = &typemap.TypeDef{Kind: typemap.TypeDefInterface, Fields: fields, Refinements: refs}
		}
	}
	return d
}
