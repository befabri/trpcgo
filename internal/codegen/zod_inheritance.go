package codegen

import (
	"fmt"
	"slices"
	"strings"

	"github.com/befabri/trpcgo/internal/typemap"
)

// expandZodInheritance flattens each definition's Extends into its own fields
// and refinements. Zod's extend, merge, and partial cannot be applied to a
// refined object, so inheritance cannot be expressed as schema composition.
func expandZodInheritance(defs map[string]typemap.TypeDef, reachable map[string]bool) (map[string]typemap.TypeDef, error) {
	expanded := make(map[string]typemap.TypeDef, len(defs))
	visiting := make(map[string]bool)
	var expand func(string) (typemap.TypeDef, error)
	expand = func(name string) (typemap.TypeDef, error) {
		if def, ok := expanded[name]; ok {
			return def, nil
		}
		def, ok := defs[name]
		if !ok {
			return typemap.TypeDef{}, fmt.Errorf("zod inheritance base %q is not defined", name)
		}
		if visiting[name] {
			return typemap.TypeDef{}, fmt.Errorf("cyclic Zod inheritance at %q", name)
		}
		visiting[name] = true
		defer delete(visiting, name)
		var fields []typemap.Field
		var refs []typemap.Refinement
		for _, ext := range def.Extends {
			baseName, partial := strings.CutPrefix(ext, "Partial<")
			if partial {
				baseName = strings.TrimSuffix(baseName, ">")
			}
			if base, ok := defs[stripGenericArgs(baseName)]; !ok || base.Kind != typemap.TypeDefInterface {
				// A base mapped to a scalar such as time.Time has no object
				// shape; degrade to unknown rather than fail the whole module.
				def.Kind, def.AliasOf = typemap.TypeDefAlias, "unknown"
				def.Fields, def.Refinements, def.Extends = nil, nil, nil
				expanded[name] = def
				return def, nil
			}
			base, err := expand(stripGenericArgs(baseName))
			if err != nil {
				return typemap.TypeDef{}, err
			}
			baseFields := slices.Clone(base.Fields)
			baseRefs := slices.Clone(base.Refinements)
			if partial {
				var presence []string
				for i := range baseFields {
					baseFields[i].Optional = true
					if !baseFields[i].ZodOmit {
						presence = append(presence, baseFields[i].Name)
					}
				}
				for i := range baseRefs {
					if len(baseRefs[i].WhenAnyPresent) == 0 {
						baseRefs[i].WhenAnyPresent = presence
					}
				}
			}
			fields = append(fields, baseFields...)
			refs = append(refs, baseRefs...)
		}
		def.Fields = append(fields, def.Fields...)
		def.Refinements = append(refs, def.Refinements...)
		def.Extends = nil
		expanded[name] = def
		return def, nil
	}
	for name := range reachable {
		if _, ok := defs[name]; !ok {
			continue
		}
		if _, err := expand(name); err != nil {
			return nil, err
		}
	}
	return expanded, nil
}
