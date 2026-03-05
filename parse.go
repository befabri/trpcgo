package trpcgo

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// parsedRequest represents a single procedure call extracted from an HTTP request.
type parsedRequest struct {
	path  string
	input json.RawMessage
}

// containsTraversal rejects paths that contain directory traversal segments.
// Procedure names are flat identifiers (e.g. "user.getById") — they should
// never contain "..", ".", or leading/trailing slashes. This is defense-in-depth:
// while the map lookup is inherently safe against traversal, the raw path
// is exposed to middleware via ProcedureMeta.Path, to onError callbacks, and
// to the error envelope data.path field.
func containsTraversal(path string) bool {
	for _, segment := range strings.Split(path, "/") {
		if segment == "." || segment == ".." {
			return true
		}
	}
	return false
}

// parseRequest extracts procedure path(s) and input(s) from an HTTP request.
// Returns one parsedRequest per procedure call (multiple if batched).
func parseRequest(r *http.Request, basePath string, isBatch bool, maxBodySize int64) ([]parsedRequest, error) {
	// Extract procedure path from URL.
	// r.URL.Path is already decoded by Go's net/http — do NOT call
	// PathUnescape again, as that would create a double-decode allowing
	// double-encoded paths to bypass reverse proxy path-based ACLs.
	rawPath, ok := stripBasePath(r.URL.Path, basePath)
	if !ok {
		return nil, NewError(CodeNotFound, "no procedure path specified")
	}

	if rawPath == "" {
		return nil, NewError(CodeNotFound, "no procedure path specified")
	}

	if !isBatch {
		if containsTraversal(rawPath) {
			return nil, NewError(CodeBadRequest, "invalid procedure path")
		}
		input, err := parseInput(r, maxBodySize)
		if err != nil {
			return nil, err
		}
		return []parsedRequest{{path: rawPath, input: input}}, nil
	}

	// Batch: paths are comma-separated
	paths := strings.Split(rawPath, ",")
	for _, p := range paths {
		if containsTraversal(p) {
			return nil, NewError(CodeBadRequest, "invalid procedure path")
		}
	}
	var indexedInputs map[string]json.RawMessage
	if r.Method == http.MethodGet {
		// GET batch: input is a JSON object keyed by index in the query param.
		// r.URL.Query().Get() already percent-decodes — do NOT call
		// url.QueryUnescape again (same double-decode bug as parseInput).
		rawInput := r.URL.Query().Get("input")
		if maxBodySize > 0 && int64(len(rawInput)) > maxBodySize {
			return nil, NewError(CodePayloadTooLarge, "query input too large")
		}
		if rawInput != "" {
			if err := json.Unmarshal([]byte(rawInput), &indexedInputs); err != nil {
				return nil, NewError(CodeParseError, "failed to parse batch input")
			}
		}
	} else {
		// POST batch: body is a JSON object keyed by index.
		body, err := readBody(r, maxBodySize)
		if err != nil {
			return nil, err
		}
		if len(body) > 0 {
			if err := json.Unmarshal(body, &indexedInputs); err != nil {
				return nil, NewError(CodeParseError, "failed to parse batch input")
			}
		}
	}

	results := make([]parsedRequest, len(paths))
	for i, path := range paths {
		results[i] = parsedRequest{
			path:  path,
			input: indexedInputs[fmt.Sprintf("%d", i)],
		}
	}

	return results, nil
}

// parseInput extracts the input for a single (non-batch) procedure call.
func parseInput(r *http.Request, maxBodySize int64) (json.RawMessage, error) {
	if r.Method == http.MethodGet {
		// r.URL.Query().Get() already percent-decodes the query parameter.
		// Do NOT call url.QueryUnescape again — that would create a
		// double-decode allowing double-encoded input to bypass proxy/WAF
		// input validation.
		rawInput := r.URL.Query().Get("input")
		if rawInput == "" {
			return nil, nil
		}
		if maxBodySize > 0 && int64(len(rawInput)) > maxBodySize {
			return nil, NewError(CodePayloadTooLarge, "query input too large")
		}
		return json.RawMessage(rawInput), nil
	}

	// POST: read body
	body, err := readBody(r, maxBodySize)
	if err != nil {
		return nil, err
	}
	if len(body) == 0 {
		return nil, nil
	}
	return json.RawMessage(body), nil
}

// readBody reads the request body with a size limit to prevent DoS.
func readBody(r *http.Request, maxSize int64) ([]byte, error) {
	reader := r.Body
	if maxSize > 0 {
		reader = http.MaxBytesReader(nil, r.Body, maxSize)
	}
	body, err := io.ReadAll(reader)
	if err != nil {
		return nil, NewError(CodePayloadTooLarge, "request body too large")
	}
	return body, nil
}

// isBatchRequest checks if the request is a batch request (has ?batch=1).
func isBatchRequest(r *http.Request) bool {
	return r.URL.Query().Get("batch") == "1"
}

// parsePaths extracts procedure paths from the URL without parsing inputs.
func parsePaths(r *http.Request, basePath string) []string {
	rawPath, ok := stripBasePath(r.URL.Path, basePath)
	if !ok || rawPath == "" {
		return nil
	}
	return strings.Split(rawPath, ",")
}

// stripBasePath removes the configured handler base path from an HTTP path.
// It enforces a path-segment boundary so basePath "/trpc" does not match
// "/trpcx". Returns ("", true) when path == basePath.
func stripBasePath(path, basePath string) (string, bool) {
	if basePath == "" {
		return strings.TrimPrefix(path, "/"), true
	}

	if !strings.HasPrefix(basePath, "/") {
		basePath = "/" + basePath
	}

	// Normalize trailing slash so "/trpc" and "/trpc/" behave the same.
	for len(basePath) > 1 && strings.HasSuffix(basePath, "/") {
		basePath = strings.TrimSuffix(basePath, "/")
	}

	if basePath == "/" {
		return strings.TrimPrefix(path, "/"), true
	}

	if path == basePath {
		return "", true
	}

	prefix := basePath + "/"
	if strings.HasPrefix(path, prefix) {
		return strings.TrimPrefix(path, prefix), true
	}

	return "", false
}
