// Package zodconfig describes explicit client counterparts of Go validation.
// It contains configuration only; it never executes or translates Go callbacks.
package zodconfig

import (
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"regexp"
	"slices"
	"strings"
)

// Config is shared by reflection generation, source generation and the CLI.
// Applications register the corresponding Go validators themselves.
type Config struct {
	TagName     string                  `json:"tagName,omitempty"`
	Aliases     map[string]string       `json:"aliases,omitempty"`
	Rules       map[string]Rule         `json:"rules,omitempty"`
	StructRules map[string][]StructRule `json:"structRules,omitempty"`
	Imports     map[string]string       `json:"imports,omitempty"`
	Strict      bool                    `json:"strict,omitempty"`
}

// Rule validates a decoded scalar or a collection. Predicate is a TypeScript
// function expression or imported function taking (value, parameter) and
// returning boolean. GoKinds optionally restricts the Go kinds where it applies.
// ServerOnly explicitly records a rule that has no browser counterpart.
type Rule struct {
	Predicate  string   `json:"predicate,omitempty"`
	Message    string   `json:"message,omitempty"`
	GoKinds    []string `json:"goKinds,omitempty"`
	ServerOnly bool     `json:"serverOnly,omitempty"`
}

// StructRule runs on the parsed object with JSON property names. The map key
// in Config.StructRules is a fully-qualified Go type name, or a generated schema
// name when that name is unambiguous. Predicate takes one object and returns bool.
type StructRule struct {
	Predicate string   `json:"predicate"`
	Message   string   `json:"message,omitempty"`
	Path      []string `json:"path,omitempty"`
}

// Decode reads exactly one configuration object and rejects misspelled keys.
func Decode(r io.Reader) (Config, error) {
	var decoded *Config
	d := json.NewDecoder(r)
	d.DisallowUnknownFields()
	if err := d.Decode(&decoded); err != nil {
		return Config{}, fmt.Errorf("invalid Zod validation configuration: %w", err)
	}
	if decoded == nil {
		return Config{}, fmt.Errorf("validation configuration must be a JSON object")
	}
	c := *decoded
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return c, fmt.Errorf("validation configuration must contain exactly one JSON object")
	}
	return c, c.Validate()
}

var identifier = regexp.MustCompile(`^[A-Za-z_$][A-Za-z0-9_$]*$`)

// Validate checks configuration structure. Alias grammar and cycles are checked
// by the validation compiler, before any schema output is written.
func (c Config) Validate() error {
	if strings.ContainsAny(c.TagName, " \t\r\n:\"") {
		return fmt.Errorf("invalid validation tag name %q", c.TagName)
	}
	for name, body := range c.Aliases {
		if !identifier.MatchString(name) || strings.TrimSpace(body) == "" {
			return fmt.Errorf("invalid validation alias %q", name)
		}
		if _, exists := c.Rules[name]; exists {
			return fmt.Errorf("validation name %q is both an alias and a custom rule", name)
		}
	}
	for name, rule := range c.Rules {
		hasPredicate := strings.TrimSpace(rule.Predicate) != ""
		if !identifier.MatchString(name) || hasPredicate == rule.ServerOnly {
			return fmt.Errorf("custom rule %q must have exactly one of predicate or serverOnly", name)
		}
		for _, kind := range rule.GoKinds {
			if !slices.Contains(goKinds, kind) {
				return fmt.Errorf("custom rule %q has unknown Go kind %q", name, kind)
			}
		}
	}
	for name, module := range c.Imports {
		if !identifier.MatchString(name) || slices.Contains(reservedBindings, name) || strings.HasPrefix(name, "$") || strings.TrimSpace(module) == "" || strings.ContainsAny(module, "\r\n") {
			return fmt.Errorf("invalid Zod validation import %q", name)
		}
	}
	for name, rules := range c.StructRules {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("struct validation requires a type name")
		}
		for _, rule := range rules {
			if strings.TrimSpace(rule.Predicate) == "" {
				return fmt.Errorf("struct validation %q requires a predicate", name)
			}
		}
	}
	return nil
}

var goKinds = []string{"bool", "string", "int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64", "uintptr", "float32", "float64", "array", "slice", "map", "struct", "interface", "time.Time", "json.Number", "[]byte"}

// Generated schemas use these ECMAScript globals directly. An imported
// namespace must not change the meaning of a built-in parser or helper.
var reservedBindings = strings.Fields("z parseGoJSON await break case catch class const continue debugger default delete do else enum export extends false finally for function if implements import in instanceof interface let new null package private protected public return static super switch this throw true try typeof var void while with yield arguments eval undefined NaN Infinity globalThis Math Object JSON Number String Boolean Array Set Map WeakMap WeakSet BigInt Symbol Date RegExp Error TypeError RangeError SyntaxError Promise TextEncoder TextDecoder Uint8Array ArrayBuffer DataView Function Reflect Proxy Intl parseInt parseFloat isNaN isFinite atob btoa")

// Clone detaches mutable collections from the caller's configuration.
func (c Config) Clone() Config {
	c.Aliases, c.Imports = maps.Clone(c.Aliases), maps.Clone(c.Imports)
	c.Rules = maps.Clone(c.Rules)
	for name, rule := range c.Rules {
		rule.GoKinds = slices.Clone(rule.GoKinds)
		c.Rules[name] = rule
	}
	c.StructRules = maps.Clone(c.StructRules)
	for name, rules := range c.StructRules {
		rules = slices.Clone(rules)
		for i := range rules {
			rules[i].Path = slices.Clone(rules[i].Path)
		}
		c.StructRules[name] = rules
	}
	return c
}
