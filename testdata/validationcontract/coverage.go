package validationcontract

import (
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/befabri/trpcgo/internal/typemap"
)

// SupportedTags lists the validate tags the generator claims to translate.
// The example server module cannot import internal packages, so the corpus
// re-exports the generator's own table rather than keeping a copy.
func SupportedTags() []string {
	return typemap.SupportedZodTags()
}

// StructuralTag reports whether tag is a directive that never rejects a value
// on its own, so the Go validator can never attribute a rejection to it.
func StructuralTag(tag string) bool {
	return typemap.StructuralZodTag(tag)
}

// ValidateTags returns every validate tag name declared on t or on any type
// reachable through its fields, including OR alternatives and rules inside
// dive, keys, and endkeys scopes.
func ValidateTags(t reflect.Type) map[string]bool {
	tags := make(map[string]bool)
	collectValidateTags(t, tags, make(map[reflect.Type]bool))
	return tags
}

func collectValidateTags(t reflect.Type, tags map[string]bool, visited map[reflect.Type]bool) {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if visited[t] {
		return
	}
	visited[t] = true
	switch t.Kind() {
	case reflect.Struct:
		for i := range t.NumField() {
			field := t.Field(i)
			for _, rule := range typemap.ParseValidateTag(string(field.Tag)) {
				collectRuleTags(rule, tags)
			}
			collectValidateTags(field.Type, tags, visited)
		}
	case reflect.Slice, reflect.Array:
		collectValidateTags(t.Elem(), tags, visited)
	case reflect.Map:
		collectValidateTags(t.Key(), tags, visited)
		collectValidateTags(t.Elem(), tags, visited)
	}
}

func collectRuleTags(rule typemap.ValidateRule, tags map[string]bool) {
	if rule.Tag != "" {
		tags[rule.Tag] = true
	}
	for _, branch := range rule.Alternatives {
		collectRuleTags(branch, tags)
	}
}

// RejectionTags extracts tag names from a validator field error tag. An OR
// group reports its whole expression, such as "startswith=a|startswith=b";
// every branch failed, so each branch counts. A literal pipe inside a parameter
// can only produce an extra name outside the supported set, which is ignored.
func RejectionTags(errorTag string) []string {
	var names []string
	for branch := range strings.SplitSeq(errorTag, "|") {
		name, _, _ := strings.Cut(branch, "=")
		if name != "" {
			names = append(names, name)
		}
	}
	return names
}

// TagCoverage records the corpus evidence for one validate tag.
type TagCoverage struct {
	Types      []string // fixture types declaring the tag, sorted
	Accepted   int      // cases on those types that Go accepts
	Rejected   int      // cases on those types that Go rejects
	Attributed int      // rejections the Go validator blamed on this tag
}

// Coverage indexes cases by the tags their fixture types declare. attribute
// may be nil; the Go oracle passes the tags validator reported for a rejected
// case, which distinguishes a rule that fired from one that merely coexisted
// with another failure.
func Coverage(cases []Case, attribute func(Case) []string) (map[string]TagCoverage, error) {
	declared := make(map[string]map[string]bool)
	coverage := make(map[string]TagCoverage)
	types := make(map[string]map[string]bool)
	for _, tc := range cases {
		tags, ok := declared[tc.Type]
		if !ok {
			input := NewInput(tc.Type)
			if input == nil {
				return nil, fmt.Errorf("case %q uses unregistered fixture type %q", tc.Name, tc.Type)
			}
			tags = ValidateTags(reflect.TypeOf(input))
			declared[tc.Type] = tags
		}
		for tag := range tags {
			c := coverage[tag]
			if tc.Valid {
				c.Accepted++
			} else {
				c.Rejected++
			}
			coverage[tag] = c
			if types[tag] == nil {
				types[tag] = make(map[string]bool)
			}
			types[tag][tc.Type] = true
		}
		if attribute != nil && !tc.Valid {
			for _, tag := range attribute(tc) {
				c := coverage[tag]
				c.Attributed++
				coverage[tag] = c
			}
		}
	}
	for tag, names := range types {
		c := coverage[tag]
		c.Types = slices.Sorted(func(yield func(string) bool) {
			for name := range names {
				if !yield(name) {
					return
				}
			}
		})
		coverage[tag] = c
	}
	return coverage, nil
}

// Uncovered reports every supported tag whose evidence is incomplete. Each
// tag needs a fixture that declares it, an accepted case, and a rejected case.
// When attribution is available, rule tags also need a rejection the Go
// validator blamed on them, which a rejection caused by a sibling rule cannot
// satisfy. Structural tags cannot fire, so they are exempt from attribution.
func Uncovered(coverage map[string]TagCoverage, attributed bool) []string {
	var problems []string
	for _, tag := range SupportedTags() {
		c := coverage[tag]
		switch {
		case len(c.Types) == 0:
			problems = append(problems, tag+": no contract fixture declares this tag")
		case c.Accepted == 0:
			problems = append(problems, tag+": no accepted case exercises "+strings.Join(c.Types, ", "))
		case c.Rejected == 0:
			problems = append(problems, tag+": no rejected case exercises "+strings.Join(c.Types, ", "))
		case attributed && !StructuralTag(tag) && c.Attributed == 0:
			problems = append(problems, tag+": the Go validator never rejected a case because of this tag on "+strings.Join(c.Types, ", "))
		}
	}
	return problems
}
