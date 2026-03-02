package trpcgo

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/befabri/trpcgo/internal/codegen"
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
	procs, defs := r.convertProcedures()

	// Write to buffer first — only write file on success.
	var buf bytes.Buffer
	if err := codegen.WriteAppRouter(&buf, procs, defs); err != nil {
		return fmt.Errorf("generating TypeScript: %w", err)
	}

	return os.WriteFile(outputPath, buf.Bytes(), 0o644)
}

// convertProcedures converts reflect-based procedure registrations to
// codegen ProcEntry and typemap TypeDef slices.
func (r *Router) convertProcedures() ([]codegen.ProcEntry, []typemap.TypeDef) {
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
	sort.Slice(procs, func(i, j int) bool { return procs[i].path < procs[j].path })

	defs := map[string]*reflectDef{}

	var entries []codegen.ProcEntry
	for _, p := range procs {
		inputTS := "void"
		if p.inputType != nil {
			inputTS = goTypeToTS(p.inputType, defs, nil)
		}
		outputTS := goTypeToTS(p.outputType, defs, nil)
		entries = append(entries, codegen.ProcEntry{
			Path:     p.path,
			ProcType: string(p.typ),
			InputTS:  inputTS,
			OutputTS: outputTS,
		})
	}

	// Convert reflect defs to typemap.TypeDef for the shared writer.
	sortedDefs := make([]*reflectDef, 0, len(defs))
	for _, d := range defs {
		sortedDefs = append(sortedDefs, d)
	}
	sort.Slice(sortedDefs, func(i, j int) bool { return sortedDefs[i].name < sortedDefs[j].name })

	typeDefs := make([]typemap.TypeDef, len(sortedDefs))
	for i, d := range sortedDefs {
		fields := make([]typemap.Field, len(d.fields))
		for j, f := range d.fields {
			fields[j] = typemap.Field{
				Name:     f.name,
				Type:     f.tsType,
				Optional: f.optional,
				Readonly: f.readonly,
				Required: f.required,
			}
		}
		typeDefs[i] = typemap.TypeDef{
			Name:       d.name,
			Kind:       typemap.TypeDefInterface,
			TypeParams: d.typeParams,
			Fields:     fields,
		}
	}

	return entries, typeDefs
}

type reflectDef struct {
	name       string
	typeParams []string
	fields     []reflectField
}

type reflectField struct {
	name     string
	tsType   string
	optional bool
	readonly bool
	required bool
}

// goTypeToTS converts a reflect.Type to its TypeScript representation.
// subs maps concrete reflect.Types to generic type parameter names (e.g., "T")
// for building generic interface definitions. Pass nil for non-generic contexts.
func goTypeToTS(t reflect.Type, defs map[string]*reflectDef, subs map[reflect.Type]string) string {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}

	// Generic type parameter substitution.
	if len(subs) > 0 {
		if paramName, ok := subs[t]; ok {
			return paramName
		}
	}

	// Well-known types that need special handling regardless of Kind.
	if t.PkgPath() == "encoding/json" && t.Name() == "RawMessage" {
		return "unknown"
	}

	// TrackedEvent[T] — unwrap to T for TypeScript output.
	// The tracking ID is a transport concern, not a type concern.
	// In reflect, generic instantiation names include type args, e.g.
	// "TrackedEvent[pkg.Foo·1]", so we check with HasPrefix.
	if t.PkgPath() == "github.com/befabri/trpcgo" && strings.HasPrefix(t.Name(), "TrackedEvent[") {
		if dataField, ok := t.FieldByName("Data"); ok {
			return goTypeToTS(dataField.Type, defs, subs)
		}
	}

	switch t.Kind() {
	case reflect.String:
		return "string"
	case reflect.Bool:
		return "boolean"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return "number"

	case reflect.Slice:
		if t.Elem().Kind() == reflect.Uint8 {
			return "string"
		}
		elem := goTypeToTS(t.Elem(), defs, subs)
		if strings.Contains(elem, "|") {
			return "(" + elem + ")[]"
		}
		return elem + "[]"

	case reflect.Array:
		return goTypeToTS(t.Elem(), defs, subs) + "[]"

	case reflect.Map:
		key := goTypeToTS(t.Key(), defs, subs)
		val := goTypeToTS(t.Elem(), defs, subs)
		return fmt.Sprintf("Record<%s, %s>", key, val)

	case reflect.Struct:
		name := t.Name()
		if t.PkgPath() == "time" && name == "Time" {
			return "string"
		}
		if name == "" {
			return inlineStructTS(t, defs, subs)
		}
		// Generic instantiation: PageResult[github.com/pkg.Foo]
		if bracketIdx := strings.IndexByte(name, '['); bracketIdx >= 0 {
			return handleGenericTS(t, name, bracketIdx, defs, subs)
		}

		key := t.PkgPath() + "." + name
		if _, ok := defs[key]; !ok {
			resolveStructTS(t, defs)
		}
		return name

	case reflect.Interface:
		return "unknown"

	default:
		return "unknown"
	}
}

