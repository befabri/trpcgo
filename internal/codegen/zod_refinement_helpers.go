package codegen

import "github.com/befabri/trpcgo/internal/typemap"

// writeZodRefinementHelpers emits each runtime helper once per module, including
// helpers used by refinements inside inline struct schemas.
func writeZodRefinementHelpers(ew *errWriter, defs map[string]typemap.TypeDef, reachable map[string]bool) {
	needsTime, needsString := false, false
	var visitDef func(typemap.TypeDef)
	var visitField func(typemap.Field)
	var visitElement func(*typemap.ElementType)
	visitElement = func(element *typemap.ElementType) {
		if element == nil {
			return
		}
		if element.GoKind == "time.Time" {
			needsTime = true
		}
		if element.Inline != nil {
			visitDef(*element.Inline)
		}
		visitElement(element.Element)
		visitElement(element.Key)
	}
	visitField = func(field typemap.Field) {
		if field.Inline != nil {
			visitDef(*field.Inline)
		}
		visitElement(field.Element)
		visitElement(field.Key)
	}
	visitDef = func(def typemap.TypeDef) {
		fields := make(map[string]typemap.Field, len(def.Fields))
		for _, field := range def.Fields {
			fields[field.Name] = field
			visitField(field)
		}
		if def.Underlying != nil {
			visitField(*def.Underlying)
		}
		var visitRef func(typemap.Refinement)
		visitRef = func(ref typemap.Refinement) {
			if fields[ref.Field].GoKind == "time.Time" || fields[ref.OtherField].GoKind == "time.Time" || ref.OtherHidden != nil && ref.OtherHidden.GoKind == "time.Time" {
				needsTime = true
			}
			if zodComparisonKind(fields[ref.Field]) == "string" && (ref.Op == "===" || ref.Op == "!==") {
				needsString = true
			}
			for _, branch := range ref.Alternatives {
				visitRef(branch)
			}
		}
		for _, ref := range def.Refinements {
			visitRef(ref)
		}
	}
	for name := range reachable {
		visitDef(defs[name])
	}
	if needsString {
		ew.println("// encoding/json replaces lone UTF-16 surrogate escapes with U+FFFD.")
		ew.println("function $goString(value: string): string { return " + typemap.ZodGoStringValue("value") + "; }")
		ew.println("")
	}
	if !needsTime {
		return
	}
	ew.println(`// Compare Go time.Time instants without losing fractional nanoseconds to Date.
function $goTimeCompare(left: string, right: string): number {
  const a = ` + typemap.ZodGoTimeParts("left") + `, b = ` + typemap.ZodGoTimeParts("right") + `;
  if (a === null || b === null) return NaN;
  return a[0] === b[0] ? Math.sign(a[1] - b[1]) : Math.sign(a[0] - b[0]);
}`)
	ew.println("")
}
