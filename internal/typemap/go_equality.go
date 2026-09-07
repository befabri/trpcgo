package typemap

import (
	"go/types"
	"reflect"
	"strings"
)

// GoEqualityType describes Go == after validator's single pointer dereference.
// It is independent of TS naming: distinct Go types can share a TS expression.
// Error applies to whole-value equality; Fields remain available for unique=Field
// even when another, unselected field makes the struct noncomparable.
type GoEqualityType struct {
	Kind          string
	Length        int64 // Fixed arrays need their Go zero shape for omitted fields.
	Pointer       bool
	EmptySentinel bool // unnamed struct{} equals validator's sentinel for nil.
	Error         string
	Element       *GoEqualityType
	Fields        []GoEqualityField
}

type GoEqualityField struct {
	GoName     string
	JSONName   string
	JSONString bool
	Selectable bool
	Type       *GoEqualityType
}

type equalityAdapter[T comparable] struct {
	fields        FieldAdapter[T]
	kind          func(T) string
	deref         func(T) (T, bool)
	element       func(T) T
	length        func(T) int64
	comparable    func(T) bool
	emptySentinel func(T) bool
}

func describeGoEquality[T comparable](root T, a equalityAdapter[T]) *GoEqualityType {
	active := make(map[T]bool)
	var describe func(T) *GoEqualityType
	describe = func(t T) *GoEqualityType {
		d := &GoEqualityType{}
		if element, pointer := a.deref(t); pointer {
			d.Pointer, t = true, element
			if _, pointer := a.deref(t); pointer {
				d.Error = "Go pointer identity after validator's single dereference cannot be represented from JSON"
				return d
			}
		}
		d.Kind = a.kind(t)
		if active[t] {
			d.Error = "recursive Go pointer identity cannot be represented from JSON"
			return d
		}
		active[t] = true
		defer delete(active, t)
		if !a.comparable(t) {
			d.Error = "noncomparable Go values panic in validator unique"
		}
		switch d.Kind {
		case "string", "bool", "int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64", "float32", "float64":
		case "array":
			d.Length = a.length(t)
			d.Element = describe(a.element(t))
			if d.Element.Pointer {
				d.Error = "nested Go pointer identity cannot be represented from JSON"
			} else if d.Element.Error != "" {
				d.Error = d.Element.Error
			}
		case "struct":
			d.EmptySentinel = a.emptySentinel(t)
			adapter := a.fields
			selection := make(map[string]bool)
			adapter.Map = func(owner T, index int, name string, _ bool, _ TSTypeTag, _ bool) Field {
				field := a.fields.Fields(owner)[index]
				path, selectable := a.fields.Lookup(t, field.Name)
				selectedOwner := t
				for _, parent := range path[:max(0, len(path)-1)] {
					selectedOwner, _, _ = a.fields.Struct(a.fields.Fields(selectedOwner)[parent].Type)
				}
				selection[field.Name+"\x00"+name] = selectable && len(path) > 0 && selectedOwner == owner && path[len(path)-1] == index
				child := describe(field.Type)
				if ParseZodOmitTag(field.Tag) {
					child.Error = "zod_omit hides a Go equality field"
				}
				return Field{Name: name, GoName: field.Name, Equality: child, JSONString: JSONStringOption(field.Tag, child.Kind)}
			}
			fields, _, _, _ := CollectJSONFields(t, adapter, false)
			for _, field := range fields {
				if len(field.WhenAnyPresent) > 0 {
					field.Equality.Error = "field selection through a nil embedded Go pointer can panic"
				}
				selectable := selection[field.GoName+"\x00"+field.Name]
				d.Fields = append(d.Fields, GoEqualityField{GoName: field.GoName, JSONName: field.Name, JSONString: field.JSONString, Selectable: selectable, Type: field.Equality})
				if field.Equality.Pointer {
					d.Error = "nested Go pointer identity cannot be represented from JSON"
				} else if field.Equality.Error != "" {
					d.Error = field.Equality.Error
				}
			}
			// Removed TS properties can still be populated by the Go JSON decoder.
			for _, field := range a.fields.Fields(t) {
				jsonName, _, _ := strings.Cut(reflect.StructTag(field.Tag).Get("json"), ",")
				if _, pointer := a.deref(field.Type); pointer && field.Exported && jsonName != "-" {
					d.Error = "nested Go pointer identity cannot be represented from JSON"
				}
				if field.Exported && jsonName != "-" && (ParseZodOmitTag(field.Tag) || reflect.StructTag(field.Tag).Get("tstype") == "-") {
					d.Error = "schema omission hides a Go equality field"
				}
			}
		default:
			if d.Error == "" {
				d.Error = "Go equality for " + d.Kind + " cannot be represented from JSON"
			}
		}
		return d
	}
	return describe(root)
}

