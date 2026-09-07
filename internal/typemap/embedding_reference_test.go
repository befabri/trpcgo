package typemap

import "testing"

func TestBoundFieldReferencesPreserveRulePositionAndMissingTarget(t *testing.T) {
	rules := []ValidateRule{{Tag: "gtfield", Param: "A"}, {Tag: "omitempty"}, {Tag: "gtfield", Param: "Missing"}}
	refs := BindFieldReferences(rules, "2", func(name string) ([]int, bool) { return []int{3}, name == "A" })
	if len(refs) != 2 || refs[0].RuleIndex != 1 || refs[0].OtherPath != "2.3" || refs[1].RuleIndex != 3 || refs[1].OtherPath != "" || refs[1].OtherName != "Missing" {
		t.Fatalf("incorrect bound references: %#v", refs)
	}
	_, _, _, refinements := ResolveFields([]FieldCandidate{
		{Field: Field{Name: "a"}, Path: "2.3"},
		{Field: Field{Name: "b"}, Path: "2.4", References: refs},
	}, nil, false)
	if len(refinements) != 2 || refinements[0].MissingTarget || !refinements[1].MissingTarget || refinements[1].RuleIndex != 3 || refinements[1].OtherField != "Missing" {
		t.Fatalf("incorrect resolved references: %#v", refinements)
	}
}

func TestBoundFieldReferencesPreserveMixedAlternatives(t *testing.T) {
	rule := ValidateRule{Alternatives: []ValidateRule{{Tag: "eqfield", Param: "A"}, {Tag: "eq", Param: "0"}}}
	refs := BindFieldReferences([]ValidateRule{rule}, "", func(name string) ([]int, bool) { return []int{0}, name == "A" })
	_, _, _, resolved := ResolveFields([]FieldCandidate{
		{Field: Field{Name: "a"}, Path: "0"},
		{Field: Field{Name: "value"}, Path: "1", References: refs},
	}, nil, false)
	if len(resolved) != 1 || len(resolved[0].Alternatives) != 2 || resolved[0].Alternatives[0].OtherField != "a" || resolved[0].Alternatives[1].ScalarRule == nil || resolved[0].Alternatives[1].ScalarRule.Param != "0" {
		t.Fatalf("mixed OR binding lost alternatives: %#v", resolved)
	}
	_, _, _, omitted := ResolveFields([]FieldCandidate{
		{Field: Field{Name: "a", ZodOmit: true}, Path: "0"},
		{Field: Field{Name: "value"}, Path: "1", References: refs},
	}, nil, false)
	if len(omitted) != 0 {
		t.Fatalf("OR with an omitted target must be omitted as a whole: %#v", omitted)
	}
}

// A target that exists in Go but never decodes from JSON keeps its zero value,
// so the refinement compares against that instead of being dropped or failing.
func TestResolveFieldsBindsHiddenTargets(t *testing.T) {
	refs := BindFieldReferences([]ValidateRule{{Tag: "gtefield", Param: "max"}}, "", func(name string) ([]int, bool) { return []int{1}, name == "max" })
	_, _, _, resolved := ResolveFields([]FieldCandidate{
		{Field: Field{Name: "count", GoKind: "int"}, Path: "0", References: refs},
		{Field: Field{GoName: "max", GoKind: "int"}, Path: "1", Hidden: true},
	}, nil, false)
	if len(resolved) != 1 || resolved[0].OtherHidden == nil || resolved[0].OtherHidden.GoKind != "int" || resolved[0].OtherField != "max" || resolved[0].MissingTarget {
		t.Fatalf("hidden target was not bound: %#v", resolved)
	}
}
