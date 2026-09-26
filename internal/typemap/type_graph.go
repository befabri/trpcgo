package typemap

import (
	"crypto/sha256"
	"fmt"
	"go/types"
	"reflect"
	"slices"
	"strings"
)

// ResolveTypeDef returns a deep copy with all type references resolved. Keeping
// metadata independent matters when one mapper feeds both TS and Zod writers.
func ResolveTypeDef(d TypeDef, display map[string]string) TypeDef {
	if name, ok := display[d.ID]; ok {
		d.Name = name
	}
	d.TypeParams = slices.Clone(d.TypeParams)
	d.UnionMembers = slices.Clone(d.UnionMembers)
	d.Refinements = cloneRefinements(d.Refinements)
	d.Fields = slices.Clone(d.Fields)
	for i := range d.Fields {
		d.Fields[i] = ResolveField(d.Fields[i], display)
	}
	d.ZodExtends = slices.Clone(d.ZodExtends)
	for i := range d.ZodExtends {
		d.ZodExtends[i] = ResolveTokens(d.ZodExtends[i], display)
	}
	d.Extends = slices.Clone(d.Extends)
	for i := range d.Extends {
		d.Extends[i] = ResolveTokens(d.Extends[i], display)
	}
	d.AliasOf = ResolveTokens(d.AliasOf, display)
	d.InstanceOf = ResolveTokens(d.InstanceOf, display)
	if d.Underlying != nil {
		f := ResolveField(*d.Underlying, display)
		d.Underlying = &f
	}
	d.Specializations = slices.Clone(d.Specializations)
	for i := range d.Specializations {
		d.Specializations[i] = ResolveTypeDef(d.Specializations[i], display)
	}
	return d
}

func ResolveField(f Field, display map[string]string) Field {
	f.Equality = cloneGoEquality(f.Equality)
	if f.ArrayLen != nil {
		length := *f.ArrayLen
		f.ArrayLen = &length
	}
	f.WhenAnyPresent = slices.Clone(f.WhenAnyPresent)
	f.Validate = cloneValidationRules(f.Validate)
	f.ElementValidate = cloneValidationRules(f.ElementValidate)
	f.UnsupportedZod = cloneValidationRules(f.UnsupportedZod)
	f.InvalidZod = cloneValidationRules(f.InvalidZod)
	f.EnumValues = slices.Clone(f.EnumValues)
	f.Type = ResolveTokens(f.Type, display)
	f.ZodType = ResolveTokens(f.ZodType, display)
	if f.Inline != nil {
		d := ResolveTypeDef(*f.Inline, display)
		f.Inline = &d
	}
	f.Element = resolveElement(f.Element, display)
	f.Key = resolveElement(f.Key, display)
	return f
}

func resolveElement(e *ElementType, display map[string]string) *ElementType {
	if e == nil {
		return nil
	}
	resolved := *e
	resolved.Equality = cloneGoEquality(e.Equality)
	if e.ArrayLen != nil {
		length := *e.ArrayLen
		resolved.ArrayLen = &length
	}
	resolved.EnumValues = slices.Clone(e.EnumValues)
	resolved.Type = ResolveTokens(e.Type, display)
	if e.Inline != nil {
		d := ResolveTypeDef(*e.Inline, display)
		resolved.Inline = &d
	}
	resolved.Element = resolveElement(e.Element, display)
	resolved.Key = resolveElement(e.Key, display)
	return &resolved
}

// JSONStringOption follows encoding/json's restriction of the string option
// to booleans, strings, integer kinds, and floating point kinds. json.Number
// is a string kind, so it takes the option too.
func JSONStringOption(tag, kind string) bool {
	switch kind {
	case "string", "bool", "int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64", "float32", "float64", "json.Number":
	default:
		return false
	}
	value := reflect.StructTag(tag).Get("json")
	_, options, _ := strings.Cut(value, ",")
	for option := range strings.SplitSeq(options, ",") {
		if option == "string" {
			return true
		}
	}
	return false
}

func (m *Mapper) typeField(t types.Type) *Field {
	d := m.describeType(t)
	return &Field{Type: d.Type, Equality: d.Equality, ArrayLen: d.ArrayLen, GoKind: d.GoKind, ValidatorKind: d.ValidatorKind, GoType: d.GoType, EnumValues: d.EnumValues, IsPointer: d.IsPointer, Inline: d.Inline, Element: d.Element, Key: d.Key}
}

