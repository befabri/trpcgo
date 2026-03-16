package orpc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"testing"

	trpcgo "github.com/befabri/trpcgo"
)

type testBlob struct {
	filename string
	mimeType string
	data     []byte
}

// buildMultipartRequest creates an HTTP request with the oRPC FormData envelope.
// Uses CreatePart (not CreateFormFile) to preserve the blob's actual MIME type.
func buildMultipartRequest(t *testing.T, dataJSON string, blobs map[string]testBlob) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)

	if err := mw.WriteField("data", dataJSON); err != nil {
		t.Fatal(err)
	}
	for key, blob := range blobs {
		ct := blob.mimeType
		if ct == "" {
			ct = "application/octet-stream"
		}
		h := textproto.MIMEHeader{
			"Content-Disposition": {fmt.Sprintf(`form-data; name="%s"; filename="%s"`, key, blob.filename)},
			"Content-Type":        {ct},
		}
		part, err := mw.CreatePart(h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(blob.data); err != nil {
			t.Fatal(err)
		}
	}
	mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/upload", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return req
}

func TestDecodeFormDataSingleBlob(t *testing.T) {
	type Input struct {
		Name   string     `json:"name"`
		Avatar trpcgo.Blob `json:"avatar"`
	}

	dataJSON := `{"json":{"name":"alice","avatar":{}},"meta":[],"maps":[["avatar"]]}`
	blobs := map[string]testBlob{
		"0": {"photo.png", "image/png", []byte("png-bytes")},
	}

	req := buildMultipartRequest(t, dataJSON, blobs)
	raw, err := decodeFormData(req, 0)
	if err != nil {
		t.Fatal(err)
	}

	var input Input
	if err := json.Unmarshal(raw, &input); err != nil {
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
	if string(input.Avatar.Bytes()) != "png-bytes" {
		t.Errorf("Avatar data = %q, want %q", input.Avatar.Bytes(), "png-bytes")
	}
}

func TestDecodeFormDataMultipleBlobs(t *testing.T) {
	type Input struct {
		Photo  trpcgo.Blob `json:"photo"`
		Resume trpcgo.Blob `json:"resume"`
	}

	dataJSON := `{"json":{"photo":{},"resume":{}},"meta":[],"maps":[["photo"],["resume"]]}`
	blobs := map[string]testBlob{
		"0": {"face.jpg", "image/jpeg", []byte("jpeg-data")},
		"1": {"cv.pdf", "application/pdf", []byte("pdf-data")},
	}

	req := buildMultipartRequest(t, dataJSON, blobs)
	raw, err := decodeFormData(req, 0)
	if err != nil {
		t.Fatal(err)
	}

	var input Input
	if err := json.Unmarshal(raw, &input); err != nil {
		t.Fatal(err)
	}

	if input.Photo.Name != "face.jpg" {
		t.Errorf("Photo.Name = %q", input.Photo.Name)
	}
	if input.Photo.Type != "image/jpeg" {
		t.Errorf("Photo.Type = %q, want %q", input.Photo.Type, "image/jpeg")
	}
	if string(input.Photo.Bytes()) != "jpeg-data" {
		t.Errorf("Photo data = %q", input.Photo.Bytes())
	}
	if input.Resume.Name != "cv.pdf" {
		t.Errorf("Resume.Name = %q", input.Resume.Name)
	}
	if input.Resume.Type != "application/pdf" {
		t.Errorf("Resume.Type = %q, want %q", input.Resume.Type, "application/pdf")
	}
	if string(input.Resume.Bytes()) != "pdf-data" {
		t.Errorf("Resume data = %q", input.Resume.Bytes())
	}
}

func TestDecodeFormDataNestedBlob(t *testing.T) {
	type Profile struct {
		Avatar trpcgo.Blob `json:"avatar"`
	}
	type Input struct {
		User    string  `json:"user"`
		Profile Profile `json:"profile"`
	}

	dataJSON := `{"json":{"user":"bob","profile":{"avatar":{}}},"meta":[],"maps":[["profile","avatar"]]}`
	blobs := map[string]testBlob{
		"0": {"avatar.png", "image/png", []byte("nested-blob")},
	}

	req := buildMultipartRequest(t, dataJSON, blobs)
	raw, err := decodeFormData(req, 0)
	if err != nil {
		t.Fatal(err)
	}

	var input Input
	if err := json.Unmarshal(raw, &input); err != nil {
		t.Fatal(err)
	}

	if input.User != "bob" {
		t.Errorf("User = %q", input.User)
	}
	if string(input.Profile.Avatar.Bytes()) != "nested-blob" {
		t.Errorf("Profile.Avatar data = %q", input.Profile.Avatar.Bytes())
	}
}

func TestDecodeFormDataBlobInArray(t *testing.T) {
	type Input struct {
		Files []trpcgo.Blob `json:"files"`
	}

	// Array with two blobs: maps paths use integer indices.
	dataJSON := `{"json":{"files":[{},{}]},"meta":[],"maps":[["files",0],["files",1]]}`
	blobs := map[string]testBlob{
		"0": {"a.txt", "text/plain", []byte("file-a")},
		"1": {"b.txt", "text/plain", []byte("file-b")},
	}

	req := buildMultipartRequest(t, dataJSON, blobs)
	raw, err := decodeFormData(req, 0)
	if err != nil {
		t.Fatal(err)
	}

	var input Input
	if err := json.Unmarshal(raw, &input); err != nil {
		t.Fatal(err)
	}

	if len(input.Files) != 2 {
		t.Fatalf("Files len = %d, want 2", len(input.Files))
	}
	if string(input.Files[0].Bytes()) != "file-a" {
		t.Errorf("Files[0] data = %q", input.Files[0].Bytes())
	}
	if string(input.Files[1].Bytes()) != "file-b" {
		t.Errorf("Files[1] data = %q", input.Files[1].Bytes())
	}
}

func TestDecodeFormDataNoBlobs(t *testing.T) {
	dataJSON := `{"json":{"name":"alice"},"meta":[],"maps":[]}`

	req := buildMultipartRequest(t, dataJSON, nil)
	raw, err := decodeFormData(req, 0)
	if err != nil {
		t.Fatal(err)
	}

	var obj map[string]string
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatal(err)
	}
	if obj["name"] != "alice" {
		t.Errorf("name = %q, want %q", obj["name"], "alice")
	}
}

