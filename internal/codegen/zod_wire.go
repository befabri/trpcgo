package codegen

import (
	"maps"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/befabri/trpcgo/internal/typemap"
)

// writeZodJSONHelpers preserves the information JSON.parse discards when
// multiple JSON property names decode to the same Go field or map key. The
// metadata symbol is private to the module, so only decodeGoJSON can attach
// source order; an ordinary object can never claim it.
func writeZodJSONHelpers(ew *errWriter) {
	ew.println(`const $goJSONMetadataKey = /* @__PURE__ */ Symbol("trpcgo.go-json.entries");

type $GoJSONEntries = readonly (readonly [string, unknown])[];

function $goJSONObject(entries: $GoJSONEntries): { [key: string]: unknown } {
  const object: { [key: string]: unknown } = {};
  const freeze = (pairs: $GoJSONEntries): $GoJSONEntries =>
    Object.freeze(pairs.map(([name, value]) => Object.freeze([name, value] as const)));
  for (const [name, value] of entries) {
    Object.defineProperty(object, name, { value, enumerable: true, writable: true, configurable: true });
  }
  Object.defineProperty(object, $goJSONMetadataKey, {
    value: Object.freeze({ entries: freeze(entries), snapshot: freeze(Object.entries(object)) }),
  });
  return object;
}

function $goJSONEntries(value: object): $GoJSONEntries | undefined {
  try {
    const metadata = Object.getOwnPropertyDescriptor(value, $goJSONMetadataKey)?.value;
    if (metadata === null || typeof metadata !== "object" || !Object.isFrozen(metadata)) return undefined;
    const entries = Object.getOwnPropertyDescriptor(metadata, "entries")?.value;
    const snapshot = Object.getOwnPropertyDescriptor(metadata, "snapshot")?.value;
    if (!Array.isArray(entries) || !Object.isFrozen(entries) || !Array.isArray(snapshot) || !Object.isFrozen(snapshot)) return undefined;
    const pair = (item: unknown): readonly [string, unknown] | undefined => {
      if (!Array.isArray(item) || item.length !== 2 || !Object.isFrozen(item)) return undefined;
      const key = Object.getOwnPropertyDescriptor(item, "0");
      const value = Object.getOwnPropertyDescriptor(item, "1");
      if (!key || !("value" in key) || typeof key.value !== "string" || !value || !("value" in value)) return undefined;
      return [key.value, value.value];
    };
    const latest = new Map<string, unknown>();
    for (const item of entries) {
      const entry = pair(item);
      if (!entry) return undefined;
      latest.set(entry[0], entry[1]);
    }
    const keys = Object.keys(value);
    if (snapshot.length !== keys.length || latest.size !== keys.length) return undefined;
    for (let i = 0; i < keys.length; i++) {
      const entry = pair(snapshot[i]);
      if (!entry || entry[0] !== keys[i] || !latest.has(entry[0]) || !Object.is(latest.get(entry[0]), entry[1])) return undefined;
      const current = Object.getOwnPropertyDescriptor(value, entry[0]);
      if (!current || !("value" in current) || !current.enumerable || !Object.is(current.value, entry[1])) return undefined;
    }
    return entries as $GoJSONEntries;
  } catch {
    // Ordinary objects may carry an unrelated symbol or hostile accessors.
    return undefined;
  }
}

// Parse raw JSON while retaining ordered object entries for Go decoding.
function $goParseJSON(raw: string): unknown {
  // encoding/json rejects arrays and objects nested more than 10000 deep as a
  // syntax error. Counting without recursion also keeps such input from
  // overflowing the recursive scanner below.
  let depth = 0;
  for (let index = 0, quoted = false; index < raw.length; index++) {
    const code = raw.charCodeAt(index);
    if (quoted) {
      if (code === 92) index++;
      else if (code === 34) quoted = false;
    } else if (code === 34) quoted = true;
    else if (code === 91 || code === 123) {
      if (++depth > 10000) throw new SyntaxError("JSON nests arrays and objects more than 10000 deep");
    } else if (code === 93 || code === 125) depth--;
  }
  // Native parsing validates the complete grammar before the scanner reads it.
  JSON.parse(raw);
  let position = 0;
  const whitespace = () => {
    while (position < raw.length && raw.charCodeAt(position) <= 32) position++;
  };
  const string = (): string => {
    const start = position++;
    while (position < raw.length) {
      const character = raw[position++];
      if (character === "\\") position++;
      else if (character === '"') break;
    }
    // encoding/json replaces an unpaired surrogate escape with U+FFFD, in
    // object keys as well as values, so such keys name one Go map entry.
    return (JSON.parse(raw.slice(start, position)) as string).replace(/\p{Surrogate}/gu, "\uFFFD");
  };
  // Containers being read, innermost last, so nesting as deep as Go accepts
  // needs no recursion. An object keeps the key of the value being read.
  type Container = { array: unknown[] } | { entries: [string, unknown][]; key: string };
  const open: Container[] = [];
  while (true) {
    whitespace();
    let value: unknown;
    const character = raw[position];
    if (character === "{" || character === "[") {
      position++;
      whitespace();
      if (raw[position] === (character === "{" ? "}" : "]")) {
        position++;
        value = character === "{" ? $goJSONObject([]) : [];
      } else if (character === "[") {
        open.push({ array: [] });
        continue;
      } else {
        const key = string();
        whitespace();
        position++; // colon
        open.push({ entries: [], key });
        continue;
      }
    } else if (character === '"') {
      value = string();
    } else {
      const start = position;
      while (position < raw.length && !/[\s,\]}]/.test(raw[position]!)) position++;
      value = JSON.parse(raw.slice(start, position));
    }
    // Add the value to its container, closing each container it completes.
    while (true) {
      const container = open[open.length - 1];
      if (container === undefined) return value;
      whitespace();
      const end = raw[position++];
      if ("array" in container) {
        container.array.push(value);
        if (end !== "]") break;
        value = container.array;
      } else {
        container.entries.push([container.key, value]);
        if (end !== "}") {
          whitespace();
          container.key = string();
          whitespace();
          position++; // colon
          break;
        }
        value = $goJSONObject(container.entries);
      }
      open.pop();
    }
  }
}
`)
}

