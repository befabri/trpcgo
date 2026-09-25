package typemap

import (
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/befabri/trpcgo/zodconfig"
)

func TestConfiguredAliasGrammar(t *testing.T) {
	for _, tc := range []struct {
		aliases map[string]string
		want    string
	}{
		{map[string]string{"a": "b", "b": "a"}, "cycle"},
		{map[string]string{"a": "min=1,,max=2"}, "empty"},
		{map[string]string{"a": "required,-"}, "entire"},
		{map[string]string{"a": "dive,keys,required"}, "matching"},
		{map[string]string{"a": "required", "b": "a|eq=x"}, "complete"},
		{map[string]string{"a": "required", "b": "a=1"}, "complete"},
		{map[string]string{"min": "required"}, "shadows"},
	} {
		_, err := CompileValidation(zodconfig.Config{Aliases: tc.aliases})
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("aliases %v: got %v, want %s", tc.aliases, err, tc.want)
		}
	}
}

func TestConfiguredRulesKeepScopeAndOwnedMetadata(t *testing.T) {
	c := zodconfig.Config{TagName: "check", Aliases: map[string]string{"entry": "required,dive,keys,alpha,endkeys,dive,custom=0x2C0x7C"}, Rules: map[string]zodconfig.Rule{"custom": {Predicate: "(v,p) => true", GoKinds: []string{"string"}}}}
	p, err := CompileValidation(c)
	if err != nil {
		t.Fatal(err)
	}
	c.Rules["custom"].GoKinds[0] = "int"
	f := Field{GoKind: "map"}
	ApplyValidation(&f, `check:"entry" validate:"required,min=99"`, p)
	scope, err := FieldValidationScope(f)
	if err != nil {
		t.Fatal(err)
	}
	if scope.Keys == nil || scope.Element == nil || scope.Element.Element == nil {
		t.Fatalf("lost nested scopes: %#v", scope)
	}
	rule := scope.Element.Element.Rules[0]
	if rule.Param != ",|" || rule.Custom == nil || rule.Custom.GoKinds[0] != "string" {
		t.Fatalf("lost custom rule metadata: %#v", rule)
	}
	rule.Custom.GoKinds[0] = "bool"
	other := Field{}
	ApplyValidation(&other, `check:"entry"`, p)
	otherScope, err := FieldValidationScope(other)
	if err != nil {
		t.Fatal(err)
	}
	if otherScope.Element.Element.Rules[0].Custom.GoKinds[0] != "string" {
		t.Fatal("field mutation affected compiler or another field")
	}
}

func TestValidationScopes(t *testing.T) {
	rules := ParseValidateTag(`validate:"min=1,dive,keys,startswith=team_,endkeys,min=2,dive,required,email"`)
	scope, err := ParseValidationScope(rules)
	if err != nil {
		t.Fatal(err)
	}
	if len(scope.Rules) != 1 || scope.Rules[0].Tag != "min" || scope.Keys == nil || scope.Element == nil {
		t.Fatalf("lost map scope: %#v", scope)
	}
	if len(scope.Keys.Rules) != 1 || scope.Keys.Rules[0].Tag != "startswith" {
		t.Fatalf("lost key rules: %#v", scope.Keys)
	}
	if len(scope.Element.Rules) != 1 || scope.Element.Rules[0].Param != "2" || scope.Element.Element == nil {
		t.Fatalf("lost value container boundary: %#v", scope.Element)
	}
	leaf := scope.Element.Element
	if len(leaf.Rules) != 2 || leaf.Rules[0].Tag != "required" || leaf.Rules[1].Tag != "email" {
		t.Fatalf("lost ordered leaf rules: %#v", leaf)
	}
}

func TestValidationScopeRejectsMalformedKeys(t *testing.T) {
	for _, tag := range []string{
		`validate:"keys,email,endkeys"`,
		`validate:"dive,keys,email"`,
		`validate:"dive,email,keys,required,endkeys"`,
		`validate:"dive,endkeys"`,
	} {
		t.Run(tag, func(t *testing.T) {
			if _, err := ParseValidationScope(ParseValidateTag(tag)); err == nil {
				t.Fatal("invalid key scope accepted")
			}
		})
	}
}

