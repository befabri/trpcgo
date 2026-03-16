// Example: oRPC REST-style API
//
// Demonstrates how trpcgo procedures map to REST endpoints via WithRoute.
// All procedures are defined once in Go and served as a standard REST API
// over the oRPC wire format.
//
//	GET    /api/planets          → list all planets
//	POST   /api/planets          → create a planet (returns 201)
//	GET    /api/planets/{id}     → get planet by ID
//	PUT    /api/planets/{id}     → update a planet
//	DELETE /api/planets/{id}     → delete a planet
//	GET    /api/health           → health check
//
// Run:
//
//	go run .
//	curl localhost:8080/api/planets
//	curl localhost:8080/api/planets/1
//	curl -X POST localhost:8080/api/planets -H 'Content-Type: application/json' -d '{"json":{"name":"Venus","radius":6051}}'
//	curl -X PUT  localhost:8080/api/planets/1 -H 'Content-Type: application/json' -d '{"json":{"name":"Earth","radius":6371}}'
//	curl -X DELETE localhost:8080/api/planets/2
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sync"

	"github.com/befabri/trpcgo"
	"github.com/befabri/trpcgo/orpc"
)

// Types

type Planet struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Radius int    `json:"radius"`
}

type GetPlanetInput struct {
	ID int `json:"id"`
}

type CreatePlanetInput struct {
	Name   string `json:"name"`
	Radius int    `json:"radius"`
}

type UpdatePlanetInput struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Radius int    `json:"radius"`
}

type DeletePlanetInput struct {
	ID int `json:"id"`
}

// Service

type planetService struct {
	mu     sync.RWMutex
	data   []Planet
	nextID int
}

func (s *planetService) List(ctx context.Context) ([]Planet, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Planet, len(s.data))
	copy(out, s.data)
	return out, nil
}

func (s *planetService) Get(ctx context.Context, in GetPlanetInput) (Planet, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, p := range s.data {
		if p.ID == in.ID {
			return p, nil
		}
	}
	return Planet{}, trpcgo.NewError(trpcgo.CodeNotFound, fmt.Sprintf("planet %d not found", in.ID))
}

func (s *planetService) Create(ctx context.Context, in CreatePlanetInput) (Planet, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	p := Planet{ID: s.nextID, Name: in.Name, Radius: in.Radius}
	s.data = append(s.data, p)
	return p, nil
}

func (s *planetService) Update(ctx context.Context, in UpdatePlanetInput) (Planet, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, p := range s.data {
		if p.ID == in.ID {
			s.data[i].Name = in.Name
			s.data[i].Radius = in.Radius
			return s.data[i], nil
		}
	}
	return Planet{}, trpcgo.NewError(trpcgo.CodeNotFound, fmt.Sprintf("planet %d not found", in.ID))
}

func (s *planetService) Delete(ctx context.Context, in DeletePlanetInput) (Planet, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, p := range s.data {
		if p.ID == in.ID {
			s.data = append(s.data[:i], s.data[i+1:]...)
			return p, nil
		}
	}
	return Planet{}, trpcgo.NewError(trpcgo.CodeNotFound, fmt.Sprintf("planet %d not found", in.ID))
}

func main() {
	svc := &planetService{
		nextID: 2,
		data: []Planet{
			{ID: 1, Name: "Earth", Radius: 6371},
			{ID: 2, Name: "Mars", Radius: 3389},
		},
	}

	router := trpcgo.NewRouter()

	// Register procedures with REST routes.
	// The oRPC handler maps these to standard HTTP methods and paths.
	// Path parameters like {id} are extracted and injected into the input struct.

	trpcgo.VoidQuery(router, "planet.list", svc.List,
		trpcgo.WithRoute(http.MethodGet, "/planets"),
	)

	trpcgo.Query(router, "planet.get", svc.Get,
		trpcgo.WithRoute(http.MethodGet, "/planets/{id}"),
	)

	trpcgo.Mutation(router, "planet.create", svc.Create,
		trpcgo.WithRoute(http.MethodPost, "/planets"),
		trpcgo.WithSuccessStatus(http.StatusCreated),
	)

	trpcgo.Mutation(router, "planet.update", svc.Update,
		trpcgo.WithRoute(http.MethodPut, "/planets/{id}"),
	)

	trpcgo.Mutation(router, "planet.delete", svc.Delete,
		trpcgo.WithRoute(http.MethodDelete, "/planets/{id}"),
	)

	trpcgo.VoidQuery(router, "health", func(ctx context.Context) (string, error) {
		return "ok", nil
	}, trpcgo.WithRoute(http.MethodGet, "/health"))

	handler := orpc.NewHandler(router, "/api")

	log.Println("oRPC REST server on :8080")
	log.Println("  GET    /api/planets")
	log.Println("  POST   /api/planets")
	log.Println("  GET    /api/planets/{id}")
	log.Println("  PUT    /api/planets/{id}")
	log.Println("  DELETE /api/planets/{id}")
	log.Fatal(http.ListenAndServe(":8080", handler))
}
