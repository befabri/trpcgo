package analysis_test

import (
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/befabri/trpcgo/internal/analysis"
	"github.com/befabri/trpcgo/internal/typemap"
)

// findMeta finds a TypeMeta by the short type name (suffix after last ".").
func findMeta(metas map[string]typemap.TypeMeta, shortName string) (typemap.TypeMeta, bool) {
	for key, meta := range metas {
		// Key is "pkgpath.Name" — extract the name part.
		if idx := strings.LastIndexByte(key, '.'); idx >= 0 {
			if key[idx+1:] == shortName {
				return meta, true
			}
		} else if key == shortName {
			return meta, true
		}
	}
	return typemap.TypeMeta{}, false
}

func testdataDir(name string) string {
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(thisFile), "testdata", name)
}

func TestAnalyzeBasic(t *testing.T) {
	dir := testdataDir("basic")
	result, err := analysis.Analyze([]string{"."}, dir)
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Procedures) != 4 {
		t.Fatalf("got %d procedures, want 4", len(result.Procedures))
	}

	byPath := make(map[string]analysis.Procedure)
	for _, p := range result.Procedures {
		byPath[p.Path] = p
	}

	q := byPath["user.getById"]
	if q.Type != "query" {
		t.Errorf("user.getById type = %q, want query", q.Type)
	}
	if q.InputType == nil {
		t.Error("user.getById should have an input type")
	}
	if q.OutputType == nil {
		t.Error("user.getById should have an output type")
	}

	vq := byPath["user.listUsers"]
	if vq.Type != "query" {
		t.Errorf("user.listUsers type = %q, want query", vq.Type)
	}
	if vq.InputType != nil {
		t.Error("user.listUsers should have nil input type (void)")
	}

	m := byPath["user.createUser"]
	if m.Type != "mutation" {
		t.Errorf("user.createUser type = %q, want mutation", m.Type)
	}

	s := byPath["user.onCreated"]
	if s.Type != "subscription" {
		t.Errorf("user.onCreated type = %q, want subscription", s.Type)
	}
	if s.InputType != nil {
		t.Error("user.onCreated should have nil input type (void)")
	}
}

