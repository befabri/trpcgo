package codegen

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/befabri/trpcgo/internal/typemap"
	"github.com/befabri/trpcgo/zodconfig"
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
		d.Type = rewrite(zodMetadataType(d.Type, d.Inline, d.Element, d.Key, zodShapeField))
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
		type inherited struct {
			at     int
			fields []typemap.Field
		}
		var bases []inherited
		var refs []typemap.Refinement
		for i, ext := range def.Extends {
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
					// Omitted validation does not prevent a supplied field from
					// allocating the embedded Go pointer.
					presence = append(presence, baseFields[i].Name)
				}
				for i := range baseFields {
					if len(baseFields[i].WhenAnyPresent) == 0 {
						baseFields[i].WhenAnyPresent = presence
					}
				}
				for i := range baseRefs {
					if len(baseRefs[i].WhenAnyPresent) == 0 {
						baseRefs[i].WhenAnyPresent = presence
					}
				}
			}
			at := 0
			if i < len(def.ExtendsAt) {
				at = def.ExtendsAt[i]
			}
			bases = append(bases, inherited{at: at, fields: baseFields})
			refs = append(refs, baseRefs...)
		}
		// Inherited fields take the place of their embedded Go field, so the
		// flattened list follows encoding/json's byIndex order. That order
		// decides which field a case-insensitive key match selects.
		slices.SortStableFunc(bases, func(a, b inherited) int { return cmp.Compare(a.at, b.at) })
		var fields []typemap.Field
		next := 0
		for _, base := range bases {
			for next < base.at && next < len(def.Fields) {
				fields = append(fields, def.Fields[next])
				next++
			}
			fields = append(fields, base.fields...)
		}
		def.Fields = append(fields, def.Fields[next:]...)
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

// validateZodScopes runs before writing the module, so malformed structural
// rules produce a field-specific generation error and never a partial schema.
func validateZodScopes(order []string, defs map[string]typemap.TypeDef, strict bool) error {
	var checkField func(typemap.Field, typemap.ValidationScope, string) error
	var checkDefinition func(typemap.TypeDef, string) error
	activeElements := make(map[*typemap.ElementType]bool)
	activeInline := make(map[*typemap.TypeDef]bool)
	checkDefinition = func(def typemap.TypeDef, path string) error {
		for _, field := range def.Fields {
			if field.ZodOmit {
				continue
			}
			scope, err := typemap.FieldValidationScope(field)
			if err != nil {
				return fmt.Errorf("zod validation %s.%s: %w", path, field.Name, err)
			}
			if err := checkField(field, scope, path+"."+field.Name); err != nil {
				return err
			}
		}
		if def.Underlying != nil {
			scope, err := typemap.FieldValidationScope(*def.Underlying)
			if err != nil {
				return fmt.Errorf("zod validation %s: %w", path, err)
			}
			return checkField(*def.Underlying, scope, path)
		}
		if def.Kind == typemap.TypeDefAlias {
			return checkField(typemap.Field{Type: def.AliasOf}, typemap.ValidationScope{}, path)
		}
		return nil
	}
	checkField = func(field typemap.Field, scope typemap.ValidationScope, path string) error {
		if field.ValidationError != "" {
			return fmt.Errorf("zod validation %s: %s", path, field.ValidationError)
		}
		field.Validate = scope.Rules
		if err := validateZodConfiguredRules(field, strict); err != nil {
			return fmt.Errorf("zod validation %s: %w", path, err)
		}
		if err := typemap.ValidateZodFieldRules(field); err != nil {
			return fmt.Errorf("zod validation %s: %w", path, err)
		}
		if err := validateZodArrayRules(field); err != nil {
			return fmt.Errorf("zod validation %s: %w", path, err)
		}
		if err := validateZodReferencePaths(scope.Rules); err != nil {
			return fmt.Errorf("zod validation %s: %w", path, err)
		}
		ts := zodFieldType(field)
		key, value, record := zodRecordTypes(ts)
		kind := field.GoKind
		if record {
			kind = "map"
			if field.Key != nil && (field.Key.IsPointer || field.Key.GoKind != "" && field.Key.GoKind != "string" && !zodIntegerKind(field.Key.GoKind)) {
				return fmt.Errorf("zod validation %s{key}: Go map key kind %s requires a custom JSON decoder; only string and integer keys are supported", path, field.Key.GoKind)
			}
		} else if strings.HasSuffix(ts, "[]") && kind == "" {
			kind = "slice"
		}
		if scope.Keys != nil && kind != "map" {
			return fmt.Errorf("zod validation %s: keys/endkeys requires a map, got %s", path, ts)
		}
		if scope.Element != nil && kind != "map" && kind != "slice" && kind != "array" && kind != "[]byte" {
			return fmt.Errorf("zod validation %s: dive requires an array, slice or map, got %s", path, ts)
		}
		if field.Inline == nil && !record && !strings.HasSuffix(ts, "[]") && strings.Contains(ts, "<") {
			return fmt.Errorf("zod validation %s: generic type %s requires concrete Go type metadata", path, ts)
		}
		if field.Inline != nil && !activeInline[field.Inline] {
			activeInline[field.Inline] = true
			defer delete(activeInline, field.Inline)
			if err := checkDefinition(*field.Inline, path); err != nil {
				return err
			}
		}
		if scope.Keys != nil || (record || field.Key != nil) && (field.Key == nil || !activeElements[field.Key]) {
			keyScope := typemap.ValidationScope{}
			if scope.Keys != nil {
				keyScope = *scope.Keys
			}
			if field.Key != nil && !activeElements[field.Key] {
				activeElements[field.Key] = true
				defer delete(activeElements, field.Key)
			}
			if err := checkField(zodElementField(key, field.Key), keyScope, path+"{key}"); err != nil {
				return err
			}
		}
		if scope.Element != nil || (record || strings.HasSuffix(ts, "[]") || field.Element != nil) && (field.Element == nil || !activeElements[field.Element]) {
			if !record {
				value = unwrapZodParentheses(strings.TrimSuffix(ts, "[]"))
			}
			elementScope := typemap.ValidationScope{}
			if scope.Element != nil {
				elementScope = *scope.Element
			}
			if field.Element != nil && !activeElements[field.Element] {
				activeElements[field.Element] = true
				defer delete(activeElements, field.Element)
			}
			return checkField(zodElementField(value, field.Element), elementScope, path+"[]")
		}
		return nil
	}
	for _, name := range order {
		if err := checkDefinition(defs[name], name); err != nil {
			return err
		}
	}
	return nil
}

