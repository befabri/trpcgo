package codegen

import (
	"strings"

	"github.com/befabri/trpcgo/internal/typemap"
)

// Optional object properties can disappear without running a child refinement.
// Validate an absent fixed array at the containing object, against the actual
// Go zero array, with application predicates evaluated only during parsing.
func (e zodSchemaEmitter) writeMissingArrayChecks(ew *errWriter, fields []typemap.Field) {
	for _, field := range fields {
		if field.ZodOmit || field.IsPointer || field.ArrayLen == nil || !typemap.ZodFieldOptional(field) {
			continue
		}
		scope, err := typemap.FieldValidationScope(field)
		if err != nil || len(scope.Rules) == 0 && scope.Element == nil {
			continue
		}
		plain := field
		plain.Optional, plain.ValidateOmitempty = false, false
		plain.WhenAnyPresent = nil
		var schema string
		if scope.Element == nil {
			// Go only visits array elements after dive. Checking an outer len/custom
			// rule must not accidentally activate struct validators in those elements.
			plain.Validate, plain.ElementValidate = scope.Rules, nil
			schema = applyZodArrayRules("z.array(z.unknown())", plain, e.style)
		} else {
			schema = e.scopedFieldToZod(plain, scope)
		}
		predicate := zodDataAccess(field.Name) + " !== undefined"
		if len(field.WhenAnyPresent) > 0 {
			absent := make([]string, len(field.WhenAnyPresent))
			for i, name := range field.WhenAnyPresent {
				absent[i] = zodDataAccess(name) + " === undefined"
			}
			predicate += " || (" + strings.Join(absent, " && ") + ")"
		}
		predicate += " || " + schema + ".safeParse(" + zodZeroValue(field) + ").success"
		ew.printf(".check(z.refine((data) => %s, { message: %s, path: [%s] }))", predicate, typemap.ZodStringLiteral(field.Name+" must satisfy validation when absent"), typemap.ZodStringLiteral(field.Name))
	}
}
