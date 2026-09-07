package codegen

import (
	"strings"

	"github.com/befabri/trpcgo/internal/typemap"
)

// zodVariants names the private schema variants a module emits beside each
// public schema. Types in contexts get fixed-array variants; types in
// unvalidated get a variant that checks the JSON wire shape but applies no
// validation rule, because validator never reaches those values.
type zodVariants struct {
	contexts    map[string]bool
	unvalidated map[string]bool
}

// zodUnvalidatedTypes finds the named types validator never validates: the
// struct elements of a slice or map that no dive reaches, the struct behind a
// structonly or nostructlevel field, and every type reachable from those.
// Fixed-array elements without dive already use the array-context wire variant.
func zodUnvalidatedTypes(defs map[string]typemap.TypeDef, reachable map[string]bool) map[string]bool {
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
		if !skip {
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
	for _, rule := range rules {
		if typemap.StructOnlyZodTag(rule.Tag) {
			return true
		}
	}
	return false
}
