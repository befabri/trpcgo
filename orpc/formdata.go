package orpc

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// decodeFormData parses a multipart/form-data oRPC request.
//
// The oRPC wire format for file uploads:
//   - Field "data": JSON string containing {"json": ..., "meta": [...], "maps": [...]}
//   - Fields "0", "1", ...: blob data (files)
//
// The maps array describes where each blob belongs in the JSON tree.
// Each maps[i] is a path (string keys / int indices) to the position
// of blob i. This function injects blob data at those paths so that
// json.Unmarshal into a struct with trpcgo.Blob fields works correctly.
func decodeFormData(r *http.Request, maxBodySize int64) (json.RawMessage, error) {
	// maxMem controls how much multipart data is kept in RAM; the rest
	// spills to temp files.  This is independent of the total body size
	// limit enforced by MaxBytesReader.
	const defaultMaxMem = 32 << 20 // 32 MB
	maxMem := int64(defaultMaxMem)
	if maxBodySize > 0 {
		r.Body = http.MaxBytesReader(nil, r.Body, maxBodySize)
		if maxBodySize < maxMem {
			maxMem = maxBodySize
		}
	}
	if err := r.ParseMultipartForm(maxMem); err != nil {
		return nil, fmt.Errorf("failed to parse multipart form: %w", err)
	}
	// Clean up temp files eagerly once we've read all blob data into
	// memory. Without this, temp files linger until the HTTP server
	// calls RemoveAll after the handler returns — which is too late
	// if the handler is slow or if many concurrent uploads are active.
	defer func() {
		if r.MultipartForm != nil {
			r.MultipartForm.RemoveAll()
		}
	}()

	dataField := r.FormValue("data")
	if dataField == "" {
		return nil, nil
	}

	var envelope struct {
		JSON json.RawMessage `json:"json"`
		Meta json.RawMessage `json:"meta"`
		Maps [][]any         `json:"maps"`
	}
	if err := json.Unmarshal([]byte(dataField), &envelope); err != nil {
		return nil, fmt.Errorf("invalid form data envelope: %w", err)
	}

	if len(envelope.Maps) == 0 {
		return envelope.JSON, nil
	}

	// Parse the JSON portion as a generic tree so we can inject blob
	// data at the positions described by maps.
	var tree any
	if len(envelope.JSON) > 0 {
		if err := json.Unmarshal(envelope.JSON, &tree); err != nil {
			return nil, fmt.Errorf("invalid JSON in form data: %w", err)
		}
	}

	// Wrap in a container to handle root-level blobs (empty path),
	// same pattern as the reference: const ref = { data: json }.
	container := map[string]any{"$": tree}

	for i, pathRaw := range envelope.Maps {
		file, header, err := r.FormFile(strconv.Itoa(i))
		if err != nil {
			return nil, fmt.Errorf("missing blob field %d: %w", i, err)
		}
		data, err := io.ReadAll(file)
		file.Close()
		if err != nil {
			return nil, fmt.Errorf("failed to read blob %d: %w", i, err)
		}

		blobValue := map[string]any{
			"name": header.Filename,
			"type": header.Header.Get("Content-Type"),
			"data": data, // []byte → base64 when re-marshaled to JSON
		}

		// Build the full path including the container key.
		fullPath := make([]any, 0, 1+len(pathRaw))
		fullPath = append(fullPath, "$")
		fullPath = append(fullPath, pathRaw...)

		if err := setTreeValue(container, fullPath, blobValue); err != nil {
			return nil, fmt.Errorf("failed to inject blob at path: %w", err)
		}
	}

	tree = container["$"]
	modified, err := json.Marshal(tree)
	if err != nil {
		return nil, fmt.Errorf("failed to re-encode JSON with blobs: %w", err)
	}
	return json.RawMessage(modified), nil
}

// isMultipart reports whether the request has a multipart content type.
func isMultipart(r *http.Request) bool {
	return strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/")
}

// setTreeValue sets a value at a path in a JSON tree parsed via json.Unmarshal.
// Path segments are strings (object keys) or float64/int (array indices).
func setTreeValue(tree any, path []any, value any) error {
	if len(path) == 0 {
		return fmt.Errorf("empty path")
	}

	current := tree
	for _, seg := range path[:len(path)-1] {
		next, err := traverseSegment(current, seg)
		if err != nil {
			return err
		}
		current = next
	}

	return assignSegment(current, path[len(path)-1], value)
}

func traverseSegment(current any, seg any) (any, error) {
	switch s := seg.(type) {
	case string:
		m, ok := current.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("expected object for key %q, got %T", s, current)
		}
		v, exists := m[s]
		if !exists {
			return nil, fmt.Errorf("key %q not found in object", s)
		}
		return v, nil
	case float64:
		return traverseIndex(current, int(s))
	case int:
		return traverseIndex(current, s)
	default:
		return nil, fmt.Errorf("unsupported path segment type %T", seg)
	}
}

func traverseIndex(current any, idx int) (any, error) {
	arr, ok := current.([]any)
	if !ok {
		return nil, fmt.Errorf("expected array for index %d, got %T", idx, current)
	}
	if idx < 0 || idx >= len(arr) {
		return nil, fmt.Errorf("index %d out of bounds (len %d)", idx, len(arr))
	}
	return arr[idx], nil
}

func assignSegment(current any, seg any, value any) error {
	switch s := seg.(type) {
	case string:
		m, ok := current.(map[string]any)
		if !ok {
			return fmt.Errorf("expected object for key %q, got %T", s, current)
		}
		m[s] = value
		return nil
	case float64:
		return assignIndex(current, int(s), value)
	case int:
		return assignIndex(current, s, value)
	default:
		return fmt.Errorf("unsupported path segment type %T", seg)
	}
}

func assignIndex(current any, idx int, value any) error {
	arr, ok := current.([]any)
	if !ok {
		return fmt.Errorf("expected array for index %d, got %T", idx, current)
	}
	if idx < 0 || idx >= len(arr) {
		return fmt.Errorf("index %d out of bounds (len %d)", idx, len(arr))
	}
	arr[idx] = value
	return nil
}
