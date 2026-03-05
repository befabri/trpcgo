package outputparser

import (
	"context"

	"github.com/befabri/trpcgo"
)

type User struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type PublicUser struct {
	ID string `json:"id"`
}

type GetInput struct {
	ID string `json:"id"`
}

var noopMW trpcgo.Middleware = func(next trpcgo.HandlerFunc) trpcgo.HandlerFunc { return next }

func getUser(_ context.Context, _ GetInput) (User, error) { return User{}, nil }
func listUsers(_ context.Context) (User, error)           { return User{}, nil }
func noParser(_ context.Context) (User, error)            { return User{}, nil }

func Setup(router *trpcgo.Router) {
	// Explicit type args: OutputParser[User, PublicUser](fn)
	trpcgo.MustQuery(router, "user.get", getUser,
		trpcgo.OutputParser[User, PublicUser](func(u User) (PublicUser, error) {
			return PublicUser{ID: u.ID}, nil
		}),
	)

	// Inferred type args: OutputParser(func(u User) (PublicUser, error) {...})
	trpcgo.MustVoidQuery(router, "user.list", listUsers,
		trpcgo.OutputParser(func(u User) (PublicUser, error) {
			return PublicUser{ID: u.ID}, nil
		}),
	)

	// No output parser — OutputType stays as User.
	trpcgo.MustVoidQuery(router, "user.noparser", noParser)

	// Two OutputParser calls: last one wins (matches runtime left-to-right apply order).
	trpcgo.MustVoidQuery(router, "user.lastwins", listUsers,
		trpcgo.OutputParser[User, string](func(u User) (string, error) { return u.Name, nil }),
		trpcgo.OutputParser[User, PublicUser](func(u User) (PublicUser, error) {
			return PublicUser{ID: u.ID}, nil
		}),
	)

	// Procedure(OutputParser(...)) — OutputParser nested inside Procedure call.
	trpcgo.MustVoidQuery(router, "user.procedure", listUsers,
		trpcgo.Procedure(
			trpcgo.OutputParser(func(u User) (PublicUser, error) {
				return PublicUser{ID: u.ID}, nil
			}),
		),
	)

	// Builder chain: Procedure(OutputParser(...)).Use(mw)
	trpcgo.MustVoidQuery(router, "user.chain", listUsers,
		trpcgo.Procedure(
			trpcgo.OutputParser(func(u User) (PublicUser, error) {
				return PublicUser{ID: u.ID}, nil
			}),
		).Use(noopMW),
	)

	// Pre-bound variable: parser declared separately, then passed as option.
	parser := trpcgo.OutputParser(func(u User) (PublicUser, error) {
		return PublicUser{ID: u.ID}, nil
	})
	trpcgo.MustVoidQuery(router, "user.prebound", listUsers, parser)

	// With(opts ...ProcedureOption) — typed method, discoverable by static analysis.
	trpcgo.MustVoidQuery(router, "user.with", listUsers,
		trpcgo.Procedure().With(trpcgo.OutputParser(func(u User) (PublicUser, error) {
			return PublicUser{ID: u.ID}, nil
		})),
	)

	// Pre-bound builder with With(OutputParser(...)).
	typedBuilder := trpcgo.Procedure().With(trpcgo.OutputParser(func(u User) (PublicUser, error) {
		return PublicUser{ID: u.ID}, nil
	}))
	trpcgo.MustVoidQuery(router, "user.withprebound", listUsers, typedBuilder)

	// Last parser wins inside With(...).
	trpcgo.MustVoidQuery(router, "user.withlastwins", listUsers,
		trpcgo.Procedure().With(
			trpcgo.OutputParser[User, string](func(u User) (string, error) { return u.Name, nil }),
			trpcgo.OutputParser(func(u User) (PublicUser, error) {
				return PublicUser{ID: u.ID}, nil
			}),
		),
	)

	// Builder method arg form: Procedure().WithOutputParser(...)
	trpcgo.MustVoidQuery(router, "user.withmethod", listUsers,
		trpcgo.Procedure().WithOutputParser(func(v any) (any, error) {
			u, _ := v.(User)
			return PublicUser{ID: u.ID}, nil
		}),
	)

	// Direct untyped option: exact post-parser type is runtime-only.
	trpcgo.MustVoidQuery(router, "user.withoption", listUsers,
		trpcgo.WithOutputParser(func(v any) (any, error) {
			u, _ := v.(User)
			return PublicUser{ID: u.ID}, nil
		}),
	)

	// Mixed precedence: later untyped parser wins over earlier typed parser.
	trpcgo.MustVoidQuery(router, "user.typedthenuntyped", listUsers,
		trpcgo.OutputParser(func(u User) (PublicUser, error) {
			return PublicUser{ID: u.ID}, nil
		}),
		trpcgo.WithOutputParser(func(v any) (any, error) {
			u, _ := v.(User)
			return PublicUser{ID: u.ID}, nil
		}),
	)

	// Mixed precedence: later typed parser wins over earlier untyped parser.
	trpcgo.MustVoidQuery(router, "user.untypedthentyped", listUsers,
		trpcgo.WithOutputParser(func(v any) (any, error) {
			return v, nil
		}),
		trpcgo.OutputParser(func(u User) (PublicUser, error) {
			return PublicUser{ID: u.ID}, nil
		}),
	)

	// Nil untyped parser clears a previous typed parser.
	trpcgo.MustVoidQuery(router, "user.typedthennil", listUsers,
		trpcgo.OutputParser(func(u User) (PublicUser, error) {
			return PublicUser{ID: u.ID}, nil
		}),
		trpcgo.WithOutputParser(nil),
	)

	// Nil parser function variable also clears.
	nilParser := (func(any) (any, error))(nil)
	trpcgo.MustVoidQuery(router, "user.typedthennilvar", listUsers,
		trpcgo.OutputParser(func(u User) (PublicUser, error) {
			return PublicUser{ID: u.ID}, nil
		}),
		trpcgo.WithOutputParser(nilParser),
	)

	// Pre-bound builder variable that carries WithOutputParser(...)
	baseBuilder := trpcgo.Procedure().WithOutputParser(func(v any) (any, error) {
		u, _ := v.(User)
		return PublicUser{ID: u.ID}, nil
	})
	trpcgo.MustVoidQuery(router, "user.varbuilder", listUsers, baseBuilder)

	// Builder method nil clears typed parser set earlier in chain.
	trpcgo.MustVoidQuery(router, "user.buildertypedthennil", listUsers,
		trpcgo.Procedure().With(
			trpcgo.OutputParser(func(u User) (PublicUser, error) {
				return PublicUser{ID: u.ID}, nil
			}),
		).WithOutputParser(nil),
	)

	// Very indirect typed builder chain should still resolve through aliases.
	deepBuilder0 := trpcgo.Procedure().With(trpcgo.OutputParser(func(u User) (PublicUser, error) {
		return PublicUser{ID: u.ID}, nil
	}))
	deepBuilder1 := deepBuilder0
	deepBuilder2 := deepBuilder1
	deepBuilder3 := deepBuilder2
	deepBuilder4 := deepBuilder3
	deepBuilder5 := deepBuilder4
	deepBuilder6 := deepBuilder5
	deepBuilder7 := deepBuilder6
	deepBuilder8 := deepBuilder7
	deepBuilder9 := deepBuilder8
	deepBuilder10 := deepBuilder9
	trpcgo.MustVoidQuery(router, "user.deepwith", listUsers, deepBuilder10)

	// Very indirect nil parser chain should still clear correctly.
	nilParser0 := (func(any) (any, error))(nil)
	nilParser1 := nilParser0
	nilParser2 := nilParser1
	nilParser3 := nilParser2
	nilParser4 := nilParser3
	nilParser5 := nilParser4
	nilParser6 := nilParser5
	nilParser7 := nilParser6
	nilParser8 := nilParser7
	nilParser9 := nilParser8
	nilParser10 := nilParser9
	trpcgo.MustVoidQuery(router, "user.deepnilclear", listUsers,
		trpcgo.OutputParser(func(u User) (PublicUser, error) {
			return PublicUser{ID: u.ID}, nil
		}),
		trpcgo.WithOutputParser(nilParser10),
	)
}
