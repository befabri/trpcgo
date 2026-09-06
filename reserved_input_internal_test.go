package trpcgo

import (
	"encoding/json"
	"reflect"
	"slices"
	"testing"
)

func TestReservedInputKeys(t *testing.T) {
	type cursorPage struct {
		Limit  int     `json:"limit,omitempty"`
		Cursor *string `json:"cursor,omitempty"`
	}
	type cursorDirection struct {
		Cursor    *string `json:"cursor,omitempty"`
		Direction string  `json:"direction,omitempty"`
	}
	type cursorUntaggedDirection struct {
		Cursor    *string `json:"cursor,omitempty"`
		Direction string
	}
	type cursorSkippedDirection struct {
		Cursor    *string `json:"cursor,omitempty"`
		Direction string  `json:"-"`
	}
	type cursorUnexportedDirection struct {
		Cursor *string `json:"cursor,omitempty"`
		//lint:ignore U1000 json never binds unexported fields, so this must not count as declared
		direction string
	}
	type offsetPage struct {
		Page int `json:"page,omitempty"`
	}
	type embeddedCursor struct {
		cursorPage
		Filter string `json:"filter,omitempty"`
	}
	type embeddedDirection struct {
		cursorDirection
	}
	type channel struct {
		Channel string `json:"channel"`
	}
	type channelLastEvent struct {
		Channel     string `json:"channel"`
		LastEventID string `json:"lastEventId,omitempty"`
	}
	type channelUntaggedLastEvent struct {
		LastEventId string
	}

	tests := []struct {
		name  string
		typ   ProcedureType
		input reflect.Type
		want  []string
	}{
		{"query cursor without direction", ProcedureQuery, reflect.TypeFor[cursorPage](), []string{"direction"}},
		{"query pointer input", ProcedureQuery, reflect.TypeFor[*cursorPage](), []string{"direction"}},
		{"query embedded cursor", ProcedureQuery, reflect.TypeFor[embeddedCursor](), []string{"direction"}},
		{"query direction declared", ProcedureQuery, reflect.TypeFor[cursorDirection](), nil},
		{"query direction declared by field name", ProcedureQuery, reflect.TypeFor[cursorUntaggedDirection](), nil},
		{"query direction declared through embedding", ProcedureQuery, reflect.TypeFor[embeddedDirection](), nil},
		{"query direction skipped by json", ProcedureQuery, reflect.TypeFor[cursorSkippedDirection](), []string{"direction"}},
		{"query direction unexported", ProcedureQuery, reflect.TypeFor[cursorUnexportedDirection](), []string{"direction"}},
		{"query without cursor", ProcedureQuery, reflect.TypeFor[offsetPage](), nil},
		{"query map input", ProcedureQuery, reflect.TypeFor[map[string]any](), nil},
		{"query void input", ProcedureQuery, nil, nil},
		{"mutation cursor", ProcedureMutation, reflect.TypeFor[cursorPage](), nil},
		{"subscription without lastEventId", ProcedureSubscription, reflect.TypeFor[channel](), []string{"lastEventId"}},
		{"subscription lastEventId declared", ProcedureSubscription, reflect.TypeFor[channelLastEvent](), nil},
		{"subscription lastEventId declared by field name", ProcedureSubscription, reflect.TypeFor[channelUntaggedLastEvent](), nil},
		{"subscription scalar input", ProcedureSubscription, reflect.TypeFor[string](), nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := reservedInputKeys(tt.typ, tt.input); !slices.Equal(got, tt.want) {
				t.Errorf("reservedInputKeys(%s, %v) = %v, want %v", tt.typ, tt.input, got, tt.want)
			}
		})
	}
}

func TestStripTopLevelKeys(t *testing.T) {
	keys := []string{"direction"}
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"removes middle member and keeps order", `{"a":1,"direction":"forward","b":[1,2]}`, `{"a":1,"b":[1,2]}`},
		{"removes first member", `{"direction":"forward","a":1}`, `{"a":1}`},
		{"removes last member", `{"a":1,"direction":"forward"}`, `{"a":1}`},
		{"removes only member", `{"direction":"forward"}`, `{}`},
		{"removes escaped key", `{"direction":"forward","a":1}`, `{"a":1}`},
		{"keeps duplicates", `{"a":1,"direction":"x","a":2}`, `{"a":1,"a":2}`},
		{"keeps value bytes", `{"n":1.50e2,"s":"A","direction":null}`, `{"n":1.50e2,"s":"A"}`},
		{"keeps nested member", `{"o":{"direction":"x"}}`, `{"o":{"direction":"x"}}`},
		{"keeps other case", `{"Direction":"x"}`, `{"Direction":"x"}`},
		{"drops whitespace", `{ "a" : 1 , "direction" : "x" }`, `{"a":1}`},
		{"array unchanged", `[{"direction":"x"}]`, `[{"direction":"x"}]`},
		{"scalar unchanged", `"direction"`, `"direction"`},
		{"null unchanged", `null`, `null`},
		{"empty unchanged", ``, ``},
		{"truncated unchanged", `{"direction":"x"`, `{"direction":"x"`},
		{"malformed unchanged", `{"direction":}`, `{"direction":}`},
		{"keeps trailing value", `{"direction":"x"} 1`, `{} 1`},
		{"keeps trailing object", `{"direction":"x"}{}`, `{}{}`},
		{"keeps trailing garbage", `{"direction":"x"} x`, `{} x`},
		{"keeps trailing whitespace", `{"direction":"x"}  `, `{}  `},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripTopLevelKeys(json.RawMessage(tt.raw), keys)
			if string(got) != tt.want {
				t.Errorf("stripTopLevelKeys(%s) = %s, want %s", tt.raw, got, tt.want)
			}
		})
	}
}
