package omitempty

import (
	"context"

	"github.com/befabri/trpcgo"
)

type ConfirmTOTPInput struct {
	Code        string `json:"code" validate:"omitempty,len=6"`
	CurrentCode string `json:"current_code" validate:"omitempty,len=6"`
	Name        string `json:"name" validate:"required,min=1"`
}

type OptionalEmailInput struct {
	BackupEmail  string  `json:"backup_email" validate:"omitempty,email"`
	PrimaryEmail string  `json:"primary_email" validate:"required,email"`
	Nickname     *string `json:"nickname,omitempty" validate:"omitempty,min=3,max=30"`
}

type Result struct {
	OK bool `json:"ok"`
}

func confirmTotp(ctx context.Context, input ConfirmTOTPInput) (Result, error) {
	return Result{}, nil
}

func updateEmail(ctx context.Context, input OptionalEmailInput) (Result, error) {
	return Result{}, nil
}

func Setup() *trpcgo.Router {
	router := trpcgo.NewRouter()
	trpcgo.Mutation(router, "auth.confirmTotp", confirmTotp)
	trpcgo.Mutation(router, "user.updateEmail", updateEmail)
	return router
}
