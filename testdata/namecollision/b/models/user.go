// Package models mirrors testdata/namecollision/a/models with a different shape.
package models

type User struct {
	Email string `json:"email" validate:"required,email"`
}
