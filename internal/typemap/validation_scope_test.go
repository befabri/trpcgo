package typemap

import (
	"strconv"
	"strings"
	"testing"
)

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
