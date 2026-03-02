package trpcgo

import (
	"context"
	"net/http"
	"sync"
)

type responseMetadataKey struct{}

// responseMetadata collects cookies and headers that handlers want to set
// on the HTTP response. It is injected into the context before procedure
// execution and applied to the ResponseWriter before the status line.
// A mutex protects concurrent access from JSONL batch handlers.
type responseMetadata struct {
	mu      sync.Mutex
	cookies []*http.Cookie
	headers http.Header
}

// WithResponseMetadata injects a fresh responseMetadata into the context.
// This is called automatically by the HTTP handler. For RawCall, callers
// should call this before RawCall if they need to access cookies/headers
// set by handlers via GetResponseCookies/GetResponseHeaders.
func WithResponseMetadata(ctx context.Context) context.Context {
	return context.WithValue(ctx, responseMetadataKey{}, &responseMetadata{
		headers: make(http.Header),
	})
}

func getResponseMetadata(ctx context.Context) *responseMetadata {
	rm, _ := ctx.Value(responseMetadataKey{}).(*responseMetadata)
	return rm
}

// applyResponseMetadata writes any accumulated cookies and headers to the
// ResponseWriter. Must be called before WriteHeader.
func applyResponseMetadata(ctx context.Context, w http.ResponseWriter) {
	rm := getResponseMetadata(ctx)
	if rm == nil {
		return
	}
	rm.mu.Lock()
	defer rm.mu.Unlock()
	for key, values := range rm.headers {
		for _, v := range values {
			w.Header().Add(key, v)
		}
	}
	for _, c := range rm.cookies {
		http.SetCookie(w, c)
	}
}

// SetCookie adds a cookie to be set on the HTTP response. Call this from
// within a procedure handler or middleware. If the context does not carry
// response metadata (e.g. called outside the HTTP handler), this is a no-op.
// Safe for concurrent use from JSONL batch handlers.
func SetCookie(ctx context.Context, c *http.Cookie) {
	if c == nil {
		return
	}
	if rm := getResponseMetadata(ctx); rm != nil {
		rm.mu.Lock()
		rm.cookies = append(rm.cookies, c)
		rm.mu.Unlock()
	}
}

// GetResponseCookies returns the cookies collected in the context by SetCookie.
// This is useful for RawCall callers that need to inspect cookies set by handlers.
// Returns nil if the context does not carry response metadata.
func GetResponseCookies(ctx context.Context) []*http.Cookie {
	rm := getResponseMetadata(ctx)
	if rm == nil {
		return nil
	}
	rm.mu.Lock()
	defer rm.mu.Unlock()
	out := make([]*http.Cookie, len(rm.cookies))
	copy(out, rm.cookies)
	return out
}

// GetResponseHeaders returns the headers collected in the context by SetResponseHeader.
// This is useful for RawCall callers that need to inspect headers set by handlers.
// Returns nil if the context does not carry response metadata.
func GetResponseHeaders(ctx context.Context) http.Header {
	rm := getResponseMetadata(ctx)
	if rm == nil {
		return nil
	}
	rm.mu.Lock()
	defer rm.mu.Unlock()
	out := rm.headers.Clone()
	return out
}

// SetResponseHeader adds a header value to be set on the HTTP response.
// If the context does not carry response metadata, this is a no-op.
// Safe for concurrent use from JSONL batch handlers.
func SetResponseHeader(ctx context.Context, key, value string) {
	if rm := getResponseMetadata(ctx); rm != nil {
		rm.mu.Lock()
		rm.headers.Add(key, value)
		rm.mu.Unlock()
	}
}
