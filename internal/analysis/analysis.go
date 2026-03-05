package analysis

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"strings"

	"github.com/befabri/trpcgo/internal/typemap"
	"golang.org/x/tools/go/packages"
)

const trpcgoPkgPath = "github.com/befabri/trpcgo"

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
		varDefs := buildVarDefs(pkg.Syntax, pkg.TypesInfo)
		for _, file := range pkg.Syntax {
			ast.Inspect(file, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				if proc, ok := extractProcedure(call, pkg.TypesInfo, varDefs); ok {
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
					key := typemap.TypeID(named.Obj())
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
				key := typemap.TypeID(obj)
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
	// Must* variants — identical signature and semantics; panic instead of returning error.
	"MustQuery":         {procType: "query", hasInput: true},
	"MustVoidQuery":     {procType: "query", hasInput: false},
	"MustMutation":      {procType: "mutation", hasInput: true},
	"MustVoidMutation":  {procType: "mutation", hasInput: false},
	"MustSubscribe":     {procType: "subscription", hasInput: true, isStream: true},
	"MustVoidSubscribe": {procType: "subscription", hasInput: false, isStream: true},
}

func extractProcedure(call *ast.CallExpr, info *types.Info, varDefs map[types.Object]ast.Expr) (Procedure, bool) {
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

	// OutputParser[O, P] overrides the output type with P. Untyped
	// WithOutputParser falls back to any because the post-parse type is unknown.
	parserInfo := extractOutputParserInfo(call.Args[3:], info, varDefs)
	if parserInfo.active {
		if parserInfo.lastWasUntyped {
			outputType = anyType()
		} else if parserInfo.lastTypedOverride != nil {
			outputType = parserInfo.lastTypedOverride
		}
	}

	return Procedure{
		Path:       path,
		Type:       fi.procType,
		InputType:  inputType,
		OutputType: outputType,
	}, true
}

type outputParserInfo struct {
	active            bool
	lastTypedOverride types.Type
	lastWasUntyped    bool
}

type walkState struct {
	exprs map[ast.Expr]bool
	objs  map[types.Object]bool
}

func newWalkState() *walkState {
	return &walkState{
		exprs: make(map[ast.Expr]bool),
		objs:  make(map[types.Object]bool),
	}
}

func (s *walkState) enterExpr(expr ast.Expr) bool {
	if expr == nil {
		return false
	}
	if s.exprs[expr] {
		return false
	}
	s.exprs[expr] = true
	return true
}

func (s *walkState) leaveExpr(expr ast.Expr) {
	if expr == nil {
		return
	}
	delete(s.exprs, expr)
}

func (s *walkState) enterObj(obj types.Object) bool {
	if obj == nil {
		return false
	}
	if s.objs[obj] {
		return false
	}
	s.objs[obj] = true
	return true
}

func (s *walkState) leaveObj(obj types.Object) {
	if obj == nil {
		return
	}
	delete(s.objs, obj)
}

// extractOutputParserInfo scans option arguments in runtime application order.
// The last parser wins: typed OutputParser[O, P] sets P, while untyped
// WithOutputParser forces a fallback to any.
func extractOutputParserInfo(args []ast.Expr, info *types.Info, varDefs map[types.Object]ast.Expr) outputParserInfo {
	var out outputParserInfo
	state := newWalkState()
	for _, arg := range args {
		scanOptionExpr(arg, info, varDefs, &out, state)
	}
	return out
}

// outputParserResultType extracts P from a confirmed OutputParser call expression,
// using types.Info.Instances to handle both explicit [O, P] and inferred type args.
func outputParserResultType(call *ast.CallExpr, info *types.Info) types.Type {
	funExpr := call.Fun
	switch ie := funExpr.(type) {
	case *ast.IndexListExpr:
		funExpr = ie.X
	case *ast.IndexExpr:
		funExpr = ie.X
	}
	var funcIdent *ast.Ident
	switch fn := funExpr.(type) {
	case *ast.SelectorExpr:
		funcIdent = fn.Sel
	case *ast.Ident:
		funcIdent = fn
	}
	if funcIdent == nil {
		return nil
	}
	inst, ok := info.Instances[funcIdent]
	if !ok || inst.TypeArgs == nil || inst.TypeArgs.Len() < 2 {
		return nil
	}
	return inst.TypeArgs.At(1) // P is the second type argument
}

// scanOptionExpr recursively scans a single option argument in left-to-right
// depth-first order matching the runtime apply order.
// It handles:
//   - OutputParser(fn) — typed override leaf
//   - WithOutputParser(fn) — untyped parser leaf
//   - Procedure(opts...) — recurse into each opt
//   - Builder method chains (.Use, .WithMeta, etc.) — recurse into receiver
//   - Ident references — resolve to defining expression via varDefs and recurse
func scanOptionExpr(expr ast.Expr, info *types.Info, varDefs map[types.Object]ast.Expr, out *outputParserInfo, state *walkState) {
	if !state.enterExpr(expr) {
		return
	}
	defer state.leaveExpr(expr)
	switch e := expr.(type) {
	case *ast.CallExpr:
		// Strip explicit type-argument wrappers to identify the callee.
		funExpr := e.Fun
		switch ie := funExpr.(type) {
		case *ast.IndexListExpr:
			funExpr = ie.X
		case *ast.IndexExpr:
			funExpr = ie.X
		}
		name, pkg := resolveFuncCall(funExpr, info)
		if pkg == trpcgoPkgPath {
			switch name {
			case "OutputParser":
				out.active = true
				out.lastWasUntyped = false
				if t := outputParserResultType(e, info); t != nil {
					out.lastTypedOverride = t
				}
				return
			case "WithOutputParser":
				if withOutputParserIsNilArg(e, info, varDefs) {
					out.active = false
					out.lastWasUntyped = false
					out.lastTypedOverride = nil
				} else {
					out.active = true
					out.lastWasUntyped = true
					out.lastTypedOverride = nil
				}
				return
			case "Procedure":
				for _, arg := range e.Args {
					scanOptionExpr(arg, info, varDefs, out, state)
				}
				return
			}
		}
		if sel, ok := e.Fun.(*ast.SelectorExpr); ok {
			scanOptionExpr(sel.X, info, varDefs, out, state)
			if isProcedureBuilder(sel.X, info) {
				switch sel.Sel.Name {
				case "With":
					for _, arg := range e.Args {
						scanOptionExpr(arg, info, varDefs, out, state)
					}
				case "WithOutputParser":
					if withOutputParserIsNilArg(e, info, varDefs) {
						out.active = false
						out.lastWasUntyped = false
						out.lastTypedOverride = nil
					} else {
						out.active = true
						out.lastWasUntyped = true
						out.lastTypedOverride = nil
					}
				}
			}
		}

	case *ast.Ident:
		if obj := info.Uses[e]; obj != nil {
			if !state.enterObj(obj) {
				return
			}
			defer state.leaveObj(obj)
			if rhs, ok := varDefs[obj]; ok {
				scanOptionExpr(rhs, info, varDefs, out, state)
			}
		}
	}
}

func withOutputParserIsNilArg(call *ast.CallExpr, info *types.Info, varDefs map[types.Object]ast.Expr) bool {
	if len(call.Args) == 0 {
		return false
	}
	return isDefinitelyNilExpr(call.Args[0], info, varDefs, newWalkState())
}

func isDefinitelyNilExpr(expr ast.Expr, info *types.Info, varDefs map[types.Object]ast.Expr, state *walkState) bool {
	if !state.enterExpr(expr) {
		return false
	}
	defer state.leaveExpr(expr)
	switch e := expr.(type) {
	case *ast.Ident:
		if e.Name == "nil" {
			return true
		}
		if obj := info.Uses[e]; obj != nil {
			if !state.enterObj(obj) {
				return false
			}
			defer state.leaveObj(obj)
			if rhs, ok := varDefs[obj]; ok {
				return isDefinitelyNilExpr(rhs, info, varDefs, state)
			}
		}
	case *ast.CallExpr:
		// Typed nil conversion, e.g. (func(any) (any, error))(nil).
		if len(e.Args) == 1 {
			if tv, ok := info.Types[e.Fun]; ok && tv.IsType() {
				return isDefinitelyNilExpr(e.Args[0], info, varDefs, state)
			}
		}
	}
	return false
}

func anyType() types.Type {
	if obj := types.Universe.Lookup("any"); obj != nil {
		return obj.Type()
	}
	return types.NewInterfaceType(nil, nil).Complete()
}

// isProcedureBuilder reports whether expr has type *trpcgo.ProcedureBuilder,
// used to guard the With-method arg recursion against false matches.
func isProcedureBuilder(expr ast.Expr, info *types.Info) bool {
	t := exprType(info, expr)
	if t == nil {
		return false
	}
	if ptr, ok := t.(*types.Pointer); ok {
		t = ptr.Elem()
	}
	named, ok := t.(*types.Named)
	if !ok {
		return false
	}
	obj := named.Obj()
	return obj.Pkg() != nil && obj.Pkg().Path() == trpcgoPkgPath && obj.Name() == "ProcedureBuilder"
}

// buildVarDefs builds a best-effort map from variable objects to their initializing
// expression. This enables scanOptionExpr to follow pre-bound option variables such as:
//
//	parser := trpcgo.OutputParser(fn)
//	proc   := trpcgo.Procedure(parser)
//
// Only single-assignment :=-declaration sites are tracked (info.Defs). Reassignments
// and multi-return decompositions are skipped; the walk simply finds nothing for them.
func buildVarDefs(files []*ast.File, info *types.Info) map[types.Object]ast.Expr {
	defs := make(map[types.Object]ast.Expr)
	for _, file := range files {
		ast.Inspect(file, func(n ast.Node) bool {
			switch s := n.(type) {
			case *ast.AssignStmt:
				if len(s.Lhs) == len(s.Rhs) {
					for i, lhs := range s.Lhs {
						ident, ok := lhs.(*ast.Ident)
						if !ok {
							continue
						}
						if obj := info.Defs[ident]; obj != nil {
							defs[obj] = s.Rhs[i]
						}
					}
				}
			case *ast.ValueSpec:
				for i, name := range s.Names {
					if i >= len(s.Values) {
						break
					}
					if obj := info.Defs[name]; obj != nil {
						defs[obj] = s.Values[i]
					}
				}
			}
			return true
		})
	}
	return defs
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
