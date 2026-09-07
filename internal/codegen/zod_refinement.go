package codegen

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/befabri/trpcgo/internal/typemap"
)

func zodRefinementPredicate(ref typemap.Refinement, fields map[string]typemap.Field) string {
	predicate := zodRefinementPredicateUnchecked(ref, fields)
	// Zod may run object refinements after a field refinement has failed.
	// Malformed wire strings must report validation errors, never throw from
	// BigInt or JSON.parse while safeParse is evaluating another constraint.
	if strings.Contains(predicate, "BigInt(") || strings.Contains(predicate, "JSON.parse(") {
		return "(() => { try { return " + predicate + "; } catch { return false; } })()"
	}
	return predicate
}

func zodRefinementPredicateUnchecked(ref typemap.Refinement, fields map[string]typemap.Field) string {
	if len(ref.Alternatives) > 0 {
		branches := make([]string, 0, len(ref.Alternatives))
		for _, branch := range ref.Alternatives {
			branches = append(branches, "("+zodRefinementPredicateUnchecked(branch, fields)+")")
		}
		result := "(" + strings.Join(branches, " || ") + ")"
		if guard := zodPresence(ref.WhenAnyPresent, fields); guard != "" {
			result = "!(" + guard + ") || " + result
		}
		return result
	}
	field, leftExists := fields[ref.Field]
	other, rightExists := fields[ref.OtherField]
	if !leftExists {
		return "false"
	}
	left, leftGuard := zodRefinementOperand(ref.Field, field)
	right, rightGuard := zodRefinementOperand(ref.OtherField, other)
	result := "false"
	if ref.Op == "!==" {
		// validator's nefield succeeds when lookup fails or kinds differ.
		result = "true"
	}
	if ref.ScalarRule != nil {
		if field.ArrayLen != nil {
			// Required tests the decoded array's zero value, including padded
			// elements, even when it is one alternative of a cross-field rule.
			result = zodArrayRuleProgram(field, []typemap.ValidateRule{*ref.ScalarRule}, left, zodArrayIsZero(field, left))
		} else if field.JSONString {
			// Keep the precise wire decoder (including int64/uint64 BigInt
			// rules) when a scalar rule participates in a cross-field OR.
			branch := field
			branch.Validate, branch.ElementValidate = []typemap.ValidateRule{*ref.ScalarRule}, nil
			branch.Optional, branch.ValidateOmitempty = false, false
			result = typemap.ZodType(branch, typemap.ZodMini) + ".safeParse(" + zodRefinementRawOperand(ref.Field, field) + ").success"
		} else if predicate, ok := typemap.ZodRulePredicate(field, *ref.ScalarRule, left); ok {
			result = predicate
		} else {
			// An alternative the generator cannot express may still satisfy the
			// group in Go, so the client must not reject the value.
			result = "true"
		}
	} else if ref.OtherHidden != nil {
		// JSON never sets the target, so Go compares against its zero value. A
		// nil pointer target fails validator's lookup like a missing field.
		hidden := *ref.OtherHidden
		if !hidden.IsPointer && zodComparisonKind(field) == zodComparisonKind(hidden) {
			result = zodCompareGoValues(left, zodHiddenTargetValue(hidden), ref.Op, field, hidden)
		}
	} else if rightExists && !ref.MissingTarget && zodComparisonKind(field) == zodComparisonKind(other) {
		result = zodCompareGoValues(left, right, ref.Op, field, other)
		var targetGuards []string
		if guard := zodPresence(ref.OtherWhenAnyPresent, fields); guard != "" {
			targetGuards = append(targetGuards, guard)
		}
		if rightGuard != "" {
			targetGuards = append(targetGuards, rightGuard)
		}
		if guard := strings.Join(targetGuards, " && "); guard != "" {
			if ref.Op == "!==" {
				result = "!(" + guard + ") || (" + result + ")"
			} else {
				result = guard + " && " + result
			}
		}
	}
	if leftGuard != "" {
		result = leftGuard + " && (" + result + ")"
	}
	if skip := zodRefinementSkip(ref, field, left); skip != "" {
		result = "(" + skip + " || (" + result + "))"
	}
	if guard := zodPresence(ref.WhenAnyPresent, fields); guard != "" {
		result = "!(" + guard + ") || (" + result + ")"
	}
	return result
}

func zodRefinementMessage(ref typemap.Refinement) string {
	if len(ref.Alternatives) > 0 {
		var branches []string
		for _, branch := range ref.Alternatives {
			branches = append(branches, zodRefinementMessage(branch))
		}
		return strings.Join(branches, " or ")
	}
	if ref.ScalarRule != nil {
		rule := ref.ScalarRule.Tag
		if ref.ScalarRule.Param != "" {
			rule += "=" + ref.ScalarRule.Param
		}
		return ref.Field + " must satisfy " + rule
	}
	return fmt.Sprintf("%s must be %s %s", ref.Field, ref.Op, ref.OtherField)
}

