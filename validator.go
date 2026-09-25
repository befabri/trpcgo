package trpcgo

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"
)

// StructValidator adapts a validator that accepts only structs, such as
// validator.Struct from go-playground/validator, to every typed input root.
//
// The returned function validates each struct value reachable from the input
// through pointers, interfaces, slices, arrays, and map values, so a []Item
// root validates every element and a map[string]*Item root validates every
// non-nil value. Structs are passed as they are found: a *Item root reaches
// validate as the pointer, even when it is nil, so validate.Struct rejects a
// JSON null or absent input as it does when called directly. A nil pointer
// inside a collection is skipped, as validator skips a nil pointer field that
// has no required rule. Primitive roots and time.Time values, which validator
// refuses as roots and never enters as fields, are never passed to validate.
// Every other struct is, including one that decodes itself through
// json.Unmarshaler or encoding.TextUnmarshaler. Errors from several values are
// joined, each prefixed with its path such as [2] or ["key"], and remain
// visible to errors.As.
func StructValidator(validate func(any) error) func(any) error {
	return func(input any) error {
		w := structWalker{validate: validate, visiting: make(map[valueIdentity]bool)}
		w.walk(reflect.ValueOf(input), "")
		return errors.Join(w.errs...)
	}
}

type structWalker struct {
	validate func(any) error
	visiting map[valueIdentity]bool
	errs     []error
}

// valueIdentity identifies a pointer, map, or slice on the current walk path.
// Length distinguishes a slice from its prefix sub-slices, which share storage.
type valueIdentity struct {
	pointer uintptr
	typ     reflect.Type
	length  int
}

var timeType = reflect.TypeFor[time.Time]()

// timeLike reports whether validator treats a struct type as a time.Time
// value: it rejects such a root and compares, rather than enters, such a field.
func timeLike(t reflect.Type) bool {
	return t.ConvertibleTo(timeType)
}

// walk validates the structs reachable from v. The root has an empty path;
// every nested value carries the index or key that led to it.
func (w *structWalker) walk(v reflect.Value, path string) {
	if !v.IsValid() || !v.CanInterface() {
		return
	}
	switch v.Kind() {
	case reflect.Struct:
		if !timeLike(v.Type()) {
			w.call(v, path)
		}
	case reflect.Pointer:
		if elem := v.Type().Elem(); elem.Kind() == reflect.Struct {
			// A nil root reaches validate, which rejects it as validate.Struct
			// does. A nil element is absent, as a nil pointer field is.
			if !timeLike(elem) && (!v.IsNil() || path == "") {
				w.call(v, path)
			}
			return
		}
		if !v.IsNil() {
			w.descend(v, func() { w.walk(v.Elem(), path) })
		}
	case reflect.Interface:
		if !v.IsNil() {
			w.walk(v.Elem(), path)
		}
	case reflect.Slice:
		w.descend(v, func() { w.walkElements(v, path) })
	case reflect.Array:
		w.walkElements(v, path)
	case reflect.Map:
		w.descend(v, func() {
			keys := v.MapKeys()
			slices.SortFunc(keys, func(a, b reflect.Value) int {
				return strings.Compare(fmt.Sprint(a.Interface()), fmt.Sprint(b.Interface()))
			})
			for _, key := range keys {
				w.walk(v.MapIndex(key), path+"["+strconv.Quote(fmt.Sprint(key.Interface()))+"]")
			}
		})
	}
}

func (w *structWalker) walkElements(v reflect.Value, path string) {
	for i := range v.Len() {
		w.walk(v.Index(i), path+"["+strconv.Itoa(i)+"]")
	}
}

// descend runs visit unless v is already on the current path. JSON-decoded
// values are acyclic; the guard keeps hand-built inputs from recursing forever.
func (w *structWalker) descend(v reflect.Value, visit func()) {
	id := valueIdentity{pointer: v.Pointer(), typ: v.Type()}
	if v.Kind() == reflect.Slice {
		id.length = v.Len()
	}
	if w.visiting[id] {
		return
	}
	w.visiting[id] = true
	defer delete(w.visiting, id)
	visit()
}

func (w *structWalker) call(v reflect.Value, path string) {
	err := w.validate(v.Interface())
	if err == nil {
		return
	}
	if path != "" {
		err = fmt.Errorf("%s: %w", path, err)
	}
	w.errs = append(w.errs, err)
}
