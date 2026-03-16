package trpcgo

import (
	"encoding/json"
	"testing"
)

func TestBlobMarshalRoundTrip(t *testing.T) {
	orig := NewBlob([]byte("hello world"), "test.txt", "text/plain")

	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatal(err)
	}

	var got Blob
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}

	if got.Name != orig.Name {
		t.Errorf("Name = %q, want %q", got.Name, orig.Name)
	}
	if got.Type != orig.Type {
		t.Errorf("Type = %q, want %q", got.Type, orig.Type)
	}
	if string(got.Bytes()) != string(orig.Bytes()) {
		t.Errorf("Data = %q, want %q", got.Bytes(), orig.Bytes())
	}
}

func TestBlobUnmarshalEmpty(t *testing.T) {
	var b Blob
	if err := json.Unmarshal([]byte(`{}`), &b); err != nil {
		t.Fatal(err)
	}
	if b.Name != "" || b.Type != "" || b.Len() != 0 {
		t.Errorf("empty JSON should produce zero Blob, got %+v", b)
	}
}

func TestBlobInStruct(t *testing.T) {
	type Input struct {
		Name   string `json:"name"`
		Avatar Blob   `json:"avatar"`
	}

	raw := `{"name":"alice","avatar":{"name":"photo.png","type":"image/png","data":"aGVsbG8="}}`
	var input Input
	if err := json.Unmarshal([]byte(raw), &input); err != nil {
		t.Fatal(err)
	}

	if input.Name != "alice" {
		t.Errorf("Name = %q, want %q", input.Name, "alice")
	}
	if input.Avatar.Name != "photo.png" {
		t.Errorf("Avatar.Name = %q, want %q", input.Avatar.Name, "photo.png")
	}
	if input.Avatar.Type != "image/png" {
		t.Errorf("Avatar.Type = %q, want %q", input.Avatar.Type, "image/png")
	}
	if string(input.Avatar.Bytes()) != "hello" {
		t.Errorf("Avatar data = %q, want %q", input.Avatar.Bytes(), "hello")
	}
}

func TestBlobPointerNil(t *testing.T) {
	type Input struct {
		File *Blob `json:"file"`
	}

	var input Input
	if err := json.Unmarshal([]byte(`{"file":null}`), &input); err != nil {
		t.Fatal(err)
	}
	if input.File != nil {
		t.Error("null JSON should produce nil *Blob")
	}
}

func TestBlobLen(t *testing.T) {
	b := NewBlob([]byte("test"), "f.txt", "text/plain")
	if b.Len() != 4 {
		t.Errorf("Len() = %d, want 4", b.Len())
	}
}
