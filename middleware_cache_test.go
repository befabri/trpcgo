package trpcgo_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/befabri/trpcgo"
)

func TestMiddlewareChainsSharedAcrossCallPaths(t *testing.T) {
	r := trpcgo.NewRouter()
	builds := 0
	r.Use(func(next trpcgo.HandlerFunc) trpcgo.HandlerFunc {
		builds++
		generation := builds
		return func(ctx context.Context, input any) (any, error) {
			v, err := next(ctx, input)
			return fmt.Sprintf("%d:%v", generation, v), err
		}
	})
	trpcgo.MustVoidQuery(r, "ping", func(context.Context) (string, error) { return "pong", nil })
	snapshot := r.BuildProcedureMap()
	entry, _ := snapshot.Lookup("ping")
	for range 5 {
		got, err := r.RawCall(t.Context(), "ping", nil)
		if err != nil || got != "1:pong" {
			t.Fatalf("RawCall=(%v,%v); chain was rebuilt", got, err)
		}
		got, err = r.ExecuteEntry(t.Context(), entry, nil)
		if err != nil || got != "1:pong" {
			t.Fatalf("ExecuteEntry=(%v,%v)", got, err)
		}
		if r.BuildProcedureMap() != snapshot {
			t.Fatal("unchanged router rebuilt its snapshot")
		}
	}
	if builds != 1 {
		t.Fatalf("middleware constructed %d times", builds)
	}
	r.Use(func(next trpcgo.HandlerFunc) trpcgo.HandlerFunc { return next })
	updated := r.BuildProcedureMap()
	if updated == snapshot || builds != 2 {
		t.Fatalf("middleware change did not invalidate cache: builds=%d", builds)
	}
	got, err := r.RawCall(t.Context(), "ping", nil)
	if err != nil || got != "2:pong" {
		t.Fatalf("updated RawCall=(%v,%v)", got, err)
	}
	got, err = r.ExecuteEntry(t.Context(), entry, nil)
	if err != nil || got != "1:pong" {
		t.Fatalf("existing snapshot mutated: (%v,%v)", got, err)
	}
	trpcgo.MustVoidQuery(r, "new", func(context.Context) (string, error) { return "new", nil })
	if _, ok := r.BuildProcedureMap().Lookup("new"); !ok {
		t.Fatal("registration missing from refreshed snapshot")
	}
	source := trpcgo.NewRouter()
	trpcgo.MustVoidQuery(source, "merged", func(context.Context) (string, error) { return "merged", nil })
	if err := r.Merge(source); err != nil {
		t.Fatal(err)
	}
	if _, ok := r.BuildProcedureMap().Lookup("merged"); !ok {
		t.Fatal("merge missing from refreshed snapshot")
	}
}

func BenchmarkRawCallCachedMiddleware(b *testing.B) {
	r := trpcgo.NewRouter()
	for range 10 {
		r.Use(func(next trpcgo.HandlerFunc) trpcgo.HandlerFunc {
			return func(ctx context.Context, input any) (any, error) { return next(ctx, input) }
		})
	}
	trpcgo.MustVoidQuery(r, "ping", func(context.Context) (string, error) { return "pong", nil })
	r.BuildProcedureMap()
	ctx := trpcgo.WithResponseMetadata(trpcgo.WithProcedureMeta(context.Background(), trpcgo.ProcedureMeta{Path: "ping", Type: trpcgo.ProcedureQuery}))
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := r.RawCall(ctx, "ping", nil); err != nil {
			b.Fatal(err)
		}
	}
}