func TestAnalyzeEnhanced(t *testing.T) {
	dir := testdataDir("enhanced")
	result, err := analysis.Analyze([]string{"."}, dir)
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Procedures) != 3 {
		t.Fatalf("got %d procedures, want 3", len(result.Procedures))
	}

	t.Run("const group Status", func(t *testing.T) {
		meta, ok := findMeta(result.TypeMetas, "Status")
		if !ok {
			t.Fatal("no metadata for Status")
		}
		vals := make([]string, len(meta.ConstValues))
		copy(vals, meta.ConstValues)
		sort.Strings(vals)

		want := []string{`"active"`, `"banned"`, `"pending"`}
		if len(vals) != len(want) {
			t.Fatalf("got %d const values %v, want %v", len(vals), vals, want)
		}
		for i := range want {
			if vals[i] != want[i] {
				t.Errorf("const[%d] = %s, want %s", i, vals[i], want[i])
			}
		}
	})

	t.Run("const group Priority", func(t *testing.T) {
		meta, ok := findMeta(result.TypeMetas, "Priority")
		if !ok {
			t.Fatal("no metadata for Priority")
		}
		vals := make([]string, len(meta.ConstValues))
		copy(vals, meta.ConstValues)
		sort.Strings(vals)

		want := []string{"1", "2", "3"}
		if len(vals) != len(want) {
			t.Fatalf("got %d const values %v, want %v", len(vals), vals, want)
		}
		for i := range want {
			if vals[i] != want[i] {
				t.Errorf("const[%d] = %s, want %s", i, vals[i], want[i])
			}
		}
	})

	t.Run("type alias UserRole", func(t *testing.T) {
		meta, ok := findMeta(result.TypeMetas, "UserRole")
		if !ok {
			t.Fatal("no metadata for UserRole")
		}
		if !meta.IsAlias {
			t.Error("UserRole should be marked as alias")
		}
		if meta.Comment != "UserRole is a type alias for string." {
			t.Errorf("UserRole comment = %q", meta.Comment)
		}
	})

	t.Run("type comment on User", func(t *testing.T) {
		meta, ok := findMeta(result.TypeMetas, "User")
		if !ok {
			t.Fatal("no metadata for User")
		}
		if meta.Comment != "User represents a registered user." {
			t.Errorf("User comment = %q", meta.Comment)
		}
	})

	t.Run("field comments on User", func(t *testing.T) {
		meta, _ := findMeta(result.TypeMetas, "User")
		if len(meta.FieldComments) == 0 {
			t.Fatal("no field comments")
		}
		// Field 0 is ID.
		if c := meta.FieldComments[0]; c != "The unique identifier." {
			t.Errorf("field 0 comment = %q, want %q", c, "The unique identifier.")
		}
		// Field 1 is Name.
		if c := meta.FieldComments[1]; c != "Display name of the user." {
			t.Errorf("field 1 comment = %q, want %q", c, "Display name of the user.")
		}
	})

	t.Run("comment on Status type", func(t *testing.T) {
		meta, _ := findMeta(result.TypeMetas, "Status")
		if meta.Comment != "Status represents the account status." {
			t.Errorf("Status comment = %q", meta.Comment)
		}
	})

	t.Run("comment on Paginated type", func(t *testing.T) {
		meta, ok := findMeta(result.TypeMetas, "Paginated")
		if !ok {
			t.Fatal("no metadata for Paginated")
		}
		if meta.Comment != "Paginated wraps a list with pagination info." {
			t.Errorf("Paginated comment = %q", meta.Comment)
		}
	})

	t.Run("no const values for non-const types", func(t *testing.T) {
		for _, name := range []string{"User", "GetUserInput", "CreateUserInput", "Paginated"} {
			meta, _ := findMeta(result.TypeMetas, name)
			if len(meta.ConstValues) > 0 {
				t.Errorf("%s should have no const values, got %v", name, meta.ConstValues)
			}
		}
	})
}

func TestAnalyzeMustVariants(t *testing.T) {
	dir := testdataDir("must")
	result, err := analysis.Analyze([]string{"."}, dir)
	if err != nil {
		t.Fatal(err)
	}

	want := map[string]string{
		"item.get":       "query",
		"item.list":      "query",
		"item.create":    "mutation",
		"item.reset":     "mutation",
		"item.stream":    "subscription",
		"item.broadcast": "subscription",
	}

	if len(result.Procedures) != len(want) {
		t.Fatalf("got %d procedures, want %d", len(result.Procedures), len(want))
	}

	byPath := make(map[string]analysis.Procedure)
	for _, p := range result.Procedures {
		byPath[p.Path] = p
	}

	for path, wantType := range want {
		p, ok := byPath[path]
		if !ok {
			t.Errorf("missing procedure %q", path)
			continue
		}
		if p.Type != wantType {
			t.Errorf("%q type = %q, want %q", path, p.Type, wantType)
		}
	}

	// Input-bearing variants must have a non-nil InputType.
	for _, path := range []string{"item.get", "item.create", "item.stream"} {
		p, ok := byPath[path]
		if !ok {
			t.Errorf("missing procedure %q (cannot check InputType)", path)
			continue
		}
		if p.InputType == nil {
			t.Errorf("%q should have a non-nil InputType", path)
		}
	}
	// Void variants must have a nil InputType.
	for _, path := range []string{"item.list", "item.reset", "item.broadcast"} {
		p, ok := byPath[path]
		if !ok {
			t.Errorf("missing procedure %q (cannot check InputType)", path)
			continue
		}
		if p.InputType != nil {
			t.Errorf("%q should have nil InputType, got %v", path, p.InputType)
		}
	}
}