// writeZodJSONDecoder exports decodeGoJSON, which validates raw JSON as Go's
// encoding/json decodes it. Schemas stay plain objects: repeated fields and
// case-insensitive names exist only in raw JSON text, so decoding them is a
// separate step from validating a JavaScript value.
func writeZodJSONDecoder(ew *errWriter, order []string, defs map[string]typemap.TypeDef) {
	var schemas []string
	ew.println("const $goJSONDecoders = /* @__PURE__ */ new Map<unknown, readonly [$GoWire, $GoJSONDecoder]>([")
	for _, name := range order {
		if _, ok := defs[name]; ok {
			ew.printf("  [%sSchema, [$goWire%s, $goMerge%s]],\n", name, name, name)
			schemas = append(schemas, "typeof "+name+"Schema")
		}
	}
	ew.println(`]);

// Raw JSON that never reaches the schema fails through this one, which
// reports the issues found while decoding it. It is built on first use.
let $goJSONFailureSchema: z.core.$ZodType | undefined;
function $goJSONFailure(failure: $GoWireFailure) {
  if ($goJSONFailureSchema === undefined) {
    $goJSONFailureSchema = z.custom<$GoWireFailure>().check(z.superRefine((failure: $GoWireFailure, ctx) => {
      for (const issue of $goWireIssues(failure)) ctx.addIssue(issue);
    }));
  }
  return z.safeParse($goJSONFailureSchema, failure);
}

/**
 * Validates raw JSON text as Go's encoding/json decodes it. Repeated fields
 * merge or overwrite in source order, field names match case-insensitively,
 * integer map keys resolve in source order, and every overwritten value must
 * still decode. JSON.parse discards all of these, so validate raw request
 * bodies with this function rather than with schema.safeParse(JSON.parse(raw)).
 * Malformed JSON, including JSON nested deeper than Go accepts, fails
 * validation instead of throwing. Every failure reports Zod's issues at the
 * path of the failing value.
 */`)
	// The intersection admits only this module's schemas while S keeps the
	// argument's exact type; a union constraint on S loses the output type.
	ew.printf("export function decodeGoJSON<S extends z.core.$ZodType>(schema: S & (%s), raw: string) {\n", strings.Join(schemas, " | "))
	ew.println(`  const decoder = $goJSONDecoders.get(schema);
  if (decoder === undefined) throw new TypeError("decodeGoJSON needs a schema exported by this module");
  let value: unknown;
  try {
    value = $goParseJSON(raw);
  } catch (error) {
    if (!(error instanceof SyntaxError)) throw error;
    return $goJSONFailure({ path: [], issues: [{ code: "invalid_format", format: "json_string", input: raw }] }) as ReturnType<typeof z.safeParse<S>>;
  }
  const result = z.safeParse<S>(schema, decoder[1](value, undefined));
  if (!result.success) return result;
  // The decoded value is valid, but a value it no longer contains, such as an
  // overwritten occurrence of a field, can still fail Go decoding.
  const failure = decoder[0](value, []);
  return failure === undefined ? result : $goJSONFailure(failure) as ReturnType<typeof z.safeParse<S>>;
}`)
}

