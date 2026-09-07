package codegen

import (
	"strconv"
	"strings"

	"github.com/befabri/trpcgo/internal/typemap"
)

// Integer aliases can overwrite a value before validator runs. Keep decoding
// checks separate from validation: an overwritten min violation is harmless,
// but an overwritten JSON type/range error still makes encoding/json fail.
// Named checks are functions so recursive Go types require no initialization
// ordering or recursive schema inference. These checks never change values.
func writeZodWireChecks(ew *errWriter, order []string, defs map[string]typemap.TypeDef, style typemap.ZodStyle, allowUnknownFields ...bool) {
	allowUnknown := len(allowUnknownFields) > 0 && allowUnknownFields[0]
	ew.println(strings.ReplaceAll(`function $goWireObject(value: unknown, fields: Record<string, (value: unknown) => boolean>): boolean {
  if (value === null || typeof value !== "object" || Array.isArray(value)) return false;
  for (const [name, item] of $goJSONEntries(value) ?? Object.entries(value)) {
    const field = $goJSONFieldName(name, Object.keys(fields));
    if (field === undefined) { if (!ALLOW_UNKNOWN_FIELDS) return false; }
    else if (!fields[field]!(item)) return false;
  }
  return true;
}
function $goWireMap(value: unknown, key: (value: string) => boolean, element: (value: unknown) => boolean): boolean {
  return value !== null && typeof value === "object" && !Array.isArray(value)
    && ($goJSONEntries(value) ?? Object.entries(value)).every(([name, item]) => key(name) && element(item));
}`, "ALLOW_UNKNOWN_FIELDS", strconv.FormatBool(allowUnknown)))
	for _, name := range order {
		def, ok := defs[name]
		if !ok {
			continue
		}
		var predicate string
		switch def.Kind {
		case typemap.TypeDefInterface:
			predicate = zodWireObjectPredicate(def.Fields, "value", style)
		case typemap.TypeDefAlias:
			field := typemap.Field{Type: def.AliasOf}
			if def.Underlying != nil {
				field = *def.Underlying
			}
			predicate = zodWirePredicate(field, "value", style)
		case typemap.TypeDefUnion:
			kind := "number"
			if def.IsStringUnion() {
				kind = "string"
			}
			predicate = "typeof value === " + typemap.ZodStringLiteral(kind)
		default:
			predicate = "true"
		}
		ew.printf("function $goWire%s(value: unknown): boolean { return value == null || (%s); }\n", name, predicate)
	}
	ew.println("")
}

func zodWireObjectPredicate(fields []typemap.Field, value string, style typemap.ZodStyle) string {
	var checks []string
	for _, field := range fields {
		predicate := "true"
		if !field.ZodOmit {
			predicate = zodWirePredicate(field, "item", style)
		}
		// Computed keys preserve a Go JSON field literally named __proto__.
		checks = append(checks, "["+typemap.ZodStringLiteral(field.Name)+"]: (item: unknown) => "+predicate)
	}
	return "$goWireObject(" + value + ", {" + strings.Join(checks, ", ") + "})"
}

func zodWirePredicate(f typemap.Field, value string, style typemap.ZodStyle) string {
	f.Type = zodFieldType(f)
	f.Validate, f.ElementValidate, f.EnumValues = nil, nil, nil
	f.Required, f.Optional, f.ValidateOmitempty = false, false, false
	var predicate string
	switch {
	case f.Inline != nil:
		predicate = zodWireObjectPredicate(f.Inline.Fields, value, style)
	case f.JSONString:
		predicate = value + " === \"null\" || " + typemap.ZodType(f, style) + ".safeParse(" + value + ").success"
	case f.GoKind == "[]byte":
		predicate = typemap.ZodType(f, style) + ".safeParse(" + value + ").success || (Array.isArray(" + value + ") && " + value + ".every((item) => " + zodWirePredicate(typemap.Field{Type: "number", GoKind: "uint8"}, "item", style) + "))"
	default:
		if element, ok := strings.CutSuffix(f.Type, "[]"); ok {
			items := value
			if f.ArrayLen != nil {
				items += ".slice(0, " + strconv.FormatInt(*f.ArrayLen, 10) + ")"
			}
			predicate = "Array.isArray(" + value + ") && " + items + ".every((item) => " + zodWirePredicate(zodElementField(unwrapZodParentheses(element), f.Element), "item", style) + ")"
		} else if key, element, ok := zodRecordTypes(f.Type); ok {
			keyField := zodElementField(key, f.Key)
			if keyField.Type == "number" && keyField.GoKind == "" {
				keyField.GoKind = "int"
			}
			keyCheck := "true"
			if zodIntegerKind(keyField.GoKind) {
				keyField.Type, keyField.JSONString, keyField.MapKey = "string", true, true
				keyField.EnumValues = nil
				keyCheck = typemap.ZodType(keyField, style) + ".safeParse(name).success"
			}
			predicate = "$goWireMap(" + value + ", (name) => " + keyCheck + ", (item) => " + zodWirePredicate(zodElementField(element, f.Element), "item", style) + ")"
		} else if base := typemap.ZodBaseForTSType(f.Type, f.GoKind); base != "" {
			predicate = base + ".safeParse(" + value + ").success"
		} else if fields, ok := inlineTypeFields(f.Type); ok {
			predicate = zodWireObjectPredicate(fields, value, style)
		} else {
			predicate = "$goWire" + f.Type + "(" + value + ")"
		}
	}
	return "(" + value + " == null || (" + predicate + "))"
}
