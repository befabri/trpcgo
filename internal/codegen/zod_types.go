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
			refs = append(refs, extractTypeRefs(zodShapeFieldType(f))...)
		}
	}
	if def.Kind == typemap.TypeDefAlias {
		ts := def.AliasOf
		if def.Underlying != nil {
			ts = zodShapeFieldType(*def.Underlying)
		}
		refs = append(refs, extractTypeRefs(ts)...)
	}
	for _, base := range def.Extends {
		refs = append(refs, extractTypeRefs(base)...)
	}
	return refs
}

func writeZodShapeTypes(ew *errWriter, plan EmitPlan, defs map[string]typemap.TypeDef, variants zodVariants) {
	roots := make(map[string]bool)
	for name := range plan.Cycles {
		roots[name] = true
	}
	for name := range variants.contexts {
		roots[name] = true
	}
	for name := range variants.unvalidated {
		roots[name] = true
	}
	reachable := transitiveReachable(roots, defs)
	shapeType := func(ts string) string {
		return mapTypeNames(ts, func(name string) string {
			if _, ok := defs[name]; ok {
				return "$" + name
			}
			for _, prefix := range []string{"$goArrayWire", "$goArray"} {
				if original, ok := strings.CutPrefix(name, prefix); ok && variants.contexts[original] {
					return "$" + name
				}
			}
			if original, ok := strings.CutPrefix(name, "$goUnvalidated"); ok && variants.unvalidated[original] {
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
		modes := []shapeVariant{{}}
		if variants.contexts[name] {
			modes = append(modes, shapeVariant{context: true, prefix: "$goArray"}, shapeVariant{context: true, wireOnly: true, prefix: "$goArrayWire"})
		}
		if variants.unvalidated[name] {
			modes = append(modes, shapeVariant{wireOnly: true, prefix: "$goUnvalidated"})
		}
		for _, mode := range modes {
			context, wireOnly := mode.context, mode.wireOnly
			var shape string
			switch def.Kind {
			case typemap.TypeDefInterface:
				var fields []typemap.Field
				for _, field := range def.Fields {
					if field.ZodOmit {
						field.Type, field.Optional = zodOmittedFieldType(field), true
					} else {
						field.Type = shapeType(zodArrayShapeFieldType(field, context, variants, wireOnly))
						if wireOnly {
							field.ValidateOmitempty = false
						}
						field.Optional = typemap.ZodFieldOptional(field)
					}
					fields = append(fields, field)
				}
				shape = typemap.InlineObjectType(fields)
			case typemap.TypeDefAlias:
				ts := def.AliasOf
				if def.Underlying != nil {
					ts = zodArrayShapeFieldType(*def.Underlying, context, variants, wireOnly)
				}
				shape = directRecordType(shapeType(ts))
			case typemap.TypeDefUnion:
				shape = strings.Join(def.UnionMembers, " | ")
				if def.Underlying != nil {
					shape = shapeType(zodArrayShapeFieldType(*def.Underlying, context, variants, wireOnly))
				}
			}
			ew.printf("type $%s = %s;\n\n", mode.prefix+name, shape)
		}
	}
}

// zodShapeFieldType reconstructs only the schema type, using retained metadata
// for anonymous objects so omissions and overrides agree with runtime schemas.
func zodShapeFieldType(f typemap.Field) string {
	return zodMetadataType(zodFieldType(f), f.Inline, f.Element, f.Key)
}

// zodOmittedFieldType is the type a zod_omit field keeps in the schema: its
// public TypeScript type, with named references the schema module cannot
// import widened to unknown.
func zodOmittedFieldType(f typemap.Field) string {
	return mapTypeNames(f.Type, func(string) string { return "unknown" })
}

func zodMetadataType(ts string, inline *typemap.TypeDef, element, key *typemap.ElementType) string {
	if inline != nil {
		var fields []typemap.Field
		for _, f := range inline.Fields {
			if f.ZodOmit {
				f.Type, f.Optional = zodOmittedFieldType(f), true
			} else {
				f.Type = zodShapeFieldType(f)
				f.Optional = typemap.ZodFieldOptional(f)
			}
			fields = append(fields, f)
		}
		return typemap.InlineObjectType(fields)
	}
	if element != nil && strings.HasSuffix(ts, "[]") {
		elementType := cmp.Or(element.Type, strings.TrimSuffix(ts, "[]"))
		return zodMetadataType(elementType, element.Inline, element.Element, element.Key) + "[]"
	}
	partial := strings.HasPrefix(ts, "Partial<Record<")
	if partial {
		ts = ts[len("Partial<") : len(ts)-1]
	}
	if strings.HasPrefix(ts, "Record<") && element != nil && key != nil {
		parts := splitTopLevel(ts[len("Record<"):len(ts)-1], ',')
		if len(parts) == 2 {
			keyType, elementType := cmp.Or(key.Type, strings.TrimSpace(parts[0])), cmp.Or(element.Type, strings.TrimSpace(parts[1]))
			ts = "Record<" + zodMetadataType(keyType, key.Inline, key.Element, key.Key) + ", " + zodMetadataType(elementType, element.Inline, element.Element, element.Key) + ">"
		}
	}
	if partial {
		ts = "Partial<" + ts + ">"
	}
	return ts
}

// TypeScript permits recursive object literals but rejects a type alias that
// immediately instantiates Record with itself. Emit the equivalent direct
// object type at declaration boundaries, preserving named references in the IR.
func directRecordType(ts string) string {
	partial := strings.HasPrefix(ts, "Partial<Record<")
	original := ts
	if partial {
		ts = ts[len("Partial<") : len(ts)-1]
	}
	if !strings.HasPrefix(ts, "Record<") || !strings.HasSuffix(ts, ">") {
		return original
	}
	parts := splitTopLevel(ts[len("Record<"):len(ts)-1], ',')
	if len(parts) != 2 {
		return original
	}
	key, value := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	if !partial && (key == "string" || key == "number") {
		return "{ [key: " + key + "]: " + value + " }"
	}
	optional := ""
	if partial {
		optional = "?"
	}
	return "{ [key in " + key + "]" + optional + ": " + value + " }"
}

// shapeVariant selects which private schema variant a shape type describes.
type shapeVariant struct {
	context  bool
	wireOnly bool
	prefix   string
}