// Integer aliases can overwrite a value before validator runs. Keep decoding
// checks separate from validation: an overwritten min violation is harmless,
// but an overwritten JSON type/range error still makes encoding/json fail.
// Named checks are functions so recursive Go types require no initialization
// ordering or recursive schema inference. These checks never change values.
//
// A check returns the first value encoding/json would reject, with the issues
// Zod reports for it and its path in the raw input, or undefined.
func writeZodWireChecks(ew *errWriter, order []string, defs map[string]typemap.TypeDef, style typemap.ZodStyle, checks *zodSchemaChecks, allowUnknown bool) {
	unknownField := `return { path, issues: [{ code: "unrecognized_keys", keys: [name], input: value }] };`
	if allowUnknown {
		unknownField = "continue;"
	}
	ew.println(strings.ReplaceAll(`type $GoCheck = ((value: unknown) => boolean) & { issues(value: unknown): readonly object[] };
type $GoWireFailure = { path: readonly PropertyKey[]; issues: readonly object[] };
type $GoWire = (value: unknown, path: readonly PropertyKey[]) => $GoWireFailure | undefined;

// Issues keep the codes and fields Zod gives them, so a failure reads like
// Zod's own validation error at the failing value's path.
function $goWireIssues(failure: $GoWireFailure): z.core.$ZodRawIssue[] {
  return failure.issues.map((issue) => ({ ...issue, path: [...failure.path, ...((issue as { path?: PropertyKey[] }).path ?? [])] }) as z.core.$ZodRawIssue);
}
function $goWireValue(check: $GoCheck, value: unknown, path: readonly PropertyKey[]): $GoWireFailure | undefined {
  return check(value) ? undefined : { path, issues: check.issues(value) };
}
function $goWireKind(expected: string, value: unknown, path: readonly PropertyKey[]): $GoWireFailure {
  return { path, issues: [{ code: "invalid_type", expected, input: value }] };
}
function $goWireObject(value: unknown, path: readonly PropertyKey[], fields: Record<string, $GoWire>): $GoWireFailure | undefined {
  if (value === null || typeof value !== "object" || Array.isArray(value)) return $goWireKind("object", value, path);
  for (const [name, item] of $goJSONEntries(value) ?? Object.entries(value)) {
    const field = $goJSONFieldName(name, Object.keys(fields));
    if (field === undefined) UNKNOWN_FIELD
    const failure = fields[field]!(item, [...path, field]);
    if (failure) return failure;
  }
  return undefined;
}
function $goWireMap(value: unknown, path: readonly PropertyKey[], key: $GoCheck | undefined, element: $GoWire): $GoWireFailure | undefined {
  if (value === null || typeof value !== "object" || Array.isArray(value)) return $goWireKind("record", value, path);
  for (const [name, item] of $goJSONEntries(value) ?? Object.entries(value)) {
    if (key !== undefined && !key(name)) return { path: [...path, name], issues: [{ code: "invalid_key", origin: "record", issues: key.issues(name), input: name }] };
    const failure = element(item, [...path, name]);
    if (failure) return failure;
  }
  return undefined;
}
// encoding/json skips the elements beyond a fixed array's length.
function $goWireArray(value: unknown, path: readonly PropertyKey[], length: number | undefined, element: $GoWire): $GoWireFailure | undefined {
  if (!Array.isArray(value)) return $goWireKind("array", value, path);
  const count = length === undefined ? value.length : Math.min(length, value.length);
  for (let index = 0; index < count; index++) {
    const failure = element(value[index], [...path, index]);
    if (failure) return failure;
  }
  return undefined;
}`, "UNKNOWN_FIELD", unknownField))
	for _, name := range order {
		def, ok := defs[name]
		if !ok {
			continue
		}
		// zodWireCheck guards null itself; an object check needs the guard here.
		var check string
		switch def.Kind {
		case typemap.TypeDefInterface:
			check = "value == null ? undefined : " + zodWireObjectCheck(def.Fields, "value", "path", style, checks)
		case typemap.TypeDefAlias:
			field := typemap.Field{Type: def.AliasOf}
			if def.Underlying != nil {
				field = *def.Underlying
			}
			check = zodWireCheck(field, "value", "path", style, checks)
		case typemap.TypeDefUnion:
			check = zodWireCheck(enumUnderlyingField(def), "value", "path", style, checks)
		default:
			check = "undefined"
		}
		ew.printf("function $goWire%s(value: unknown, path: readonly PropertyKey[]): $GoWireFailure | undefined { return %s; }\n", name, check)
	}
	ew.println("")
}

