package validationconfig

import (
	"context"

	"github.com/befabri/trpcgo"
)

func Register(r *trpcgo.Router) {
	trpcgo.MustQuery(r, "config.input", func(context.Context, Input) (bool, error) { return true, nil })
}