// Cross-field validators compare Go reflection kinds, not the corresponding
// JavaScript types. In particular an int and an int64 are different kinds,
// and a byte slice is a slice even though JSON represents it as base64.
func zodComparisonKind(field typemap.Field) string {
	kind := field.GoKind
	if kind == "" {
		kind = field.Type
	}
	switch kind {
	case "[]byte", "json.RawMessage":
		return "slice"
	case "time.Time":
		return "struct"
	case "boolean":
		return "bool"
	case "json.Number":
		// json.Number is a string kind: validator compares its decimal text.
		return "string"
	}
	return kind
}

func zodCompareGoValues(left, right, op string, field, other typemap.Field) string {
	if field.GoKind == "time.Time" && other.GoKind == "time.Time" {
		return "$goTimeCompare(" + left + ", " + right + ") " + op + " 0"
	}
	kind := zodComparisonKind(field)
	if zodIntegerKind(kind) && (field.JSONString || other.JSONString) {
		if !field.JSONString {
			left = "BigInt(" + left + ")"
		}
		if !other.JSONString {
			right = "BigInt(" + right + ")"
		}
	}
	equality := op == "===" || op == "!=="
	switch kind {
	case "string":
		if equality {
			left = "$goString(" + left + ")"
			right = "$goString(" + right + ")"
		} else {
			left = "new TextEncoder().encode(" + left + ").length"
			right = "new TextEncoder().encode(" + right + ").length"
		}
	case "slice", "array", "map":
		if equality {
			if field.ArrayLen != nil && other.ArrayLen != nil {
				// Go compares array lengths even when an optional wire property
				// is absent. Fold constants to avoid impossible TS literal checks.
				equal := *field.ArrayLen == *other.ArrayLen
				return strconv.FormatBool(equal == (op == "==="))
			}
			left = zodCollectionLength(left, field)
			right = zodCollectionLength(right, other)
		} else {
			// Ordered field validators use reflect.Value.String() for
			// collections (unlike scalar gt/lt validators, which use Len).
			left = strconv.Itoa(len("<" + field.GoType + " Value>"))
			right = strconv.Itoa(len("<" + other.GoType + " Value>"))
		}
	case "struct":
		// Non-time structs fall through to reflect.Value.String(), after
		// checking exact Go type identity. Their field values are not compared.
		if field.GoType != other.GoType {
			return strconv.FormatBool(op == "!==")
		}
		return strconv.FormatBool(op == "===" || op == "<=" || op == ">=")
	case "bool":
		if !equality {
			left = strconv.Itoa(len("<" + field.GoType + " Value>"))
			right = strconv.Itoa(len("<" + other.GoType + " Value>"))
		}
	}
	return left + " " + op + " " + right
}

func zodCollectionLength(value string, field typemap.Field) string {
	if field.ArrayLen != nil {
		return strconv.FormatInt(*field.ArrayLen, 10)
	}
	if field.GoKind == "[]byte" {
		// The wire value is base64, but Go compares decoded byte lengths.
		return "(" + value + " == null ? 0 : Math.floor((" + value + ").replace(/[\\r\\n]/g, \"\").replace(/=+$/, \"\").length * 3 / 4))"
	}
	if zodComparisonKind(field) == "map" {
		return "Object.keys(" + value + " ?? {}).length"
	}
	return "(" + value + "?.length ?? 0)"
}

func zodRefinementOperand(name string, field typemap.Field) (value, guard string) {
	value = zodRefinementRawOperand(name, field)
	if field.IsPointer {
		guard = zodDataAccess(name) + " != null"
		if field.JSONString {
			guard += " && " + zodDataAccess(name) + " !== \"null\""
		}
	}
	if field.JSONString {
		value = typemap.ZodQuotedScalarValue(value, field.GoKind)
	}
	if field.GoKind == "float32" && !field.JSONString {
		value = "Math.fround(" + value + ")"
	}
	if field.GoKind == "json.Number" {
		value = "String(" + value + ")"
	}
	return value, guard
}

func zodRefinementRawOperand(name string, field typemap.Field) string {
	value := zodDataAccess(name)
	if !field.IsPointer && typemap.ZodFieldOptional(field) {
		if zero := zodGoZero(field); zero != "" {
			if field.JSONString {
				zero = typemap.ZodStringLiteral(zero)
			}
			// A missing property decodes to Go's zero value. Use it for
			// comparisons while preserving the original parsed wire value.
			value = "(" + value + " ?? " + zero + ")"
		}
	}
	return value
}