// handleGenericTS handles a generic type instantiation (e.g., PageResult[pkg.Foo]).
// It registers a single generic interface definition for the base type and
// returns a TypeScript reference like PageResult<Foo>.
func handleGenericTS(t reflect.Type, name string, bracketIdx int, defs map[string]*reflectDef, outerSubs map[reflect.Type]string) string {
	baseName := name[:bracketIdx]
	argsStr := name[bracketIdx+1 : len(name)-1]
	argParts := splitGenericArgs(argsStr)

	// Find reflect.Type for each type argument by scanning struct field types.
	fieldTypes := map[string]reflect.Type{}
	visited := map[reflect.Type]bool{}
	for f := range t.Fields() {
		collectNamedTypes(f.Type, fieldTypes, visited)
	}

	argTypes := make([]reflect.Type, len(argParts))
	for i, argStr := range argParts {
		argTypes[i] = findArgType(fieldTypes, argStr)
	}

	// Register the generic interface once per base type.
	// The definition is built from whichever instantiation is encountered first.
	// This is correct because all instantiations share the same struct layout;
	// only the concrete type arguments differ.
	genericKey := t.PkgPath() + "." + baseName
	if _, ok := defs[genericKey]; !ok {
		paramNames := makeParamNames(len(argParts))
		interfaceSubs := make(map[reflect.Type]string, len(argTypes))
		for i, at := range argTypes {
			if at != nil {
				interfaceSubs[at] = paramNames[i]
			}
		}
		resolveGenericStructTS(t, baseName, genericKey, paramNames, interfaceSubs, defs)
	}

	// Convert type args to TypeScript names for the reference.
	argTSNames := make([]string, len(argTypes))
	for i, at := range argTypes {
		if at != nil {
			argTSNames[i] = goTypeToTS(at, defs, outerSubs)
		} else {
			argTSNames[i] = basicArgToTS(argParts[i])
		}
	}

	return baseName + "<" + strings.Join(argTSNames, ", ") + ">"
}

// resolveGenericStructTS registers a generic interface definition.
// subs maps concrete type arg types to parameter names (e.g., EffectRow → "T").
func resolveGenericStructTS(t reflect.Type, baseName, key string, paramNames []string, subs map[reflect.Type]string, defs map[string]*reflectDef) {
	defs[key] = &reflectDef{
		name:       baseName,
		typeParams: paramNames,
	}

	var fields []reflectField
	collectFieldsTS(t, defs, &fields, subs)
	defs[key].fields = fields
}

func resolveStructTS(t reflect.Type, defs map[string]*reflectDef) {
	name := t.Name()
	key := t.PkgPath() + "." + name
	defs[key] = &reflectDef{name: name}

	var fields []reflectField
	collectFieldsTS(t, defs, &fields, nil)
	defs[key].fields = fields
}

func collectFieldsTS(t reflect.Type, defs map[string]*reflectDef, fields *[]reflectField, subs map[reflect.Type]string) {
	for f := range t.Fields() {
		jsonName, omitempty, skip := typemap.ParseJSONTag(string(f.Tag))
		if skip {
			continue
		}

		// Check tstype tag for skip.
		tstag, hasTSTag := typemap.ParseTSTypeTag(string(f.Tag))
		if hasTSTag && tstag.Type == "-" {
			continue
		}

		// Embedded struct: flatten fields.
		if f.Anonymous && jsonName == "" {
			ft := f.Type
			if ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
			if ft.Kind() == reflect.Struct {
				collectFieldsTS(ft, defs, fields, subs)
				continue
			}
		}

		if !f.IsExported() {
			continue
		}

		if jsonName == "" {
			jsonName = f.Name
		}

		tsType := goTypeToTS(f.Type, defs, subs)
		optional := omitempty || f.Type.Kind() == reflect.Pointer

		rf := reflectField{name: jsonName, tsType: tsType, optional: optional}

		// Apply tstype tag overrides.
		if hasTSTag {
			if tstag.Type != "" {
				rf.tsType = tstag.Type
			}
			rf.readonly = tstag.Readonly
			if tstag.Required {
				rf.required = true
				rf.optional = false
			}
		}

		*fields = append(*fields, rf)
	}
}