// zodWireCheck returns an expression that checks value at path the way
// encoding/json decodes it into f, evaluating to a $GoWireFailure or undefined.
func zodWireCheck(f typemap.Field, value, path string, style typemap.ZodStyle, checks *zodSchemaChecks) string {
	f.Type = zodFieldType(f)
	f.Validate, f.ElementValidate, f.EnumValues = nil, nil, nil
	f.Required, f.Optional, f.ValidateOmitempty = false, false, false
	var check string
	switch {
	case f.Inline != nil:
		check = zodWireObjectCheck(f.Inline.Fields, value, path, style, checks)
	case f.JSONString:
		check = zodWireValue(typemap.ZodType(f, style), value, path, checks)
		if typemap.ZodQuotedNull(f.GoKind) {
			check = value + " === \"null\" ? undefined : " + check
		}
	case f.GoKind == "[]byte":
		// A JSON array of bytes also decodes into a []byte.
		check = "Array.isArray(" + value + ") ? $goWireArray(" + value + ", " + path + ", undefined, " + zodWireElement(typemap.Field{Type: "number", GoKind: "uint8"}, style, checks) + ") : " + zodWireValue(typemap.ZodType(f, style), value, path, checks)
	default:
		if element, ok := strings.CutSuffix(f.Type, "[]"); ok {
			length := "undefined"
			if f.ArrayLen != nil {
				length = strconv.FormatInt(*f.ArrayLen, 10)
			}
			check = "$goWireArray(" + value + ", " + path + ", " + length + ", " + zodWireElement(zodElementField(unwrapZodParentheses(element), f.Element), style, checks) + ")"
		} else if key, element, ok := zodRecordTypes(f.Type); ok {
			keyField := zodElementField(key, f.Key)
			if keyField.Type == "number" && keyField.GoKind == "" {
				keyField.GoKind = "int"
			}
			keyCheck := "undefined"
			if zodIntegerKind(keyField.GoKind) {
				keyField.Type, keyField.JSONString, keyField.MapKey = "string", true, true
				keyField.EnumValues = nil
				keyCheck = checks.check(typemap.ZodType(keyField, style))
			}
			check = "$goWireMap(" + value + ", " + path + ", " + keyCheck + ", " + zodWireElement(zodElementField(element, f.Element), style, checks) + ")"
		} else if base := typemap.ZodBaseForTSType(f.Type, f.GoKind); base != "" {
			check = zodWireValue(base, value, path, checks)
		} else if fields, ok := inlineTypeFields(f.Type); ok {
			check = zodWireObjectCheck(fields, value, path, style, checks)
		} else {
			check = "$goWire" + f.Type + "(" + value + ", " + path + ")"
		}
	}
	return "(" + value + " == null ? undefined : " + check + ")"
}

