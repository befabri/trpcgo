package trpcgo

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// parsedRequest represents a single procedure call extracted from an HTTP request.
type parsedRequest struct {
	path  string
	input json.RawMessage
}

// parseRequest extracts procedure path(s) and input(s) from an HTTP request.
// Returns one parsedRequest per procedure call (multiple if batched).
func parseRequest(r *http.Request, basePath string, isBatch bool, maxBodySize int64) ([]parsedRequest, error) {
	// Extract procedure path from URL
	rawPath := strings.TrimPrefix(r.URL.Path, basePath)
	rawPath = strings.TrimPrefix(rawPath, "/")
	rawPath, _ = url.PathUnescape(rawPath)

	if rawPath == "" {
		return nil, NewError(CodeNotFound, "no procedure path specified")
	}

	if !isBatch {
		input, err := parseInput(r, maxBodySize)
		if err != nil {
			return nil, err
		}
		return []parsedRequest{{path: rawPath, input: input}}, nil
	}

	// Batch: paths are comma-separated
	paths := strings.Split(rawPath, ",")
	results := make([]parsedRequest, len(paths))

	if r.Method == http.MethodGet {
		// GET batch: input is a JSON object keyed by index in the query param
		rawInput := r.URL.Query().Get("input")
		var indexedInputs map[string]json.RawMessage
		if rawInput != "" {
			decoded, err := url.QueryUnescape(rawInput)
			if err != nil {
				decoded = rawInput
			}
			if err := json.Unmarshal([]byte(decoded), &indexedInputs); err != nil {
				return nil, NewError(CodeParseError, "failed to parse batch input")
			}
		}
		for i, path := range paths {
			results[i] = parsedRequest{
				path:  path,
				input: indexedInputs[fmt.Sprintf("%d", i)],
			}
		}
	} else {
		// POST batch: body is a JSON object keyed by index
		body, err := readBody(r, maxBodySize)
		if err != nil {
			return nil, err
		}
		var indexedInputs map[string]json.RawMessage
		if len(body) > 0 {
			if err := json.Unmarshal(body, &indexedInputs); err != nil {
				return nil, NewError(CodeParseError, "failed to parse batch input")
			}
		}
		for i, path := range paths {
			results[i] = parsedRequest{
				path:  path,
				input: indexedInputs[fmt.Sprintf("%d", i)],
			}
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
		decoded, err := url.QueryUnescape(rawInput)
		if err != nil {
			decoded = rawInput
		}
		return json.RawMessage(decoded), nil
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
	rawPath := strings.TrimPrefix(r.URL.Path, basePath)
	rawPath = strings.TrimPrefix(rawPath, "/")
	rawPath, _ = url.PathUnescape(rawPath)
	return strings.Split(rawPath, ",")
}