// describeType records the element chain and anonymous objects of t. A named
// type already being described within the current definition, reached again
// through an anonymous struct's field, keeps only its name, like a reference
// to a named struct. Every Go type cycle passes through a named type, and
// stopping there keeps the anonymous levels above it fully described.
func (m *Mapper) describeType(t types.Type) *ElementType {
	d := &ElementType{Type: m.ConvertZod(t), Equality: DescribeTypesEquality(t), GoKind: goKind(t), ValidatorKind: typesValidatorKind(t), IsPointer: isPointer(types.Unalias(t))}
	t = types.Unalias(t)
	for {
		ptr, ok := t.(*types.Pointer)
		if !ok {
			break
		}
		t = types.Unalias(ptr.Elem())
	}
	d.GoType = types.TypeString(t, func(p *types.Package) string { return p.Name() })
	if named, ok := t.(*types.Named); ok {
		d.EnumValues = slices.Clone(m.metas[TypeID(named.Obj())].ConstValues)
		if m.describing.At(t) != nil {
			return d
		}
		m.describing.Set(t, true)
		defer m.describing.Delete(t)
	}
	switch underlying := t.Underlying().(type) {
	case *types.Slice:
		if d.GoKind != "[]byte" {
			d.Element = m.describeType(underlying.Elem())
		}
	case *types.Array:
		length := underlying.Len()
		d.ArrayLen = &length
		d.Element = m.describeType(underlying.Elem())
	case *types.Map:
		d.Key = m.describeType(underlying.Key())
		d.Element = m.describeType(underlying.Elem())
	case *types.Struct:
		if _, named := t.(*types.Named); !named {
			inline := &TypeDef{Kind: TypeDefInterface}
			inline.Refinements = m.collectFields(underlying, &inline.Fields, nil, nil)
			d.Inline = inline
		}
	}
	return d
}

// containsTypeParameter distinguishes concrete instantiations from generic
// references encountered while traversing a declaration's recursive body.
func containsTypeParameter(t types.Type, seen map[types.Type]bool) bool {
	t = types.Unalias(t)
	if seen[t] {
		return false
	}
	seen[t] = true
	switch t := t.(type) {
	case *types.TypeParam:
		return true
	case *types.Named:
		for arg := range t.TypeArgs().Types() {
			if containsTypeParameter(arg, seen) {
				return true
			}
		}
	case *types.Pointer:
		return containsTypeParameter(t.Elem(), seen)
	case *types.Slice:
		return containsTypeParameter(t.Elem(), seen)
	case *types.Array:
		return containsTypeParameter(t.Elem(), seen)
	case *types.Map:
		return containsTypeParameter(t.Key(), seen) || containsTypeParameter(t.Elem(), seen)
	case *types.Struct:
		for i := 0; i < t.NumFields(); i++ {
			if containsTypeParameter(t.Field(i).Type(), seen) {
				return true
			}
		}
	}
	return false
}

func (m *Mapper) resolveSpecialization(t *types.Named, originID, name, instanceOf string) {
	id := types.TypeString(t, func(p *types.Package) string { return p.Path() })
	if _, exists := m.specializations[id]; exists {
		return
	}
	name = SpecializationName(name, id)
	m.specializations[id] = TypeDef{} // Register before walking a recursive body.
	defer m.definitionScope()()
	m.typeToken(id, name)
	d := TypeDef{ID: id, Name: name, PkgPath: pkgPath(t.Obj()), PkgName: pkgName(t.Obj()), Kind: TypeDefInterface, InstanceOf: instanceOf}
	meta := m.metas[originID]
	d.Comment = meta.Comment
	if st, ok := t.Underlying().(*types.Struct); ok {
		d.Refinements = m.collectSchemaFields(st, &d.Fields, &d.Extends, &d.ZodExtends, &d.ExtendsAt, meta.FieldComments)
	} else {
		d.Kind = TypeDefAlias
		d.AliasOf = m.convert(t.Underlying())
		m.describing.Set(t, true) // As in registerAlias.
		d.Underlying = m.typeField(t.Underlying())
	}
	m.specializations[id] = d
}

// SpecializationName is stable across traversal order and identical in the
// static and reflection mappers when their fully-qualified identities agree.
func SpecializationName(name, id string) string {
	digest := sha256.Sum256([]byte(id))
	return fmt.Sprintf("%s_%x", name, digest[:8])
}

