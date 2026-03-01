package trpcgo

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/trpcgo/trpcgo/internal/codegen"
	"github.com/trpcgo/trpcgo/internal/typemap"
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
	// Collect procedures sorted by path.
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

	// Collect interface definitions via reflect-based conversion.
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
			fields[j] = typemap.Field{Name: f.name, Type: f.tsType, Optional: f.optional}
		}
		typeDefs[i] = typemap.TypeDef{Name: d.name, Fields: fields}
	}

	return entries, typeDefs
}

// reflectDef is a collected TypeScript interface definition from reflect types.
type reflectDef struct {
	name   string
	fields []reflectField
}

type reflectField struct {
	name     string
	tsType   string
	optional bool
}

// goTypeToTS converts a reflect.Type to its TypeScript representation,
// collecting struct definitions into defs as a side effect.
func goTypeToTS(t reflect.Type, defs map[string]*reflectDef) string {
	// Unwrap pointer.
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
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
		// []byte → base64 string in JSON.
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
		// time.Time → string.
		if t.PkgPath() == "time" && name == "Time" {
			return "string"
		}
		if name == "" {
			return inlineStructTS(t, defs)
		}
		// Use PkgPath+Name as key to avoid collisions across packages.
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
	// Prevent infinite recursion.
	defs[key] = &reflectDef{name: name}

	var fields []reflectField
	collectFieldsTS(t, defs, &fields)
	defs[key].fields = fields
}

func collectFieldsTS(t reflect.Type, defs map[string]*reflectDef, fields *[]reflectField) {
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)

		tag := f.Tag.Get("json")
		if tag == "-" {
			continue
		}

		jsonName, omitempty := parseJSONFieldTag(tag)

		// Embedded struct: flatten fields.
		if f.Anonymous && jsonName == "" {
			ft := f.Type
			if ft.Kind() == reflect.Ptr {
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
		optional := omitempty || f.Type.Kind() == reflect.Ptr

		*fields = append(*fields, reflectField{name: jsonName, tsType: tsType, optional: optional})
	}
}

func inlineStructTS(t reflect.Type, defs map[string]*reflectDef) string {
	if t.NumField() == 0 {
		return "Record<string, never>"
	}
	var parts []string
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		tag := f.Tag.Get("json")
		if tag == "-" {
			continue
		}
		jsonName, omitempty := parseJSONFieldTag(tag)
		if jsonName == "" {
			jsonName = f.Name
		}
		tsType := goTypeToTS(f.Type, defs)
		opt := ""
		if omitempty || f.Type.Kind() == reflect.Ptr {
			opt = "?"
		}
		parts = append(parts, fmt.Sprintf("%s%s: %s", jsonName, opt, tsType))
	}
	if len(parts) == 0 {
		return "Record<string, never>"
	}
	return "{ " + strings.Join(parts, "; ") + " }"
}

func parseJSONFieldTag(tag string) (name string, omitempty bool) {
	if tag == "" {
		return "", false
	}
	parts := strings.Split(tag, ",")
	name = parts[0]
	for _, p := range parts[1:] {
		if p == "omitempty" {
			omitempty = true
		}
	}
	return name, omitempty
}
