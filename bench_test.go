package trpcgo_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/befabri/trpcgo"
)

// BenchmarkServeHTTP measures the full HTTP request path including the
// lock-free procedure lookup. Use httptest.NewRequest (no network) to
// keep overhead low, but note that JSON marshal and recorder allocation
// still dominate — this benchmark is best used for regression detection,
// not micro-level lock analysis.
func BenchmarkServeHTTP(b *testing.B) {
	for _, n := range []int{1, 10, 100} {
		b.Run(fmt.Sprintf("procs=%d", n), func(b *testing.B) {
			router := trpcgo.NewRouter(trpcgo.WithMethodOverride(true))
			for i := range n {
				path := fmt.Sprintf("proc.%d", i)
				trpcgo.VoidQuery(router, path, func(ctx context.Context) (string, error) {
					return "ok", nil
				})
			}

			handler := router.Handler("/trpc")
			target := fmt.Sprintf("/trpc/proc.%d", n/2)

			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					req := httptest.NewRequest(http.MethodGet, target, nil)
					w := httptest.NewRecorder()
					handler.ServeHTTP(w, req)
					if w.Code != http.StatusOK {
						b.Fatalf("status = %d", w.Code)
					}
				}
			})
		})
	}
}

// BenchmarkServeHTTPNotFound measures the fast-reject path (procedure not
// found). This is the cheapest code path through ServeHTTP and the closest
// proxy for isolating lookup + error formatting cost.
func BenchmarkServeHTTPNotFound(b *testing.B) {
	router := trpcgo.NewRouter(trpcgo.WithMethodOverride(true))
	trpcgo.VoidQuery(router, "exists", func(ctx context.Context) (string, error) {
		return "ok", nil
	})
	handler := router.Handler("/trpc")

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			req := httptest.NewRequest(http.MethodGet, "/trpc/missing", nil)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			if w.Code != http.StatusNotFound {
				b.Fatalf("status = %d, want 404", w.Code)
			}
		}
	})
}

// BenchmarkCall measures the server-side Call path which still uses RWMutex
// on the Router's live procedure map (not the handler snapshot). This serves
// as a baseline for comparing the locked vs lock-free paths.
func BenchmarkCall(b *testing.B) {
	router := trpcgo.NewRouter()
	trpcgo.VoidQuery(router, "echo", func(ctx context.Context) (string, error) {
		return "ok", nil
	})
	_ = router.Handler("/trpc") // pre-compute middleware chains

	ctx := context.Background()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, err := trpcgo.Call[any, string](router, ctx, "echo", nil)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}
