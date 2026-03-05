package analysis_test

import (
	"go/types"
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

func isUnknownOutputType(t types.Type) bool {
	if t == nil {
		return false
	}
	switch t.String() {
	case "any", "interface{}":
		return true
	default:
		return false
	}
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

func TestAnalyzeOutputParser(t *testing.T) {
	dir := testdataDir("outputparser")
	result, err := analysis.Analyze([]string{"."}, dir)
	if err != nil {
		t.Fatal(err)
	}

	byPath := make(map[string]analysis.Procedure)
	for _, p := range result.Procedures {
		byPath[p.Path] = p
	}

	if len(byPath) != 20 {
		t.Fatalf("got %d procedures, want 20", len(byPath))
	}

	t.Run("explicit type args override output type", func(t *testing.T) {
		p := byPath["user.get"]
		if p.OutputType == nil {
			t.Fatal("user.get has nil OutputType")
		}
		if !strings.HasSuffix(p.OutputType.String(), ".PublicUser") {
			t.Errorf("user.get OutputType = %q, want PublicUser", p.OutputType)
		}
	})

	t.Run("inferred type args override output type", func(t *testing.T) {
		p := byPath["user.list"]
		if p.OutputType == nil {
			t.Fatal("user.list has nil OutputType")
		}
		if !strings.HasSuffix(p.OutputType.String(), ".PublicUser") {
			t.Errorf("user.list OutputType = %q, want PublicUser", p.OutputType)
		}
	})

	t.Run("no output parser keeps handler output type", func(t *testing.T) {
		p := byPath["user.noparser"]
		if p.OutputType == nil {
			t.Fatal("user.noparser has nil OutputType")
		}
		if !strings.HasSuffix(p.OutputType.String(), ".User") {
			t.Errorf("user.noparser OutputType = %q, want User", p.OutputType)
		}
	})

	t.Run("last output parser wins", func(t *testing.T) {
		p := byPath["user.lastwins"]
		if p.OutputType == nil {
			t.Fatal("user.lastwins has nil OutputType")
		}
		if !strings.HasSuffix(p.OutputType.String(), ".PublicUser") {
			t.Errorf("user.lastwins OutputType = %q, want PublicUser (last wins)", p.OutputType)
		}
	})

	t.Run("OutputParser nested inside Procedure call", func(t *testing.T) {
		p := byPath["user.procedure"]
		if p.OutputType == nil {
			t.Fatal("user.procedure has nil OutputType")
		}
		if !strings.HasSuffix(p.OutputType.String(), ".PublicUser") {
			t.Errorf("user.procedure OutputType = %q, want PublicUser", p.OutputType)
		}
	})

	t.Run("OutputParser in builder chain", func(t *testing.T) {
		p := byPath["user.chain"]
		if p.OutputType == nil {
			t.Fatal("user.chain has nil OutputType")
		}
		if !strings.HasSuffix(p.OutputType.String(), ".PublicUser") {
			t.Errorf("user.chain OutputType = %q, want PublicUser", p.OutputType)
		}
	})

	t.Run("pre-bound option variable", func(t *testing.T) {
		p := byPath["user.prebound"]
		if p.OutputType == nil {
			t.Fatal("user.prebound has nil OutputType")
		}
		if !strings.HasSuffix(p.OutputType.String(), ".PublicUser") {
			t.Errorf("user.prebound OutputType = %q, want PublicUser", p.OutputType)
		}
	})

	t.Run("OutputParser via With method", func(t *testing.T) {
		p := byPath["user.with"]
		if p.OutputType == nil {
			t.Fatal("user.with has nil OutputType")
		}
		if !strings.HasSuffix(p.OutputType.String(), ".PublicUser") {
			t.Errorf("user.with OutputType = %q, want PublicUser", p.OutputType)
		}
	})

	t.Run("pre-bound builder via With method", func(t *testing.T) {
		p := byPath["user.withprebound"]
		if p.OutputType == nil {
			t.Fatal("user.withprebound has nil OutputType")
		}
		if !strings.HasSuffix(p.OutputType.String(), ".PublicUser") {
			t.Errorf("user.withprebound OutputType = %q, want PublicUser", p.OutputType)
		}
	})

	t.Run("last output parser wins inside With", func(t *testing.T) {
		p := byPath["user.withlastwins"]
		if p.OutputType == nil {
			t.Fatal("user.withlastwins has nil OutputType")
		}
		if !strings.HasSuffix(p.OutputType.String(), ".PublicUser") {
			t.Errorf("user.withlastwins OutputType = %q, want PublicUser", p.OutputType)
		}
	})

	t.Run("OutputParser in builder method arg", func(t *testing.T) {
		p := byPath["user.withmethod"]
		if p.OutputType == nil {
			t.Fatal("user.withmethod has nil OutputType")
		}
		if !isUnknownOutputType(p.OutputType) {
			t.Errorf("user.withmethod OutputType = %q, want any/interface{}", p.OutputType)
		}
	})

	t.Run("direct untyped option falls back to unknown", func(t *testing.T) {
		p := byPath["user.withoption"]
		if p.OutputType == nil {
			t.Fatal("user.withoption has nil OutputType")
		}
		if !isUnknownOutputType(p.OutputType) {
			t.Errorf("user.withoption OutputType = %q, want any/interface{}", p.OutputType)
		}
	})

	t.Run("later untyped parser overrides earlier typed parser", func(t *testing.T) {
		p := byPath["user.typedthenuntyped"]
		if p.OutputType == nil {
			t.Fatal("user.typedthenuntyped has nil OutputType")
		}
		if !isUnknownOutputType(p.OutputType) {
			t.Errorf("user.typedthenuntyped OutputType = %q, want any/interface{}", p.OutputType)
		}
	})

	t.Run("later typed parser overrides earlier untyped parser", func(t *testing.T) {
		p := byPath["user.untypedthentyped"]
		if p.OutputType == nil {
			t.Fatal("user.untypedthentyped has nil OutputType")
		}
		if !strings.HasSuffix(p.OutputType.String(), ".PublicUser") {
			t.Errorf("user.untypedthentyped OutputType = %q, want PublicUser", p.OutputType)
		}
	})

	t.Run("later nil untyped parser clears earlier typed parser", func(t *testing.T) {
		p := byPath["user.typedthennil"]
		if p.OutputType == nil {
			t.Fatal("user.typedthennil has nil OutputType")
		}
		if !strings.HasSuffix(p.OutputType.String(), ".User") {
			t.Errorf("user.typedthennil OutputType = %q, want User", p.OutputType)
		}
	})

	t.Run("later nil-parser variable clears earlier typed parser", func(t *testing.T) {
		p := byPath["user.typedthennilvar"]
		if p.OutputType == nil {
			t.Fatal("user.typedthennilvar has nil OutputType")
		}
		if !strings.HasSuffix(p.OutputType.String(), ".User") {
			t.Errorf("user.typedthennilvar OutputType = %q, want User", p.OutputType)
		}
	})

	t.Run("pre-bound builder variable", func(t *testing.T) {
		p := byPath["user.varbuilder"]
		if p.OutputType == nil {
			t.Fatal("user.varbuilder has nil OutputType")
		}
		if !isUnknownOutputType(p.OutputType) {
			t.Errorf("user.varbuilder OutputType = %q, want any/interface{}", p.OutputType)
		}
	})

	t.Run("builder nil untyped parser clears earlier typed parser", func(t *testing.T) {
		p := byPath["user.buildertypedthennil"]
		if p.OutputType == nil {
			t.Fatal("user.buildertypedthennil has nil OutputType")
		}
		if !strings.HasSuffix(p.OutputType.String(), ".User") {
			t.Errorf("user.buildertypedthennil OutputType = %q, want User", p.OutputType)
		}
	})

	t.Run("very indirect typed builder chain stays typed", func(t *testing.T) {
		p := byPath["user.deepwith"]
		if p.OutputType == nil {
			t.Fatal("user.deepwith has nil OutputType")
		}
		if !strings.HasSuffix(p.OutputType.String(), ".PublicUser") {
			t.Errorf("user.deepwith OutputType = %q, want PublicUser", p.OutputType)
		}
	})

	t.Run("very indirect nil parser chain still clears", func(t *testing.T) {
		p := byPath["user.deepnilclear"]
		if p.OutputType == nil {
			t.Fatal("user.deepnilclear has nil OutputType")
		}
		if !strings.HasSuffix(p.OutputType.String(), ".User") {
			t.Errorf("user.deepnilclear OutputType = %q, want User", p.OutputType)
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
