package trpcgo

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/befabri/trpcgo/internal/typemap"
)

func reflectTypeName(t reflect.Type) string {
	name := t.Name()
	if i := strings.IndexByte(name, '['); i >= 0 {
		return typemap.SpecializationName(name[:i], t.PkgPath()+"."+name)
	}
	return name
}

// reflectContainerToTS expands exactly one named container layer. Definitions
// are registered before expansion, so recursive references remain named edges.
func reflectContainerToTS(t reflect.Type, defs map[string]*reflectDef, validation ...*typemap.ValidationProgram) string {
	switch t.Kind() {
	case reflect.Slice:
		return reflectSliceToTS(t, defs, validation...)
	case reflect.Array:
		return goTypeToTS(t.Elem(), defs, validation...) + "[]"
	case reflect.Map:
		return fmt.Sprintf("Record<%s, %s>", goTypeToTS(t.Key(), defs, validation...), goTypeToTS(t.Elem(), defs, validation...))
	default:
		panic("reflectContainerToTS called for a non-container type")
	}
}

func reflectTypeField(t reflect.Type, defs map[string]*reflectDef, ts string, validation ...*typemap.ValidationProgram) *typemap.Field {
	d := reflectDescribeType(t, defs, make(map[reflect.Type]bool), validation...)
	return &typemap.Field{Type: ts, Equality: d.Equality, ArrayLen: d.ArrayLen, GoKind: d.GoKind, GoType: d.GoType, IsPointer: d.IsPointer, Inline: d.Inline, Element: d.Element, Key: d.Key}
}

func reflectDescribeType(t reflect.Type, defs map[string]*reflectDef, visiting map[reflect.Type]bool, validation ...*typemap.ValidationProgram) *typemap.ElementType {
	d := &typemap.ElementType{Type: goTypeToTS(t, defs, validation...), Equality: typemap.DescribeReflectEquality(t), GoKind: reflectGoKind(t), IsPointer: t.Kind() == reflect.Pointer}
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	d.GoType = t.String()
	if visiting[t] {
		return d
	}
	visiting[t] = true
	defer delete(visiting, t)
	switch t.Kind() {
	case reflect.Slice, reflect.Array:
		if t.Kind() == reflect.Array {
			length := int64(t.Len())
			d.ArrayLen = &length
		}
		if d.GoKind != "[]byte" {
			d.Element = reflectDescribeType(t.Elem(), defs, visiting, validation...)
		}
	case reflect.Map:
		d.Key = reflectDescribeType(t.Key(), defs, visiting, validation...)
		d.Element = reflectDescribeType(t.Elem(), defs, visiting, validation...)
	case reflect.Struct:
		if t.Name() == "" {
			fields, _, _, refs := typemap.CollectJSONFields(t, reflectFieldAdapter(defs, validation...), false)
			d.Inline = &typemap.TypeDef{Kind: typemap.TypeDefInterface, Fields: fields, Refinements: refs}
		}
	}
	return d
}