func cloneGoEquality(t *GoEqualityType) *GoEqualityType {
	if t == nil {
		return nil
	}
	clone := *t
	clone.Element = cloneGoEquality(t.Element)
	clone.Fields = make([]GoEqualityField, len(t.Fields))
	for index, field := range t.Fields {
		clone.Fields[index] = field
		clone.Fields[index].Type = cloneGoEquality(field.Type)
	}
	return &clone
}

func DescribeTypesEquality(t types.Type) *GoEqualityType {
	deref := func(t types.Type) (types.Type, bool) {
		p, ok := types.Unalias(t).Underlying().(*types.Pointer)
		if ok {
			return p.Elem(), true
		}
		return t, false
	}
	a := equalityAdapter[types.Type]{kind: goKind, deref: deref, comparable: types.Comparable,
		element:       func(t types.Type) types.Type { return t.Underlying().(*types.Array).Elem() },
		length:        func(t types.Type) int64 { return t.Underlying().(*types.Array).Len() },
		emptySentinel: func(t types.Type) bool { s, ok := types.Unalias(t).(*types.Struct); return ok && s.NumFields() == 0 },
	}
	a.fields = FieldAdapter[types.Type]{
		Fields: func(t types.Type) []EmbeddedField[types.Type] {
			s := t.Underlying().(*types.Struct)
			out := make([]EmbeddedField[types.Type], s.NumFields())
			for i := range out {
				f := s.Field(i)
				out[i] = EmbeddedField[types.Type]{Type: f.Type(), Name: f.Name(), Tag: s.Tag(i), Exported: f.Exported(), Embedded: f.Embedded()}
			}
			return out
		},
		Struct: func(t types.Type) (types.Type, bool, bool) {
			t, p := deref(t)
			_, ok := t.Underlying().(*types.Struct)
			return t, ok, p
		},
		TypeName: func(types.Type) string { return "" },
		Lookup: func(t types.Type, name string) ([]int, bool) {
			o, path, _ := types.LookupFieldOrMethod(t, false, nil, name)
			f, ok := o.(*types.Var)
			return path, ok && f.IsField() && f.Exported()
		},
	}
	return describeGoEquality(t, a)
}

func DescribeReflectEquality(t reflect.Type) *GoEqualityType {
	deref := func(t reflect.Type) (reflect.Type, bool) {
		if t.Kind() == reflect.Pointer {
			return t.Elem(), true
		}
		return t, false
	}
	a := equalityAdapter[reflect.Type]{
		kind: func(t reflect.Type) string {
			for t.Kind() == reflect.Pointer {
				t = t.Elem()
			}
			if t.PkgPath() == "time" && t.Name() == "Time" {
				return "time.Time"
			}
			if t.PkgPath() == "encoding/json" && t.Name() == "Number" {
				return "json.Number"
			}
			return t.Kind().String()
		},
		deref: deref, comparable: func(t reflect.Type) bool { return t.Comparable() }, element: func(t reflect.Type) reflect.Type { return t.Elem() }, length: func(t reflect.Type) int64 { return int64(t.Len()) }, emptySentinel: func(t reflect.Type) bool { return t == reflect.TypeFor[struct{}]() },
	}
	a.fields = FieldAdapter[reflect.Type]{
		Fields: func(t reflect.Type) []EmbeddedField[reflect.Type] {
			out := make([]EmbeddedField[reflect.Type], t.NumField())
			for i := range out {
				f := t.Field(i)
				out[i] = EmbeddedField[reflect.Type]{Type: f.Type, Name: f.Name, Tag: string(f.Tag), Exported: f.IsExported(), Embedded: f.Anonymous}
			}
			return out
		},
		Struct: func(t reflect.Type) (reflect.Type, bool, bool) {
			t, p := deref(t)
			return t, t.Kind() == reflect.Struct, p
		},
		TypeName: func(reflect.Type) string { return "" },
		Lookup: func(t reflect.Type, name string) ([]int, bool) {
			f, ok := t.FieldByName(name)
			return f.Index, ok && f.IsExported()
		},
	}
	return describeGoEquality(t, a)
}
