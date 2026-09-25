package codegen

import (
	"cmp"
	"maps"
	"slices"
	"strings"

	"github.com/befabri/trpcgo/internal/typemap"
)

func zodFieldType(f typemap.Field) string { return cmp.Or(f.ZodType, f.Type) }

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

// zodPassthroughTypes finds the named types that zod_omit fields reference,
// with every type their declarations reference in turn. The module declares
// their shapes without emitting schemas, which could fail on rules the omission
// exists to avoid.
func zodPassthroughTypes(defs map[string]typemap.TypeDef, reachable map[string]bool) map[string]bool {
	passthrough := make(map[string]bool)
	var visitFields func(fields []typemap.Field, omitted bool)
	visitType := func(ts string) {
		for _, name := range extractTypeRefs(ts) {
			def, ok := defs[name]
			if !ok || passthrough[name] {
				continue
			}
			passthrough[name] = true
			visitFields(def.Fields, true)
			if def.Underlying != nil {
				visitFields([]typemap.Field{*def.Underlying}, true)
			}
			for _, base := range def.Extends {
				visitFields([]typemap.Field{{Type: base}}, true)
			}
		}
	}
	var visitElement func(*typemap.ElementType)
	visitElement = func(e *typemap.ElementType) {
		for ; e != nil; e = e.Element {
			if e.Inline != nil {
				visitFields(e.Inline.Fields, false)
			}
			visitElement(e.Key)
		}
	}
	visitFields = func(fields []typemap.Field, omitted bool) {
		for _, f := range fields {
			switch {
			case omitted || f.ZodOmit:
				visitType(zodPassthroughType(f))
			case f.Inline != nil:
				visitFields(f.Inline.Fields, false)
			default:
				visitElement(f.Element)
				visitElement(f.Key)
			}
		}
	}
	for name := range reachable {
		def := defs[name]
		visitFields(def.Fields, false)
		if def.Underlying != nil {
			visitFields([]typemap.Field{*def.Underlying}, false)
		}
	}
	return passthrough
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
	// Types referenced only by zod_omit fields have no schema, so they follow
	// the schemas in name order.
	names := slices.Clone(plan.Order)
	scheduled := make(map[string]bool, len(names))
	for _, name := range names {
		scheduled[name] = true
	}
	for _, name := range slices.Sorted(maps.Keys(variants.passthrough)) {
		if !scheduled[name] {
			names = append(names, name)
		}
	}
	for _, name := range names {
		def, ok := defs[name]
		if !ok || !reachable[name] && !variants.passthrough[name] {
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
						field.Type, field.Optional = shapeType(zodPassthroughType(field)), true
					} else {
						field.Type = shapeType(zodArrayShapeFieldType(field, context, wireOnly, variants))
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
					ts = zodArrayShapeFieldType(*def.Underlying, context, wireOnly, variants)
					if variants.collections[name] && !context && !wireOnly {
						ts = zodArrayScopedShape(*def.Underlying, zodRootScope(def.Underlying.Element), false, false, variants)
					}
				}
				shape = directRecordType(shapeType(ts))
			case typemap.TypeDefUnion:
				shape = strings.Join(def.UnionMembers, " | ")
				if def.Underlying != nil {
					shape = shapeType(zodArrayShapeFieldType(*def.Underlying, context, wireOnly, variants))
				}
			}
			ew.printf("type $%s = %s;\n\n", mode.prefix+name, shape)
		}
	}
}

// zodShapeFieldType reconstructs only the schema type, using retained metadata
// for anonymous objects so omissions and overrides agree with runtime schemas.
func zodShapeFieldType(f typemap.Field) string {
	return zodMetadataType(zodFieldType(f), f.Inline, f.Element, f.Key, zodShapeField)
}

// zodShapeField renders an anonymous object's field for dependency discovery.
// A zod_omit field references no schema, so its named types become unknown.
func zodShapeField(f typemap.Field) typemap.Field {
	if f.ZodOmit {
		f.Type, f.Optional = mapTypeNames(f.Type, func(string) string { return "unknown" }), true
	} else {
		f.Type = zodShapeFieldType(f)
		f.Optional = typemap.ZodFieldOptional(f)
	}
	return f
}

// zodPassthroughType is the type a zod_omit field keeps in the schema. Nothing
// checks the value, so anonymous objects keep every field with its public
// optionality. Named references keep the module's concrete names; writers
// resolve them to shape declarations so parsed values keep their structure.
func zodPassthroughType(f typemap.Field) string {
	if f.TypeOverride {
		return f.Type
	}
	return zodMetadataType(zodFieldType(f), f.Inline, f.Element, f.Key, func(f typemap.Field) typemap.Field {
		f.Type = zodPassthroughType(f)
		return f
	})
}

func zodMetadataType(ts string, inline *typemap.TypeDef, element, key *typemap.ElementType, field func(typemap.Field) typemap.Field) string {
	if inline != nil {
		var fields []typemap.Field
		for _, f := range inline.Fields {
			fields = append(fields, field(f))
		}
		return typemap.InlineObjectType(fields)
	}
	if element != nil && strings.HasSuffix(ts, "[]") {
		elementType := cmp.Or(element.Type, strings.TrimSuffix(ts, "[]"))
		return zodMetadataType(elementType, element.Inline, element.Element, element.Key, field) + "[]"
	}
	partial := strings.HasPrefix(ts, "Partial<Record<")
	if partial {
		ts = ts[len("Partial<") : len(ts)-1]
	}
	if strings.HasPrefix(ts, "Record<") && element != nil && key != nil {
		parts := splitTopLevel(ts[len("Record<"):len(ts)-1], ',')
		if len(parts) == 2 {
			keyType, elementType := cmp.Or(key.Type, strings.TrimSpace(parts[0])), cmp.Or(element.Type, strings.TrimSpace(parts[1]))
			ts = "Record<" + zodMetadataType(keyType, key.Inline, key.Element, key.Key, field) + ", " + zodMetadataType(elementType, element.Inline, element.Element, element.Key, field) + ">"
		}
	}
	if partial {
		ts = "Partial<" + ts + ">"
	}
	return ts
}

