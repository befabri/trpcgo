package analysis

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"strings"

	"github.com/trpcgo/trpcgo/internal/typemap"
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

// Result contains the full output of analysis.
type Result struct {
	Procedures []Procedure
	TypeMetas  map[string]typemap.TypeMeta
}

// Analyze loads the given Go package patterns and finds all tRPC procedure registrations,
// along with type metadata (const groups, type aliases, comments).
func Analyze(patterns []string, dir string) (*Result, error) {
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
	metas := make(map[string]typemap.TypeMeta)

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

		// Extract type metadata from AST.
		extractConstGroups(pkg, metas)
		extractTypeInfo(pkg, metas)
	}

	return &Result{Procedures: procedures, TypeMetas: metas}, nil
}

// extractConstGroups scans const declarations and groups constants by their named type.
// This enables generating TypeScript union types like: type Status = "active" | "inactive"
func extractConstGroups(pkg *packages.Package, metas map[string]typemap.TypeMeta) {
	for _, file := range pkg.Syntax {
		for _, decl := range file.Decls {
			genDecl, ok := decl.(*ast.GenDecl)
			if !ok || genDecl.Tok != token.CONST {
				continue
			}
			for _, spec := range genDecl.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for _, name := range vs.Names {
					obj := pkg.TypesInfo.Defs[name]
					if obj == nil {
						continue
					}
					c, ok := obj.(*types.Const)
					if !ok {
						continue
					}
					// Only group constants with named types.
					named, ok := c.Type().(*types.Named)
					if !ok {
						continue
					}
					// Only basic underlying types (string, int, etc.).
					if _, ok := named.Underlying().(*types.Basic); !ok {
						continue
					}
					key := named.Obj().Id()
					meta := metas[key]
					meta.ConstValues = append(meta.ConstValues, constToTSLiteral(c))
					metas[key] = meta
				}
			}
		}
	}
}

// extractTypeInfo scans type declarations for aliases and comments.
func extractTypeInfo(pkg *packages.Package, metas map[string]typemap.TypeMeta) {
	for _, file := range pkg.Syntax {
		for _, decl := range file.Decls {
			genDecl, ok := decl.(*ast.GenDecl)
			if !ok || genDecl.Tok != token.TYPE {
				continue
			}
			for _, spec := range genDecl.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				obj := pkg.TypesInfo.Defs[ts.Name]
				if obj == nil {
					continue
				}
				key := obj.Id()
				meta := metas[key]

				// Detect type aliases and defined basic types.
				if ts.Assign.IsValid() {
					// `type X = string` (alias syntax)
					meta.IsAlias = true
				} else if tn, ok := obj.(*types.TypeName); ok {
					// `type X string` (defined type) — treat as alias if basic underlying and no consts.
					if _, isBasic := tn.Type().Underlying().(*types.Basic); isBasic {
						if len(meta.ConstValues) == 0 {
							meta.IsAlias = true
						}
					}
				}

				// Extract doc comment.
				doc := ts.Doc
				if doc == nil && len(genDecl.Specs) == 1 {
					doc = genDecl.Doc
				}
				if doc != nil {
					meta.Comment = strings.TrimSpace(doc.Text())
				}

				// Extract field comments for struct types.
				if st, ok := ts.Type.(*ast.StructType); ok && st.Fields != nil {
					fieldComments := make(map[int]string)
					idx := 0
					for _, field := range st.Fields.List {
						n := len(field.Names)
						if n == 0 {
							n = 1 // embedded field
						}
						if field.Doc != nil {
							comment := strings.TrimSpace(field.Doc.Text())
							for j := 0; j < n; j++ {
								fieldComments[idx+j] = comment
							}
						}
						idx += n
					}
					if len(fieldComments) > 0 {
						meta.FieldComments = fieldComments
					}
				}

				metas[key] = meta
			}
		}
	}
}

func constToTSLiteral(c *types.Const) string {
	val := c.Val()
	if val.Kind() == constant.String {
		return fmt.Sprintf("%q", constant.StringVal(val))
	}
	return val.ExactString()
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
	funcName, pkgPath := resolveFuncCall(call.Fun, info)
	if pkgPath != trpcgoPkgPath {
		return Procedure{}, false
	}

	fi, ok := registrationFuncs[funcName]
	if !ok {
		return Procedure{}, false
	}

	if len(call.Args) < 3 {
		return Procedure{}, false
	}

	pathLit, ok := call.Args[1].(*ast.BasicLit)
	if !ok {
		return Procedure{}, false
	}
	path := pathLit.Value
	path = path[1 : len(path)-1]

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

func resolveFuncCall(expr ast.Expr, info *types.Info) (name string, pkgPath string) {
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

func exprType(info *types.Info, expr ast.Expr) types.Type {
	if tv, ok := info.Types[expr]; ok {
		return tv.Type
	}
	if ident, ok := expr.(*ast.Ident); ok {
		if obj := info.ObjectOf(ident); obj != nil {
			return obj.Type()
		}
	}
	if sel, ok := expr.(*ast.SelectorExpr); ok {
		if s, ok := info.Selections[sel]; ok {
			return s.Type()
		}
		if obj := info.ObjectOf(sel.Sel); obj != nil {
			return obj.Type()
		}
	}
	return nil
}
