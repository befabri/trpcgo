package trpcgo

import (
	"bytes"
	"encoding/json"
	"reflect"
	"slices"
	"strings"
)

// tRPC clients add fields to procedure input that the Go input struct may not
// declare. Strict input drops these reserved keys instead of rejecting the
// call, so a paginated query or a reconnecting subscription keeps working
// without the handler declaring a field it does not use.
const (
	reservedKeyCursor      = "cursor"
	reservedKeyDirection   = "direction"
	reservedKeyLastEventID = "lastEventId"
)

// reservedInputKeys returns the protocol keys a client may add to the input
// of a procedure of type typ that inputType does not declare. The keys are
// removed from the raw input before strict decoding.
//
// The TanStack React Query integration sends direction with every page of an
// infinite query, which tRPC offers for queries whose input has a cursor. The
// SSE handler merges the Last-Event-Id of a reconnecting subscription into its
// input as lastEventId.
func reservedInputKeys(typ ProcedureType, inputType reflect.Type) []string {
	st := structType(inputType)
	if st == nil {
		return nil
	}
	var keys []string
	switch typ {
	case ProcedureQuery:
		if structDeclaresJSONKey(st, reservedKeyCursor) && !structDeclaresJSONKey(st, reservedKeyDirection) {
			keys = append(keys, reservedKeyDirection)
		}
	case ProcedureSubscription:
		if !structDeclaresJSONKey(st, reservedKeyLastEventID) {
			keys = append(keys, reservedKeyLastEventID)
		}
	}
	return keys
}

// structType returns the struct type behind t, following pointers, or nil.
func structType(t reflect.Type) reflect.Type {
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == nil || t.Kind() != reflect.Struct {
		return nil
	}
	return t
}

// structDeclaresJSONKey reports whether encoding/json binds key to a field of
// st. Matching folds case, as the decoder does. Fields that json would skip
// are ignored; embedded structs contribute their promoted fields.
func structDeclaresJSONKey(st reflect.Type, key string) bool {
	for _, f := range reflect.VisibleFields(st) {
		if !f.IsExported() && !f.Anonymous {
			continue
		}
		if name, ok := jsonFieldName(f); ok && strings.EqualFold(name, key) {
			return true
		}
	}
	return false
}

// jsonFieldName returns the JSON object key for f and false when f itself is
// not a key: json skips it, or it is an embedded struct whose fields are
// promoted instead.
func jsonFieldName(f reflect.StructField) (string, bool) {
	tag := f.Tag.Get("json")
	if tag == "-" {
		return "", false
	}
	if name, _, _ := strings.Cut(tag, ","); name != "" {
		return name, true
	}
	if !f.IsExported() || (f.Anonymous && structType(f.Type) != nil) {
		return "", false
	}
	return f.Name, true
}

// stripTopLevelKeys returns raw without the top-level object members named in
// keys. Remaining members keep their order, their duplicates and their exact
// value bytes, and any bytes after the object are copied through so the strict
// decoder still rejects trailing tokens. raw is returned unchanged when it is
// not a well-formed JSON object.
func stripTopLevelKeys(raw json.RawMessage, keys []string) json.RawMessage {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return raw
	}
	var out bytes.Buffer
	out.WriteByte('{')
	kept := 0
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return raw
		}
		key, ok := tok.(string)
		if !ok {
			return raw
		}
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return raw
		}
		if slices.Contains(keys, key) {
			continue
		}
		if kept > 0 {
			out.WriteByte(',')
		}
		kept++
		name, _ := json.Marshal(key)
		out.Write(name)
		out.WriteByte(':')
		out.Write(value)
	}
	if tok, err := dec.Token(); err != nil || tok != json.Delim('}') {
		return raw
	}
	out.WriteByte('}')
	out.Write(raw[dec.InputOffset():])
	return out.Bytes()
}
