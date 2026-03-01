package trpcgo

import (
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
