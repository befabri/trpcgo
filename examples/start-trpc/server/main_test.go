package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/befabri/trpcgo"
)

func TestListUsersReturnsSnapshot(t *testing.T) {
	s := &userService{
		nextID: 2,
		users:  []User{{ID: "1", Name: "First"}, {ID: "2", Name: "Second"}},
	}
	list, err := s.ListUsers(t.Context(), ListUsersInput{Page: 1, PerPage: 10})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.DeleteUser(t.Context(), DeleteUserInput{ID: "1"}); err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != 2 || list.Items[0].ID != "1" || list.Items[1].ID != "2" {
		t.Fatalf("an already-returned list changed after deletion: %+v", list.Items)
	}
}

func TestListUsersPaginationSnapshotSurvivesReset(t *testing.T) {
	for _, tc := range []struct{ page, perPage, count int }{{1, 2, 2}, {2, 2, 1}, {3, 2, 0}, {0, 0, 3}} {
		t.Run(fmt.Sprintf("%d/%d", tc.page, tc.perPage), func(t *testing.T) {
			s := &userService{nextID: 3, users: []User{{ID: "1", Name: "First"}, {ID: "2", Name: "Second"}, {ID: "3", Name: "Third"}}}
			list, err := s.ListUsers(t.Context(), ListUsersInput{Page: tc.page, PerPage: tc.perPage})
			if err != nil {
				t.Fatal(err)
			}
			if len(list.Items) != tc.count || list.Total != 3 {
				t.Fatalf("list=%+v", list)
			}
			before, err := json.Marshal(list)
			if err != nil {
				t.Fatal(err)
			}
			r := trpcgo.NewRouter()
			trpcgo.MustMutation(r, "user.create", s.CreateUser)
			if _, err := s.ResetDemo(r)(t.Context()); err != nil {
				t.Fatal(err)
			}
			after, err := json.Marshal(list)
			if err != nil {
				t.Fatal(err)
			}
			if string(after) != string(before) {
				t.Fatalf("snapshot changed: before=%s after=%s", before, after)
			}
		})
	}
}

