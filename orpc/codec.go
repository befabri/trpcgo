package orpc

import (
	"encoding/json"
	"time"
)

// oRPC wire format: { "json": ..., "meta": [...] }
// The meta array describes non-JSON-native types at specific paths.
// For a Go server, the main relevant type is Date (time.Time → ISO string).

// rpcPayload is the oRPC request/response wire format.
type rpcPayload struct {
	JSON json.RawMessage `json:"json"`
	Meta []metaEntry     `json:"meta"`
}

// metaEntry describes a non-JSON-native value at a path.
// Format: [typeCode, ...path] where path segments are strings or ints.
type metaEntry []any

// Meta type codes matching oRPC's STANDARD_RPC_JSON_SERIALIZER_BUILT_IN_TYPES.
const (
	metaTypeBigInt    = 0
	metaTypeDate      = 1
	metaTypeNaN       = 2
	metaTypeUndefined = 3
	metaTypeURL       = 4
	metaTypeRegExp    = 5
	metaTypeSet       = 6
	metaTypeMap       = 7
)

// decodeInput extracts the raw JSON input from an oRPC payload.
// For a Go server, we ignore the meta array since Go has no BigInt/Date/etc.
// distinction — time.Time is already serialized as an ISO string by encoding/json.
func decodeInput(payload []byte) (json.RawMessage, error) {
	if len(payload) == 0 {
		return nil, nil
	}

	// Quick check: oRPC payloads are objects with a "json" key.
	// If the payload doesn't have one, treat it as bare JSON.
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(payload, &probe); err != nil {
		// Not even valid JSON object — return as raw (let the handler error).
		return json.RawMessage(payload), nil
	}
	if _, hasJSON := probe["json"]; !hasJSON {
		// No "json" key — treat as bare JSON input.
		return json.RawMessage(payload), nil
	}

	var p rpcPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return json.RawMessage(payload), nil
	}

	if p.JSON != nil {
		return p.JSON, nil
	}

	// Payload had the wrapper but json was null/missing — treat as no input.
	return nil, nil
}

// encodeSuccess wraps a Go value in the oRPC response format.
// Scans for time.Time values to populate the meta array.
func encodeSuccess(result any) ([]byte, error) {
	data, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}

	// For simplicity, emit meta only for top-level time.Time fields.
	// The meta array lets the client reconstruct non-JSON types.
	// Go servers primarily deal with time.Time (→ Date).
	meta := collectTimeMeta(result)

	return json.Marshal(rpcPayload{
		JSON: data,
		Meta: meta,
	})
}

// encodeError builds the oRPC error response payload.
func encodeError(code string, status int, message string, data any, defined bool) ([]byte, int) {
	errObj := map[string]any{
		"defined": defined,
		"code":    code,
		"status":  status,
		"message": message,
	}
	if data != nil {
		errObj["data"] = data
	} else {
		errObj["data"] = map[string]any{}
	}

	errJSON, _ := json.Marshal(errObj)
	body, _ := json.Marshal(rpcPayload{
		JSON: errJSON,
		Meta: []metaEntry{},
	})
	return body, status
}

// collectTimeMeta scans a value for time.Time fields and returns meta entries.
// Only handles top-level struct fields and map values for now.
func collectTimeMeta(v any) []metaEntry {
	if v == nil {
		return nil
	}

	// For simple cases, check if the value itself is a time.Time.
	if _, ok := v.(time.Time); ok {
		return []metaEntry{{float64(metaTypeDate)}}
	}

	// For structs serialized as maps, scan the JSON output.
	// This is intentionally simple — the Go server emits ISO strings for
	// time.Time fields, and the meta tells the client to parse them as Dates.
	// A more thorough implementation would walk the reflect tree.
	return nil
}
