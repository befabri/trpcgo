package typemap

import (
	"cmp"
	"slices"
	"sort"
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
	Hidden        bool   // JSON never sets the field (unexported or json:"-"); it is only a cross-field target
}

// FieldReference binds a cross-field rule to its Go field before promotion,
// shadowing, or TypeScript exclusion changes the emitted property set.
type FieldReference struct {
	Alternatives []FieldReference
	ScalarRule   *ValidateRule
	OtherPath    string
	OtherName    string // original parameter, retained when the Go field does not exist
	RuleIndex    int    // one-based position in the field's validation rules
	Op           string
	Tag          string
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
// under parent. Failed lookups remain explicit: validator rejects those rules,
// except nefield, which succeeds when the target cannot be found.
func BindFieldReferences(rules []ValidateRule, parent string, lookup func(string) ([]int, bool)) []FieldReference {
	var refs []FieldReference
	var bind func(ValidateRule, int) FieldReference
	bind = func(rule ValidateRule, index int) FieldReference {
		ref := FieldReference{OtherName: rule.Param, RuleIndex: index, Tag: rule.Tag}
		if len(rule.Alternatives) > 0 {
			for _, branch := range rule.Alternatives {
				ref.Alternatives = append(ref.Alternatives, bind(branch, index))
			}
		} else if op, ok := CrossFieldOp(rule.Tag); ok {
			ref.Op = op
			if indexes, ok := lookup(rule.Param); ok {
				ref.OtherPath = FieldIndexPath(parent, indexes...)
			}
		} else {
			ref.ScalarRule = &rule
		}
		return ref
	}
	for i, rule := range rules {
		if HasCrossFieldRule(rule) {
			refs = append(refs, bind(rule, i+1))
		}
	}
	return refs
}

// HasCrossFieldRule includes references nested inside an OR group.
func HasCrossFieldRule(rule ValidateRule) bool {
	if _, ok := CrossFieldOp(rule.Tag); ok {
		return true
	}
	for _, branch := range rule.Alternatives {
		if HasCrossFieldRule(branch) {
			return true
		}
	}
	return false
}

// ExtendsCandidate is an explicit tstype extends base and the index path of
// the embedded Go field that supplies it.
type ExtendsCandidate struct {
	Type string
	Path string
}

// ResolveFields retains explicit inheritance when it is compatible with JSON
// field selection. On a collision, flatten the selected fields instead: a TS
// extends clause cannot represent an overridden or ambiguous base property.
// The returned positions count the emitted fields declared before each
// retained base's embedded Go field, so a schema that flattens inheritance can
// keep encoding/json's byIndex order, which decides case-insensitive key matches.
func ResolveFields(candidates []FieldCandidate, extends []ExtendsCandidate, recursive bool) ([]Field, []string, []int, []Refinement) {
	hiddenByPath := make(map[string]Field)
	visible := make([]FieldCandidate, 0, len(candidates))
	for _, c := range candidates {
		if c.Hidden {
			hiddenByPath[c.Path] = c.Field
		} else {
			visible = append(visible, c)
		}
	}
	candidates = visible
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
	var paths []string
	var refs []Refinement
	var resolve func(FieldCandidate, FieldReference) (Refinement, bool)
	resolve = func(c FieldCandidate, ref FieldReference) (Refinement, bool) {
		resolved := Refinement{Field: c.Field.Name, Op: ref.Op, Tag: ref.Tag, RuleIndex: ref.RuleIndex,
			WhenAnyPresent: scopeFields(c.OptionalScope, selected), ScalarRule: ref.ScalarRule}
		if len(ref.Alternatives) > 0 {
			for _, branch := range ref.Alternatives {
				child, ok := resolve(c, branch)
				if !ok {
					// An omitted alternative might satisfy the group. Preserve
					// zod_omit semantics by dropping the whole unknown constraint.
					return Refinement{}, false
				}
				resolved.Alternatives = append(resolved.Alternatives, child)
			}
			return resolved, true
		}
		if ref.ScalarRule != nil {
			return resolved, true
		}
		if ref.OtherPath == "" {
			resolved.MissingTarget, resolved.OtherField = true, ref.OtherName
			return resolved, true
		}
		other, ok := byPath[ref.OtherPath]
		if !ok {
			if hidden, found := hiddenByPath[ref.OtherPath]; found {
				// JSON never sets the target, so Go compares against its zero value.
				resolved.OtherField, resolved.OtherHidden = hidden.GoName, &hidden
				return resolved, true
			}
			return Refinement{}, false
		}
		resolved.OtherField = other.Field.Name
		if other.OptionalScope != c.OptionalScope {
			resolved.OtherWhenAnyPresent = scopeFields(other.OptionalScope, selected)
		}
		return resolved, true
	}
	for _, c := range selected {
		if c.Excluded {
			continue
		}
		if flatten || !c.Inherited {
			c.Field.WhenAnyPresent = scopeFields(c.OptionalScope, selected)
			fields = append(fields, c.Field)
			paths = append(paths, c.Path)
			if c.Field.ZodOmit {
				continue
			}
			for _, ref := range c.References {
				if resolved, ok := resolve(c, ref); ok {
					refs = append(refs, resolved)
				}
			}
		}
	}
	bases := make([]string, len(extends))
	positions := make([]int, len(extends))
	for i, ext := range extends {
		bases[i] = ext.Type
		positions[i] = sort.Search(len(paths), func(j int) bool { return compareFieldPath(paths[j], ext.Path) > 0 })
	}
	return fields, bases, positions, refs
}

func scopeFields(scope string, candidates []FieldCandidate) []string {
	if scope == "" {
		return nil
	}
	var fields []string
	for _, c := range candidates {
		// A preserved zod_omit field still causes Go to allocate its embedded
		// pointer; it skips only its own client validation.
		if !c.Excluded && strings.HasPrefix(c.Path, scope+".") {
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
