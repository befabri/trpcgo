package typemap

import (
	"reflect"
	"strings"
	"testing"
)

func TestGoUniqueMetadataRejectsBackendPanicsAndIdentity(t *testing.T) {
	for _, tc := range []struct {
		name, source, selector, reason string
		typ                            reflect.Type
	}{
		{"map values", "map[string]int", "", "noncomparable", reflect.TypeFor[map[string]int]()},
		{"slice values", "[]int", "", "noncomparable", reflect.TypeFor[[]int]()},
		{"array of slices", "[1][]int", "", "noncomparable", reflect.TypeFor[[1][]int]()},
		{"nested pointer", "struct { Value *int }", "", "pointer identity", reflect.TypeFor[struct{ Value *int }]()},
		{"multiple pointers", "**int", "", "single dereference", reflect.TypeFor[**int]()},
		{"missing selector", "struct { ID int }", "Missing", "missing", reflect.TypeFor[struct{ ID int }]()},
		{"nonstruct selector", "int", "ID", "struct elements", reflect.TypeFor[int]()},
		{"uncomparable selector", "struct { ID []int }", "ID", "noncomparable", reflect.TypeFor[struct{ ID []int }]()},
		{"unexported selector", "struct { id int }", "id", "unexported", reflect.TypeFor[struct{ id int }]()},
		{"omitted selector", "struct { ID int `json:\"-\"` }", "ID", "unavailable", reflect.TypeFor[struct {
			ID int `json:"-"`
		}]()},
		{"schema omission", "struct { ID int `zod_omit:\"true\"` }", "", "omission", reflect.TypeFor[struct {
			ID int `zod_omit:"true"`
		}]()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := graphTypes(t, "type Element "+tc.source)
			for name, d := range map[string]*GoEqualityType{"static": DescribeTypesEquality(p.Scope().Lookup("Element").Type()), "reflection": DescribeReflectEquality(tc.typ)} {
				t.Run(name, func(t *testing.T) {
					field := Field{GoKind: "slice", Element: &ElementType{Equality: d}, Validate: []ValidateRule{{Tag: "unique", Param: tc.selector}}}
					err := ValidateZodFieldRules(field)
					if err == nil || !strings.Contains(err.Error(), tc.reason) {
						t.Fatalf("error=%v, want %q", err, tc.reason)
					}
					if _, ok := ZodRulePredicate(field, field.Validate[0], "value"); ok {
						t.Fatal("unsupported Go equality emitted")
					}
				})
			}
		})
	}
}

func TestGoUniqueSelectionDoesNotRequireComparableParent(t *testing.T) {
	type item struct {
		ID      *int `json:"wireID"`
		Payload map[string]int
	}
	d := DescribeReflectEquality(reflect.TypeFor[item]())
	field := Field{GoKind: "slice", Element: &ElementType{Equality: d}, Validate: []ValidateRule{{Tag: "unique", Param: "ID"}}}
	if err := ValidateZodFieldRules(field); err != nil {
		t.Fatal(err)
	}
	expression, ok := ZodRulePredicate(field, field.Validate[0], "value")
	if !ok || !strings.Contains(expression, `["wireID"]`) {
		t.Fatalf("selected field not retained: %s", expression)
	}
	field.Validate[0].Param = ""
	if err := ValidateZodFieldRules(field); err == nil {
		t.Fatal("whole struct must reject noncomparable payload")
	}
}

func TestResolveFieldClonesEqualityMetadata(t *testing.T) {
	original := Field{Equality: DescribeReflectEquality(reflect.TypeFor[struct{ Value [1]int }]())}
	cloned := ResolveField(original, nil)
	cloned.Equality.Fields[0].Type.Element.Kind = "string"
	if original.Equality.Fields[0].Type.Element.Kind != "int" {
		t.Fatal("resolved metadata mutated mapper metadata")
	}
}
