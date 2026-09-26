package codegen

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/befabri/trpcgo/internal/typemap"
)

// zodRefinementPredicate returns the JavaScript predicate of a cross-field
// refinement. Zod may run an object refinement after a field check has failed,
// so a predicate whose operands can throw on a malformed wire string is
// wrapped: it must report a validation issue, never escape safeParse.
func zodRefinementPredicate(ref typemap.Refinement, fields map[string]typemap.Field, checks *zodSchemaChecks) string {
	predicate, throws := zodRefinementPredicateUnchecked(ref, fields, checks)
	if throws {
		return "(() => { try { return " + predicate + "; } catch { return false; } })()"
	}
	return predicate
}

// zodRefinementPredicateUnchecked builds the predicate and reports whether an
// operand it embeds can throw.
func zodRefinementPredicateUnchecked(ref typemap.Refinement, fields map[string]typemap.Field, checks *zodSchemaChecks) (string, bool) {
	if len(ref.Alternatives) > 0 {
		branches := make([]string, 0, len(ref.Alternatives))
		throws := false
		for _, branch := range ref.Alternatives {
			predicate, branchThrows := zodRefinementPredicateUnchecked(branch, fields, checks)
			branches = append(branches, "("+predicate+")")
			throws = throws || branchThrows
		}
		result := "(" + strings.Join(branches, " || ") + ")"
		if guard := zodPresence(ref.WhenAnyPresent, fields); guard != "" {
			result = "!(" + guard + ") || " + result
		}
		return result, throws
	}
	field, leftExists := fields[ref.Field]
	other, rightExists := fields[ref.OtherField]
	if !leftExists {
		return "false", false
	}
	left := zodRefinementOperand(ref.Field, field)
	right := zodRefinementOperand(ref.OtherField, other)
	// use embeds an operand's value and records whether it can throw.
	throws := false
	use := func(operand zodRefinementValue) string {
		throws = throws || operand.throws
		return operand.value
	}
	result := "false"
	if ref.Op == "!==" {
		// validator's nefield succeeds when lookup fails or kinds differ.
		result = "true"
	}
	if ref.ScalarRule != nil {
		if field.ArrayLen != nil {
			// Required tests the decoded array's zero value, including padded
			// elements, even when it is one alternative of a cross-field rule.
			result = zodArrayRuleProgram(field, []typemap.ValidateRule{*ref.ScalarRule}, use(left), zodArrayIsZero(field, use(left)))
		} else if field.JSONString {
			// Keep the precise wire decoder (including int64/uint64 BigInt
			// rules) when a scalar rule participates in a cross-field OR.
			branch := field
			branch.Validate, branch.ElementValidate = []typemap.ValidateRule{*ref.ScalarRule}, nil
			branch.Optional, branch.ValidateOmitempty = false, false
			result = checks.call(typemap.ZodType(branch, typemap.ZodMini), zodRefinementRawOperand(ref.Field, field))
		} else if predicate, ok := typemap.ZodRulePredicate(field, *ref.ScalarRule, use(left)); ok {
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
			var compareThrows bool
			result, compareThrows = zodCompareGoValues(use(left), zodHiddenTargetValue(hidden), ref.Op, field, hidden)
			throws = throws || compareThrows
		}
	} else if rightExists && !ref.MissingTarget && zodComparisonKind(field) == zodComparisonKind(other) {
		var compareThrows bool
		result, compareThrows = zodCompareGoValues(use(left), use(right), ref.Op, field, other)
		throws = throws || compareThrows
		var targetGuards []string
		if guard := zodPresence(ref.OtherWhenAnyPresent, fields); guard != "" {
			targetGuards = append(targetGuards, guard)
		}
		if right.guard != "" {
			targetGuards = append(targetGuards, right.guard)
		}
		if guard := strings.Join(targetGuards, " && "); guard != "" {
			if ref.Op == "!==" {
				result = "!(" + guard + ") || (" + result + ")"
			} else {
				result = guard + " && " + result
			}
		}
	}
	if left.guard != "" {
		result = left.guard + " && (" + result + ")"
	}
	if skip := zodRefinementSkip(ref, field, left.value); skip != "" {
		result = "(" + skip + " || (" + result + "))"
		throws = throws || left.throws
	}
	if guard := zodPresence(ref.WhenAnyPresent, fields); guard != "" {
		result = "!(" + guard + ") || (" + result + ")"
	}
	return result, throws
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

// zodCompareGoValues compares two decoded Go values as validator's field
// functions do. It reports whether the comparison itself can throw: a plain
// integer converted with BigInt to meet a quoted one raises on a value the
// schema has not rejected yet, such as a fraction.
func zodCompareGoValues(left, right, op string, field, other typemap.Field) (string, bool) {
	if field.GoKind == "time.Time" && other.GoKind == "time.Time" {
		return "$goTimeCompare(" + left + ", " + right + ") " + op + " 0", false
	}
	kind := zodComparisonKind(field)
	throws := false
	if zodIntegerKind(kind) && (field.JSONString || other.JSONString) {
		if !field.JSONString {
			left, throws = "BigInt("+left+")", true
		}
		if !other.JSONString {
			right, throws = "BigInt("+right+")", true
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
				return strconv.FormatBool(equal == (op == "===")), false
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
			return strconv.FormatBool(op == "!=="), false
		}
		return strconv.FormatBool(op == "===" || op == "<=" || op == ">="), false
	case "bool":
		if !equality {
			left = strconv.Itoa(len("<" + field.GoType + " Value>"))
			right = strconv.Itoa(len("<" + other.GoType + " Value>"))
		}
	}
	return left + " " + op + " " + right, throws
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

// zodRefinementValue is a field's decoded Go value as a JavaScript expression.
// guard is the presence test a pointer needs before the value is read. throws
// reports that evaluating the value can raise, as BigInt and JSON.parse do on
// a malformed wire string, so a predicate embedding it must catch.
type zodRefinementValue struct {
	value, guard string
	throws       bool
}

func zodRefinementOperand(name string, field typemap.Field) zodRefinementValue {
	operand := zodRefinementValue{value: zodRefinementRawOperand(name, field)}
	if field.IsPointer {
		operand.guard = zodDataAccess(name) + " != null"
		if field.JSONString && typemap.ZodQuotedNull(field.GoKind) {
			operand.guard += " && " + zodDataAccess(name) + " !== \"null\""
		}
	}
	if field.JSONString {
		operand.value, operand.throws = typemap.ZodQuotedScalar(operand.value, field.GoKind)
	}
	if field.GoKind == "float32" && !field.JSONString {
		operand.value = "Math.fround(" + operand.value + ")"
	}
	if field.GoKind == "json.Number" {
		operand.value = "String(" + operand.value + ")"
	}
	return operand
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
	kind := zodComparisonKind(field)
	if field.IsPointer || kind == "slice" || kind == "map" {
		skip := zodDataAccess(ref.Field) + " == null"
		if field.IsPointer && field.JSONString && typemap.ZodQuotedNull(field.GoKind) {
			skip += " || " + zodDataAccess(ref.Field) + " === \"null\""
		}
		if omitzero {
			// omitzero tests the value validator dereferences: an empty
			// collection, or the zero value a pointer points to, skips the
			// rules after it. omitempty and omitnil only skip nil.
			plain := field
			plain.IsPointer = false
			if zero := zodGoValueIsZero(plain, value); zero != "" {
				skip += " || " + zero
			}
		}
		return skip
	}
	if omitnil && !omitempty && !omitzero {
		return ""
	}
	return zodGoValueIsZero(field, value)
}

// zodGoValueIsZero tests the decoded value of a non-pointer field against
// reflect.Value.IsZero, as validator's omission tags do: an empty collection,
// a fixed array whose elements are all zero, a time in Go's zero UTC spelling,
// or a scalar equal to its zero. A quoted value has already been decoded, so
// an integer compares as a bigint. It returns "" when the field's zero cannot
// be expressed.
func zodGoValueIsZero(field typemap.Field, value string) string {
	switch {
	case zodComparisonKind(field) == "slice" || zodComparisonKind(field) == "map":
		return zodCollectionLength(value, field) + " === 0"
	case field.ArrayLen != nil:
		return zodArrayIsZero(field, value)
	case field.GoKind == "time.Time":
		return typemap.ZodGoTimeIsZero(value)
	case zodComparisonKind(field) == "array":
		if zero := zodArrayElementZero("element", field.Element); zero != "" {
			return "(" + value + " == null || " + value + ".every((element: any) => " + zero + "))"
		}
	}
	zero := zodGoZero(field)
	if zero == "" {
		return ""
	}
	if field.JSONString && zodIntegerKind(zodComparisonKind(field)) {
		zero += "n"
	}
	return value + " === " + zero
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

// Optional object properties can disappear without running a child refinement.
// Validate an absent fixed array at the containing object, against the actual
// Go zero array, with application predicates evaluated only during parsing.
func (e zodSchemaEmitter) writeMissingArrayChecks(ew *errWriter, fields []typemap.Field) {
	for _, field := range fields {
		if field.ZodOmit || field.IsPointer || field.ArrayLen == nil || !typemap.ZodFieldOptional(field) {
			continue
		}
		scope, err := typemap.FieldValidationScope(field)
		if err != nil || len(scope.Rules) == 0 && scope.Element == nil {
			continue
		}
		plain := field
		plain.Optional, plain.ValidateOmitempty = false, false
		plain.WhenAnyPresent = nil
		var schema string
		if scope.Element == nil {
			// Go only visits array elements after dive. Checking an outer len/custom
			// rule must not accidentally activate struct validators in those elements.
			plain.Validate, plain.ElementValidate = scope.Rules, nil
			schema = applyZodArrayRules("z.array(z.unknown())", plain, e.style, e.checks, "")
		} else {
			schema = e.scopedFieldToZod(plain, scope)
		}
		predicate := zodDataAccess(field.Name) + " !== undefined"
		if len(field.WhenAnyPresent) > 0 {
			absent := make([]string, len(field.WhenAnyPresent))
			for i, name := range field.WhenAnyPresent {
				absent[i] = zodDataAccess(name) + " === undefined"
			}
			predicate += " || (" + strings.Join(absent, " && ") + ")"
		}
		predicate += " || " + e.checks.call(schema, zodZeroValue(field))
		ew.printf(".check(z.refine((data) => %s, { message: %s, path: [%s] }))", predicate, typemap.ZodStringLiteral(field.Name+" must satisfy validation when absent"), typemap.ZodStringLiteral(field.Name))
	}
}

func writeZodMissingCustomChecks(ew *errWriter, fields []typemap.Field) {
	for _, field := range fields {
		if field.ZodOmit || len(field.WhenAnyPresent) > 0 || !typemap.ZodFieldOptional(field) || !typemap.HasCustomZodRule(field.Validate) {
			continue
		}
		predicate := typemap.ZodMissingValuePredicate(field)
		if predicate != "true" {
			ew.printf(".check(z.refine((data) => %s !== undefined || (%s), { message: %s, path: [%s] }))", zodDataAccess(field.Name), predicate, typemap.ZodStringLiteral(field.Name+" must satisfy validation when absent"), typemap.ZodStringLiteral(field.Name))
		}
	}
}

// zodSchemaChecks declares, once per module, each schema that a predicate
// tests. A schema written inside the predicate would be built again on every
// call. Each declaration builds its schema on first use, so it may reference
// schemas the module declares later, including recursive ones. It also keeps
// the runes of the JSON field names decodeGoJSON matches, for their folding.
type zodSchemaChecks struct {
	names      map[string]string
	schemas    []string
	fieldRunes map[rune]bool
}

func newZodSchemaChecks() *zodSchemaChecks {
	return &zodSchemaChecks{names: map[string]string{}, fieldRunes: map[rune]bool{}}
}

// fieldName returns the literal for a JSON field name that decodeGoJSON
// matches case-insensitively, recording its runes for the module's folds.
func (c *zodSchemaChecks) fieldName(name string) string {
	for _, r := range name {
		c.fieldRunes[r] = true
	}
	return typemap.ZodStringLiteral(name)
}

// check returns the name of the predicate that tests a value against schema.
// Its issues method returns the issues the schema reports for a value.
func (c *zodSchemaChecks) check(schema string) string {
	name, ok := c.names[schema]
	if !ok {
		name = "$goCheck" + strconv.Itoa(len(c.schemas))
		c.names[schema] = name
		c.schemas = append(c.schemas, schema)
	}
	return name
}

// call returns an expression that tests value against schema.
func (c *zodSchemaChecks) call(schema, value string) string {
	return c.check(schema) + "(" + value + ")"
}

// declarations declares the checks and the field name folds. Declaring one
// builds nothing, so a bundler may drop the checks an application never calls.
func (c *zodSchemaChecks) declarations() string {
	var out strings.Builder
	out.WriteString(zodGoFolds(c.fieldRunes))
	for i, schema := range c.schemas {
		out.WriteString("const $goCheck" + strconv.Itoa(i) + " = /* @__PURE__ */ " + typemap.ZodSchemaCheck(schema) + ";\n")
	}
	if out.Len() > 0 {
		out.WriteString("\n")
	}
	return out.String()
}

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
