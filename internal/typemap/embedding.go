package typemap

import (
	"cmp"
	"slices"
	"strconv"
	"strings"
)

// FieldCandidate is a struct field collected before embedded-field precedence
// is resolved.
type FieldCandidate struct {
	Field         Field
	Depth         int    // number of embedded structs between the outer struct and the field
	Tagged        bool   // the JSON name comes from an explicit json tag
	Inherited     bool   // supplied by an explicit tstype:",extends" base
	Excluded      bool   // tstype:"-"; participates in JSON dominance but is not emitted
	Path          string // index path from the outer struct, identifying the original Go field
	References    []FieldReference
	OptionalScope string // index path of the innermost optional embedded pointer
}

// FieldReference binds a cross-field rule to its Go field before promotion,
// shadowing, or TypeScript exclusion changes the emitted property set.
type FieldReference struct {
	OtherPath string
	Op        string
	Tag       string
}

// FieldIndexPath appends indexes to parent as a dot-separated field index path.
func FieldIndexPath(parent string, indexes ...int) string {
	for _, index := range indexes {
		if parent != "" {
			parent += "."
		}
		parent += strconv.Itoa(index)
	}
	return parent
}

// BindFieldReferences resolves the cross-field rules in rules to index paths
// under parent. Rules whose target field lookup fails are dropped.
func BindFieldReferences(rules []ValidateRule, parent string, lookup func(string) ([]int, bool)) []FieldReference {
	var refs []FieldReference
	for _, rule := range rules {
		if op, ok := CrossFieldOp(rule.Tag); ok {
			if indexes, ok := lookup(rule.Param); ok {
				refs = append(refs, FieldReference{OtherPath: FieldIndexPath(parent, indexes...), Op: op, Tag: rule.Tag})
			}
		}
	}
	return refs
}

// ResolveFields retains explicit inheritance when it is compatible with JSON
// field selection. On a collision, flatten the selected fields instead: a TS
// extends clause cannot represent an overridden or ambiguous base property.
func ResolveFields(candidates []FieldCandidate, extends []string, recursive bool) ([]Field, []string, []Refinement) {
	// Circular extends clauses are invalid TypeScript, so recursive
	// embedding is flattened.
	flatten := recursive
	seen := make(map[string]bool, len(candidates))
	for _, c := range candidates {
		if seen[c.Field.Name] {
			flatten = true
		}
		seen[c.Field.Name] = true
	}
	if flatten {
		extends = nil
	}
	selected := dominantCandidates(candidates)
	byPath := make(map[string]FieldCandidate, len(selected))
	for _, c := range selected {
		if !c.Excluded && !c.Field.ZodOmit {
			byPath[c.Path] = c
		}
	}
	var fields []Field
	var refs []Refinement
	for _, c := range selected {
		if c.Excluded {
			continue
		}
		if flatten || !c.Inherited {
			fields = append(fields, c.Field)
			if c.Field.ZodOmit {
				continue
			}
			for _, ref := range c.References {
				if other, ok := byPath[ref.OtherPath]; ok {
					resolved := Refinement{Field: c.Field.Name, OtherField: other.Field.Name, Op: ref.Op, Tag: ref.Tag,
						WhenAnyPresent: scopeFields(c.OptionalScope, selected)}
					if other.OptionalScope != c.OptionalScope {
						resolved.OtherWhenAnyPresent = scopeFields(other.OptionalScope, selected)
					}
					refs = append(refs, resolved)
				}
			}
		}
	}
	return fields, extends, refs
}

func scopeFields(scope string, candidates []FieldCandidate) []string {
	if scope == "" {
		return nil
	}
	var fields []string
	for _, c := range candidates {
		if !c.Excluded && !c.Field.ZodOmit && strings.HasPrefix(c.Path, scope+".") {
			fields = append(fields, c.Field.Name)
		}
	}
	return fields
}

// DominantFields applies encoding/json's precedence to promoted fields that
// share a JSON name: the shallowest field wins, then the explicitly tagged
// one, and equally deep fields with the same tag status are ambiguous and are
// all dropped. Surviving fields keep their original order.
func DominantFields(candidates []FieldCandidate) []Field {
	var fields []Field
	for _, c := range dominantCandidates(candidates) {
		if !c.Excluded {
			fields = append(fields, c.Field)
		}
	}
	return fields
}

func dominantCandidates(candidates []FieldCandidate) []FieldCandidate {
	byName := make(map[string][]int, len(candidates))
	for i, c := range candidates {
		byName[c.Field.Name] = append(byName[c.Field.Name], i)
	}
	keep := make([]bool, len(candidates))
	for _, group := range byName {
		if len(group) == 1 {
			keep[group[0]] = true
			continue
		}
		slices.SortStableFunc(group, func(a, b int) int {
			return cmp.Or(
				cmp.Compare(candidates[a].Depth, candidates[b].Depth),
				cmp.Compare(tagRank(candidates[a]), tagRank(candidates[b])),
			)
		})
		first, second := candidates[group[0]], candidates[group[1]]
		if first.Depth == second.Depth && first.Tagged == second.Tagged {
			continue
		}
		keep[group[0]] = true
	}
	var fields []FieldCandidate
	for i, c := range candidates {
		if keep[i] {
			fields = append(fields, c)
		}
	}
	return fields
}

func tagRank(c FieldCandidate) int {
	if c.Tagged {
		return 0
	}
	return 1
}
