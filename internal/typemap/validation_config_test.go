package typemap

import (
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
