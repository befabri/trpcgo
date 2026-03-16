package trpcgo

import (
	"bytes"
	"encoding/json"
	"io"
)

// Blob represents binary data (file) in procedure inputs and outputs.
// Use this type in your input/output structs to accept files via
// oRPC's multipart/form-data wire format.
//
//	type UploadInput struct {
//	    Name   string `json:"name"`
//	    Avatar Blob   `json:"avatar"`
//	}
type Blob struct {
	Name string // filename (from Content-Disposition)
	Type string // MIME type (from Content-Type)
	data []byte
}

// NewBlob creates a Blob from raw bytes with a filename and MIME type.
func NewBlob(data []byte, name, mimeType string) Blob {
	return Blob{Name: name, Type: mimeType, data: data}
}

// Bytes returns the blob's raw data.
func (b Blob) Bytes() []byte { return b.data }

// Reader returns an io.Reader over the blob's data.
func (b Blob) Reader() io.Reader { return bytes.NewReader(b.data) }

// Len returns the size of the blob in bytes.
func (b Blob) Len() int { return len(b.data) }

// MarshalJSON encodes the blob as JSON with base64-encoded data.
func (b Blob) MarshalJSON() ([]byte, error) {
	return json.Marshal(blobJSON{
		Name: b.Name,
		Type: b.Type,
		Size: len(b.data),
		Data: b.data,
	})
}

// UnmarshalJSON decodes a blob from JSON. Handles both the enriched
// format (with base64 data) and empty objects ({}).
func (b *Blob) UnmarshalJSON(p []byte) error {
	var raw blobJSON
	if err := json.Unmarshal(p, &raw); err != nil {
		return err
	}
	b.Name = raw.Name
	b.Type = raw.Type
	b.data = raw.Data
	return nil
}

// blobJSON is the JSON representation used to marshal/unmarshal Blob values.
type blobJSON struct {
	Name string `json:"name,omitempty"`
	Type string `json:"type,omitempty"`
	Size int    `json:"size,omitempty"`
	Data []byte `json:"data,omitempty"` // base64 encoded by encoding/json
}
