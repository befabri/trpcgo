package analysis

import (
	"fmt"
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/packages"
)

const trpcgoPkgPath = "github.com/trpcgo/trpcgo"

// Procedure represents a discovered tRPC procedure registration.
type Procedure struct {
	Path       string     // e.g., "user.getById"
	Type       string     // "query", "mutation", "subscription"
	InputType  types.Type // nil for void input
	OutputType types.Type
}

// Analyze loads the given Go package patterns and finds all tRPC procedure registrations.
func Analyze(patterns []string, dir string) ([]Procedure, error) {
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedTypes | packages.NeedSyntax |
			packages.NeedTypesInfo | packages.NeedImports | packages.NeedDeps,
		Dir: dir,
	}

	pkgs, err := packages.Load(cfg, patterns...)
	if err != nil {
		return nil, fmt.Errorf("loading packages: %w", err)
	}

	for _, pkg := range pkgs {
		for _, e := range pkg.Errors {
			return nil, fmt.Errorf("package %s: %s", pkg.PkgPath, e)
		}
	}

	var procedures []Procedure

	for _, pkg := range pkgs {
		for _, file := range pkg.Syntax {
			ast.Inspect(file, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				if proc, ok := extractProcedure(call, pkg.TypesInfo); ok {
					procedures = append(procedures, proc)
				}
				return true
			})
		}
	}

	return procedures, nil
}

// funcInfo describes what we know about a top-level registration function.
type funcInfo struct {
	procType string // "query", "mutation", "subscription"
	hasInput bool   // true for Query/Mutation/Subscribe, false for Void* variants
	isStream bool   // true for Subscribe/VoidSubscribe
}

// registrationFuncs maps function names to their procedure info.
var registrationFuncs = map[string]funcInfo{
	"Query":         {procType: "query", hasInput: true},
	"VoidQuery":     {procType: "query", hasInput: false},
	"Mutation":      {procType: "mutation", hasInput: true},
	"VoidMutation":  {procType: "mutation", hasInput: false},
	"Subscribe":     {procType: "subscription", hasInput: true, isStream: true},
	"VoidSubscribe": {procType: "subscription", hasInput: false, isStream: true},
}

func extractProcedure(call *ast.CallExpr, info *types.Info) (Procedure, bool) {
	// Resolve the function name, unwrapping type parameters (IndexExpr/IndexListExpr).
	funcName, pkgPath := resolveFuncCall(call.Fun, info)
	if pkgPath != trpcgoPkgPath {
		return Procedure{}, false
	}

	fi, ok := registrationFuncs[funcName]
	if !ok {
		return Procedure{}, false
	}

	// Call signature: trpcgo.Query(router, path, fn, ...middleware)
	// Need at least 3 args: router, path, fn.
	if len(call.Args) < 3 {
		return Procedure{}, false
	}

	// Second argument is the procedure path (string literal).
	pathLit, ok := call.Args[1].(*ast.BasicLit)
	if !ok {
		return Procedure{}, false
	}
	path := pathLit.Value
	path = path[1 : len(path)-1] // strip quotes

	// Third argument is the handler function.
	fnType := exprType(info, call.Args[2])
	if fnType == nil {
		return Procedure{}, false
	}
	sig, ok := fnType.(*types.Signature)
	if !ok {
		return Procedure{}, false
	}

	var inputType, outputType types.Type

	if fi.hasInput {
		// func(ctx context.Context, input I) (O, error) or
		// func(ctx context.Context, input I) (<-chan O, error)
		if sig.Params().Len() < 2 || sig.Results().Len() < 1 {
			return Procedure{}, false
		}
		inputType = sig.Params().At(1).Type()
		resultType := sig.Results().At(0).Type()
		if fi.isStream {
			chanType, ok := resultType.(*types.Chan)
			if !ok {
				return Procedure{}, false
			}
			outputType = chanType.Elem()
		} else {
			outputType = resultType
		}
	} else {
		// func(ctx context.Context) (O, error) or
		// func(ctx context.Context) (<-chan O, error)
		if sig.Results().Len() < 1 {
			return Procedure{}, false
		}
		resultType := sig.Results().At(0).Type()
		if fi.isStream {
			chanType, ok := resultType.(*types.Chan)
			if !ok {
				return Procedure{}, false
			}
			outputType = chanType.Elem()
		} else {
			outputType = resultType
		}
	}

	return Procedure{
		Path:       path,
		Type:       fi.procType,
		InputType:  inputType,
		OutputType: outputType,
	}, true
}

// resolveFuncCall extracts the function name and package path from a call expression.
// Handles:
//   - trpcgo.Query(...)            → ("Query", "github.com/trpcgo/trpcgo")
//   - trpcgo.Query[I, O](...)      → ("Query", "github.com/trpcgo/trpcgo")
//   - Query(...)                   → ("Query", <local package or dot import>)
func resolveFuncCall(expr ast.Expr, info *types.Info) (name string, pkgPath string) {
	// Unwrap type parameter expressions.
	for {
		switch e := expr.(type) {
		case *ast.IndexExpr:
			expr = e.X
			continue
		case *ast.IndexListExpr:
			expr = e.X
			continue
		}
		break
	}

	switch fn := expr.(type) {
	case *ast.SelectorExpr:
		// trpcgo.Query — check if X is a package identifier.
		ident, ok := fn.X.(*ast.Ident)
		if !ok {
			return "", ""
		}
		obj := info.ObjectOf(ident)
		if obj == nil {
			return "", ""
		}
		pkgName, ok := obj.(*types.PkgName)
		if !ok {
			return "", ""
		}
		return fn.Sel.Name, pkgName.Imported().Path()

	case *ast.Ident:
		// Direct call (dot import or same package).
		obj := info.ObjectOf(fn)
		if obj == nil {
			return "", ""
		}
		if obj.Pkg() != nil {
			return fn.Name, obj.Pkg().Path()
		}
		return fn.Name, ""
	}

	return "", ""
}

// exprType returns the Go type of an AST expression using type info.
func exprType(info *types.Info, expr ast.Expr) types.Type {
	if tv, ok := info.Types[expr]; ok {
		return tv.Type
	}
	// For simple identifiers referring to functions/variables, use ObjectOf.
	if ident, ok := expr.(*ast.Ident); ok {
		if obj := info.ObjectOf(ident); obj != nil {
			return obj.Type()
		}
	}
	// For selector expressions (e.g., svc.GetUserById), check Selections.
	if sel, ok := expr.(*ast.SelectorExpr); ok {
		if s, ok := info.Selections[sel]; ok {
			return s.Type()
		}
		// Could also be a qualified identifier.
		if obj := info.ObjectOf(sel.Sel); obj != nil {
			return obj.Type()
		}
	}
	return nil
}
