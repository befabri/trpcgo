package codegen

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/befabri/trpcgo/internal/typemap"
)

// Fixed arrays use reflect.Value.IsZero for required and omitempty. Length is
// fixed even when every element is zero, and non-nil empty collections or
// pointers inside an array are nonzero Go values.
func applyZodArrayRules(base string, field typemap.Field, style typemap.ZodStyle, zeroSchema ...string) string {
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
		if len(zeroSchema) > 0 {
			wire = zeroSchema[0]
		}
		base = "$goOmitZeroArray(" + base + ", " + wire + ", (value: unknown) => " + zodWirePredicate(field, "value", style) + " && " + zodArrayOmissionZero(field, omitted, "value") + ")"
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
		if field.JSONString {
			return "(" + value + " == null || " + value + " === \"null\")"
		}
		return "(" + value + " == null)"
	}
	if field.JSONString {
		decoded := typemap.ZodQuotedScalarValue("String(value)", field.GoKind)
		plain := field
		plain.JSONString = false
		return "((value: unknown) => value == null || value === \"null\" || " + zodArrayIsZero(plain, decoded) + ")(" + value + ")"
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
		// JSON decoding has no monotonic clock component. Only the zero UTC time
		// has reflect.IsZero's nil location; an equivalent explicit offset does not.
		return "(" + value + " == null || (typeof " + value + " === \"string\" && /^0001-01-01T00:00:00(?:[.,]0+)?Z$(?![\\s\\S])/.test(" + value + ")))"
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
		case "time.Time":
			return fmt.Errorf("array zero-value validation of time.Time depends on Go location identity")
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
