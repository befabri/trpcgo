package zodconfig

import (
	"strings"
	"testing"
)

func TestDecodeRejectsInvalidConfiguration(t *testing.T) {
	for _, raw := range []string{
		`null`, `[]`, `{} {}`, `{"alias":{"a":"required"}}`,
		`{"rules":{"custom":{}}}`, `{"rules":{"custom":{"predicate":"x => true","serverOnly":true}}}`,
		`{"rules":{"custom":{"predicate":"x => true","goKinds":["integer"]}}}`,
		`{"imports":{"class":"./rules"}}`, `{"imports":{"parseGoJSON":"./rules"}}`,
		`{"imports":{"Math":"./rules"}}`, `{"imports":{"Object":"./rules"}}`, `{"imports":{"JSON":"./rules"}}`,
		`{"imports":{"$goString":"./rules"}}`, `{"structRules":{"Input":[{}]}}`,
	} {
		t.Run(raw, func(t *testing.T) {
			if _, err := Decode(strings.NewReader(raw)); err == nil {
				t.Fatalf("accepted invalid configuration %s", raw)
			}
		})
	}
	if _, err := Decode(strings.NewReader(`{"tagName":"check","rules":{"database":{"serverOnly":true}},"strict":true}`)); err != nil {
		t.Fatal(err)
	}
}

func TestConfigCloneOwnsNestedCollections(t *testing.T) {
	c := Config{
		Aliases: map[string]string{"name": "required"}, Imports: map[string]string{"checks": "./checks"},
		Rules:       map[string]Rule{"custom": {Predicate: "x => true", GoKinds: []string{"string"}}},
		StructRules: map[string][]StructRule{"Input": {{Predicate: "x => true", Path: []string{"value"}}}},
	}
	clone := c.Clone()
	clone.Aliases["name"] = "max=1"
	clone.Imports["checks"] = "changed"
	clone.Rules["custom"].GoKinds[0] = "int"
	clone.StructRules["Input"][0].Path[0] = "changed"
	if c.Aliases["name"] != "required" || c.Imports["checks"] != "./checks" || c.Rules["custom"].GoKinds[0] != "string" || c.StructRules["Input"][0].Path[0] != "value" {
		t.Fatalf("clone modified original: %#v", c)
	}
}
