package orpc

import (
	"encoding"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
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

// collectTimeMeta scans a value for time.Time values and returns oRPC meta
// entries for all discovered paths.
func collectTimeMeta(v any) []metaEntry {
	if v == nil {
		return nil
	}
	meta := make([]metaEntry, 0)
	walkTimeMeta(reflect.ValueOf(v), nil, &meta)
	if len(meta) == 0 {
		return nil
	}
	sort.Slice(meta, func(i, j int) bool {
		return canonicalMeta(meta[i]) < canonicalMeta(meta[j])
	})
	return meta
}

func walkTimeMeta(v reflect.Value, path []any, meta *[]metaEntry) {
	if !v.IsValid() {
		return
	}

	for v.Kind() == reflect.Interface {
		if v.IsNil() {
			return
		}
		v = v.Elem()
	}

	if v.Type() == reflect.TypeFor[time.Time]() {
		entry := make(metaEntry, 1+len(path))
		entry[0] = metaTypeDate
		copy(entry[1:], path)
		*meta = append(*meta, entry)
		return
	}

	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return
		}
		walkTimeMeta(v.Elem(), path, meta)
		return
	}

	if v.Kind() == reflect.Struct {
		t := v.Type()
		for i := 0; i < t.NumField(); i++ {
			sf := t.Field(i)
			if sf.PkgPath != "" && !sf.Anonymous {
				continue
			}
			name, omitempty, skip := parseJSONFieldTag(sf)
			if skip {
				continue
			}

			fv := v.Field(i)
			if sf.Anonymous && name == "" {
				if fv.Kind() == reflect.Pointer && fv.IsNil() {
					continue
				}
				walkTimeMeta(fv, path, meta)
				continue
			}

			if name == "" {
				name = sf.Name
			}
			if omitempty && isEmptyValue(fv) {
				continue
			}

			next := append(path, name)
			walkTimeMeta(fv, next, meta)
		}
		return
	}

	if v.Kind() == reflect.Slice || v.Kind() == reflect.Array {
		for i := 0; i < v.Len(); i++ {
			next := append(path, i)
			walkTimeMeta(v.Index(i), next, meta)
		}
		return
	}

	if v.Kind() == reflect.Map {
		if v.IsNil() {
			return
		}
		keys := make([]string, 0, v.Len())
		vals := make(map[string]reflect.Value, v.Len())
		iter := v.MapRange()
		for iter.Next() {
			k, ok := jsonMapKey(iter.Key())
			if !ok {
				continue
			}
			keys = append(keys, k)
			vals[k] = iter.Value()
		}
		sort.Strings(keys)
		for _, k := range keys {
			next := append(path, k)
			walkTimeMeta(vals[k], next, meta)
		}
	}
}

func parseJSONFieldTag(sf reflect.StructField) (name string, omitempty bool, skip bool) {
	tag := sf.Tag.Get("json")
	if tag == "-" {
		return "", false, true
	}
	if tag == "" {
		return "", false, false
	}
	parts := strings.Split(tag, ",")
	name = parts[0]
	for _, p := range parts[1:] {
		if p == "omitempty" {
			omitempty = true
		}
	}
	return name, omitempty, false
}

func isEmptyValue(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Array, reflect.Map, reflect.Slice, reflect.String:
		return v.Len() == 0
	case reflect.Bool:
		return !v.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return v.Uint() == 0
	case reflect.Float32, reflect.Float64:
		return v.Float() == 0
	case reflect.Interface, reflect.Pointer:
		return v.IsNil()
	}
	return false
}

func jsonMapKey(k reflect.Value) (string, bool) {
	if !k.IsValid() {
		return "", false
	}
	if k.Kind() == reflect.String {
		return k.String(), true
	}

	if k.CanInterface() {
		if tm, ok := k.Interface().(encoding.TextMarshaler); ok {
			b, err := tm.MarshalText()
			if err != nil {
				return "", false
			}
			return string(b), true
		}
	}

	switch k.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(k.Int(), 10), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return strconv.FormatUint(k.Uint(), 10), true
	}

	return "", false
}

func canonicalMeta(m metaEntry) string {
	if len(m) == 0 {
		return ""
	}
	parts := make([]string, 0, len(m))
	parts = append(parts, fmt.Sprintf("t:%v", m[0]))
	for _, seg := range m[1:] {
		switch s := seg.(type) {
		case string:
			parts = append(parts, "s:"+s)
		case int:
			parts = append(parts, "i:"+strconv.Itoa(s))
		default:
			parts = append(parts, fmt.Sprintf("x:%v", s))
		}
	}
	return strings.Join(parts, "|")
}
