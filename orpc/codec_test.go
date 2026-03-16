package orpc

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestEncodeSuccessCollectsNestedTimeMeta(t *testing.T) {
	t1 := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	t2 := t1.Add(time.Hour)
	t3 := t2.Add(time.Hour)
	t4 := t3.Add(time.Hour)

	type Item struct {
		When time.Time `json:"when"`
	}
	type Output struct {
		CreatedAt time.Time            `json:"createdAt"`
		Nested    *Item                `json:"nested,omitempty"`
		Items     []Item               `json:"items"`
		ByName    map[string]time.Time `json:"byName"`
		Optional  *time.Time           `json:"optional,omitempty"`
	}

	optional := t4
	out := Output{
		CreatedAt: t1,
		Nested:    &Item{When: t2},
		Items:     []Item{{When: t2}, {When: t3}},
		ByName:    map[string]time.Time{"alpha": t3, "beta": t4},
		Optional:  &optional,
	}

	encoded, err := encodeSuccess(out)
	if err != nil {
		t.Fatalf("encodeSuccess failed: %v", err)
	}

	var p rpcPayload
	if err := json.Unmarshal(encoded, &p); err != nil {
		t.Fatalf("unmarshal payload failed: %v", err)
	}

	want := map[string]bool{
		"1|createdAt":    true,
		"1|nested|when":  true,
		"1|items|0|when": true,
		"1|items|1|when": true,
		"1|byName|alpha": true,
		"1|byName|beta":  true,
		"1|optional":     true,
	}

	if len(p.Meta) != len(want) {
		t.Fatalf("meta length = %d, want %d; meta=%v", len(p.Meta), len(want), p.Meta)
	}

	for _, m := range p.Meta {
		k := metaKey(m)
		if !want[k] {
			t.Fatalf("unexpected meta entry %q in %v", k, p.Meta)
		}
	}
}

func TestEncodeSuccessTopLevelTimeMeta(t *testing.T) {
	v := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

	encoded, err := encodeSuccess(v)
	if err != nil {
		t.Fatalf("encodeSuccess failed: %v", err)
	}

	var p rpcPayload
	if err := json.Unmarshal(encoded, &p); err != nil {
		t.Fatalf("unmarshal payload failed: %v", err)
	}

	if len(p.Meta) != 1 {
		t.Fatalf("meta length = %d, want 1; meta=%v", len(p.Meta), p.Meta)
	}
	if got := metaKey(p.Meta[0]); got != "1" {
		t.Fatalf("meta key = %q, want 1", got)
	}
}

func metaKey(m metaEntry) string {
	parts := make([]string, 0, len(m))
	for _, seg := range m {
		switch v := seg.(type) {
		case float64:
			parts = append(parts, jsonNumberString(v))
		case string:
			parts = append(parts, v)
		default:
			parts = append(parts, "?")
		}
	}
	return strings.Join(parts, "|")
}

func jsonNumberString(v float64) string {
	if v == float64(int64(v)) {
		return strconv.Itoa(int(v))
	}
	return "?"
}