func validateZodReferencePaths(rules []typemap.ValidateRule) error {
	for _, rule := range rules {
		if rule.Tag == "" && len(rule.Alternatives) == 0 {
			return fmt.Errorf("empty validation rule")
		}
		if _, crossField := typemap.CrossFieldOp(rule.Tag); crossField && strings.ContainsAny(rule.Param, ".[") {
			return fmt.Errorf("%s=%s: nested field references are not supported", rule.Tag, rule.Param)
		}
		if err := validateZodReferencePaths(rule.Alternatives); err != nil {
			return err
		}
	}
	return nil
}

func validateZodConfiguredRules(f typemap.Field, strict bool) error {
	var check func([]typemap.ValidateRule) error
	check = func(rules []typemap.ValidateRule) error {
		for _, rule := range rules {
			if len(rule.Alternatives) > 0 {
				if err := check(rule.Alternatives); err != nil {
					return err
				}
				continue
			}
			if rule.Custom != nil && rule.Custom.ServerOnly {
				continue
			}
			if strict && len(typemap.UnsupportedZodRules([]typemap.ValidateRule{rule})) > 0 {
				return fmt.Errorf("rule %q has no client counterpart; configure a predicate or mark it serverOnly", rule.Tag)
			}
			if (strict || rule.Custom != nil) && len(typemap.InvalidZodRules([]typemap.ValidateRule{rule}, f.GoKind)) > 0 {
				return fmt.Errorf("rule %q with parameter %q cannot validate Go kind %s", rule.Tag, rule.Param, f.GoKind)
			}
		}
		return nil
	}
	return check(f.Validate)
}

