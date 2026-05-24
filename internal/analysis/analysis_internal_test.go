package analysis

import (
	"go/ast"
	"go/token"
	"go/types"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

func loadAnalysisTestPackage(t *testing.T, name string) *packages.Package {
	t.Helper()
	_, thisFile, _, _ := runtime.Caller(0)
	dir := filepath.Join(filepath.Dir(thisFile), "testdata", name)
	pkgs, err := packages.Load(&packages.Config{
		Mode: packages.NeedName | packages.NeedTypes | packages.NeedSyntax | packages.NeedTypesInfo | packages.NeedImports | packages.NeedDeps,
		Dir:  dir,
	}, ".")
	if err != nil {
		t.Fatal(err)
	}
	if len(pkgs) != 1 {
		t.Fatalf("loaded %d packages, want 1", len(pkgs))
	}
	for _, err := range pkgs[0].Errors {
		t.Fatal(err)
	}
	return pkgs[0]
}

func TestResolveFuncCallVariants(t *testing.T) {
	pkg := loadAnalysisTestPackage(t, "outputparser")
	var mustQuery, outputParser ast.Expr
	for _, file := range pkg.Syntax {
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			name, pkgPath := resolveFuncCall(call.Fun, pkg.TypesInfo)
			if name == "MustQuery" && pkgPath == trpcgoPkgPath && mustQuery == nil {
				mustQuery = call.Fun
			}
			if name == "OutputParser" && pkgPath == trpcgoPkgPath && outputParser == nil {
				outputParser = call.Fun
			}
			return true
		})
	}

	for _, tt := range []struct {
		name string
		expr ast.Expr
	}{
		{"selector", mustQuery},
		{"generic selector", outputParser},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if tt.expr == nil {
				t.Fatal("missing expression")
			}
			name, pkgPath := resolveFuncCall(tt.expr, pkg.TypesInfo)
			if pkgPath != trpcgoPkgPath || name == "" {
				t.Fatalf("resolveFuncCall = (%q, %q), want trpcgo function", name, pkgPath)
			}
		})
	}

	ident := ast.NewIdent("local")
	info := &types.Info{Uses: map[*ast.Ident]types.Object{
		ident: types.NewFunc(token.NoPos, types.NewPackage("example.com/local", "local"), "local", types.NewSignatureType(nil, nil, nil, nil, nil, false)),
	}}
	name, pkgPath := resolveFuncCall(ident, info)
	if name != "local" || pkgPath != "example.com/local" {
		t.Fatalf("resolveFuncCall ident = (%q, %q)", name, pkgPath)
	}
}

func TestExprTypeFallbacks(t *testing.T) {
	basic := types.Typ[types.String]
	fromTypes := ast.NewIdent("fromTypes")
	fromIdent := ast.NewIdent("fromIdent")
	fromSelector := &ast.SelectorExpr{X: ast.NewIdent("pkg"), Sel: ast.NewIdent("Value")}
	info := &types.Info{
		Types: map[ast.Expr]types.TypeAndValue{fromTypes: {Type: basic}},
		Uses: map[*ast.Ident]types.Object{
			fromIdent:       types.NewVar(token.NoPos, nil, "fromIdent", types.Typ[types.Int]),
			fromSelector.Sel: types.NewVar(token.NoPos, nil, "Value", types.Typ[types.Bool]),
		},
	}

	if got := exprType(info, fromTypes); got != basic {
		t.Fatalf("exprType from Types = %v, want %v", got, basic)
	}
	if got := exprType(info, fromIdent); got != types.Typ[types.Int] {
		t.Fatalf("exprType ident = %v, want int", got)
	}
	if got := exprType(info, fromSelector); got != types.Typ[types.Bool] {
		t.Fatalf("exprType selector = %v, want bool", got)
	}
	if got := exprType(info, ast.NewIdent("missing")); got != nil {
		t.Fatalf("exprType missing = %v, want nil", got)
	}
}

func TestOutputParserResultTypeVariants(t *testing.T) {
	pkg := loadAnalysisTestPackage(t, "outputparser")
	wantSuffixes := map[string]string{
		"IndexListExpr": ".PublicUser",
		"SelectorExpr":  ".PublicUser",
	}
	found := map[string]bool{}
	for _, file := range pkg.Syntax {
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			name, pkgPath := resolveFuncCall(call.Fun, pkg.TypesInfo)
			if name != "OutputParser" || pkgPath != trpcgoPkgPath {
				return true
			}
			kind := "SelectorExpr"
			if _, ok := call.Fun.(*ast.IndexListExpr); ok {
				kind = "IndexListExpr"
			}
			if found[kind] {
				return true
			}
			got := outputParserResultType(call, pkg.TypesInfo)
			if got == nil || !strings.HasSuffix(got.String(), wantSuffixes[kind]) {
				t.Fatalf("%s outputParserResultType = %v, want suffix %q", kind, got, wantSuffixes[kind])
			}
			found[kind] = true
			return true
		})
	}
	for kind := range wantSuffixes {
		if !found[kind] {
			t.Fatalf("did not find %s OutputParser call", kind)
		}
	}
}
