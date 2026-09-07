package codegen

import (
	"strings"

	"github.com/befabri/trpcgo/internal/typemap"
)

// Fixed-array decoding creates complete Go zero values, including nil pointers,
// maps and slices inside zero structs. Private schema variants keep that Go
// value context separate from the ordinary JSON object schema's null policy.
func zodArrayContexts(defs map[string]typemap.TypeDef, reachable map[string]bool) map[string]bool {
	contexts := make(map[string]bool)
	var visitField func(typemap.Field, bool)
	visitDefinition := func(def typemap.TypeDef, context bool) {
		for _, field := range def.Fields {
			if !field.ZodOmit {
				visitField(field, context)
			}
		}
		if def.Underlying != nil {
			visitField(*def.Underlying, context)
		}
	}
	visitField = func(field typemap.Field, context bool) {
		if field.Inline != nil {
			visitDefinition(*field.Inline, context)
			return
		}
		ts := zodFieldType(field)
		if field.Element != nil {
			if field.ArrayLen != nil || strings.HasSuffix(ts, "[]") {
				visitField(zodElementField("", field.Element), context || field.ArrayLen != nil)
			} else if _, _, ok := zodRecordTypes(ts); ok {
				visitField(zodElementField("", field.Element), context)
			}
		}
		if context {
			if def, exists := defs[ts]; exists && def.Kind != typemap.TypeDefUnion && !contexts[ts] {
				contexts[ts] = true
				visitDefinition(def, true)
			}
		}
	}
	for name := range reachable {
		visitDefinition(defs[name], false)
	}
	return contexts
}

func (e zodSchemaEmitter) schemaName(name string) string {
	if e.arrayContext && e.arrayContexts[name] {
		if e.skipValidation {
			return "$goArrayWire" + name
		}
		return "$goArray" + name
	}
	if e.unvalidated && e.unvalidatedTypes[name] {
		return "$goUnvalidated" + name
	}
	return name
}

func zodArrayNilPredicate(field typemap.Field) string {
	if !goArrayNilEligible(field) {
		return "false"
	}
	return typemap.ZodMissingValuePredicate(field)
}

// Both public wire types and schema variants use the same Go nil-capable kinds.
// Schema variants additionally evaluate validation rules before accepting nil.
func goArrayNilEligible(field typemap.Field) bool {
	return field.IsPointer || field.GoKind == "map" || field.GoKind == "slice" || field.GoKind == "[]byte" || field.GoKind == "unknown" || field.GoKind == "json.RawMessage"
}

func (e zodSchemaEmitter) arrayContextNullable(schema string, field typemap.Field) string {
	if !e.arrayContext {
		return schema
	}
	predicate := zodArrayNilPredicate(field)
	if predicate == "false" {
		return schema
	}
	nilSchema := "z.null()"
	if predicate != "true" {
		nilSchema += ".check(z.refine(() => " + predicate + "))"
	}
	return "z.union([" + schema + ", " + nilSchema + "])"
}

// Schema-only shape reconstruction uses the same context as the emitter. It
// includes nil outputs produced by array padding and private named references,
// without changing the public Go/TypeScript declaration mapper.
func zodArrayShapeFieldType(field typemap.Field, context bool, variants zodVariants, wireOnly ...bool) string {
	scope, _ := typemap.FieldValidationScope(field)
	return zodArrayScopedShape(field, scope, context, len(wireOnly) > 0 && wireOnly[0], variants)
}

func zodArrayScopedShape(field typemap.Field, scope typemap.ValidationScope, context, wireOnly bool, variants zodVariants) string {
	if wireOnly {
		scope = typemap.ValidationScope{}
		field.ValidateOmitempty = false
	}
	field.Validate = scope.Rules
	field.ElementValidate = nil
	ts := zodFieldType(field)
	if field.Inline != nil {
		fields := make([]typemap.Field, 0, len(field.Inline.Fields))
		for _, child := range field.Inline.Fields {
			if child.ZodOmit {
				child.Type, child.Optional = "unknown", true
			} else {
				child.Type = zodArrayShapeFieldType(child, context, variants, wireOnly)
				if wireOnly {
					child.ValidateOmitempty = false
				}
				child.Optional = typemap.ZodFieldOptional(child)
			}
			fields = append(fields, child)
		}
		ts = typemap.InlineObjectType(fields)
	} else if element, ok := strings.CutSuffix(ts, "[]"); ok {
		child := zodElementField(unwrapZodParentheses(element), field.Element)
		childScope := typemap.ValidationScope{}
		if scope.Element != nil {
			childScope = *scope.Element
		}
		childContext := context || field.ArrayLen != nil
		element = zodArrayScopedShape(child, childScope, childContext, wireOnly || scope.Element == nil, variants)
		if len(splitTopLevel(element, '|')) > 1 {
			element = "(" + element + ")"
		}
		ts = element + "[]"
	} else if key, value, ok := zodRecordTypes(ts); ok {
		partial := strings.HasPrefix(ts, "Partial<")
		key = zodArrayShapeFieldType(zodElementField(key, field.Key), false, variants, wireOnly)
		childScope := typemap.ValidationScope{}
		if scope.Element != nil {
			childScope = *scope.Element
		}
		value = zodArrayScopedShape(zodElementField(value, field.Element), childScope, context, wireOnly || scope.Element == nil, variants)
		ts = "Record<" + key + ", " + value + ">"
		if partial {
			ts = "Partial<" + ts + ">"
		}
	} else if context && variants.contexts[ts] {
		if wireOnly {
			ts = "$goArrayWire" + ts
		} else {
			ts = "$goArray" + ts
		}
	} else if wireOnly && variants.unvalidated[ts] {
		ts = "$goUnvalidated" + ts
	}
	if context && ts != "unknown" && zodArrayNilPredicate(field) != "false" {
		ts += " | null"
	}
	return ts
}
