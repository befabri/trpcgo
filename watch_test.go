package trpcgo

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAbsPath(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string // empty means check it became absolute
	}{
		{"empty", "", ""},
		{"already absolute", "/usr/local/bin", "/usr/local/bin"},
		{"relative", "gen/trpc.ts", ""}, // will be resolved to cwd + path
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := absPath(tt.input)
			if tt.want != "" {
				if got != tt.want {
					t.Errorf("absPath(%q) = %q, want %q", tt.input, got, tt.want)
				}
			} else if tt.input == "" {
				if got != "" {
					t.Errorf("absPath(%q) = %q, want empty", tt.input, got)
				}
			} else {
				// Relative input should become absolute.
				if !filepath.IsAbs(got) {
					t.Errorf("absPath(%q) = %q, expected absolute path", tt.input, got)
				}
			}
		})
	}
}

func TestWithWatchPackagesFiltersEmptyPatterns(t *testing.T) {
	r := NewRouter(WithWatchPackages("./internal/...", "", "./trpc"))

	want := []string{"./internal/...", "./trpc"}
	if len(r.opts.watchPackages) != len(want) {
		t.Fatalf("watchPackages = %v, want %v", r.opts.watchPackages, want)
	}
	for i := range want {
		if r.opts.watchPackages[i] != want[i] {
			t.Fatalf("watchPackages = %v, want %v", r.opts.watchPackages, want)
		}
	}
}

func TestWithWatchPackagesLeavesUnsetWhenOnlyEmpty(t *testing.T) {
	r := NewRouter(WithWatchPackages("", ""))
	if r.opts.watchPackages != nil {
		t.Fatalf("watchPackages = %v, want nil", r.opts.watchPackages)
	}
}

func TestWriteIfChangedCreatesAndSkipsUnchangedContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "router.ts")
	data := []byte("export type Router = {};\n")

	writeIfChanged(path, data, "types")
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(data) {
		t.Fatalf("file content = %q, want %q", got, data)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	writeIfChanged(path, data, "types")
	info2, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(info2.ModTime()) {
		t.Fatalf("unchanged content should not rewrite file: %v != %v", info.ModTime(), info2.ModTime())
	}

	updated := []byte("export type Router = { ping: string };\n")
	writeIfChanged(path, updated, "types")
	got, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(updated) {
		t.Fatalf("updated file content = %q, want %q", got, updated)
	}
}
