package codegen

import (
	"strconv"
	"strings"

	"github.com/befabri/trpcgo/internal/typemap"
)

// writeZodMergeHelpers decodes repeated struct fields before object schemas
// discard their source history. Go merges struct/map fields but creates fresh
// values for map entries, so object merging and map-entry merging are separate.
func writeZodMergeHelpers(ew *errWriter, order []string, defs map[string]typemap.TypeDef, style typemap.ZodStyle) {
	ew.println(`type $GoJSONDecoder = (value: unknown, previous: unknown) => unknown;

function $goJSONFieldName(name: string, names: readonly string[]): string | undefined {
  if (names.includes(name)) return name;
  // Unicode simple case folding matches Go's field lookup without expanding
  // characters such as sharp s into multiple letters. The end check is strict.
  return names.find(candidate => new RegExp("^" + candidate.replace(/[.*+?^${}()|[\]\\]/g, "\\$&") + "(?![\\s\\S])", "iu").test(name));
}

function $goDecodeStruct<S extends z.core.$ZodType>(schema: S, wire: (value: unknown) => boolean, decode: (value: unknown) => unknown, context = false) {
  return z.pipe(z.transform<z.core.input<S>, unknown>((input, ctx) => {
    if (input === null || typeof input !== "object" || (!context && !$goJSONEntries(input))) return input;
    if (!wire(input)) {
      ctx.issues.push({ code: "custom", input, message: "Invalid Go JSON struct field" });
      return input;
    }
    return decode(input);
  }), schema);
}

function $goMergeObject(raw: unknown, previous: unknown, fields: readonly (readonly [string, $GoJSONDecoder])[]): unknown {
  if (raw === null || typeof raw !== "object" || Array.isArray(raw)) return raw;
  const entries = $goJSONEntries(raw);
  if (!entries && previous === undefined) return raw;
  const values = new Map<string, unknown>(previous !== null && typeof previous === "object" && !Array.isArray(previous) ? Object.entries(previous) : []);
  const names = fields.map(([name]) => name);
  for (const [name, value] of entries ?? Object.entries(raw)) {
    const field = $goJSONFieldName(name, names);
    if (field === undefined) values.set(name, value);
    else {
      const decode = fields.find(([name]) => name === field)![1];
      values.set(field, decode(value, values.get(field)));
    }
  }
  return entries ? $goJSONObject(Array.from(values)) : Object.fromEntries(values);
}

function $goMergeMap(raw: unknown, previous: unknown): unknown {
  if (raw === null || typeof raw !== "object" || Array.isArray(raw)) return raw;
  const entries = $goJSONEntries(raw);
  if (!entries) return raw;
  if (previous === null || typeof previous !== "object" || Array.isArray(previous)) return $goJSONObject(entries);
  const prior = $goJSONEntries(previous);
  if (prior) return $goJSONObject([...prior, ...entries]);
  // Stale or ordinary objects cannot grant trustworthy alias ordering to a
  // combined map. Keep it ordinary so the map schema retains its ambiguity policy.
  const merged: { [key: string]: unknown } = {};
  for (const [name, value] of [...Object.entries(previous), ...entries]) {
    Object.defineProperty(merged, name, { value, enumerable: true, writable: true, configurable: true });
  }
  return merged;
}

// encoding/json reuses a slice's element storage while resetting its length.
// Keep truncated elements privately until a later occurrence grows the slice;
// an explicit empty array creates new zero-capacity storage in Go.
const $goArrayStorage = new WeakMap<unknown[], unknown[]>();
function $goMergeArray(raw: unknown, previous: unknown, decode: $GoJSONDecoder): unknown {
  if (!Array.isArray(raw)) return raw;
  if (raw.length === 0) return [];
  const prior = Array.isArray(previous) ? ($goArrayStorage.get(previous) ?? previous) : [];
  const storage = prior.slice();
  const result = raw.map((item, index) => {
    const value = decode(item, prior[index]);
    storage[index] = value;
    return value;
  });
  $goArrayStorage.set(result, storage);
  return result;
}
`)
	for _, name := range order {
		def, ok := defs[name]
		if !ok {
			continue
		}
		var expression string
		switch def.Kind {
		case typemap.TypeDefInterface:
			expression = zodMergeObjectExpression(def, "value", "previous")
		case typemap.TypeDefAlias:
			field := typemap.Field{Type: def.AliasOf}
			if def.Underlying != nil {
				field = *def.Underlying
			}
			expression = zodMergePredicate(field, "value", "previous")
		default:
			expression = "value === null && previous !== undefined ? previous : value"
		}
		ew.printf("function $goMerge%s(value: unknown, previous: unknown): unknown { return %s; }\n", name, expression)
	}
	ew.println("")
}

func zodMergeObjectExpression(def typemap.TypeDef, value, previous string) string {
	var fields []string
	for _, field := range def.Fields {
		decode := "item"
		if !field.ZodOmit {
			decode = zodMergePredicate(field, "item", "prior")
		}
		fields = append(fields, "["+typemap.ZodStringLiteral(field.Name)+", (item: unknown, prior: unknown) => "+decode+"]")
	}
	return "$goMergeObject(" + value + ", " + previous + ", [" + strings.Join(fields, ", ") + "])"
}

func zodMergePredicate(field typemap.Field, value, previous string) string {
	ts := zodFieldType(field)
	var expression string
	switch {
	case field.Inline != nil:
		expression = zodMergeObjectExpression(*field.Inline, value, previous)
	case field.JSONString || field.GoKind == "[]byte":
		expression = value
	default:
		if element, ok := strings.CutSuffix(ts, "[]"); ok {
			child := zodElementField(unwrapZodParentheses(element), field.Element)
			expression = "$goMergeArray(" + value + ", " + previous + ", (item, prior) => " + zodMergePredicate(child, "item", "prior") + ")"
			if field.ArrayLen != nil {
				expression = "$goMergeFixedArray(" + value + ", " + previous + ", " + strconv.FormatInt(*field.ArrayLen, 10) + ", () => " + zodZeroValue(child) + ", (item, prior) => " + zodMergePredicate(child, "item", "prior") + ")"
			}
		} else if _, _, record := zodRecordTypes(ts); record {
			expression = "$goMergeMap(" + value + ", " + previous + ")"
		} else if typemap.ZodBaseForTSType(ts, "") != "" || ts == "never" {
			expression = value
		} else if fields, ok := inlineTypeFields(ts); ok {
			expression = zodMergeObjectExpression(typemap.TypeDef{Fields: fields}, value, previous)
		} else {
			expression = "$goMerge" + ts + "(" + value + ", " + previous + ")"
		}
	}
	// JSON null resets pointers, maps, slices and interface values. It leaves
	// an existing scalar/struct unchanged. Quoted null follows the same rule.
	reset := field.IsPointer || field.GoKind == "map" || field.GoKind == "slice" || field.GoKind == "[]byte" || field.GoKind == "unknown" || field.GoKind == "json.RawMessage" || ts == "unknown"
	null := value + " === null"
	if field.JSONString {
		null = "(" + null + " || " + value + " === \"null\")"
	}
	if reset {
		return "(" + null + " ? " + value + " : " + expression + ")"
	}
	return "(" + null + " && " + previous + " !== undefined ? " + previous + " : " + expression + ")"
}
