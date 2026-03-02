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

	procs, defs := r.convertProcedures()

	style := typemap.ZodStandard
	if r.opts.zodMini {
		style = typemap.ZodMini
	}

	var buf bytes.Buffer
	if err := codegen.WriteZodSchemas(&buf, procs, defs, style); err != nil {
		return fmt.Errorf("generating Zod schemas: %w", err)
	}

	// No typed inputs → nothing to write.
	if buf.Len() == 0 {
		return nil
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

	// Resolve type name tokens. goTypeToTS embeds \x00key\x00 tokens for
	// named types. Resolve them to display names, disambiguating collisions
	// by prefixing with the title-cased package name (e.g., NpcListInput).
	display := resolveDisplayNames(defs)

	for i := range entries {
		entries[i].InputTS = resolveTypeTokens(entries[i].InputTS, display)
		entries[i].OutputTS = resolveTypeTokens(entries[i].OutputTS, display)
	}
	for _, d := range defs {
		for i := range d.fields {
			d.fields[i].tsType = resolveTypeTokens(d.fields[i].tsType, display)
		}
		for i := range d.extends {
			d.extends[i] = resolveTypeTokens(d.extends[i], display)
		}
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
	sort.Slice(sortedDefs, func(i, j int) bool {
		return display[sortedDefs[i].key] < display[sortedDefs[j].key]
	})

	typeDefs := make([]typemap.TypeDef, len(sortedDefs))
	for i, dk := range sortedDefs {
		d := dk.def
		resolvedName := display[dk.key]
		if resolvedName == "" {
			resolvedName = d.name
		}

		fields := make([]typemap.Field, len(d.fields))
		for j, f := range d.fields {
			fields[j] = typemap.Field{
				Name:              f.name,
				Type:              f.tsType,
				Comment:           f.comment,
				GoKind:            f.goKind,
				Optional:          f.optional,
				Readonly:          f.readonly,
				Required:          f.required,
				ValidateOmitempty: f.validateOmitempty,
				Validate:          f.validate,
				ElementValidate:   f.elementValidate,
				ElementGoKind:     f.elementGoKind,
			}
		}
		typeDefs[i] = typemap.TypeDef{
			Name:       resolvedName,
			Kind:       typemap.TypeDefInterface,
			TypeParams: d.typeParams,
			Extends:    d.extends,
			Fields:     fields,
		}
	}

	return entries, typeDefs
}

type reflectDef struct {
	name       string
	pkgPath    string
	typeParams []string
	extends    []string
	fields     []reflectField
}

type reflectField struct {
	name              string
	tsType            string
	comment           string // from ts_doc tag → JSDoc
	optional          bool
	readonly          bool
	required          bool
	goKind            string // Go kind for Zod: "string", "int", "float64", etc.
	validate          []typemap.ValidateRule
	elementValidate   []typemap.ValidateRule
	elementGoKind     string
	validateOmitempty bool
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
	if t.PkgPath() == "encoding/json" {
		switch t.Name() {
		case "RawMessage":
			return "unknown"
		case "Number":
			return "string"
		}
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
		return "\x00" + key + "\x00"

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

	return "\x00" + genericKey + "\x00" + "<" + strings.Join(argTSNames, ", ") + ">"
}

// resolveGenericStructTS registers a generic interface definition.
// subs maps concrete type arg types to parameter names (e.g., EffectRow → "T").
func resolveGenericStructTS(t reflect.Type, baseName, key string, paramNames []string, subs map[reflect.Type]string, defs map[string]*reflectDef) {
	defs[key] = &reflectDef{
		name:       baseName,
		pkgPath:    t.PkgPath(),
		typeParams: paramNames,
	}

	var fields []reflectField
	var extends []string
	collectFieldsTS(t, defs, &fields, &extends, subs)
	defs[key].fields = fields
	defs[key].extends = extends
}

func resolveStructTS(t reflect.Type, defs map[string]*reflectDef) {
	name := t.Name()
	key := t.PkgPath() + "." + name
	defs[key] = &reflectDef{name: name, pkgPath: t.PkgPath()}

	var fields []reflectField
	var extends []string
	collectFieldsTS(t, defs, &fields, &extends, nil)
	defs[key].fields = fields
	defs[key].extends = extends
}

func collectFieldsTS(t reflect.Type, defs map[string]*reflectDef, fields *[]reflectField, extends *[]string, subs map[reflect.Type]string) {
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

		// Embedded struct handling.
		if f.Anonymous && jsonName == "" {
			ft := f.Type
			isPtr := ft.Kind() == reflect.Pointer
			if isPtr {
				ft = ft.Elem()
			}

			// tstype:",extends" — emit extends clause instead of flattening.
			if hasTSTag && tstag.Extends && ft.Kind() == reflect.Struct {
				tsName := goTypeToTS(ft, defs, subs)
				if isPtr && !tstag.Required {
					tsName = "Partial<" + tsName + ">"
				}
				if extends != nil {
					*extends = append(*extends, tsName)
				}
				continue
			}

			// Default: flatten promoted fields.
			if ft.Kind() == reflect.Struct {
				collectFieldsTS(ft, defs, fields, extends, subs)
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

		// Extract Go kind for Zod type discrimination.
		rf.goKind = reflectGoKind(f.Type)

		// Parse validate tag and split at dive boundary.
		allRules := typemap.ParseValidateTag(string(f.Tag))
		sliceRules, elemRules := typemap.SplitAtDive(allRules)
		rf.validate = sliceRules
		rf.elementValidate = elemRules

		// Extract element Go kind for slice/array fields.
		if rf.goKind == "slice" || rf.goKind == "array" {
			rf.elementGoKind = reflectSliceElementGoKind(f.Type)
		}

		// Check for validate:"required" and validate:"omitempty".
		// Note: tstype tag overrides below take final precedence.
		for _, rule := range rf.validate {
			if rule.Tag == "required" {
				rf.optional = false
			}
			if rule.Tag == "omitempty" {
				rf.validateOmitempty = true
			}
		}

		// Apply tstype tag overrides (final precedence over validate tags).
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

		// Apply ts_doc tag for JSDoc.
		if doc, ok := typemap.ParseTSDocTag(string(f.Tag)); ok {
			rf.comment = doc
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

// resolveDisplayNames computes a mapping from def key (pkgPath.Name) to
// display name. When no collisions exist, display names equal short names.
// On collision (multiple packages define the same type name), names are
// prefixed with the title-cased last segment of the package path.
func resolveDisplayNames(defs map[string]*reflectDef) map[string]string {
	// Group keys by short name.
	byName := map[string][]string{} // shortName → [keys...]
	for key, d := range defs {
		byName[d.name] = append(byName[d.name], key)
	}

	display := make(map[string]string, len(defs))
	for key, d := range defs {
		if len(byName[d.name]) > 1 {
			// Collision — prefix with title-cased package last segment.
			pkg := d.pkgPath
			if idx := strings.LastIndexByte(pkg, '/'); idx >= 0 {
				pkg = pkg[idx+1:]
			}
			if len(pkg) > 0 {
				display[key] = strings.ToUpper(pkg[:1]) + pkg[1:] + d.name
			} else {
				display[key] = d.name
			}
		} else {
			display[key] = d.name
		}
	}
	return display
}

// resolveTypeTokens replaces all \x00key\x00 tokens in s with display names.
func resolveTypeTokens(s string, display map[string]string) string {
	if !strings.ContainsRune(s, '\x00') {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for {
		start := strings.IndexByte(s, '\x00')
		if start < 0 {
			b.WriteString(s)
			break
		}
		b.WriteString(s[:start])
		s = s[start+1:]
		end := strings.IndexByte(s, '\x00')
		if end < 0 {
			b.WriteByte('\x00')
			continue
		}
		key := s[:end]
		if name, ok := display[key]; ok {
			b.WriteString(name)
		} else {
			b.WriteString(key)
		}
		s = s[end+1:]
	}
	return b.String()
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
	if t.PkgPath() == "encoding/json" && t.Name() == "RawMessage" {
		return "json.RawMessage"
	}

	switch t.Kind() {
	case reflect.String:
		return "string"
	case reflect.Bool:
		return "bool"
	case reflect.Int:
		return "int"
	case reflect.Int8:
		return "int8"
	case reflect.Int16:
		return "int16"
	case reflect.Int32:
		return "int32"
	case reflect.Int64:
		return "int64"
	case reflect.Uint:
		return "uint"
	case reflect.Uint8:
		return "uint8"
	case reflect.Uint16:
		return "uint16"
	case reflect.Uint32:
		return "uint32"
	case reflect.Uint64:
		return "uint64"
	case reflect.Float32:
		return "float32"
	case reflect.Float64:
		return "float64"
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

// reflectSliceElementGoKind returns the Go kind of a slice or array's element type.
func reflectSliceElementGoKind(t reflect.Type) string {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Slice, reflect.Array:
		return reflectGoKind(t.Elem())
	}
	return ""
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
