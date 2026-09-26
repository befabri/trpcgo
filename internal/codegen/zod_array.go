package codegen

import (
	"fmt"
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
		switch field.GoKind {
		case "string":
			zero = `""`
		case "bool":
			zero = "false"
		case "json.Number":
			// Rules on a padded zero Number fail generation, so the spelling
			// only has to decode: the quoted null on a lenient decoder, any
			// number on a strict one, which has no spelling for empty text.
			zero = "null"
			if !typemap.ZodQuotedNull(field.GoKind) {
				zero = "0"
			}
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

// Fixed-array decoding creates complete Go zero values, including nil pointers,
// maps and slices inside zero structs. Private schema variants keep that Go
// value context separate from the ordinary JSON object schema's null policy.
func zodArrayContexts(defs map[string]typemap.TypeDef, reachable map[string]bool) map[string]bool {
	contexts := make(map[string]bool)
	var visitField func(typemap.Field, bool)
	visitDefinition := func(def typemap.TypeDef, context bool) {
		for _, field := range def.Fields {
			if !field.ZodOmit {
				visitField(field, context)
			}
		}
		if def.Underlying != nil {
			visitField(*def.Underlying, context)
		}
	}
	visitField = func(field typemap.Field, context bool) {
		if field.Inline != nil {
			visitDefinition(*field.Inline, context)
			return
		}
		ts := zodFieldType(field)
		if field.Element != nil {
			if field.ArrayLen != nil || strings.HasSuffix(ts, "[]") {
				visitField(zodElementField("", field.Element), context || field.ArrayLen != nil)
			} else if _, _, ok := zodRecordTypes(ts); ok {
				visitField(zodElementField("", field.Element), context)
			}
		}
		if context {
			if def, exists := defs[ts]; exists && def.Kind != typemap.TypeDefUnion && !contexts[ts] {
				contexts[ts] = true
				visitDefinition(def, true)
			}
		}
	}
	for name := range reachable {
		visitDefinition(defs[name], false)
	}
	return contexts
}

func (e zodSchemaEmitter) schemaName(name string) string {
	if e.arrayContext && e.arrayContexts[name] {
		if e.skipValidation {
			return "$goArrayWire" + name
		}
		return "$goArray" + name
	}
	if e.unvalidated && e.unvalidatedTypes[name] {
		return "$goUnvalidated" + name
	}
	return name
}

func zodArrayNilPredicate(field typemap.Field) string {
	if !goArrayNilEligible(field) {
		return "false"
	}
	return typemap.ZodMissingValuePredicate(field)
}

// Both public wire types and schema variants use the same Go nil-capable kinds.
// Schema variants additionally evaluate validation rules before accepting nil.
func goArrayNilEligible(field typemap.Field) bool {
	return field.IsPointer || field.GoKind == "map" || field.GoKind == "slice" || field.GoKind == "[]byte" || field.GoKind == "unknown" || field.GoKind == "json.RawMessage"
}

func (e zodSchemaEmitter) arrayContextNullable(schema string, field typemap.Field) string {
	if !e.arrayContext {
		return schema
	}
	predicate := zodArrayNilPredicate(field)
	if predicate == "false" {
		return schema
	}
	nilSchema := "z.null()"
	if predicate != "true" {
		nilSchema += ".check(z.refine(() => " + predicate + "))"
	}
	return "z.union([" + schema + ", " + nilSchema + "])"
}

// Schema-only shape reconstruction uses the same context as the emitter. It
// includes nil outputs produced by array padding and private named references,
// without changing the public Go/TypeScript declaration mapper.
func zodArrayShapeFieldType(field typemap.Field, context, wireOnly bool, variants zodVariants) string {
	scope, _ := typemap.FieldValidationScope(field)
	return zodArrayScopedShape(field, scope, context, wireOnly, variants)
}

func zodArrayScopedShape(field typemap.Field, scope typemap.ValidationScope, context, wireOnly bool, variants zodVariants) string {
	if wireOnly {
		scope = typemap.ValidationScope{}
		field.ValidateOmitempty = false
	}
	field.Validate = scope.Rules
	field.ElementValidate = nil
	ts := zodFieldType(field)
	if field.Inline != nil {
		fields := make([]typemap.Field, 0, len(field.Inline.Fields))
		for _, child := range field.Inline.Fields {
			if child.ZodOmit {
				child.Type, child.Optional = zodPassthroughType(child), true
			} else {
				child.Type = zodArrayShapeFieldType(child, context, wireOnly, variants)
				if wireOnly {
					child.ValidateOmitempty = false
				}
				child.Optional = typemap.ZodFieldOptional(child)
			}
			fields = append(fields, child)
		}
		ts = typemap.InlineObjectType(fields)
	} else if element, ok := strings.CutSuffix(ts, "[]"); ok {
		child := zodElementField(unwrapZodParentheses(element), field.Element)
		childScope := typemap.ValidationScope{}
		if scope.Element != nil {
			childScope = *scope.Element
		}
		childContext := context || field.ArrayLen != nil
		element = zodArrayScopedShape(child, childScope, childContext, wireOnly || scope.Element == nil, variants)
		if len(splitTopLevel(element, '|')) > 1 {
			element = "(" + element + ")"
		}
		ts = element + "[]"
	} else if key, value, ok := zodRecordTypes(ts); ok {
		partial := strings.HasPrefix(ts, "Partial<")
		key = zodArrayShapeFieldType(zodElementField(key, field.Key), false, wireOnly, variants)
		childScope := typemap.ValidationScope{}
		if scope.Element != nil {
			childScope = *scope.Element
		}
		value = zodArrayScopedShape(zodElementField(value, field.Element), childScope, context, wireOnly || scope.Element == nil, variants)
		ts = "Record<" + key + ", " + value + ">"
		if partial {
			ts = "Partial<" + ts + ">"
		}
	} else if context && variants.contexts[ts] {
		if wireOnly {
			ts = "$goArrayWire" + ts
		} else {
			ts = "$goArray" + ts
		}
	} else if (wireOnly || variants.collections[ts] && scope.Element == nil) && variants.unvalidated[ts] {
		ts = "$goUnvalidated" + ts
	}
	if context && ts != "unknown" && zodArrayNilPredicate(field) != "false" {
		ts += " | null"
	}
	return ts
}

// Fixed arrays use reflect.Value.IsZero for required and omitempty. Length is
// fixed even when every element is zero, and non-nil empty collections or
// pointers inside an array are nonzero Go values.
func applyZodArrayRules(base string, field typemap.Field, style typemap.ZodStyle, checks *zodSchemaChecks, zeroSchema string) string {
	if field.ArrayLen == nil {
		return typemap.ApplyZodRules(base, field, style)
	}
	zero := zodArrayIsZero(field, "value")
	predicate := zodArrayRuleProgram(field, field.Validate, "value", zero)
	omitted := ""
	for _, rule := range field.Validate {
		if rule.Tag == "omitempty" && !field.IsPointer || rule.Tag == "omitzero" {
			omitted = rule.Tag
			break
		}
	}
	if omitted != "" {
		// This branch intentionally bypasses validators after omission, including
		// dive. It must still check the Go wire type, bounds, and strict known keys.
		wire := base
		if zeroSchema != "" {
			wire = zeroSchema
		}
		base = "$goOmitZeroArray(" + base + ", " + wire + ", (value: unknown) => " + zodWireCheck(field, "value", "[]", style, checks) + " === undefined && " + zodArrayOmissionZero(field, omitted, "value") + ")"
	}
	if predicate != "true" {
		options := ""
		if len(field.Validate) == 1 && field.Validate[0].Custom != nil && field.Validate[0].Custom.Message != "" {
			options = ", { message: " + typemap.ZodStringLiteral(field.Validate[0].Custom.Message) + " }"
		}
		base += ".check(z.refine((value) => " + predicate + options + "))"
	}
	return base
}

func zodArrayRuleProgram(field typemap.Field, rules []typemap.ValidateRule, value, zero string) string {
	var parts []string
	for i, rule := range rules {
		if rule.Tag == "omitempty" || rule.Tag == "omitzero" {
			test := zodArrayOmissionZero(field, rule.Tag, value)
			if rule.Tag == "omitempty" && !field.IsPointer {
				test = zero
			}
			parts = append(parts, "("+test+" || ("+zodArrayRuleProgram(field, rules[i+1:], value, zero)+"))")
			break
		}
		if rule.Tag == "omitnil" {
			continue
		}
		if len(rule.Alternatives) > 0 {
			var alternatives []string
			for _, branch := range rule.Alternatives {
				alternatives = append(alternatives, "("+zodArrayRuleProgram(field, []typemap.ValidateRule{branch}, value, zero)+")")
			}
			parts = append(parts, "("+strings.Join(alternatives, " || ")+")")
		} else if rule.Tag == "required" {
			if field.IsPointer {
				parts = append(parts, value+" != null")
			} else {
				parts = append(parts, "!("+zero+")")
			}
		} else if predicate, ok := typemap.ZodRulePredicate(field, rule, value); ok {
			parts = append(parts, "("+predicate+")")
		}
	}
	if len(parts) == 0 {
		return "true"
	}
	return strings.Join(parts, " && ")
}

func writeZodArrayRuleHelpers(ew *errWriter, order []string, defs map[string]typemap.TypeDef) {
	ew.println(`// Use the independently compiled wire schema on the omission branch. This
// retains nil pointer outputs and zeros outside dive's literal constraints.
function $goOmitZeroArray<S extends z.core.$ZodType, W extends z.core.$ZodType>(schema: S, wire: W, zero: (value: unknown) => boolean) {
  return z.union([schema, z.pipe(wire, z.custom<z.core.output<W>>(zero))]);
}
`)
	for _, name := range order {
		def, ok := defs[name]
		if !ok {
			continue
		}
		expression := "false"
		switch def.Kind {
		case typemap.TypeDefInterface:
			expression = zodArrayStructIsZero(def, "value")
		case typemap.TypeDefAlias, typemap.TypeDefUnion:
			field := enumUnderlyingField(def)
			if def.Kind == typemap.TypeDefAlias && def.Underlying == nil {
				field = typemap.Field{Type: def.AliasOf}
			}
			expression = zodArrayIsZero(field, "value")
		}
		ew.printf("function $goIsZero%s(value: unknown): boolean { return %s; }\n", name, expression)
	}
	ew.println("")
}

func zodArrayStructIsZero(def typemap.TypeDef, value string) string {
	var parts []string
	groups := make(map[string]bool)
	for _, field := range def.Fields {
		if len(field.WhenAnyPresent) > 0 {
			for _, name := range field.WhenAnyPresent {
				if !groups[name] {
					groups[name] = true
					parts = append(parts, "(value as { [key: string]: unknown })["+typemap.ZodStringLiteral(name)+"] === undefined")
				}
			}
			continue
		}
		access := "(value as { [key: string]: unknown })[" + typemap.ZodStringLiteral(field.Name) + "]"
		if field.ZodOmit {
			parts = append(parts, access+" === undefined")
			continue
		}
		parts = append(parts, zodArrayIsZero(field, access))
	}
	predicate := "true"
	if len(parts) > 0 {
		predicate = strings.Join(parts, " && ")
	}
	return "((value: unknown) => value == null || (typeof value === \"object\" && !Array.isArray(value) && (" + predicate + ")))(" + value + ")"
}

func zodArrayIsZero(field typemap.Field, value string) string {
	return "((value: unknown) => { try { return " + zodArrayZeroExpr(field, "value") + "; } catch { return false; } })(" + value + ")"
}

func zodArrayZeroExpr(field typemap.Field, value string) string {
	if field.IsPointer {
		if field.JSONString && typemap.ZodQuotedNull(field.GoKind) {
			return "(" + value + " == null || " + value + " === \"null\")"
		}
		return "(" + value + " == null)"
	}
	if field.JSONString {
		decoded := typemap.ZodQuotedScalarValue("String(value)", field.GoKind)
		plain := field
		plain.JSONString = false
		null := "value == null"
		if typemap.ZodQuotedNull(field.GoKind) {
			null += " || value === \"null\""
		}
		return "((value: unknown) => " + null + " || " + zodArrayIsZero(plain, decoded) + ")(" + value + ")"
	}
	if field.ArrayLen != nil {
		child := zodElementField(unwrapZodParentheses(strings.TrimSuffix(zodFieldType(field), "[]")), field.Element)
		return "((value: unknown) => value == null || (Array.isArray(value) && value.slice(0, " + strconv.FormatInt(*field.ArrayLen, 10) + ").every((item: unknown) => " + zodArrayIsZero(child, "item") + ")))(" + value + ")"
	}
	if field.Inline != nil {
		return zodArrayStructIsZero(*field.Inline, value)
	}
	switch field.GoKind {
	case "map", "slice", "[]byte", "interface", "unknown", "json.RawMessage":
		return "(" + value + " == null)"
	case "time.Time":
		// The element decodes as Go does before reflect.IsZero tests it, so
		// fractional digits beyond the ninth truncate to zero and an explicit
		// offset, which records a Location, is nonzero.
		return "(" + value + " == null || (typeof " + value + " === \"string\" && " + typemap.ZodGoTimeIsZero(value) + "))"
	case "float32":
		return "(" + value + " == null || (typeof " + value + " === \"number\" && Math.fround(" + value + ") === 0))"
	case "string":
		return "(" + value + " == null || " + value + " === \"\")"
	case "bool":
		return "(" + value + " == null || " + value + " === false)"
	}
	ts := zodFieldType(field)
	if zodNumericKind(field.GoKind) || ts == "number" {
		return "(" + value + " == null || " + value + " === 0 || " + value + " === 0n)"
	}
	if field.GoKind == "string" || ts == "string" {
		return "(" + value + " == null || " + value + " === \"\")"
	}
	if field.GoKind == "bool" || ts == "boolean" {
		return "(" + value + " == null || " + value + " === false)"
	}
	if ts == "unknown" {
		return "(" + value + " == null)"
	}
	return "$goIsZero" + ts + "(" + value + ")"
}

func validateZodArrayRules(field typemap.Field) error {
	if field.ArrayLen == nil || field.IsPointer {
		return nil
	}
	var needsZero func([]typemap.ValidateRule) bool
	needsZero = func(rules []typemap.ValidateRule) bool {
		for _, rule := range rules {
			if rule.Tag == "required" || rule.Tag == "omitempty" || rule.Tag == "omitzero" || needsZero(rule.Alternatives) {
				return true
			}
		}
		return false
	}
	if !needsZero(field.Validate) {
		return nil
	}
	var check func(*typemap.GoEqualityType) error
	check = func(d *typemap.GoEqualityType) error {
		if d == nil {
			return nil
		}
		if strings.Contains(d.Error, "omission") || strings.Contains(d.Error, "zod_omit") {
			return fmt.Errorf("array zero-value validation requires fields hidden by schema omission")
		}
		if d.Pointer {
			return nil
		}
		switch d.Kind {
		case "json.Number", "json.RawMessage":
			return fmt.Errorf("array zero-value validation cannot distinguish the Go zero of %s from its JSON representation", d.Kind)
		case "map", "slice", "interface", "unknown":
			return nil
		}
		if err := check(d.Element); err != nil {
			return err
		}
		for _, field := range d.Fields {
			if err := check(field.Type); err != nil {
				return err
			}
		}
		return nil
	}
	return check(field.Equality)
}

// zodArrayOmissionZero is the value test an omission tag applies to a fixed
// array field. omitempty only dereferences a pointer, so a pointer to a zero
// array still runs later rules; omitzero tests the dereferenced array itself.
func zodArrayOmissionZero(field typemap.Field, tag, value string) string {
	if field.IsPointer && tag == "omitempty" {
		return value + " == null"
	}
	plain := field
	plain.IsPointer = false
	return zodArrayIsZero(plain, value)
}
