package trpc

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/befabri/trpcgo"
)

// parsedRequest represents a single procedure call extracted from an HTTP request.
type parsedRequest struct {
	path  string
	input json.RawMessage
}

// containsTraversal rejects paths that contain directory traversal segments.
func containsTraversal(path string) bool {
	for _, segment := range strings.Split(path, "/") {
		if segment == "." || segment == ".." {
			return true
		}
	}
	return false
}

// parseRequest extracts procedure path(s) and input(s) from an HTTP request.
func parseRequest(r *http.Request, basePath string, isBatch bool, maxBodySize int64) ([]parsedRequest, error) {
	rawPath, ok := stripBasePath(r.URL.Path, basePath)
	if !ok || rawPath == "" {
		return nil, trpcgo.NewError(trpcgo.CodeNotFound, "no procedure path specified")
	}

	if !isBatch {
		if containsTraversal(rawPath) {
			return nil, trpcgo.NewError(trpcgo.CodeBadRequest, "invalid procedure path")
		}
		input, err := parseInput(r, maxBodySize)
		if err != nil {
			return nil, err
		}
		return []parsedRequest{{path: rawPath, input: input}}, nil
	}

	// Batch: paths are comma-separated.
	paths := strings.Split(rawPath, ",")
	for _, p := range paths {
		if containsTraversal(p) {
			return nil, trpcgo.NewError(trpcgo.CodeBadRequest, "invalid procedure path")
		}
	}
	var indexedInputs map[string]json.RawMessage
	if r.Method == http.MethodGet {
		rawInput := r.URL.Query().Get("input")
		if maxBodySize > 0 && int64(len(rawInput)) > maxBodySize {
			return nil, trpcgo.NewError(trpcgo.CodePayloadTooLarge, "query input too large")
		}
		if rawInput != "" {
			if err := json.Unmarshal([]byte(rawInput), &indexedInputs); err != nil {
				return nil, trpcgo.NewError(trpcgo.CodeParseError, "failed to parse batch input")
			}
		}
	} else {
		body, err := readBody(r, maxBodySize)
		if err != nil {
			return nil, err
		}
		if len(body) > 0 {
			if err := json.Unmarshal(body, &indexedInputs); err != nil {
				return nil, trpcgo.NewError(trpcgo.CodeParseError, "failed to parse batch input")
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
		rawInput := r.URL.Query().Get("input")
		if rawInput == "" {
			return nil, nil
		}
		if maxBodySize > 0 && int64(len(rawInput)) > maxBodySize {
			return nil, trpcgo.NewError(trpcgo.CodePayloadTooLarge, "query input too large")
		}
		return json.RawMessage(rawInput), nil
	}
	body, err := readBody(r, maxBodySize)
	if err != nil {
		return nil, err
	}
	if len(body) == 0 {
		return nil, nil
	}
	return json.RawMessage(body), nil
}

// readBody reads the request body with a size limit.
func readBody(r *http.Request, maxSize int64) ([]byte, error) {
	reader := r.Body
	if maxSize > 0 {
		reader = http.MaxBytesReader(nil, r.Body, maxSize)
	}
	body, err := io.ReadAll(reader)
	if err != nil {
		return nil, trpcgo.NewError(trpcgo.CodePayloadTooLarge, "request body too large")
	}
	return body, nil
}

// isBatchRequest checks if the request is a batch request (?batch=1).
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
func stripBasePath(path, basePath string) (string, bool) {
	if basePath == "" {
		return strings.TrimPrefix(path, "/"), true
	}
	if !strings.HasPrefix(basePath, "/") {
		basePath = "/" + basePath
	}
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

// mergeLastEventId reads the lastEventId from the request and merges it into input.
func mergeLastEventId(r *http.Request, input json.RawMessage) json.RawMessage {
	lastEventId := r.Header.Get("Last-Event-Id")
	if lastEventId == "" {
		lastEventId = r.URL.Query().Get("lastEventId")
	}
	if lastEventId == "" {
		lastEventId = r.URL.Query().Get("Last-Event-Id")
	}
	if lastEventId == "" {
		return input
	}
	if len(input) == 0 || string(input) == "null" {
		merged, _ := json.Marshal(map[string]string{"lastEventId": lastEventId})
		return merged
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(input, &obj); err != nil {
		return input
	}
	idVal, _ := json.Marshal(lastEventId)
	obj["lastEventId"] = idVal
	merged, _ := json.Marshal(obj)
	return merged
}
