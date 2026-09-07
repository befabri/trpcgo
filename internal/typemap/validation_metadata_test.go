package typemap

import (
	"reflect"
	"testing"
)

func TestApplyValidateRulesFollowsContainerScopes(t *testing.T) {
	nested := &ElementType{GoKind: "map", Key: &ElementType{GoKind: "string"}, Element: &ElementType{GoKind: "int"}}
	tests := []struct {
		name        string
		field       Field
		tag         string
		wantInvalid []ValidateRule
	}{
		{name: "scalar invalid kind", field: Field{GoKind: "int"}, tag: `validate:"email"`, wantInvalid: []ValidateRule{{Tag: "email"}}},
		{name: "valid map keys and values", field: Field{GoKind: "map", Key: nested.Key, Element: nested.Element}, tag: `validate:"dive,keys,email,endkeys,min=1"`},
		{name: "nested map keys and values", field: Field{GoKind: "slice", Element: nested}, tag: `validate:"dive,dive,keys,email,endkeys,min=1"`},
		{name: "invalid nested map value", field: Field{GoKind: "slice", Element: nested}, tag: `validate:"dive,dive,keys,email,endkeys,email"`, wantInvalid: []ValidateRule{{Tag: "email"}}},
		{name: "invalid numeric map key", field: Field{GoKind: "map", Key: &ElementType{GoKind: "int"}, Element: &ElementType{GoKind: "string"}}, tag: `validate:"dive,keys,email,endkeys,min=1"`, wantInvalid: []ValidateRule{{Tag: "email"}}},
		{name: "byte dive uses byte kind", field: Field{GoKind: "[]byte"}, tag: `validate:"dive,email"`, wantInvalid: []ValidateRule{{Tag: "email"}}},
		{name: "valid byte constraint", field: Field{GoKind: "[]byte"}, tag: `validate:"dive,min=1"`},
		{name: "malformed scopes stay writer errors", field: Field{GoKind: "map", Key: nested.Key, Element: nested.Element}, tag: `validate:"dive,keys,email"`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ApplyValidateRules(&tc.field, tc.tag)
			if !reflect.DeepEqual(tc.field.InvalidZod, tc.wantInvalid) {
				t.Fatalf("invalid rules = %#v, want %#v", tc.field.InvalidZod, tc.wantInvalid)
			}
		})
	}
}

func TestZodFieldOptionalRequiredOverride(t *testing.T) {
	for _, tc := range []struct {
		name  string
		field Field
		want  bool
	}{
		{name: "plain required", field: Field{Required: true}},
		{name: "required beats validator omission", field: Field{Required: true, ValidateOmitempty: true}},
		{name: "required beats optional flag", field: Field{Required: true, Optional: true}},
		{name: "ordinary validator omission", field: Field{ValidateOmitempty: true}, want: true},
		{name: "required within nullable embedded parent", field: Field{Required: true, Optional: true, WhenAnyPresent: []string{"value", "other"}}, want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ZodFieldOptional(tc.field); got != tc.want {
				t.Fatalf("optional=%v,want %v", got, tc.want)
			}
		})
	}
	if got := ZodMissingValuePredicate(Field{Required: true, ValidateOmitempty: true}); got != "false" {
		t.Fatalf("missing explicit required field predicate=%s,want false", got)
	}
}
