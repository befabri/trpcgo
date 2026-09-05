package typemap

import (
	"cmp"
	"slices"
	"strconv"
	"strings"
)

// EmbeddedField is one struct field as reported by a FieldAdapter.
type EmbeddedField[T comparable] struct {
	Type               T
	Name, Tag          string
	Exported, Embedded bool
}

// FieldAdapter supplies struct field access for either reflect.Type or
// types.Type, so CollectJSONFields owns all visibility, embedding, and tag
// policy.
type FieldAdapter[T comparable] struct {
	Fields   func(T) []EmbeddedField[T]
	Struct   func(T) (base T, isStruct, pointer bool)
	TypeName func(T) string
	Map      func(owner T, index int, name string, omitted bool, tag TSTypeTag, hasTag bool) Field
	Lookup   func(T, string) ([]int, bool)
}

// CollectJSONFields collects the JSON-visible fields of root using
// encoding/json's breadth-first walk. A type reached twice at one depth
// contributes its direct fields twice, making them ambiguous, but its embedded
// children are enqueued once; duplicating whole subtrees would wrongly drop
// deeper diamonds.
func CollectJSONFields[T comparable](root T, a FieldAdapter[T], allowExtends bool) ([]Field, []string, []Refinement) {
	type node struct {
		typ                 T
		path, scope         string
		depth               int
		inherited, excluded bool
	}
	next := []node{{typ: root}}
	nextCount := map[T]int{root: 1}
	visited := make(map[T]bool)
	var candidates []FieldCandidate
	var extends []string
	recursive := false
	for len(next) > 0 {
		current, count := next, nextCount
		next = nil
		nextCount = make(map[T]int)
		for _, parent := range current {
			if visited[parent.typ] {
				recursive = true
				continue
			}
			visited[parent.typ] = true
			for index, f := range a.Fields(parent.typ) {
				base, isStruct, pointer := a.Struct(f.Type)
				if !f.Exported && (!f.Embedded || !isStruct) {
					continue
				}
				name, omitted, skip := ParseJSONTag(f.Tag)
				if skip {
					continue
				}
				tag, hasTag := ParseTSTypeTag(f.Tag)
				excluded := parent.excluded || tag.Type == "-"
				path := FieldIndexPath(parent.path, index)
				if f.Embedded && name == "" && isStruct {
					if visited[base] {
						recursive = true
						continue
					}
					child := node{typ: base, path: path, scope: parent.scope, depth: parent.depth + 1, inherited: parent.inherited, excluded: excluded}
					if pointer && !tag.Required {
						child.scope = path
					}
					if !excluded && !parent.inherited && allowExtends && tag.Extends {
						ts := a.TypeName(base)
						if pointer && !tag.Required {
							ts = "Partial<" + ts + ">"
						}
						extends = append(extends, ts)
						child.inherited = true
					}
					nextCount[base]++
					if nextCount[base] == 1 {
						next = append(next, child)
					}
					continue
				}
				tagged := name != ""
				if !tagged {
					name = f.Name
				}
				candidate := FieldCandidate{Field: Field{Name: name}, Depth: parent.depth, Tagged: tagged, Inherited: parent.inherited, Excluded: excluded, Path: path, OptionalScope: parent.scope}
				if !excluded {
					candidate.Field = a.Map(parent.typ, index, name, omitted, tag, hasTag)
					if parent.scope != "" {
						candidate.Field.Optional = true
					}
					candidate.References = BindFieldReferences(candidate.Field.Validate, parent.path, func(name string) ([]int, bool) { return a.Lookup(parent.typ, name) })
				}
				candidates = append(candidates, candidate)
				if count[parent.typ] > 1 {
					candidates = append(candidates, candidate)
				}
			}
		}
	}
	slices.SortStableFunc(candidates, func(a, b FieldCandidate) int { return compareFieldPath(a.Path, b.Path) })
	return ResolveFields(candidates, extends, recursive)
}

func compareFieldPath(a, b string) int {
	x, y := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(x) && i < len(y); i++ {
		xi, _ := strconv.Atoi(x[i])
		yi, _ := strconv.Atoi(y[i])
		if c := cmp.Compare(xi, yi); c != 0 {
			return c
		}
	}
	return cmp.Compare(len(x), len(y))
}