// JSON encoding of a ListUsers result happens after its read lock is released,
// so the result must survive concurrent mutation.
func TestListUsersResponseConcurrentMutation(t *testing.T) {
	s := &userService{
		nextID: 2,
		users:  []User{{ID: "1", Name: "First"}, {ID: "2", Name: "Second"}},
	}
	list, err := s.ListUsers(t.Context(), ListUsersInput{Page: 1, PerPage: 10})
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		for range 1000 {
			if _, err := json.Marshal(list); err != nil {
				t.Errorf("encoding list: %v", err)
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		<-start
		id := "1"
		for range 1000 {
			if _, err := s.DeleteUser(t.Context(), DeleteUserInput{ID: id}); err != nil {
				t.Errorf("deleting user: %v", err)
				return
			}
			user, err := s.CreateUser(t.Context(), CreateUserInput{Name: "Another"})
			if err != nil {
				t.Errorf("creating user: %v", err)
				return
			}
			id = user.ID
		}
	}()
	close(start)
	wg.Wait()
}

func TestResetDemoKeepsUnrelatedCreateEvents(t *testing.T) {
	s := &userService{}
	events := make(chan User, 8)
	s.addSubscriber(events)
	defer s.removeSubscriber(events)
	r := trpcgo.NewRouter()
	trpcgo.MustMutation(r, "user.create", s.CreateUser)

	// Blocking the first seed call makes the unrelated create observable
	// without timing assumptions.
	seedStarted := make(chan struct{})
	resumeSeed := make(chan struct{})
	r.Use(func(next trpcgo.HandlerFunc) trpcgo.HandlerFunc {
		return func(ctx context.Context, input any) (any, error) {
			if input.(CreateUserInput).Name == "Alice" {
				close(seedStarted)
				select {
				case <-resumeSeed:
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}
			return next(ctx, input)
		}
	})
	// t.Context() is cancelled before Cleanup runs and would race the resume
	// signal, so this test owns its own context.
	ctx, cancel := context.WithCancel(context.Background())
	resetDone := make(chan error, 1)
	go func() {
		_, err := s.ResetDemo(r)(ctx)
		resetDone <- err
	}()
	t.Cleanup(func() {
		close(resumeSeed)
		defer cancel()
		select {
		case err := <-resetDone:
			if err != nil {
				t.Errorf("reset: %v", err)
			}
		case <-time.After(3 * time.Second):
			t.Error("reset did not finish after releasing the seed call")
		}
	})
	select {
	case <-seedStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("reset did not reach its first seed call")
	}

	created, err := s.CreateUser(t.Context(), CreateUserInput{Name: "Carol"})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-events:
		if event.ID != created.ID || event.Name != "Carol" {
			t.Fatalf("event = %+v, want the unrelated creation %+v", event, created)
		}
	default:
		t.Fatal("reset suppressed the unrelated user's creation event")
	}
}

func TestResetDemoConcurrentCreate(t *testing.T) {
	s := &userService{}
	r := trpcgo.NewRouter()
	trpcgo.MustMutation(r, "user.create", s.CreateUser)
	reset := s.ResetDemo(r)
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		for range 100 {
			if _, err := reset(t.Context()); err != nil {
				t.Errorf("reset: %v", err)
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		<-start
		for i := range 1000 {
			if _, err := s.CreateUser(t.Context(), CreateUserInput{Name: fmt.Sprintf("user-%d", i)}); err != nil {
				t.Errorf("create: %v", err)
				return
			}
		}
	}()
	close(start)
	wg.Wait()
}

func TestResetDemoSeedCallsKeepMiddlewareAndSuppressEvents(t *testing.T) {
	for _, rejectBob := range []bool{false, true} {
		t.Run(fmt.Sprintf("rejectBob=%t", rejectBob), func(t *testing.T) {
			s := &userService{}
			events := make(chan User, 8)
			s.addSubscriber(events)
			defer s.removeSubscriber(events)
			r := trpcgo.NewRouter()
			var calls []string
			r.Use(func(next trpcgo.HandlerFunc) trpcgo.HandlerFunc {
				return func(ctx context.Context, input any) (any, error) {
					if ctx.Value(contextKeyRequestID) != "request-1" {
						t.Error("seed lost request context")
					}
					name := input.(CreateUserInput).Name
					calls = append(calls, name)
					if rejectBob && name == "Bob" {
						return nil, trpcgo.NewError(trpcgo.CodeBadRequest, "seed rejected")
					}
					return next(ctx, input)
				}
			})
			trpcgo.MustMutation(r, "user.create", s.CreateUser)
			ctx := context.WithValue(t.Context(), contextKeyRequestID, "request-1")
			result, err := s.ResetDemo(r)(ctx)
			if rejectBob {
				var trpcErr *trpcgo.Error
				if !errors.As(err, &trpcErr) || trpcErr.Code != trpcgo.CodeBadRequest {
					t.Fatalf("seed error=%v", err)
				}
			} else if err != nil || result.UserCount != 2 {
				t.Fatalf("reset=(%+v,%v)", result, err)
			}
			if fmt.Sprint(calls) != "[Alice Bob]" {
				t.Fatalf("seed middleware calls=%v", calls)
			}
			select {
			case event := <-events:
				t.Fatalf("seed event broadcast: %+v", event)
			default:
			}
			created, err := s.CreateUser(ctx, CreateUserInput{Name: "Carol"})
			if err != nil {
				t.Fatal(err)
			}
			select {
			case event := <-events:
				if event.ID != created.ID || event.Name != "Carol" {
					t.Fatalf("wrong event=%+v", event)
				}
			default:
				t.Fatal("seed context affected the subsequent create")
			}
		})
	}
}
