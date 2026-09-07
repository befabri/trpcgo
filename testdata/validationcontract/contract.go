// Package validationcontract shares boundary cases between the generated Zod
// contracts in the library and the go-playground validator oracle in the
// example server module.
//
// Each feature is a pair of files. The *_types.go file holds only Go
// declarations: the static mapper type-checks those files directly, without
// loading the module, so they must not import the router. The *_cases.go file
// declares the feature's cases and registers its input types from init through
// defineCases, which makes every case visible to both suites at once.
//
// Add a regression to the feature it belongs to rather than to a new file.
// Every tag needs an accepted and a rejected control, and a rejection must be
// attributable to the tag under test; the coverage gates in both suites enforce
// this for every tag the generator supports. Case names are the identifiers
// used with -run, so rename them deliberately.
package validationcontract

import (
	"context"
	"reflect"
	"slices"

	"github.com/befabri/trpcgo"
)

// Case records the expected result of JSON decoding followed by Go validation.
// Names remain stable when cases move between feature files.
type Case struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	JSON  string `json:"json"`
	Valid bool   `json:"valid"`
}

type fixture struct {
	name      string
	construct func() any
	register  func(*trpcgo.Router)
}

var fixtures = make(map[string]fixture)

var cases []Case

// inputFixture couples Go decoding and router registration to one type.
func inputFixture[T any]() fixture {
	name := reflect.TypeFor[T]().Name()
	return fixture{
		name:      name,
		construct: func() any { return new(T) },
		register: func(r *trpcgo.Router) {
			trpcgo.MustQuery(r, name, func(context.Context, T) (bool, error) { return true, nil })
		},
	}
}

// defineCases adds a feature's cases to both the Go and Zod suites. Registering
// its types also registers its cases, preventing an oracle coverage omission.
func defineCases(family []Case, inputs ...fixture) {
	defineFixtures(inputs...)
	cases = append(cases, family...)
}

// Custom cases use separate Go registrations and Zod configuration, so they
// share fixture lookup while their runners explicitly select CustomCases.
func defineFixtures(inputs ...fixture) {
	for _, input := range inputs {
		if _, exists := fixtures[input.name]; exists {
			panic("duplicate contract fixture: " + input.name)
		}
		fixtures[input.name] = input
	}
}

// Cases returns every case using the default validator configuration.
func Cases() []Case { return slices.Clone(cases) }

// RegisterCases exposes exactly the selected case types to reflection, matching
// the static mapper's inputs, including when a test isolates a helper dependency.
func RegisterCases(r *trpcgo.Router, cases []Case) {
	seen := make(map[string]bool)
	for _, tc := range cases {
		if seen[tc.Type] {
			continue
		}
		seen[tc.Type] = true
		input, ok := fixtures[tc.Type]
		if !ok {
			panic("unregistered contract fixture: " + tc.Type)
		}
		input.register(r)
	}
}

func NewInput(name string) any {
	if input, ok := fixtures[name]; ok {
		return input.construct()
	}
	return nil
}
