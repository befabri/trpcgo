package codegen

import (
	"cmp"
	"strconv"
	"strings"

	"github.com/befabri/trpcgo/internal/typemap"
)

func zodFieldType(f typemap.Field) string { return cmp.Or(f.ZodType, f.Type) }

// mapTypeNames visits type references without treating object property names
// or string literal contents as references.
func mapTypeNames(ts string, visit func(string) string) string {
	ts = strings.TrimSpace(ts)
	if parts := splitTopLevel(ts, '|'); len(parts) > 1 {
		for i := range parts {
			parts[i] = mapTypeNames(parts[i], visit)
		}
		return strings.Join(parts, " | ")
	}
	if strings.HasSuffix(ts, "[]") {
		return mapTypeNames(ts[:len(ts)-2], visit) + "[]"
	}
	if strings.HasPrefix(ts, "(") && strings.HasSuffix(ts, ")") {
		return "(" + mapTypeNames(ts[1:len(ts)-1], visit) + ")"
	}
	if fields, ok := inlineTypeFields(ts); ok {
		for i := range fields {
			fields[i].Type = mapTypeNames(fields[i].Type, visit)
		}
		return typemap.InlineObjectType(fields)
	}
	if i := strings.IndexByte(ts, '<'); i >= 0 && strings.HasSuffix(ts, ">") {
		base := ts[:i]
		if base != "Record" && base != "Partial" {
			base = visit(base)
		}
		args := splitTopLevel(ts[i+1:len(ts)-1], ',')
		for i := range args {
			args[i] = mapTypeNames(args[i], visit)
		}
		return base + "<" + strings.Join(args, ", ") + ">"
	}
	switch ts {
	case "string", "number", "boolean", "unknown", "void", "null", "undefined", "never", "true", "false":
		return ts
	}
	if strings.HasPrefix(ts, `"`) || strings.HasPrefix(ts, "'") {
		return ts
	}
	if _, err := strconv.ParseFloat(ts, 64); err == nil {
		return ts
	}
	return visit(ts)
}

func inlineTypeFields(ts string) ([]typemap.Field, bool) {
	if !strings.HasPrefix(ts, "{") || !strings.HasSuffix(ts, "}") {
		return nil, false
	}
	var fields []typemap.Field
	for _, prop := range splitTopLevel(ts[1:len(ts)-1], ';') {
		prop = strings.TrimSpace(prop)
		if prop == "" {
			continue
		}
		pair := splitTopLevel(prop, ':')
		if len(pair) != 2 {
			return nil, false
		}
		name, readonly := strings.CutPrefix(strings.TrimSpace(pair[0]), "readonly ")
		name, optional := strings.CutSuffix(name, "?")
		if decoded, err := strconv.Unquote(name); err == nil {
			name = decoded
		}
		fields = append(fields, typemap.Field{Name: name, Type: strings.TrimSpace(pair[1]), Readonly: readonly, Optional: optional})
	}
	return fields, true
}

func definitionRefs(def typemap.TypeDef) []string {
	var refs []string
	for _, f := range def.Fields {
		if !f.ZodOmit {
			refs = append(refs, extractTypeRefs(zodFieldType(f))...)
		}
	}
	if def.Kind == typemap.TypeDefAlias {
		refs = append(refs, extractTypeRefs(def.AliasOf)...)
	}
	for _, base := range def.Extends {
		refs = append(refs, extractTypeRefs(base)...)
	}
	return refs
}

func writeZodShapeTypes(ew *errWriter, plan EmitPlan, defs map[string]typemap.TypeDef) {
	roots := make(map[string]bool)
	for name := range plan.Cycles {
		roots[name] = true
	}
	reachable := transitiveReachable(roots, defs)
	shapeType := func(ts string) string {
		return mapTypeNames(ts, func(name string) string {
			if _, ok := defs[name]; ok {
				return "$" + name
			}
			return "unknown"
		})
	}
	for _, name := range plan.Order {
		def, ok := defs[name]
		if !ok || !reachable[name] {
			continue
		}
		var shape string
		switch def.Kind {
		case typemap.TypeDefInterface:
			var fields []typemap.Field
			for _, f := range def.Fields {
				if !f.ZodOmit {
					f.Type = shapeType(zodFieldType(f))
					fields = append(fields, f)
				}
			}
			shape = typemap.InlineObjectType(fields)
		case typemap.TypeDefAlias:
			shape = shapeType(def.AliasOf)
		case typemap.TypeDefUnion:
			shape = strings.Join(def.UnionMembers, " | ")
		}
		ew.printf("type $%s = %s;\n\n", name, shape)
	}
}
