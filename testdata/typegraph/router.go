package typegraph

import (
	"context"
	"reflect"

	"github.com/befabri/trpcgo"
)

var registrations = map[string]func(*trpcgo.Router){}

func define[T any]() {
	name := reflect.TypeFor[T]().Name()
	registrations[name] = func(r *trpcgo.Router) {
		trpcgo.MustQuery(r, name, func(context.Context, T) (string, error) { return "", nil })
	}
}

func init() {
	define[RecursiveInput]()
	define[RecursiveMap]()
	define[RecursiveSlice]()
	define[GenericInput]()
	define[GenericInlineRecursiveInput]()
	define[GenericExtendedInput]()
	define[RequiredOmitInput]()
	define[InlineInput]()
	define[IntegerMapInput]()
}

// Register exposes the named fixtures through the public router API so
// reflection sees the procedure inputs the static mapper reads from types.go.
// An unknown name panics rather than letting a test silently skip a fixture.
func Register(r *trpcgo.Router, names ...string) {
	for _, name := range names {
		register, ok := registrations[name]
		if !ok {
			panic("unregistered type graph fixture: " + name)
		}
		register(r)
	}
}
