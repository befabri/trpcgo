package typemap

import (
	"cmp"
	"maps"
	"slices"
	"strconv"
	"strings"
	"unicode"
)

// UniqueTypeNames assigns TypeScript names to the Go types identified by ids.
// Types that share a short name are prefixed with title-cased package path
// segments, from the last segment backwards until the group is distinct, so
// a/models.User and b/models.User become AModelsUser and BModelsUser rather
// than one ModelsUser that silently replaces the other. A prefixed name that
// still matches another type's name takes a numeric suffix in sorted id order.
func UniqueTypeNames(ids []string, shortName, pkgPath func(id string) string) map[string]string {
	result := make(map[string]string, len(ids))
	groups := make(map[string][]string)
	for _, id := range ids {
		groups[shortName(id)] = append(groups[shortName(id)], id)
	}
	renamed := make(map[string]bool)
	for name, group := range groups {
		if len(group) == 1 {
			result[group[0]] = name
			continue
		}
		slices.Sort(group)
		for depth := 1; ; depth++ {
			candidates := make(map[string]string, len(group))
			counts := make(map[string]int, len(group))
			deeper := false
			for _, id := range group {
				segments := packageNameSegments(pkgPath(id))
				if depth < len(segments) {
					deeper = true
				}
				candidate := strings.Join(segments[max(0, len(segments)-depth):], "") + name
				candidates[id] = candidate
				counts[candidate]++
			}
			distinct := true
			for _, candidate := range candidates {
				if counts[candidate] > 1 {
					distinct = false
					break
				}
			}
			if distinct || !deeper {
				for id, candidate := range candidates {
					result[id] = candidate
					renamed[id] = true
				}
				break
			}
		}
	}
	// A type that keeps its own short name wins over a renamed one.
	ordered := slices.SortedFunc(maps.Keys(result), func(a, b string) int {
		return cmp.Or(cmp.Compare(boolRank(renamed[a]), boolRank(renamed[b])), cmp.Compare(a, b))
	})
	used := make(map[string]bool, len(result))
	for _, id := range ordered {
		name := result[id]
		if r, _ := utf8First(name); unicode.IsDigit(r) {
			name = "_" + name
			result[id] = name
		}
		if !used[name] {
			used[name] = true
			continue
		}
		for n := 2; ; n++ {
			if candidate := name + strconv.Itoa(n); !used[candidate] {
				result[id] = candidate
				used[candidate] = true
				break
			}
		}
	}
	return result
}

func boolRank(b bool) int {
	if b {
		return 1
	}
	return 0
}

func utf8First(s string) (rune, bool) {
	for _, r := range s {
		return r, true
	}
	return 0, false
}

// packageNameSegments turns each path segment into identifier text, dropping
// punctuation and capitalizing the letter that follows it: "go-models" becomes
// GoModels and "github.com" becomes GithubCom.
func packageNameSegments(path string) []string {
	var segments []string
	for segment := range strings.SplitSeq(path, "/") {
		var b strings.Builder
		upper := true
		for _, r := range segment {
			if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' {
				upper = true
				continue
			}
			if upper {
				r = unicode.ToUpper(r)
				upper = false
			}
			b.WriteRune(r)
		}
		if b.Len() > 0 {
			segments = append(segments, b.String())
		}
	}
	return segments
}
