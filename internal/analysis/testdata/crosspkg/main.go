package crosspkg

import (
	"context"
	"time"

	"example.com/crosspkg/domain"
	"github.com/befabri/trpcgo"
)

// Role is an enum declared in the scanned package itself — the control case
// that already worked before cross-package const discovery.
type Role string

const (
	RoleViewer Role = "viewer"
	RoleAdmin  Role = "admin"
	RoleOwner  Role = "owner"
)

// VideoResponse mixes three kinds of enum-ish field:
//   - Status: a named enum from an imported, non-scanned package.
//   - Role: a named enum from this package.
//   - Job.Phase: a named enum from a transitive same-module import.
//   - Timeout: a stdlib named type (time.Duration) whose Nanosecond…Hour are
//     typed constants. It must NOT become a union — dependency enums are out of
//     scope, gated by the root module check.
type VideoResponse struct {
	ID      string        `json:"id"`
	Status  domain.Status `json:"status"`
	Role    Role          `json:"role"`
	Job     domain.Job    `json:"job"`
	Timeout time.Duration `json:"timeout"`
}

type GetInput struct {
	ID string `json:"id"`
}

func getVideo(ctx context.Context, input GetInput) (VideoResponse, error) {
	return VideoResponse{}, nil
}

func Setup() *trpcgo.Router {
	router := trpcgo.NewRouter()
	trpcgo.Query(router, "video.get", getVideo)
	return router
}