func (m *Mapper) convertNamedInstance(t *types.Named) string {
	origin := t.Origin()
	originID, name := TypeID(origin.Obj()), origin.Obj().Name()
	m.convertNamed(origin)
	if !m.seen[originID] {
		return m.convert(t.Underlying())
	}
	var args []string
	for arg := range t.TypeArgs().Types() {
		args = append(args, m.convert(arg))
	}
	instanceOf := fmt.Sprintf("%s<%s>", m.typeToken(originID, name), strings.Join(args, ", "))
	if !containsTypeParameter(t, make(map[types.Type]bool)) {
		m.resolveSpecialization(t, originID, name, instanceOf)
	}
	return instanceOf
}

// ConvertZod preserves the identity of concrete Go generic arguments whose
// public TypeScript representations may coincide (for example int8 and int64).
func (m *Mapper) ConvertZod(t types.Type) string {
	public := m.convert(t)
	if alias, ok := t.(*types.Alias); ok && alias.TypeArgs().Len() > 0 {
		return m.ConvertZod(alias.Rhs())
	}
	t = types.Unalias(t)
	switch t := t.(type) {
	case *types.Named:
		if t.TypeArgs().Len() > 0 && !containsTypeParameter(t, make(map[types.Type]bool)) {
			id := types.TypeString(t, func(p *types.Package) string { return p.Path() })
			if _, exists := m.specializations[id]; exists {
				return m.typeToken(id, SpecializationName(t.Obj().Name(), id))
			}
		}
	case *types.Pointer:
		return m.ConvertZod(t.Elem())
	case *types.Slice:
		if goKind(t) == "[]byte" {
			return "string"
		}
		elem := m.ConvertZod(t.Elem())
		if strings.Contains(elem, "|") {
			elem = "(" + elem + ")"
		}
		return elem + "[]"
	case *types.Array:
		return m.ConvertZod(t.Elem()) + "[]"
	case *types.Map:
		result := "Record<" + m.ConvertZod(t.Key()) + ", " + m.ConvertZod(t.Elem()) + ">"
		if strings.HasPrefix(public, "Partial<") {
			result = "Partial<" + result + ">"
		}
		return result
	}
	return public
}

func cloneValidationRules(rules []ValidateRule) []ValidateRule {
	rules = slices.Clone(rules)
	for i := range rules {
		rules[i].Alternatives = cloneValidationRules(rules[i].Alternatives)
		if rules[i].Custom != nil {
			custom := *rules[i].Custom
			custom.GoKinds = slices.Clone(custom.GoKinds)
			rules[i].Custom = &custom
		}
	}
	return rules
}

func cloneRefinements(refs []Refinement) []Refinement {
	refs = slices.Clone(refs)
	for i := range refs {
		refs[i].Alternatives = cloneRefinements(refs[i].Alternatives)
		refs[i].WhenAnyPresent = slices.Clone(refs[i].WhenAnyPresent)
		refs[i].OtherWhenAnyPresent = slices.Clone(refs[i].OtherWhenAnyPresent)
		if refs[i].ScalarRule != nil {
			rule := cloneValidationRules([]ValidateRule{*refs[i].ScalarRule})[0]
			refs[i].ScalarRule = &rule
		}
	}
	return refs
}

// typeParamBounds returns the TypeScript constraint each type parameter of a
// generic declaration needs, in declaration order, with "" for a parameter
// TypeScript may leave unconstrained. body is the declaration's underlying
// type; its uses of a parameter decide what an unrestricted one needs.
//
// Go constrains a parameter by a type set. The union of the set's basic kinds
// keeps every TypeScript use of the parameter well-formed, in particular a
// mapped-type key, which TypeScript restricts to string and number. A set
// that names no types, such as any or comparable, or one with a term no basic
// TypeScript type covers, constrains the parameter only where the declaration
// uses it as a map key: JSON keys are strings or integers, so the parameter
// admits what a non-generic map key does.
func typeParamBounds(params *types.TypeParamList, body types.Type) []string {
	if params.Len() == 0 {
		return nil
	}
	bounds := make([]string, params.Len())
	for i := range params.Len() {
		param := params.At(i)
		if set := constraintTypeSet(param.Constraint()); set.exact() {
			bounds[i] = set.typeScript()
		} else if usesAsMapKey(body, param, make(map[keyUse]bool)) {
			bounds[i] = "string | number"
		}
	}
	return bounds
}

// typeSet is the part of a constraint's type set a TypeScript constraint can
// express.
type typeSet struct {
	kinds      map[string]bool // TypeScript types of the terms seen so far
	restricted bool            // the constraint names types; false for any and comparable
	covered    bool            // every term maps to a basic TypeScript type
}

func (s typeSet) exact() bool { return s.restricted && s.covered && len(s.kinds) > 0 }