// zodWireObjectCheck checks a struct's fields, matched as encoding/json
// matches JSON keys, and reports a failure at the Go field's JSON name.
func zodWireObjectCheck(fields []typemap.Field, value, path string, style typemap.ZodStyle, checks *zodSchemaChecks) string {
	var members []string
	for _, field := range fields {
		check := "() => undefined"
		if !field.ZodOmit {
			check = zodWireElement(field, style, checks)
		}
		// Computed keys preserve a Go JSON field literally named __proto__.
		members = append(members, "["+checks.fieldName(field.Name)+"]: "+check)
	}
	return "$goWireObject(" + value + ", " + path + ", {" + strings.Join(members, ", ") + "})"
}

// zodWireElement returns a $GoWire function checking one field or element.
func zodWireElement(f typemap.Field, style typemap.ZodStyle, checks *zodSchemaChecks) string {
	return "(item: unknown, path: readonly PropertyKey[]) => " + zodWireCheck(f, "item", "path", style, checks)
}

func zodWireValue(schema, value, path string, checks *zodSchemaChecks) string {
	return "$goWireValue(" + checks.check(schema) + ", " + value + ", " + path + ")"
}

// writeZodMergeHelpers decodes repeated struct fields before object schemas
// discard their source history. Go merges struct/map fields but creates fresh
// values for map entries, so object merging and map-entry merging are separate.
func writeZodMergeHelpers(ew *errWriter, order []string, defs map[string]typemap.TypeDef, style typemap.ZodStyle) {
	ew.println(`type $GoJSONDecoder = (value: unknown, previous: unknown) => unknown;

function $goJSONFieldName(name: string, names: readonly string[]): string | undefined {
  if (names.includes(name)) return name;
  const folded = $goFoldName(name);
  return names.find(candidate => $goFoldName(candidate) === folded);
}

// Go folds each rune to the smallest rune of its case orbit with its own
// Unicode tables. ASCII, the Kelvin sign and the long s fold alike in every
// version; $goFolds covers the other runes of this module's field names.
function $goFoldName(name: string): string {
  let folded = "";
  for (const rune of name.replace(/\p{Surrogate}/gu, "\uFFFD")) {
    const point = rune.codePointAt(0)!;
    if (point < 0x80) folded += rune.toUpperCase();
    else if (point === 0x212a) folded += "K";
    else if (point === 0x17f) folded += "S";
    else folded += String.fromCodePoint($goFolds.get(point) ?? point);
  }
  return folded;
}

// A fixed-array element decodes into its Go zero value, so an element object
// is merged onto that value before its schema validates it.
function $goDecodeStruct<S extends z.core.$ZodType>(schema: S, wire: $GoWire, decode: (value: unknown) => unknown) {
  return z.pipe(z.transform<z.core.input<S>, unknown>((input, ctx) => {
    if (input === null || typeof input !== "object") return input;
    const failure = wire(input, []);
    if (failure) {
      ctx.issues.push(...$goWireIssues(failure));
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

// Go decodes every map entry into a fresh zero value, so an entry never merges
// with an earlier entry for the same key, but its own value still decodes.
function $goMergeMap(raw: unknown, previous: unknown, decode?: (value: unknown) => unknown): unknown {
  if (raw === null || typeof raw !== "object" || Array.isArray(raw)) return raw;
  const source = $goJSONEntries(raw);
  if (!source) return raw;
  const entries = decode === undefined ? source : source.map(([name, value]) => [name, decode(value)] as const);
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
const $goArrayStorage = /* @__PURE__ */ new WeakMap<unknown[], unknown[]>();
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
		} else if _, element, record := zodRecordTypes(ts); record {
			expression = "$goMergeMap(" + value + ", " + previous + ")"
			if decode := zodMergePredicate(zodElementField(element, field.Element), "item", "undefined"); decode != "item" {
				expression = "$goMergeMap(" + value + ", " + previous + ", (item) => " + decode + ")"
			}
		} else if typemap.ZodBaseForTSType(ts, "") != "" || ts == "never" {
			expression = value
		} else if fields, ok := inlineTypeFields(ts); ok {
			expression = zodMergeObjectExpression(typemap.TypeDef{Fields: fields}, value, previous)
		} else {
			expression = "$goMerge" + ts + "(" + value + ", " + previous + ")"
		}
	}
	// Without a previous value, null has nothing to retain or reset.
	if previous == "undefined" {
		return expression
	}
	// JSON null resets pointers, maps, slices and interface values. It leaves
	// an existing scalar/struct unchanged. Quoted null follows the same rule.
	reset := field.IsPointer || field.GoKind == "map" || field.GoKind == "slice" || field.GoKind == "[]byte" || field.GoKind == "unknown" || field.GoKind == "json.RawMessage" || ts == "unknown"
	null := value + " === null"
	if field.JSONString && typemap.ZodQuotedNull(field.GoKind) {
		null = "(" + null + " || " + value + " === \"null\")"
	}
	if reset {
		return "(" + null + " ? " + value + " : " + expression + ")"
	}
	return "(" + null + " && " + previous + " !== undefined ? " + previous + " : " + expression + ")"
}

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
function $goIntegerMap<S extends z.core.$ZodType>(schema: S, key: z.core.$ZodType, wireValue: $GoWire) {
  // Keep the public input type; the normalized object still needs schema
  // validation before it can claim any particular output type.
  return z.pipe(z.transform<z.core.input<S>, unknown>((input, ctx) => {
    if (input === null || typeof input !== "object" || Array.isArray(input)) return input;
    const source = $goJSONEntries(input);
    const entries = source ?? Object.entries(input);
    const result: Record<string, unknown> = {};
    const seen = new Set<string>();
    for (const [name, value] of entries) {
      const keyResult = z.safeParse(key, name);
      if (!keyResult.success) {
        ctx.issues.push({ code: "invalid_key", origin: "record", issues: keyResult.error.issues, input: name, path: [name] } as z.core.$ZodRawIssue);
        return input;
      }
      const failure = wireValue(value, [name]);
      if (failure) {
        ctx.issues.push(...$goWireIssues(failure));
        return input;
      }
      const canonical = BigInt(name).toString();
      if (!source && seen.has(canonical)) {
        ctx.issues.push({ code: "custom", input, path: [name], message: "Ambiguous integer map keys; use decodeGoJSON with the original JSON text" });
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

// zodGoFolds declares $goFolds, mapping each non-ASCII rune in a field name
// rune's case orbit to the orbit's smallest rune, as Go folds it. The Kelvin
// sign and long s are folded inline, so ASCII names declare an empty map in
// every Unicode version.
func zodGoFolds(runes map[rune]bool) string {
	folds := map[rune]rune{}
	for r := range runes {
		if r < utf8.RuneSelf {
			continue
		}
		orbit := []rune{r}
		for next := unicode.SimpleFold(r); next != r; next = unicode.SimpleFold(next) {
			orbit = append(orbit, next)
		}
		smallest := slices.Min(orbit)
		for _, member := range orbit {
			if member != smallest && member >= utf8.RuneSelf && member != '\u212a' && member != '\u017f' {
				folds[member] = smallest
			}
		}
	}
	if len(folds) == 0 {
		return "const $goFolds: ReadonlyMap<number, number> = /* @__PURE__ */ new Map();\n\n"
	}
	entries := make([]string, 0, len(folds))
	for _, member := range slices.Sorted(maps.Keys(folds)) {
		entries = append(entries, "["+strconv.Itoa(int(member))+", "+strconv.Itoa(int(folds[member]))+"]")
	}
	return "// Go Unicode " + unicode.Version + " case folding for this module's field names.\n" +
		"const $goFolds: ReadonlyMap<number, number> = /* @__PURE__ */ new Map([" + strings.Join(entries, ", ") + "]);\n\n"
}
