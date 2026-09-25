// Package models is one of two packages sharing a name and a type name, so
// generated TypeScript must disambiguate beyond the last path segment.
package models

type User struct {
	Name string `json:"name" validate:"required"`
}
