package codegen

import (
	"strconv"
	"strings"

	"github.com/befabri/trpcgo/internal/typemap"
)

func zodNeedsFixedArrays(defs map[string]typemap.TypeDef, reachable map[string]bool) bool {
	var field func(typemap.Field) bool
	var definition func(typemap.TypeDef) bool
	field = func(f typemap.Field) bool {
		if f.ZodOmit {
			return false
		}
		return f.ArrayLen != nil || f.Inline != nil && definition(*f.Inline) || f.Element != nil && field(zodElementField("", f.Element))
	}
	definition = func(d typemap.TypeDef) bool {
		for _, f := range d.Fields {
			if field(f) {
				return true
			}
		}
		return d.Underlying != nil && field(*d.Underlying)
	}
	for name := range reachable {
		if definition(defs[name]) {
			return true
		}
	}
	return false
}

func writeZodArrayHelpers(ew *errWriter, order []string, defs map[string]typemap.TypeDef) {
	ew.println(`function $goEmbeddedPresent(value: unknown, names: readonly string[]): boolean {
  if (value === null || typeof value !== "object" || Array.isArray(value)) return false;
  return ($goJSONEntries(value) ?? Object.entries(value)).some(([name, item]) => item !== undefined && $goJSONFieldName(name, names) !== undefined);
}

function $goFixedArray<S extends z.core.$ZodType>(schema: S, length: number, zero: () => unknown, decode: (value: unknown, previous: unknown) => unknown) {
  return z.pipe(z.transform<z.core.input<S>, unknown>((input) => $goMergeFixedArray(input, undefined, length, zero, decode)), schema);
}

function $goMergeFixedArray(raw: unknown, previous: unknown, length: number, zero: () => unknown, decode: (value: unknown, previous: unknown) => unknown): unknown {
  if (!Array.isArray(raw)) return raw;
  const result: unknown[] = [];
  for (let index = 0; index < length; index++) {
    if (index >= raw.length) result.push(zero());
    else result.push(decode(raw[index], Array.isArray(previous) && index < previous.length ? previous[index] : zero()));
  }
  return result;
}
`)
	for _, name := range order {
		def, ok := defs[name]
		if !ok {
			continue
		}
		expression := "null"
		switch def.Kind {
		case typemap.TypeDefInterface:
			expression = zodZeroObject(def)
		case typemap.TypeDefAlias, typemap.TypeDefUnion:
			if def.Underlying != nil {
				expression = zodZeroValue(*def.Underlying)
			} else if def.Kind == typemap.TypeDefUnion && def.IsStringUnion() {
				expression = `""`
			} else if def.Kind == typemap.TypeDefUnion {
				expression = "0"
			} else {
				expression = zodZeroValue(typemap.Field{Type: def.AliasOf})
			}
		}
		ew.printf("function $goZero%s(): unknown { return %s; }\n", name, expression)
	}
	ew.println("")
}

func zodZeroObject(def typemap.TypeDef) string {
	return zodZeroObjectForValue(def, "")
}

// Promoted fields do not exist while their embedded pointer owner is nil.
// For a supplied object, each independently allocated embedding scope gets
// all its sibling Go zeros before source fields are merged over them.
func zodZeroObjectForValue(def typemap.TypeDef, value string) string {
	var fields []string
	var groups [][]string
	var groupFields [][]string
	groupIndex := make(map[string]int)
	for _, field := range def.Fields {
		if field.ZodOmit {
			continue
		}
		member := "[" + typemap.ZodStringLiteral(field.Name) + "]: " + zodZeroValue(field)
		if len(field.WhenAnyPresent) == 0 {
			fields = append(fields, member)
			continue
		}
		if value == "" {
			continue
		}
		names := make([]string, len(field.WhenAnyPresent))
		for i, name := range field.WhenAnyPresent {
			names[i] = typemap.ZodStringLiteral(name)
		}
		key := strings.Join(names, ",")
		index, exists := groupIndex[key]
		if !exists {
			index = len(groups)
			groupIndex[key] = index
			groups = append(groups, names)
			groupFields = append(groupFields, nil)
		}
		groupFields[index] = append(groupFields[index], member)
	}
	for index, names := range groups {
		fields = append(fields, "...($goEmbeddedPresent("+value+", ["+strings.Join(names, ", ")+"]) ? {"+strings.Join(groupFields[index], ", ")+"} : {})")
	}
	return "({" + strings.Join(fields, ", ") + "})"
}

func zodZeroValue(field typemap.Field) string {
	if field.IsPointer {
		return "null"
	}
	if field.JSONString {
		zero := "0"
		if field.GoKind == "string" {
			zero = `""`
		} else if field.GoKind == "bool" {
			zero = "false"
		}
		return typemap.ZodStringLiteral(zero)
	}
	if field.ArrayLen != nil {
		return "Array.from({length: " + strconv.FormatInt(*field.ArrayLen, 10) + "}, () => " + zodZeroValue(zodElementField("", field.Element)) + ")"
	}
	if field.Inline != nil {
		return zodZeroObject(*field.Inline)
	}
	switch field.GoKind {
	case "map", "slice", "[]byte", "unknown", "json.RawMessage":
		return "null"
	case "string":
		return `""`
	case "bool":
		return "false"
	case "time.Time":
		return `"0001-01-01T00:00:00Z"`
	case "json.Number":
		return "0"
	}
	if zodNumericKind(field.GoKind) {
		return "0"
	}
	ts := zodFieldType(field)
	switch ts {
	case "string":
		return `""`
	case "number":
		return "0"
	case "boolean":
		return "false"
	case "unknown":
		return "null"
	}
	return "$goZero" + ts + "()"
}

func zodWrapFixedArray(schema string, field, element typemap.Field) string {
	if field.ArrayLen == nil {
		return schema
	}
	return "$goFixedArray(" + schema + ", " + strconv.FormatInt(*field.ArrayLen, 10) + ", () => " + zodZeroValue(element) + ", (value, previous) => " + zodMergePredicate(element, "value", "previous") + ")"
}
