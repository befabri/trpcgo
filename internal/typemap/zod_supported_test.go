package typemap

import (
	"slices"
	"testing"
)

// Every rendering table must stay a subset of the supported set. Otherwise a
// tag could produce Zod output while being reported as unsupported, or the
// contract coverage gate could miss it.
func TestSupportedZodTagsAreConsistent(t *testing.T) {
	supported := SupportedZodTags()
	if !slices.IsSorted(supported) || len(supported) != len(supportedZodTags) {
		t.Fatalf("SupportedZodTags() = %v, want the sorted keys of supportedZodTags", supported)
	}
	tables := map[string][]string{
		"zodFormatBases":    slices.Collect(mapKeys(zodFormatBases)),
		"zodStringRegexes":  slices.Collect(mapKeys(zodStringRegexes)),
		"zodStructuralTags": slices.Collect(mapKeys(zodStructuralTags)),
		"crossFieldOps":     slices.Collect(mapKeys(crossFieldOps)),
	}
	for name, tags := range tables {
		for _, tag := range tags {
			if !supportedZodTags[tag] {
				t.Errorf("%s contains %q, which supportedZodTags does not list", name, tag)
			}
		}
	}
	for tag := range zodStructuralTags {
		if _, cross := CrossFieldOp(tag); cross || zodFormatBases[tag] != "" || zodStringRegexes[tag] != "" {
			t.Errorf("%q is structural and cannot also be a rule", tag)
		}
	}
}

func mapKeys[V any](m map[string]V) func(yield func(string) bool) {
	return func(yield func(string) bool) {
		for key := range m {
			if !yield(key) {
				return
			}
		}
	}
}
