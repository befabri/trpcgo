package typemap

// ApplyValidateRules attaches the ordered rule program and its diagnostics to
// a field whose Go type metadata is already available. Both mappers use this
// path, so rules on map keys and nested elements are checked against the same
// Go kinds that the schema emitter will use.
func ApplyValidateRules(f *Field, tag string) {
	rules := ParseValidateTag(tag)
	applyValidateRules(f, rules)
}

func applyValidateRules(f *Field, rules []ValidateRule) {
	rules = TruncateAtStructOnly(rules)
	f.Validate, f.ElementValidate = SplitAtDive(rules)
	f.UnsupportedZod = UnsupportedZodRules(rules)
	f.InvalidZod = nil
	f.ValidateOmitempty = false
	if ValidationRequiresPresence(f.Validate) {
		f.Optional = false
	}
	for _, rule := range f.Validate {
		if rule.Tag == "omitempty" || rule.Tag == "omitzero" {
			f.ValidateOmitempty = true
		}
	}
	scope, err := ParseValidationScope(rules)
	if err != nil {
		// The writer reports malformed scope grammar before writing any output.
		// Do not infer misleading scalar diagnostics from malformed boundaries.
		return
	}
	root := &ElementType{GoKind: f.GoKind, Element: f.Element, Key: f.Key}
	var collect func(ValidationScope, *ElementType)
	collect = func(scope ValidationScope, typ *ElementType) {
		if typ == nil {
			typ = &ElementType{}
		}
		f.InvalidZod = append(f.InvalidZod, InvalidZodRules(scope.Rules, typ.GoKind)...)
		if scope.Keys != nil {
			collect(*scope.Keys, typ.Key)
		}
		if scope.Element != nil {
			element := typ.Element
			if typ.GoKind == "[]byte" {
				element = &ElementType{GoKind: "uint8"}
			}
			collect(*scope.Element, element)
		}
	}
	collect(scope, root)
}

// ZodFieldOptional centralizes the schema's structural optionality. An explicit
// tstype required override wins over omission tags. Promoted fields remain
// conditional on their embedded pointer; the object-level presence checks
// enforce the override once that parent exists.
func ZodFieldOptional(f Field) bool {
	return (f.Optional || f.ValidateOmitempty) && (!f.Required || len(f.WhenAnyPresent) > 0)
}

// TruncateAtStructOnly drops the rules after structonly or nostructlevel.
// validator stops traversing a field at either directive, so later rules,
// including dive, never run; the directive itself is kept so struct-typed
// fields can report that their nested validation is skipped.
func TruncateAtStructOnly(rules []ValidateRule) []ValidateRule {
	for i, rule := range rules {
		if StructOnlyZodTag(rule.Tag) {
			return rules[:i+1]
		}
	}
	return rules
}

// StructOnlyZodTag reports whether tag ends field validation in validator.
func StructOnlyZodTag(tag string) bool {
	return tag == "structonly" || tag == "nostructlevel"
}
