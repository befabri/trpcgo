package codegen

import (
	"slices"
	"strconv"
	"strings"

	"github.com/befabri/trpcgo/internal/typemap"
	"github.com/befabri/trpcgo/zodconfig"
)

// Explicit predicates must resolve their free identifiers in module scope.
// Rendering one inside a generated callback can shadow an imported namespace
// with locals such as value, data, check or result. Factories preserve the
// original expression and its parse-time evaluation (including live imports),
// while the existing caller still catches evaluation/invocation failures.
func hoistZodCustomPredicates(order []string, defs map[string]typemap.TypeDef, structRules map[string][]zodconfig.StructRule) (map[string]typemap.TypeDef, map[string][]zodconfig.StructRule, string) {
	var declarations strings.Builder
	names := make(map[string]string)
	bind := func(expression string) string {
		if name, ok := names[expression]; ok {
			return name + "()"
		}
		name := "$goCustomPredicate" + strconv.Itoa(len(names))
		names[expression] = name
		declarations.WriteString("const " + name + ": () => (...args: any[]) => unknown = () => (" + expression + ");\n")
		return name + "()"
	}
	var rules func([]typemap.ValidateRule)
	rules = func(program []typemap.ValidateRule) {
		for i := range program {
			if custom := program[i].Custom; custom != nil && !custom.ServerOnly {
				custom.Predicate = bind(custom.Predicate)
			}
			rules(program[i].Alternatives)
		}
	}
	var definition func(*typemap.TypeDef)
	var field func(*typemap.Field)
	var element func(*typemap.ElementType)
	var refinement func(*typemap.Refinement)
	refinement = func(ref *typemap.Refinement) {
		if ref.ScalarRule != nil {
			rules([]typemap.ValidateRule{*ref.ScalarRule})
		}
		for i := range ref.Alternatives {
			refinement(&ref.Alternatives[i])
		}
	}
	element = func(value *typemap.ElementType) {
		if value == nil {
			return
		}
		if value.Inline != nil {
			definition(value.Inline)
		}
		element(value.Element)
		element(value.Key)
	}
	field = func(value *typemap.Field) {
		if value.ZodOmit {
			return
		}
		rules(value.Validate)
		rules(value.ElementValidate)
		if value.Inline != nil {
			definition(value.Inline)
		}
		element(value.Element)
		element(value.Key)
	}
	definition = func(value *typemap.TypeDef) {
		omitted := make(map[string]bool)
		for i := range value.Fields {
			if value.Fields[i].ZodOmit {
				omitted[value.Fields[i].Name] = true
			}
			field(&value.Fields[i])
		}
		if value.Underlying != nil {
			field(value.Underlying)
		}
		for i := range value.Refinements {
			if !zodRefinementOmitted(value.Refinements[i], omitted) {
				refinement(&value.Refinements[i])
			}
		}
	}
	cloned := make(map[string]typemap.TypeDef, len(defs))
	for name, def := range defs {
		cloned[name] = def
	}
	clonedStructRules := make(map[string][]zodconfig.StructRule, len(structRules))
	for _, name := range order {
		def, exists := defs[name]
		if !exists {
			continue
		}
		def = typemap.ResolveTypeDef(def, nil)
		definition(&def)
		cloned[name] = def
		if values := structRules[name]; len(values) > 0 {
			values = slices.Clone(values)
			for i := range values {
				values[i].Path = slices.Clone(values[i].Path)
				values[i].Predicate = bind(values[i].Predicate)
			}
			clonedStructRules[name] = values
		}
	}
	if declarations.Len() > 0 {
		declarations.WriteByte('\n')
	}
	return cloned, clonedStructRules, declarations.String()
}