func (s typeSet) typeScript() string {
	var parts []string
	for _, kind := range []string{"string", "number", "boolean"} {
		if s.kinds[kind] {
			parts = append(parts, kind)
		}
	}
	return strings.Join(parts, " | ")
}

// intersect narrows s by another embedded element of the same constraint.
func (s typeSet) intersect(o typeSet) typeSet {
	covered := s.covered && o.covered
	switch {
	case !o.restricted:
		s.covered = covered
		return s
	case !s.restricted:
		o.covered = covered
		return o
	}
	kinds := make(map[string]bool)
	for kind := range s.kinds {
		if o.kinds[kind] {
			kinds[kind] = true
		}
	}
	return typeSet{kinds: kinds, restricted: true, covered: covered}
}

// constraintTypeSet describes the type set of a constraint, an interface whose
// embedded elements intersect.
func constraintTypeSet(t types.Type) typeSet {
	iface, ok := t.Underlying().(*types.Interface)
	if !ok {
		return typeSet{}
	}
	set := typeSet{covered: true}
	for i := range iface.NumEmbeddeds() {
		set = set.intersect(embeddedTypeSet(iface.EmbeddedType(i)))
	}
	return set
}

// embeddedTypeSet describes one embedded element: an interface whose own
// type set applies, a union of terms, or a single term, which go/types
// embeds without a union.
func embeddedTypeSet(t types.Type) typeSet {
	if _, ok := t.Underlying().(*types.Interface); ok {
		return constraintTypeSet(t)
	}
	set := typeSet{kinds: make(map[string]bool), restricted: true, covered: true}
	union, ok := t.(*types.Union)
	if !ok {
		return set.addTerm(t)
	}
	for i := range union.Len() {
		if set = set.addTerm(union.Term(i).Type()); !set.restricted {
			break
		}
	}
	return set
}

// addTerm widens s by one term. An interface term contributes its own type
// set; an unrestricted one makes the whole union unrestricted.
func (s typeSet) addTerm(term types.Type) typeSet {
	if _, ok := term.Underlying().(*types.Interface); ok {
		inner := constraintTypeSet(term)
		if !inner.restricted {
			return typeSet{}
		}
		for kind := range inner.kinds {
			s.kinds[kind] = true
		}
		s.covered = s.covered && inner.covered
		return s
	}
	if kind := basicTypeScript(term); kind != "" {
		s.kinds[kind] = true
	} else {
		s.covered = false
	}
	return s
}

// basicTypeScript names the TypeScript type of a basic Go kind, or "" for a
// kind with no basic TypeScript counterpart.
func basicTypeScript(t types.Type) string {
	basic, ok := t.Underlying().(*types.Basic)
	if !ok {
		return ""
	}
	switch info := basic.Info(); {
	case info&types.IsString != 0:
		return "string"
	case info&(types.IsInteger|types.IsFloat) != 0:
		return "number"
	case info&types.IsBoolean != 0:
		return "boolean"
	}
	return ""
}

// keyUse records a visited type together with the parameter searched for, so
// a recursive declaration terminates without hiding a second parameter.
type keyUse struct {
	t     types.Type
	param *types.TypeParam
}

// usesAsMapKey reports whether t uses param as a map key, directly or as the
// argument of an instantiation whose own parameter is one.
func usesAsMapKey(t types.Type, param *types.TypeParam, seen map[keyUse]bool) bool {
	t = types.Unalias(t)
	use := keyUse{t, param}
	if seen[use] {
		return false
	}
	seen[use] = true
	switch t := t.(type) {
	case *types.Map:
		return types.Identical(t.Key(), param) || usesAsMapKey(t.Key(), param, seen) || usesAsMapKey(t.Elem(), param, seen)
	case *types.Pointer:
		return usesAsMapKey(t.Elem(), param, seen)
	case *types.Slice:
		return usesAsMapKey(t.Elem(), param, seen)
	case *types.Array:
		return usesAsMapKey(t.Elem(), param, seen)
	case *types.Struct:
		for i := range t.NumFields() {
			if usesAsMapKey(t.Field(i).Type(), param, seen) {
				return true
			}
		}
	case *types.Named:
		args, origin := t.TypeArgs(), t.Origin()
		for i := range args.Len() {
			if !types.Identical(args.At(i), param) {
				if usesAsMapKey(args.At(i), param, seen) {
					return true
				}
				continue
			}
			if usesAsMapKey(origin.Underlying(), origin.TypeParams().At(i), seen) {
				return true
			}
		}
	}
	return false
}
