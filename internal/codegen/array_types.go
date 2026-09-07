package codegen

import (
	"maps"
	"slices"
	"strings"

	"github.com/befabri/trpcgo/internal/typemap"
)

// Go fixed-array decoding materializes zero elements, including nil values in
// nested structs. Public array positions must admit those values so parsed Zod
// data can be passed to a typed tRPC client. Ordinary named structs keep their
// existing declaration; only array positions use the contextual variants.
func publicGoArrayTypes(procs []ProcEntry, defs []typemap.TypeDef) ([]ProcEntry, []typemap.TypeDef) {
	byName := make(map[string]typemap.TypeDef)
	for _, def := range defs {
		byName[def.Name] = def
		for _, instance := range def.Specializations {
			byName[instance.Name] = instance
		}
	}
	all := make(map[string]bool, len(byName))
	for name := range byName {
		all[name] = true
	}
	if !zodNeedsFixedArrays(byName, all) {
		return procs, defs
	}
	// Plan concrete generic identities from the same graph used by schemas.
	// Public TS arguments alone cannot distinguish Box[int] and Box[*int].
	concrete := make(map[string]bool)
	for name, def := range byName {
		if def.InstanceOf != "" {
			concrete[name] = zodNeedsFixedArrays(byName, transitiveReachable(map[string]bool{name: true}, byName))
		}
	}
	variants := make(map[string]typemap.TypeDef)
	var fieldType func(typemap.Field, bool) string
	var definition func(typemap.TypeDef, bool) typemap.TypeDef
	variant := func(name string, context bool) string {
		prefix := "$GoType"
		if context {
			prefix = "$GoArray"
		}
		key := prefix + name
		if _, ok := variants[key]; ok {
			return key
		}
		d := byName[name]
		d.Name = key
		variants[key] = d // register before recursive fields
		variants[key] = definition(d, context)
		return key
	}
	definition = func(def typemap.TypeDef, context bool) typemap.TypeDef {
		def = typemap.ResolveTypeDef(def, nil)
		for i := range def.Fields {
			def.Fields[i].Type = fieldType(def.Fields[i], context)
		}
		if def.Underlying != nil {
			def.AliasOf = fieldType(*def.Underlying, context)
		}
		for i, base := range def.Extends {
			metadata := base
			if i < len(def.ZodExtends) {
				metadata = def.ZodExtends[i]
			}
			partial := strings.HasPrefix(base, "Partial<") && strings.HasSuffix(base, ">")
			if partial {
				base = base[len("Partial<") : len(base)-1]
				metadata = strings.TrimSuffix(strings.TrimPrefix(metadata, "Partial<"), ">")
			}
			base = fieldType(typemap.Field{Type: base, ZodType: metadata}, context)
			if partial {
				base = "Partial<" + base + ">"
			}
			def.Extends[i] = base
		}
		return def
	}
	fieldType = func(field typemap.Field, context bool) string {
		ts := field.Type
		if field.TypeOverride {
			return ts
		}
		if field.Inline != nil {
			d := definition(*field.Inline, context)
			ts = typemap.InlineObjectType(d.Fields)
		} else if element, ok := strings.CutSuffix(ts, "[]"); ok {
			child := publicArrayElement(unwrapZodParentheses(element), field.Element)
			element = fieldType(child, context || field.ArrayLen != nil)
			if len(splitTopLevel(element, '|')) > 1 {
				element = "(" + element + ")"
			}
			ts = element + "[]"
		} else if key, value, ok := zodRecordTypes(ts); ok {
			partial := strings.HasPrefix(ts, "Partial<")
			value = fieldType(publicArrayElement(value, field.Element), context)
			ts = "Record<" + key + ", " + value + ">"
			if partial {
				ts = "Partial<" + ts + ">"
			}
		} else {
			metadata := zodFieldType(field)
			name := metadata
			if _, ok := byName[name]; !ok {
				name = stripGenericArgs(ts)
			}
			if def, ok := byName[name]; ok && def.Kind != typemap.TypeDefUnion {
				if context {
					ts = variant(name, true)
					if def.InstanceOf == "" && len(def.TypeParams) > 0 {
						if index := strings.IndexByte(field.Type, '<'); index >= 0 {
							ts += field.Type[index:]
						}
					}
				} else if concrete[name] {
					ts = variant(name, false)
				}
			}
		}
		if context && ts != "unknown" && goArrayNilEligible(field) {
			ts += " | null"
		}
		return ts
	}
	out := make([]typemap.TypeDef, 0, len(defs))
	for _, def := range defs {
		out = append(out, definition(def, false))
	}
	procs = slices.Clone(procs)
	var procedureType func(string, string) string
	procedureType = func(public, metadata string) string {
		if concrete[metadata] {
			return variant(metadata, false)
		}
		if publicElement, ok := strings.CutSuffix(public, "[]"); ok {
			if metadataElement, ok := strings.CutSuffix(metadata, "[]"); ok {
				return procedureType(unwrapZodParentheses(publicElement), unwrapZodParentheses(metadataElement)) + "[]"
			}
		}
		if key, value, ok := zodRecordTypes(public); ok {
			if _, metadataValue, ok := zodRecordTypes(metadata); ok {
				result := "Record<" + key + ", " + procedureType(value, metadataValue) + ">"
				if strings.HasPrefix(public, "Partial<") {
					result = "Partial<" + result + ">"
				}
				return result
			}
		}
		if fields, ok := inlineTypeFields(public); ok {
			if metadataFields, ok := inlineTypeFields(metadata); ok {
				for i := range fields {
					for _, other := range metadataFields {
						if fields[i].Name == other.Name {
							fields[i].Type = procedureType(fields[i].Type, other.Type)
						}
					}
				}
				return typemap.InlineObjectType(fields)
			}
		}
		return public
	}
	for i := range procs {
		procs[i].InputTS = procedureType(procs[i].InputTS, procs[i].InputZod)
		procs[i].OutputTS = procedureType(procs[i].OutputTS, procs[i].OutputZod)
	}
	for _, name := range slices.Sorted(maps.Keys(variants)) {
		out = append(out, variants[name])
	}
	return procs, out
}

func publicArrayElement(ts string, element *typemap.ElementType) typemap.Field {
	field := zodElementField(ts, element)
	field.ZodType, field.Type = field.Type, ts
	return field
}