// shapeVariant selects which private schema variant a shape type describes.
type shapeVariant struct {
	context  bool
	wireOnly bool
	prefix   string
}

// zodVariants names the private schema variants a module emits beside each
// public schema. Types in contexts get fixed-array variants; types in
// unvalidated get a variant that checks the JSON wire shape but applies no
// validation rule, because validator never reaches those values. Collections
// are the named collections whose public schema validates their structs, and
// passthrough types are declared for zod_omit fields without any schema.
type zodVariants struct {
	contexts    map[string]bool
	unvalidated map[string]bool
	collections map[string]bool
	passthrough map[string]bool
}

// zodRootCollections finds the named slices, arrays and maps whose elements
// reach a struct. Their exported schemas validate those structs, as
// StructValidator does for a collection input, while a field without dive
// never enters them.
func zodRootCollections(defs map[string]typemap.TypeDef, reachable map[string]bool) map[string]bool {
	collections := make(map[string]bool)
	var reachesStruct func(typemap.Field, map[string]bool) bool
	reachesStruct = func(field typemap.Field, visiting map[string]bool) bool {
		if field.Inline != nil {
			return true
		}
		ts := zodFieldType(field)
		if def, ok := defs[ts]; ok {
			if def.Kind == typemap.TypeDefInterface {
				return true
			}
			if def.Kind != typemap.TypeDefAlias || def.Underlying == nil || visiting[ts] {
				return false
			}
			visiting[ts] = true
			field = *def.Underlying
		}
		return field.Element != nil && reachesStruct(zodElementField("", field.Element), visiting)
	}
	for name := range reachable {
		def := defs[name]
		if def.Kind == typemap.TypeDefAlias && def.Underlying != nil && def.Underlying.Element != nil &&
			reachesStruct(zodElementField("", def.Underlying.Element), map[string]bool{name: true}) {
			collections[name] = true
		}
	}
	return collections
}

// zodRootScope dives into every element level below a collection input, the
// values StructValidator walks. Map keys are never validated.
func zodRootScope(element *typemap.ElementType) typemap.ValidationScope {
	if element == nil {
		return typemap.ValidationScope{}
	}
	inner := zodRootScope(element.Element)
	return typemap.ValidationScope{Element: &inner}
}

// zodUnvalidatedTypes finds the named types validator never validates: the
// struct elements of a slice or map that no dive reaches, the struct behind a
// structonly or nostructlevel field, and every type reachable from those.
// Fixed-array elements without dive already use the array-context wire variant.
// A root collection referenced without dive also needs a rule-free variant,
// because its exported schema validates its structs.
func zodUnvalidatedTypes(defs map[string]typemap.TypeDef, reachable, collections map[string]bool) map[string]bool {
	unvalidated := make(map[string]bool)
	var visitDefinition func(typemap.TypeDef, bool)
	var visitField func(typemap.Field, typemap.ValidationScope, bool)
	visitDefinition = func(def typemap.TypeDef, skip bool) {
		for _, field := range def.Fields {
			if field.ZodOmit {
				continue
			}
			scope, _ := typemap.FieldValidationScope(field)
			visitField(field, scope, skip)
		}
		if def.Underlying != nil {
			scope, _ := typemap.FieldValidationScope(*def.Underlying)
			visitField(*def.Underlying, scope, skip)
		}
	}
	visitField = func(field typemap.Field, scope typemap.ValidationScope, skip bool) {
		skip = skip || zodStructOnlyScope(scope.Rules)
		if field.Inline != nil {
			visitDefinition(*field.Inline, skip)
			return
		}
		ts := zodFieldType(field)
		if field.Element != nil {
			_, _, record := zodRecordTypes(ts)
			if field.ArrayLen != nil || strings.HasSuffix(ts, "[]") || record {
				child := typemap.ValidationScope{}
				if scope.Element != nil {
					child = *scope.Element
				}
				visitField(zodElementField("", field.Element), child, skip || scope.Element == nil && field.ArrayLen == nil)
			}
		}
		if !skip && (!collections[ts] || scope.Element != nil) {
			return
		}
		if def, exists := defs[ts]; exists && def.Kind != typemap.TypeDefUnion && !unvalidated[ts] {
			unvalidated[ts] = true
			visitDefinition(def, true)
		}
	}
	for name := range reachable {
		visitDefinition(defs[name], false)
	}
	return unvalidated
}

// zodStructOnlyScope reports whether a field's rules end in structonly or
// nostructlevel, which stop validator from entering the field's struct value.
func zodStructOnlyScope(rules []typemap.ValidateRule) bool {
	return zodStructOnlyTag(rules) != ""
}

// zodStructOnlyTag returns the structonly or nostructlevel tag among a field's
// rules, or "" when validator enters the field's struct value.
func zodStructOnlyTag(rules []typemap.ValidateRule) string {
	for _, rule := range rules {
		if typemap.StructOnlyZodTag(rule.Tag) {
			return rule.Tag
		}
	}
	return ""
}
