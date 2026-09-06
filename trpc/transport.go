package trpc

import (
	"net/http"
	"slices"
)

var (
	transportMethods = []string{http.MethodGet, http.MethodPost}
	transportHeaders = []string{"Authorization", "Content-Type", "Last-Event-Id", "trpc-accept"}
)

// Methods returns the HTTP methods the handler serves.
func Methods() []string {
	return slices.Clone(transportMethods)
}

// RequestHeaders returns the request headers a tRPC client may send that
// browsers do not treat as CORS-safelisted.
func RequestHeaders() []string {
	return slices.Clone(transportHeaders)
}
