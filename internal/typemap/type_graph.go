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
// to booleans, strings, integer kinds, and floating point kinds.
func JSONStringOption(tag, kind string) bool {
	switch kind {
	case "string", "bool", "int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64", "float32", "float64":
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
	d := m.describeType(t, make(map[types.Type]bool))
	return &Field{Type: d.Type, Equality: d.Equality, ArrayLen: d.ArrayLen, GoKind: d.GoKind, GoType: d.GoType, EnumValues: d.EnumValues, IsPointer: d.IsPointer, Inline: d.Inline, Element: d.Element, Key: d.Key}
}

func (m *Mapper) describeType(t types.Type, visiting map[types.Type]bool) *ElementType {
	d := &ElementType{Type: m.ConvertZod(t), Equality: DescribeTypesEquality(t), GoKind: goKind(t), IsPointer: isPointer(types.Unalias(t))}
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
	}
	if visiting[t] {
		return d
	}
	visiting[t] = true
	defer delete(visiting, t)
	switch underlying := t.Underlying().(type) {
	case *types.Slice:
		if d.GoKind != "[]byte" {
			d.Element = m.describeType(underlying.Elem(), visiting)
		}
	case *types.Array:
		length := underlying.Len()
		d.ArrayLen = &length
		d.Element = m.describeType(underlying.Elem(), visiting)
	case *types.Map:
		d.Key = m.describeType(underlying.Key(), visiting)
		d.Element = m.describeType(underlying.Elem(), visiting)
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
	m.typeToken(id, name)
	d := TypeDef{ID: id, Name: name, PkgPath: pkgPath(t.Obj()), PkgName: pkgName(t.Obj()), Kind: TypeDefInterface, InstanceOf: instanceOf}
	meta := m.metas[originID]
	d.Comment = meta.Comment
	if st, ok := t.Underlying().(*types.Struct); ok {
		d.Refinements = m.collectSchemaFields(st, &d.Fields, &d.Extends, &d.ZodExtends, &d.ExtendsAt, meta.FieldComments)
	} else {
		d.Kind = TypeDefAlias
		d.AliasOf = m.convert(t.Underlying())
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