func zodIntegerKind(kind string) bool {
	switch kind {
	case "int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64", "uintptr":
		return true
	}
	return false
}

// Omission only skips validators after it in the tag sequence. RuleIndex is
// one based; the legacy flag remains useful for manually constructed TypeDefs.
func zodRefinementSkip(ref typemap.Refinement, field typemap.Field, value string) string {
	omitempty, omitzero, omitnil := field.ValidateOmitempty && ref.RuleIndex == 0, false, false
	for i, rule := range field.Validate {
		if ref.RuleIndex > 0 && i >= ref.RuleIndex-1 {
			break
		}
		switch rule.Tag {
		case "omitempty":
			omitempty = true
		case "omitzero":
			omitzero = true
		case "omitnil":
			omitnil = true
		}
	}
	if !omitempty && !omitzero && !omitnil {
		return ""
	}
	if field.IsPointer || zodComparisonKind(field) == "slice" || zodComparisonKind(field) == "map" {
		nilValue := zodDataAccess(ref.Field) + " == null"
		if field.IsPointer && field.JSONString {
			nilValue += " || " + zodDataAccess(ref.Field) + " === \"null\""
		}
		if omitzero {
			// omitzero tests the dereferenced value and treats an empty
			// collection as zero; omitempty only tests for nil.
			if field.IsPointer {
				plain := field
				plain.IsPointer = false
				if zero := zodGoZero(plain); zero != "" {
					nilValue += " || " + value + " === " + zero
				}
			} else {
				nilValue += " || " + zodCollectionLength(zodDataAccess(ref.Field), field) + " === 0"
			}
		}
		return nilValue
	}
	if omitnil && !omitempty && !omitzero {
		return ""
	}
	if field.GoKind == "time.Time" {
		return "$goTimeCompare(" + value + ", \"0001-01-01T00:00:00Z\") === 0"
	}
	if field.ArrayLen != nil {
		return zodArrayIsZero(field, value)
	}
	if zodComparisonKind(field) == "array" {
		if zero := zodArrayElementZero("element", field.Element); zero != "" {
			return "(" + value + " == null || " + value + ".every((element: any) => " + zero + "))"
		}
	}
	if zero := zodGoZero(field); zero != "" {
		if field.JSONString && zodIntegerKind(zodComparisonKind(field)) {
			zero += "n"
		}
		return value + " === " + zero
	}
	return ""
}

func zodArrayElementZero(value string, element *typemap.ElementType) string {
	if element == nil {
		return ""
	}
	if element.IsPointer || element.GoKind == "slice" || element.GoKind == "map" || element.GoKind == "[]byte" {
		return value + " == null"
	}
	if element.GoKind == "array" {
		if zero := zodArrayElementZero("item", element.Element); zero != "" {
			return value + ".every((item: any) => " + zero + ")"
		}
	}
	if element.GoKind == "time.Time" {
		return "$goTimeCompare(" + value + ", \"0001-01-01T00:00:00Z\") === 0"
	}
	if zero := zodGoZero(typemap.Field{GoKind: element.GoKind, Type: element.Type}); zero != "" {
		return value + " === " + zero
	}
	return ""
}

func zodPresence(names []string, fields map[string]typemap.Field) string {
	var checks []string
	for _, name := range names {
		if field, ok := fields[name]; ok && !field.ZodOmit {
			checks = append(checks, zodDataAccess(name)+" !== undefined")
		}
	}
	if len(checks) == 0 {
		return ""
	}
	return "(" + strings.Join(checks, " || ") + ")"
}

func zodGoZero(field typemap.Field) string {
	if field.IsPointer {
		return ""
	}
	kind := field.GoKind
	if kind == "" {
		kind = field.Type
	}
	switch kind {
	case "number", "int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64", "uintptr", "float32", "float64":
		return "0"
	case "string", "json.Number":
		return `""`
	case "bool", "boolean":
		return "false"
	case "time.Time":
		return `"0001-01-01T00:00:00Z"`
	default:
		return ""
	}
}

// zodHiddenTargetValue is the JavaScript spelling of the Go zero value a
// never-decoded target holds. Fixed arrays and structs compare by constant
// length or type identity, so their operand is never read.
func zodHiddenTargetValue(hidden typemap.Field) string {
	switch zodComparisonKind(hidden) {
	case "slice", "map":
		return "null"
	}
	if zero := zodGoZero(hidden); zero != "" {
		return zero
	}
	return "undefined"
}
