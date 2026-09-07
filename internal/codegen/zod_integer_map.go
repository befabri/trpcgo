package codegen

import "github.com/befabri/trpcgo/internal/typemap"

func zodNeedsIntegerMaps(defs map[string]typemap.TypeDef, reachable map[string]bool) bool {
	var fieldHasMap func(typemap.Field) bool
	var defHasMap func(typemap.TypeDef) bool
	fieldHasMap = func(f typemap.Field) bool {
		if f.ZodOmit {
			return false
		}
		if key, _, ok := zodRecordTypes(zodFieldType(f)); ok && (key == "number" || f.Key != nil && zodIntegerKind(f.Key.GoKind)) {
			return true
		}
		return f.Inline != nil && defHasMap(*f.Inline) || f.Element != nil && fieldHasMap(zodElementField("", f.Element))
	}
	defHasMap = func(d typemap.TypeDef) bool {
		for _, f := range d.Fields {
			if fieldHasMap(f) {
				return true
			}
		}
		return d.Underlying != nil && fieldHasMap(*d.Underlying) || fieldHasMap(typemap.Field{Type: d.AliasOf})
	}
	for name := range reachable {
		if defHasMap(defs[name]) {
			return true
		}
	}
	return false
}

func writeZodIntegerMapHelpers(ew *errWriter) {
	ew.println(`// Go map keys are decoded before validator sees the map. JSON.parse alone
// discards source ordering, so aliases in ordinary objects are ambiguous.
function $goIntegerMap<S extends z.core.$ZodType>(schema: S, key: z.core.$ZodType, wireValue: (value: unknown) => boolean) {
  // Keep the public input type; the normalized object still needs schema
  // validation before it can claim any particular output type.
  return z.pipe(z.transform<z.core.input<S>, unknown>((input, ctx) => {
    if (input === null || typeof input !== "object" || Array.isArray(input)) return input;
    const source = $goJSONEntries(input);
    const entries = source ?? Object.entries(input);
    const result: Record<string, unknown> = {};
    const seen = new Set<string>();
    for (const [name, value] of entries) {
      if (!z.safeParse(key, name).success || !wireValue(value)) {
        ctx.issues.push({ code: "custom", input, path: [name], message: "Invalid Go map entry" });
        return input;
      }
      const canonical = BigInt(name).toString();
      if (!source && seen.has(canonical)) {
        ctx.issues.push({ code: "custom", input, path: [name], message: "Ambiguous integer map keys; use parseGoJSON with the original JSON text" });
        return input;
      }
      seen.add(canonical);
      result[canonical] = value;
    }
    return result;
  }), schema);
}`)
	ew.println("")
}
