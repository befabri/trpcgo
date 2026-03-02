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
			inputTS = goTypeToTS(p.inputType, defs)
		}
		outputTS := goTypeToTS(p.outputType, defs)
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
		typeDefs[i] = typemap.TypeDef{Name: d.name, Kind: typemap.TypeDefInterface, Fields: fields}
	}

	return entries, typeDefs
}

type reflectDef struct {
	name   string
	fields []reflectField
}

type reflectField struct {
	name     string
	tsType   string
	optional bool
	readonly bool
	required bool
}

func goTypeToTS(t reflect.Type, defs map[string]*reflectDef) string {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
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
			return goTypeToTS(dataField.Type, defs)
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
		elem := goTypeToTS(t.Elem(), defs)
		if strings.Contains(elem, "|") {
			return "(" + elem + ")[]"
		}
		return elem + "[]"

	case reflect.Array:
		return goTypeToTS(t.Elem(), defs) + "[]"

	case reflect.Map:
		key := goTypeToTS(t.Key(), defs)
		val := goTypeToTS(t.Elem(), defs)
		return fmt.Sprintf("Record<%s, %s>", key, val)

	case reflect.Struct:
		name := t.Name()
		if t.PkgPath() == "time" && name == "Time" {
			return "string"
		}
		if name == "" {
			return inlineStructTS(t, defs)
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

func resolveStructTS(t reflect.Type, defs map[string]*reflectDef) {
	name := t.Name()
	key := t.PkgPath() + "." + name
	defs[key] = &reflectDef{name: name}

	var fields []reflectField
	collectFieldsTS(t, defs, &fields)
	defs[key].fields = fields
}

func collectFieldsTS(t reflect.Type, defs map[string]*reflectDef, fields *[]reflectField) {
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
				collectFieldsTS(ft, defs, fields)
				continue
			}
		}

		if !f.IsExported() {
			continue
		}

		if jsonName == "" {
			jsonName = f.Name
		}

		tsType := goTypeToTS(f.Type, defs)
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

func inlineStructTS(t reflect.Type, defs map[string]*reflectDef) string {
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
		tsType := goTypeToTS(f.Type, defs)
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
