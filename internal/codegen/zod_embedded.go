package codegen

import (
	"fmt"
	"strings"

	"github.com/befabri/trpcgo/internal/typemap"
)

// A flattened embedded pointer stays nil when every visible child is absent.
// Its field validators become active only when JSON allocates that pointer.
// Child schemas validate supplied values; these object checks validate omitted
// values against the Go zeros once a sibling instantiates the parent.
func writeZodEmbeddedPresence(ew *errWriter, fields []typemap.Field, style typemap.ZodStyle) {
	for _, field := range fields {
		if field.ZodOmit || len(field.WhenAnyPresent) == 0 {
			continue
		}
		missing := typemap.ZodMissingValuePredicate(field)
		if missing == "true" {
			continue
		}
		var absent []string
		for _, sibling := range field.WhenAnyPresent {
			absent = append(absent, zodDataAccess(sibling)+" === undefined")
		}
		predicate := "(" + strings.Join(absent, " && ") + ") || " + zodDataAccess(field.Name) + " !== undefined || (" + missing + ")"
		message := fmt.Sprintf("%s must satisfy validation when its embedded parent is present", field.Name)
		if style == typemap.ZodMini {
			ew.printf(".check(z.refine((data: any) => %s, { message: %s, path: [%s] }))", predicate, typemap.ZodStringLiteral(message), typemap.ZodStringLiteral(field.Name))
		} else {
			ew.printf(".refine((data) => %s, { message: %s, path: [%s] })", predicate, typemap.ZodStringLiteral(message), typemap.ZodStringLiteral(field.Name))
		}
	}
}