func inlineStructTS(t reflect.Type, defs map[string]*reflectDef, subs map[reflect.Type]string) string {
	if t.NumField() == 0 {
		return "Record<string, never>"
	}
	var parts []string
	for f := range t.Fields() {
		if !f.IsExported() {
			continue
		}
		jsonName, omitempty, skip := typemap.ParseJSONTag(string(f.Tag))
		if skip {
			continue
		}
		tstag, hasTSTag := typemap.ParseTSTypeTag(string(f.Tag))
		if hasTSTag && tstag.Type == "-" {
			continue
		}
		if jsonName == "" {
			jsonName = f.Name
		}
		tsType := goTypeToTS(f.Type, defs, subs)
		if hasTSTag && tstag.Type != "" {
			tsType = tstag.Type
		}
		opt := ""
		if omitempty || f.Type.Kind() == reflect.Pointer {
			opt = "?"
		}
		if hasTSTag && tstag.Required {
			opt = ""
		}
		prefix := ""
		if hasTSTag && tstag.Readonly {
			prefix = "readonly "
		}
		parts = append(parts, fmt.Sprintf("%s%s%s: %s", prefix, jsonName, opt, tsType))
	}
	if len(parts) == 0 {
		return "Record<string, never>"
	}
	return "{ " + strings.Join(parts, "; ") + " }"
}

// --- Generic type helpers ---

// splitGenericArgs splits a comma-separated list of Go type arguments,
// respecting nested brackets for generic type arguments.
func splitGenericArgs(s string) []string {
	var parts []string
	depth := 0
	start := 0
	for i, c := range s {
		switch c {
		case '[':
			depth++
		case ']':
			depth--
		case ',':
			if depth == 0 {
				parts = append(parts, strings.TrimSpace(s[start:i]))
				start = i + 1
			}
		}
	}
	parts = append(parts, strings.TrimSpace(s[start:]))
	return parts
}

// collectNamedTypes recursively collects all named types reachable from t.
// Results are keyed by PkgPath + "." + Name.
func collectNamedTypes(t reflect.Type, result map[string]reflect.Type, visited map[reflect.Type]bool) {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if visited[t] {
		return
	}
	visited[t] = true

	if t.Name() != "" && t.PkgPath() != "" {
		result[t.PkgPath()+"."+t.Name()] = t
	}

	switch t.Kind() {
	case reflect.Struct:
		for f := range t.Fields() {
			collectNamedTypes(f.Type, result, visited)
		}
	case reflect.Slice, reflect.Array:
		collectNamedTypes(t.Elem(), result, visited)
	case reflect.Map:
		collectNamedTypes(t.Key(), result, visited)
		collectNamedTypes(t.Elem(), result, visited)
	case reflect.Chan:
		collectNamedTypes(t.Elem(), result, visited)
	case reflect.Func:
		for p := range t.Ins() {
			collectNamedTypes(p, result, visited)
		}
		for p := range t.Outs() {
			collectNamedTypes(p, result, visited)
		}
	}
}

// basicTypesByName maps Go basic type names to their reflect.Type.
var basicTypesByName = map[string]reflect.Type{
	"string":  reflect.TypeFor[string](),
	"bool":    reflect.TypeFor[bool](),
	"int":     reflect.TypeFor[int](),
	"int8":    reflect.TypeFor[int8](),
	"int16":   reflect.TypeFor[int16](),
	"int32":   reflect.TypeFor[int32](),
	"int64":   reflect.TypeFor[int64](),
	"uint":    reflect.TypeFor[uint](),
	"uint8":   reflect.TypeFor[uint8](),
	"uint16":  reflect.TypeFor[uint16](),
	"uint32":  reflect.TypeFor[uint32](),
	"uint64":  reflect.TypeFor[uint64](),
	"float32": reflect.TypeFor[float32](),
	"float64": reflect.TypeFor[float64](),
}

// findArgType finds the reflect.Type for a Go type argument string.
// Checks types found in struct fields first, then falls back to basic types.
func findArgType(fieldTypes map[string]reflect.Type, argStr string) reflect.Type {
	if t, ok := fieldTypes[argStr]; ok {
		return t
	}
	if t, ok := basicTypesByName[argStr]; ok {
		return t
	}
	return nil
}

// basicArgToTS converts a Go type argument string to TypeScript as a fallback
// when the reflect.Type cannot be found.
func basicArgToTS(argStr string) string {
	switch argStr {
	case "string":
		return "string"
	case "bool":
		return "boolean"
	case "int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64",
		"float32", "float64":
		return "number"
	default:
		if idx := strings.LastIndexByte(argStr, '.'); idx >= 0 {
			return argStr[idx+1:]
		}
		return argStr
	}
}

// makeParamNames generates TypeScript type parameter names.
// Single parameter: ["T"]. Multiple: ["A", "B", "C", ...].
// Beyond 26 parameters, uses T1, T2, etc.
func makeParamNames(count int) []string {
	if count == 1 {
		return []string{"T"}
	}
	names := make([]string, count)
	for i := range names {
		if i < 26 {
			names[i] = string(rune('A' + i))
		} else {
			names[i] = fmt.Sprintf("T%d", i+1)
		}
	}
	return names
}
