package trpcgo

import (
	"context"
	"fmt"
	"sync"
	"testing"
)

// BenchmarkLookupDirect measures a plain map read on the frozen snapshot
// (what httpHandler.lookup does on the hot path — no lock).
func BenchmarkLookupDirect(b *testing.B) {
	for _, n := range []int{1, 10, 100} {
		b.Run(fmt.Sprintf("procs=%d", n), func(b *testing.B) {
			procs := make(map[string]*procedure, n)
			for i := range n {
				procs[fmt.Sprintf("proc.%d", i)] = &procedure{
					typ:     ProcedureQuery,
					handler: func(ctx context.Context, input any) (any, error) { return nil, nil },
				}
			}
			key := fmt.Sprintf("proc.%d", n/2)

			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					p, ok := procs[key]
					if !ok || p == nil {
						b.Fatal("not found")
					}
				}
			})
		})
	}
}

// BenchmarkLookupRWMutex measures an RLock-protected map read
// (what the old Router.lookup did on every request).
func BenchmarkLookupRWMutex(b *testing.B) {
	for _, n := range []int{1, 10, 100} {
		b.Run(fmt.Sprintf("procs=%d", n), func(b *testing.B) {
			var mu sync.RWMutex
			procs := make(map[string]*procedure, n)
			for i := range n {
				procs[fmt.Sprintf("proc.%d", i)] = &procedure{
					typ:     ProcedureQuery,
					handler: func(ctx context.Context, input any) (any, error) { return nil, nil },
				}
			}
			key := fmt.Sprintf("proc.%d", n/2)

			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					mu.RLock()
					p, ok := procs[key]
					mu.RUnlock()
					if !ok || p == nil {
						b.Fatal("not found")
					}
				}
			})
		})
	}
}
