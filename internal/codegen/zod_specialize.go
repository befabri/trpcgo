package codegen

import (
	"fmt"
	"slices"
	"strings"

	"github.com/befabri/trpcgo/internal/typemap"
)

// specializeZodDefinitions uses the mapper's concrete Go type metadata rather
// than guessing type kinds from TypeScript substitutions. It leaves the generic
// declarations used by WriteAppRouter untouched.
func specializeZodDefinitions(procs []ProcEntry, defs []typemap.TypeDef) ([]ProcEntry, []typemap.TypeDef, error) {
	instances := make(map[string]string)
	ambiguous := make(map[string]bool)
	expanded := slices.Clone(defs)
	for _, d := range defs {
		for _, instance := range d.Specializations {
			// Different Go types can erase to the same TypeScript expression.
			// Only a unique expression is safe for callers lacking concrete metadata.
			if prior, exists := instances[instance.InstanceOf]; exists && prior != instance.Name {
				delete(instances, instance.InstanceOf)
				ambiguous[instance.InstanceOf] = true
			} else if !ambiguous[instance.InstanceOf] {
				instances[instance.InstanceOf] = instance.Name
			}
			expanded = append(expanded, instance)
		}
	}
	if len(expanded) == len(defs) {
		procs = slices.Clone(procs)
		for i := range procs {
			if procs[i].InputZod != "" {
				procs[i].InputTS = procs[i].InputZod
			}
		}
		return procs, defs, nil
	}
	rewrite := func(ts string) string { return mapTypeExpressions(ts, instances) }
	var element func(*typemap.ElementType) *typemap.ElementType
	var field func(typemap.Field) typemap.Field
	var definition func(typemap.TypeDef) typemap.TypeDef
	element = func(e *typemap.ElementType) *typemap.ElementType {
		if e == nil {
			return nil
		}
		d := *e
		if d.Inline != nil {
			inner := definition(*d.Inline)
			d.Inline = &inner
		}
		d.Key, d.Element = element(d.Key), element(d.Element)
		d.Type = rewrite(zodMetadataType(d.Type, d.Inline, d.Element, d.Key))
		return &d
	}
	field = func(f typemap.Field) typemap.Field {
		if f.Inline != nil {
			d := definition(*f.Inline)
			f.Inline = &d
		}
		f.Key, f.Element = element(f.Key), element(f.Element)
		// An anonymous object's public type may contain Box<number> even
		// though its fields retain the distinct Box[int8] and Box[int16] IDs.
		// Rebuild from those children before applying the compatibility fallback.
		f.ZodType = rewrite(zodShapeFieldType(f))
		return f
	}
	definition = func(d typemap.TypeDef) typemap.TypeDef {
		d.Fields = slices.Clone(d.Fields)
		for i := range d.Fields {
			d.Fields[i] = field(d.Fields[i])
		}
		if len(d.ZodExtends) > 0 {
			d.Extends = d.ZodExtends
		}
		d.Extends = slices.Clone(d.Extends)
		for i := range d.Extends {
			d.Extends[i] = rewrite(d.Extends[i])
		}
		d.AliasOf = rewrite(d.AliasOf)
		if d.Underlying != nil {
			f := field(*d.Underlying)
			d.Underlying = &f
		}
		return d
	}
	for i := range expanded {
		expanded[i] = definition(expanded[i])
	}
	procs = slices.Clone(procs)
	for i := range procs {
		if procs[i].InputZod != "" {
			procs[i].InputTS = procs[i].InputZod
		} else {
			procs[i].InputTS = rewrite(procs[i].InputTS)
		}
	}
	if err := validateZodSpecializationIdentity(procs, expanded, ambiguous); err != nil {
		return nil, nil, err
	}
	return procs, expanded, nil
}

// Unresolved public expressions are an error only when their definitions are
// used by an input schema. Unrelated output types must not block generation.
func validateZodSpecializationIdentity(procs []ProcEntry, defs []typemap.TypeDef, ambiguous map[string]bool) error {
	if len(ambiguous) == 0 {
		return nil
	}
	check := func(ts, path string) error {
		var conflict string
		rewriteTypeExpressions(ts, func(expression string) (string, bool) {
			if ambiguous[expression] {
				if conflict == "" {
					conflict = expression
				}
				return expression, true
			}
			return "", false
		})
		if conflict != "" {
			return fmt.Errorf("zod validation %s: generic type %s matches multiple Go instantiations; concrete Go type metadata is required", path, conflict)
		}
		return nil
	}
	roots := make(map[string]bool)
	for _, proc := range procs {
		if err := check(proc.InputTS, fmt.Sprintf("procedure %q input", proc.Path)); err != nil {
			return err
		}
		if proc.InputTS != "void" {
			roots[stripGenericArgs(proc.InputTS)] = true
		}
	}
	defsByName := make(map[string]typemap.TypeDef, len(defs))
	for _, def := range defs {
		defsByName[def.Name] = def
	}
	reachable := transitiveReachable(roots, defsByName)
	for _, def := range defs {
		if !reachable[def.Name] {
			continue
		}
		for _, field := range def.Fields {
			if !field.ZodOmit {
				if err := check(zodShapeFieldType(field), def.Name+"."+field.Name); err != nil {
					return err
				}
			}
		}
		if def.Kind == typemap.TypeDefAlias {
			ts := def.AliasOf
			if def.Underlying != nil {
				ts = zodShapeFieldType(*def.Underlying)
			}
			if err := check(ts, def.Name); err != nil {
				return err
			}
		}
		for _, base := range def.Extends {
			if err := check(base, def.Name+" base"); err != nil {
				return err
			}
		}
	}
	return nil
}

// mapTypeExpressions replaces whole concrete generic expressions while
// preserving properties and string literal contents in inline TypeScript types.
func mapTypeExpressions(ts string, replacements map[string]string) string {
	return rewriteTypeExpressions(ts, func(expression string) (string, bool) {
		replacement, ok := replacements[expression]
		return replacement, ok
	})
}

func rewriteTypeExpressions(ts string, rewrite func(string) (string, bool)) string {
	ts = strings.TrimSpace(ts)
	if replacement, ok := rewrite(ts); ok {
		return replacement
	}
	if parts := splitTopLevel(ts, '|'); len(parts) > 1 {
		for i := range parts {
			parts[i] = rewriteTypeExpressions(parts[i], rewrite)
		}
		return strings.Join(parts, " | ")
	}
	if before, ok := strings.CutSuffix(ts, "[]"); ok {
		return rewriteTypeExpressions(before, rewrite) + "[]"
	}
	if strings.HasPrefix(ts, "(") && strings.HasSuffix(ts, ")") {
		return "(" + rewriteTypeExpressions(ts[1:len(ts)-1], rewrite) + ")"
	}
	if fields, ok := inlineTypeFields(ts); ok {
		for i := range fields {
			fields[i].Type = rewriteTypeExpressions(fields[i].Type, rewrite)
		}
		return typemap.InlineObjectType(fields)
	}
	if i := strings.IndexByte(ts, '<'); i >= 0 && strings.HasSuffix(ts, ">") {
		args := splitTopLevel(ts[i+1:len(ts)-1], ',')
		for i := range args {
			args[i] = rewriteTypeExpressions(args[i], rewrite)
		}
		return ts[:i] + "<" + strings.Join(args, ", ") + ">"
	}
	return ts
}