// Resolve explicit struct registrations before writing any output. Never let a
// misspelled or ambiguous type silently lose an application validation rule.
func resolveZodStructRules(config zodconfig.Config, defs map[string]typemap.TypeDef, reachable map[string]bool) (map[string][]zodconfig.StructRule, error) {
	result := make(map[string][]zodconfig.StructRule)
	for _, key := range slices.Sorted(maps.Keys(config.StructRules)) {
		var matches []string
		for _, name := range slices.Sorted(maps.Keys(defs)) {
			def := defs[name]
			if key == def.Name || key == def.ID || def.PkgPath != "" && key == def.PkgPath+"."+def.Name {
				matches = append(matches, name)
			}
		}
		if len(matches) != 1 {
			return nil, fmt.Errorf("struct validation %q must identify one generated Go type (matched %d)", key, len(matches))
		}
		name := matches[0]
		if defs[name].Kind != typemap.TypeDefInterface {
			return nil, fmt.Errorf("struct validation %q refers to a non-struct type", key)
		}
		if !reachable[name] {
			return nil, fmt.Errorf("struct validation %q refers to %s, which is not a procedure input type or reachable from one", key, name)
		}
		result[name] = append(result[name], config.StructRules[key]...)
	}
	for alias := range config.Imports {
		if strings.HasSuffix(alias, "Schema") && reachable[strings.TrimSuffix(alias, "Schema")] {
			return nil, fmt.Errorf("validation import %q conflicts with a generated schema", alias)
		}
	}
	return result, nil
}

// Explicit predicates must resolve their free identifiers in module scope.
// Rendering one inside a generated callback can shadow an imported namespace
// with locals such as value, data, check or result. Factories preserve the
// original expression and its parse-time evaluation (including live imports),
// while the existing caller still catches evaluation/invocation failures.
func hoistZodCustomPredicates(order []string, defs map[string]typemap.TypeDef, structRules map[string][]zodconfig.StructRule) (map[string]typemap.TypeDef, map[string][]zodconfig.StructRule, string) {
	var declarations strings.Builder
	names := make(map[string]string)
	bind := func(expression string) string {
		if name, ok := names[expression]; ok {
			return name + "()"
		}
		name := "$goCustomPredicate" + strconv.Itoa(len(names))
		names[expression] = name
		declarations.WriteString("const " + name + ": () => (...args: any[]) => unknown = () => (" + expression + ");\n")
		return name + "()"
	}
	var rules func([]typemap.ValidateRule)
	rules = func(program []typemap.ValidateRule) {
		for i := range program {
			if custom := program[i].Custom; custom != nil && !custom.ServerOnly {
				custom.Predicate = bind(custom.Predicate)
			}
			rules(program[i].Alternatives)
		}
	}
	var definition func(*typemap.TypeDef)
	var field func(*typemap.Field)
	var element func(*typemap.ElementType)
	var refinement func(*typemap.Refinement)
	refinement = func(ref *typemap.Refinement) {
		if ref.ScalarRule != nil {
			rules([]typemap.ValidateRule{*ref.ScalarRule})
		}
		for i := range ref.Alternatives {
			refinement(&ref.Alternatives[i])
		}
	}
	element = func(value *typemap.ElementType) {
		if value == nil {
			return
		}
		if value.Inline != nil {
			definition(value.Inline)
		}
		element(value.Element)
		element(value.Key)
	}
	field = func(value *typemap.Field) {
		if value.ZodOmit {
			return
		}
		rules(value.Validate)
		rules(value.ElementValidate)
		if value.Inline != nil {
			definition(value.Inline)
		}
		element(value.Element)
		element(value.Key)
	}
	definition = func(value *typemap.TypeDef) {
		omitted := make(map[string]bool)
		for i := range value.Fields {
			if value.Fields[i].ZodOmit {
				omitted[value.Fields[i].Name] = true
			}
			field(&value.Fields[i])
		}
		if value.Underlying != nil {
			field(value.Underlying)
		}
		for i := range value.Refinements {
			if !zodRefinementOmitted(value.Refinements[i], omitted) {
				refinement(&value.Refinements[i])
			}
		}
	}
	cloned := make(map[string]typemap.TypeDef, len(defs))
	for name, def := range defs {
		cloned[name] = def
	}
	clonedStructRules := make(map[string][]zodconfig.StructRule, len(structRules))
	for _, name := range order {
		def, exists := defs[name]
		if !exists {
			continue
		}
		def = typemap.ResolveTypeDef(def, nil)
		definition(&def)
		cloned[name] = def
		if values := structRules[name]; len(values) > 0 {
			values = slices.Clone(values)
			for i := range values {
				values[i].Path = slices.Clone(values[i].Path)
				values[i].Predicate = bind(values[i].Predicate)
			}
			clonedStructRules[name] = values
		}
	}
	if declarations.Len() > 0 {
		declarations.WriteByte('\n')
	}
	return cloned, clonedStructRules, declarations.String()
}