func TestValidationScopeRejectsStructuralAlternatives(t *testing.T) {
	for _, directive := range []string{"dive", "keys", "endkeys", "omitempty", "omitnil", "omitzero", "structonly", "nostructlevel"} {
		for _, tag := range []string{directive + "|email", "email|" + directive, "dive,keys,email|" + directive + ",endkeys,email"} {
			t.Run(tag, func(t *testing.T) {
				rules := ParseValidateTag("validate:" + strconv.Quote(tag))
				if _, err := ParseValidationScope(rules); err == nil || !strings.Contains(err.Error(), directive+" cannot be used in an OR group") {
					t.Fatalf("expected structural OR diagnostic, got %v", err)
				}
			})
		}
	}
	// Ordinary validation functions, including required, remain valid alternatives.
	for _, tag := range []string{"required|email", "email|eqfield=Fallback", "dive,keys,email|eq=local,endkeys,required"} {
		if _, err := ParseValidationScope(ParseValidateTag("validate:" + strconv.Quote(tag))); err != nil {
			t.Errorf("valid alternatives %q rejected: %v", tag, err)
		}
	}
}

func TestValidationScopeRejectsDirectiveParameters(t *testing.T) {
	for _, tag := range []string{"dive=,email", "omitempty=,email", "dive,keys=,email,endkeys", "dive,keys,email,endkeys=", "dive=1,email", "omitempty=true,email", "omitnil=false,email", "dive,keys=email,required,endkeys", "dive,keys,email,endkeys=true"} {
		if _, err := ParseValidationScope(ParseValidateTag("validate:" + strconv.Quote(tag))); err == nil || !strings.Contains(err.Error(), "does not accept a parameter") {
			t.Errorf("%q: expected structural parameter diagnostic, got %v", tag, err)
		}
	}
	for _, tag := range []string{"-,email", "email|-", "required,-"} {
		if _, err := ParseValidationScope(ParseValidateTag("validate:" + strconv.Quote(tag))); err == nil || !strings.Contains(err.Error(), "entire validation tag") {
			t.Errorf("%q: expected skip grammar diagnostic, got %v", tag, err)
		}
	}
}

// validator splits a tag on commas and pipes alone and panics at its first use
// on an empty rule or a rule name that kept a space, so the generator reports
// the tag instead of repairing it, on the field for the writer's path.
func TestValidationScopeReportsMalformedTokens(t *testing.T) {
	for _, tc := range []struct{ tag, want string }{
		{"required,", `empty validation rule in "required,"`},
		{"min=1,,max=3", `empty validation rule in "min=1,,max=3"`},
		{"|", `empty validation rule in "|"`},
		{"email|", `empty validation rule in "email|"`},
		{"required, email", `rule " email" in "required, email" keeps its surrounding whitespace`},
		{"required ,email", `rule "required " in "required ,email" keeps its surrounding whitespace`},
		{"dive,keys,email ,endkeys", `rule "email " in "dive,keys,email ,endkeys" keeps its surrounding whitespace`},
	} {
		t.Run(tc.tag, func(t *testing.T) {
			raw := "validate:" + strconv.Quote(tc.tag)
			if _, err := ParseValidationScope(ParseValidateTag(raw)); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ParseValidationScope error = %v, want %q", err, tc.want)
			}
			f := Field{Name: "name", Type: "string", GoKind: "string"}
			ApplyValidateRules(&f, raw)
			if !strings.Contains(f.ValidationError, tc.want) {
				t.Fatalf("ValidationError = %q, want %q", f.ValidationError, tc.want)
			}
		})
	}
	// Whitespace inside a parameter is validator's own syntax.
	f := Field{Name: "name", Type: "string", GoKind: "string"}
	ApplyValidateRules(&f, `validate:"oneof=a b,startswith= ,endswith= x "`)
	if f.ValidationError != "" {
		t.Fatalf("parameter whitespace reported: %s", f.ValidationError)
	}
}

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
		{name: "validator omission keeps the key", field: Field{ValidateOmitempty: true}},
		{name: "json omission", field: Field{Optional: true}, want: true},
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