func TestDecodeFormDataRootBlob(t *testing.T) {
	// Root-level blob: maps = [[]] (empty path = the entire input is the blob).
	dataJSON := `{"json":{},"meta":[],"maps":[[]]}`
	blobs := map[string]testBlob{
		"0": {"file.bin", "application/octet-stream", []byte("raw-bytes")},
	}

	req := buildMultipartRequest(t, dataJSON, blobs)
	raw, err := decodeFormData(req, 0)
	if err != nil {
		t.Fatal(err)
	}

	var b trpcgo.Blob
	if err := json.Unmarshal(raw, &b); err != nil {
		t.Fatal(err)
	}
	if string(b.Bytes()) != "raw-bytes" {
		t.Errorf("root blob data = %q, want %q", b.Bytes(), "raw-bytes")
	}
}

func TestHandlerFormDataIntegration(t *testing.T) {
	type UploadInput struct {
		Name string     `json:"name"`
		File trpcgo.Blob `json:"file"`
	}
	type UploadOutput struct {
		FileName string `json:"fileName"`
		FileSize int    `json:"fileSize"`
	}

	r := trpcgo.NewRouter()
	trpcgo.MustMutation(r, "upload", func(_ context.Context, input UploadInput) (UploadOutput, error) {
		return UploadOutput{
			FileName: input.File.Name,
			FileSize: input.File.Len(),
		}, nil
	})

	h := NewHandler(r, "/api/")

	// Build multipart request.
	dataJSON := `{"json":{"name":"test","file":{}},"meta":[],"maps":[["file"]]}`
	blobs := map[string]testBlob{
		"0": {"report.pdf", "application/pdf", []byte("pdf-content-here")},
	}

	req := buildMultipartRequest(t, dataJSON, blobs)
	req.URL.Path = "/api/upload"
	req.RequestURI = "/api/upload"
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", rec.Code, rec.Body.String())
	}

	// Parse oRPC response envelope.
	var resp struct {
		JSON json.RawMessage `json:"json"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v. body: %s", err, rec.Body.String())
	}

	var output UploadOutput
	if err := json.Unmarshal(resp.JSON, &output); err != nil {
		t.Fatalf("failed to parse output: %v", err)
	}

	if output.FileName != "report.pdf" {
		t.Errorf("FileName = %q, want %q", output.FileName, "report.pdf")
	}
	expected := len("pdf-content-here")
	if output.FileSize != expected {
		t.Errorf("FileSize = %d, want %d", output.FileSize, expected)
	}
}

func TestSetTreeValue(t *testing.T) {
	tests := []struct {
		name  string
		tree  any
		path  []any
		value any
		check func(any) error
	}{
		{
			name:  "simple key",
			tree:  map[string]any{"a": nil},
			path:  []any{"a"},
			value: "hello",
			check: func(t any) error {
				if t.(map[string]any)["a"] != "hello" {
					return fmt.Errorf("got %v", t)
				}
				return nil
			},
		},
		{
			name:  "nested key",
			tree:  map[string]any{"a": map[string]any{"b": nil}},
			path:  []any{"a", "b"},
			value: 42,
			check: func(t any) error {
				if t.(map[string]any)["a"].(map[string]any)["b"] != 42 {
					return fmt.Errorf("got %v", t)
				}
				return nil
			},
		},
		{
			name:  "array index",
			tree:  map[string]any{"arr": []any{nil, nil}},
			path:  []any{"arr", float64(1)},
			value: "second",
			check: func(t any) error {
				arr := t.(map[string]any)["arr"].([]any)
				if arr[1] != "second" {
					return fmt.Errorf("got %v", arr)
				}
				return nil
			},
		},
		{
			name:  "int index",
			tree:  map[string]any{"arr": []any{nil}},
			path:  []any{"arr", 0},
			value: "first",
			check: func(t any) error {
				arr := t.(map[string]any)["arr"].([]any)
				if arr[0] != "first" {
					return fmt.Errorf("got %v", arr)
				}
				return nil
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := setTreeValue(tt.tree, tt.path, tt.value); err != nil {
				t.Fatal(err)
			}
			if err := tt.check(tt.tree); err != nil {
				t.Error(err)
			}
		})
	}
}

func TestSetTreeValueErrors(t *testing.T) {
	if err := setTreeValue(nil, []any{}, "v"); err == nil {
		t.Error("empty path should error")
	}
	if err := setTreeValue("not-a-map", []any{"k"}, "v"); err == nil {
		t.Error("non-object should error for string key")
	}
	if err := setTreeValue([]any{}, []any{float64(5)}, "v"); err == nil {
		t.Error("out of bounds should error")
	}
}

// --- Robustness tests ---

func TestDecodeFormDataMaxBodySize(t *testing.T) {
	// Build a multipart body that exceeds a small limit.
	dataJSON := `{"json":{"file":{}},"meta":[],"maps":[["file"]]}`
	blobs := map[string]testBlob{
		"0": {"big.bin", "application/octet-stream", bytes.Repeat([]byte("x"), 1024)},
	}

	req := buildMultipartRequest(t, dataJSON, blobs)
	// Set a body size limit smaller than the actual body.
	_, err := decodeFormData(req, 100)
	if err == nil {
		t.Fatal("expected error for body exceeding max size")
	}
}

func TestDecodeFormDataEmptyMultipartBody(t *testing.T) {
	// Valid multipart Content-Type but zero parts.
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	mw.Close() // writes only the closing boundary

	req := httptest.NewRequest(http.MethodPost, "/upload", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())

	raw, err := decodeFormData(req, 0)
	if err != nil {
		t.Fatalf("empty multipart should not error: %v", err)
	}
	if raw != nil {
		t.Errorf("expected nil raw for empty multipart, got %s", raw)
	}
}

func TestDecodeFormDataInvalidEnvelopeJSON(t *testing.T) {
	// "data" field contains invalid JSON.
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	mw.WriteField("data", `{not valid json`)
	mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/upload", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())

	_, err := decodeFormData(req, 0)
	if err == nil {
		t.Fatal("expected error for invalid envelope JSON")
	}
}

func TestDecodeFormDataMissingBlobField(t *testing.T) {
	// maps references blob "0" but no blob field is present.
	dataJSON := `{"json":{"file":{}},"meta":[],"maps":[["file"]]}`

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	mw.WriteField("data", dataJSON)
	// Deliberately omit blob field "0".
	mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/upload", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())

	_, err := decodeFormData(req, 0)
	if err == nil {
		t.Fatal("expected error for missing blob field")
	}
}

func TestDecodeFormDataInvalidMapPath(t *testing.T) {
	// maps points to a path that doesn't exist in the JSON tree.
	dataJSON := `{"json":{"name":"alice"},"meta":[],"maps":[["nonexistent","deep","path"]]}`
	blobs := map[string]testBlob{
		"0": {"f.bin", "application/octet-stream", []byte("data")},
	}

	req := buildMultipartRequest(t, dataJSON, blobs)
	_, err := decodeFormData(req, 0)
	if err == nil {
		t.Fatal("expected error for invalid map path")
	}
}

func TestDecodeFormDataMalformedMultipart(t *testing.T) {
	// Body claims multipart but has garbage content.
	body := bytes.NewReader([]byte("this is not multipart data"))
	req := httptest.NewRequest(http.MethodPost, "/upload", body)
	req.Header.Set("Content-Type", "multipart/form-data; boundary=nonexistent")

	_, err := decodeFormData(req, 0)
	if err == nil {
		t.Fatal("expected error for malformed multipart body")
	}
}

func TestDecodeFormDataTruncatedEnvelope(t *testing.T) {
	// "data" field contains truncated JSON.
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	mw.WriteField("data", `{"json":{"name":"ali`)
	mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/upload", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())

	_, err := decodeFormData(req, 0)
	if err == nil {
		t.Fatal("expected error for truncated envelope JSON")
	}
}

func TestDecodeFormDataWrongMapTypes(t *testing.T) {
	// maps contains a non-array value (wrong structure).
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	mw.WriteField("data", `{"json":{},"meta":[],"maps":"not-an-array"}`)
	mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/upload", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())

	_, err := decodeFormData(req, 0)
	if err == nil {
		t.Fatal("expected error for maps as string instead of array")
	}
}

func TestHandlerFormDataMaxBodySize(t *testing.T) {
	// Integration test: handler rejects multipart that exceeds WithMaxBodySize.
	r := trpcgo.NewRouter(trpcgo.WithMaxBodySize(100))
	trpcgo.MustMutation(r, "upload", func(_ context.Context, input struct {
		File trpcgo.Blob `json:"file"`
	}) (string, error) {
		return "ok", nil
	})

	h := NewHandler(r, "/api/")

	dataJSON := `{"json":{"file":{}},"meta":[],"maps":[["file"]]}`
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	mw.WriteField("data", dataJSON)
	part, _ := mw.CreateFormFile("0", "big.bin")
	part.Write(bytes.Repeat([]byte("x"), 1024))
	mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/upload", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code == http.StatusOK {
		t.Fatalf("expected non-200 for oversized multipart, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandlerNonMultipartContentType(t *testing.T) {
	// Verify that a non-multipart POST still works (no regression).
	r := trpcgo.NewRouter()
	trpcgo.MustMutation(r, "echo", func(_ context.Context, input struct {
		Msg string `json:"msg"`
	}) (string, error) {
		return input.Msg, nil
	})

	h := NewHandler(r, "/api/")

	body := []byte(`{"json":{"msg":"hello"},"meta":[]}`)
	req := httptest.NewRequest(http.MethodPost, "/api/echo", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", rec.Code, rec.Body.String())
	}
}

func TestDecodeFormDataBinaryData(t *testing.T) {
	// Real binary content: null bytes, high bytes, non-UTF8.
	binaryData := []byte{0x00, 0x01, 0xFF, 0xFE, 0x89, 0x50, 0x4E, 0x47} // PNG-ish header

	dataJSON := `{"json":{"file":{}},"meta":[],"maps":[["file"]]}`
	blobs := map[string]testBlob{
		"0": {"image.png", "image/png", binaryData},
	}

	req := buildMultipartRequest(t, dataJSON, blobs)
	raw, err := decodeFormData(req, 0)
	if err != nil {
		t.Fatal(err)
	}

	type Input struct {
		File trpcgo.Blob `json:"file"`
	}
	var input Input
	if err := json.Unmarshal(raw, &input); err != nil {
		t.Fatal(err)
	}

	got := input.File.Bytes()
	if !bytes.Equal(got, binaryData) {
		t.Errorf("binary data mismatch: got %x, want %x", got, binaryData)
	}
}

func TestDecodeFormDataPointerBlob(t *testing.T) {
	type Input struct {
		Name string      `json:"name"`
		File *trpcgo.Blob `json:"file"`
	}

	dataJSON := `{"json":{"name":"test","file":{}},"meta":[],"maps":[["file"]]}`
	blobs := map[string]testBlob{
		"0": {"doc.txt", "text/plain", []byte("content")},
	}

	req := buildMultipartRequest(t, dataJSON, blobs)
	raw, err := decodeFormData(req, 0)
	if err != nil {
		t.Fatal(err)
	}

	var input Input
	if err := json.Unmarshal(raw, &input); err != nil {
		t.Fatal(err)
	}

	if input.File == nil {
		t.Fatal("*Blob should not be nil")
	}
	if input.File.Name != "doc.txt" {
		t.Errorf("File.Name = %q, want %q", input.File.Name, "doc.txt")
	}
	if string(input.File.Bytes()) != "content" {
		t.Errorf("File data = %q, want %q", input.File.Bytes(), "content")
	}
}

func TestDecodeFormDataPointerBlobNil(t *testing.T) {
	// JSON has null for the blob field, no maps entry — pointer stays nil.
	type Input struct {
		Name string      `json:"name"`
		File *trpcgo.Blob `json:"file"`
	}

	dataJSON := `{"json":{"name":"test","file":null},"meta":[],"maps":[]}`
	req := buildMultipartRequest(t, dataJSON, nil)
	raw, err := decodeFormData(req, 0)
	if err != nil {
		t.Fatal(err)
	}

	var input Input
	if err := json.Unmarshal(raw, &input); err != nil {
		t.Fatal(err)
	}

	if input.File != nil {
		t.Errorf("expected nil *Blob when no file provided, got %+v", input.File)
	}
}

func TestHandlerFormDataMIMEType(t *testing.T) {
	// End-to-end: verify MIME type survives through the full pipeline.
	type UploadInput struct {
		File trpcgo.Blob `json:"file"`
	}

	r := trpcgo.NewRouter()
	trpcgo.MustMutation(r, "upload", func(_ context.Context, input UploadInput) (string, error) {
		return input.File.Type, nil
	})

	h := NewHandler(r, "/api/")

	dataJSON := `{"json":{"file":{}},"meta":[],"maps":[["file"]]}`
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	mw.WriteField("data", dataJSON)
	hdr := textproto.MIMEHeader{
		"Content-Disposition": {`form-data; name="0"; filename="photo.jpg"`},
		"Content-Type":        {"image/jpeg"},
	}
	part, _ := mw.CreatePart(hdr)
	part.Write([]byte("jpeg-data"))
	mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/upload", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		JSON json.RawMessage `json:"json"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	var mimeType string
	json.Unmarshal(resp.JSON, &mimeType)

	if mimeType != "image/jpeg" {
		t.Errorf("MIME type = %q, want %q", mimeType, "image/jpeg")
	}
}

func TestHandlerFormDataMultipleBlobs(t *testing.T) {
	type Input struct {
		Photo  trpcgo.Blob `json:"photo"`
		Resume trpcgo.Blob `json:"resume"`
	}
	type Output struct {
		PhotoName  string `json:"photoName"`
		PhotoSize  int    `json:"photoSize"`
		ResumeName string `json:"resumeName"`
		ResumeSize int    `json:"resumeSize"`
	}

	r := trpcgo.NewRouter()
	trpcgo.MustMutation(r, "upload", func(_ context.Context, input Input) (Output, error) {
		return Output{
			PhotoName:  input.Photo.Name,
			PhotoSize:  input.Photo.Len(),
			ResumeName: input.Resume.Name,
			ResumeSize: input.Resume.Len(),
		}, nil
	})

	h := NewHandler(r, "/api/")

	dataJSON := `{"json":{"photo":{},"resume":{}},"meta":[],"maps":[["photo"],["resume"]]}`
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	mw.WriteField("data", dataJSON)
	for _, b := range []struct {
		key, name, ct string
		data          []byte
	}{
		{"0", "face.jpg", "image/jpeg", []byte("jpeg-data")},
		{"1", "cv.pdf", "application/pdf", []byte("pdf-data-longer")},
	} {
		hdr := textproto.MIMEHeader{
			"Content-Disposition": {fmt.Sprintf(`form-data; name="%s"; filename="%s"`, b.key, b.name)},
			"Content-Type":        {b.ct},
		}
		part, _ := mw.CreatePart(hdr)
		part.Write(b.data)
	}
	mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/upload", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		JSON json.RawMessage `json:"json"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	var output Output
	json.Unmarshal(resp.JSON, &output)

	if output.PhotoName != "face.jpg" {
		t.Errorf("PhotoName = %q", output.PhotoName)
	}
	if output.PhotoSize != len("jpeg-data") {
		t.Errorf("PhotoSize = %d, want %d", output.PhotoSize, len("jpeg-data"))
	}
	if output.ResumeName != "cv.pdf" {
		t.Errorf("ResumeName = %q", output.ResumeName)
	}
	if output.ResumeSize != len("pdf-data-longer") {
		t.Errorf("ResumeSize = %d, want %d", output.ResumeSize, len("pdf-data-longer"))
	}
}
