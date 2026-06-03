package typemap

import (
	"go/types"
	"strings"
	"testing"
)

func TestIsStringUnion(t *testing.T) {
	cases := []struct {
		name string
		def  TypeDef
		want bool
	}{
		{"string union", TypeDef{Kind: TypeDefUnion, UnionMembers: []string{`"a"`, `"b"`}}, true},
		{"single string union", TypeDef{Kind: TypeDefUnion, UnionMembers: []string{`"only"`}}, true},
		{"numeric union", TypeDef{Kind: TypeDefUnion, UnionMembers: []string{"1", "2"}}, false},
		{"negative numeric union", TypeDef{Kind: TypeDefUnion, UnionMembers: []string{"-1", "0"}}, false},
		{"empty union", TypeDef{Kind: TypeDefUnion, UnionMembers: nil}, false},
		{"interface", TypeDef{Kind: TypeDefInterface}, false},
		{"alias", TypeDef{Kind: TypeDefAlias, AliasOf: "string"}, false},
		{"non-union with stray members", TypeDef{Kind: TypeDefInterface, UnionMembers: []string{`"a"`}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.def.IsStringUnion(); got != tc.want {
				t.Errorf("IsStringUnion() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestCollectFieldsEmbeddedExtendsAndFlattening(t *testing.T) {
	m := NewMapper(nil)
	pkg := types.NewPackage("example.com/app", "app")

	baseFields := []*types.Var{
		types.NewField(0, pkg, "BaseID", types.Typ[types.String], false),
	}
	base := types.NewNamed(types.NewTypeName(0, pkg, "Base", nil), types.NewStruct(baseFields, []string{`json:"baseId"`}), nil)
	ext := types.NewNamed(types.NewTypeName(0, pkg, "Ext", nil), types.NewStruct([]*types.Var{
		types.NewField(0, pkg, "ExtID", types.Typ[types.String], false),
	}, []string{`json:"extId"`}), nil)

	st := types.NewStruct([]*types.Var{
		types.NewField(0, pkg, "Base", base, true),
		types.NewField(0, pkg, "Ext", types.NewPointer(ext), true),
		types.NewField(0, pkg, "Name", types.Typ[types.String], false),
	}, []string{
		``,
		`tstype:",extends"`,
		`json:"name" validate:"required,omitempty,unknown_rule=abc" ts_doc:"Display name" zod_omit:"true"`,
	})

	var fields []Field
	var extends []string
	m.collectFields(st, &fields, &extends, nil)

	if len(fields) != 2 {
		t.Fatalf("collectFields produced %d fields, want 2: %#v", len(fields), fields)
	}
	if fields[0].Name != "baseId" || fields[0].Type != "string" {
		t.Errorf("flattened embedded field = %#v, want baseId string", fields[0])
	}
	name := fields[1]
	if name.Name != "name" || name.Optional || !name.ValidateOmitempty || name.Comment != "Display name" || !name.ZodOmit {
		t.Errorf("name field metadata = %#v", name)
	}
	if len(name.UnsupportedZod) != 1 || name.UnsupportedZod[0].Tag != "unknown_rule" {
		t.Errorf("unsupported rules = %#v", name.UnsupportedZod)
	}
	if len(extends) != 1 || m.Resolve(extends[0]) != "Partial<Ext>" {
		t.Errorf("extends = %#v, want Partial<Ext>", extends)
	}
}

func TestUnionType(t *testing.T) {
	pkg := types.NewPackage("main", "main")
	statusObj := types.NewTypeName(0, pkg, "Status", nil)
	statusType := types.NewNamed(statusObj, types.Typ[types.String], nil)

	// Meta key must use TypeID (fully-qualified).
	m := NewMapper(map[string]TypeMeta{
		TypeID(statusObj): {ConstValues: []string{`"active"`, `"pending"`}},
	})

	got := m.Resolve(m.Convert(statusType))
	if got != "Status" {
		t.Errorf("Resolve(Convert(Status)) = %q, want %q", got, "Status")
	}

	defs := m.Defs()
	if len(defs) != 1 {
		t.Fatalf("got %d defs, want 1", len(defs))
	}
	if defs[0].Kind != TypeDefUnion {
		t.Errorf("Kind = %v, want TypeDefUnion", defs[0].Kind)
	}
	if len(defs[0].UnionMembers) != 2 {
		t.Errorf("UnionMembers = %v, want 2 members", defs[0].UnionMembers)
	}
}

func TestUnionTypeDedup(t *testing.T) {
	pkg := types.NewPackage("main", "main")
	obj := types.NewTypeName(0, pkg, "Status", nil)
	named := types.NewNamed(obj, types.Typ[types.String], nil)

	m := NewMapper(map[string]TypeMeta{
		TypeID(obj): {ConstValues: []string{`"a"`, `"b"`}},
	})

	// Convert twice — should only produce one def.
	m.Convert(named)
	m.Convert(named)

	if len(m.Defs()) != 1 {
		t.Errorf("got %d defs after double Convert, want 1", len(m.Defs()))
	}
}

func TestAliasType(t *testing.T) {
	pkg := types.NewPackage("main", "main")
	obj := types.NewTypeName(0, pkg, "UserRole", nil)
	alias := types.NewAlias(obj, types.Typ[types.String])

	m := NewMapper(map[string]TypeMeta{
		TypeID(obj): {IsAlias: true},
	})

	got := m.Resolve(m.Convert(alias))
	if got != "UserRole" {
		t.Errorf("Resolve(Convert(UserRole)) = %q, want %q", got, "UserRole")
	}

	defs := m.Defs()
	if len(defs) != 1 {
		t.Fatalf("got %d defs, want 1", len(defs))
	}
	if defs[0].Kind != TypeDefAlias {
		t.Errorf("Kind = %v, want TypeDefAlias", defs[0].Kind)
	}
	if defs[0].AliasOf != "string" {
		t.Errorf("AliasOf = %q, want %q", defs[0].AliasOf, "string")
	}
}

func TestAliasWithoutMetaResolvesToUnderlying(t *testing.T) {
	m := NewMapper(nil)

	pkg := types.NewPackage("main", "main")
	obj := types.NewTypeName(0, pkg, "MyStr", nil)
	alias := types.NewAlias(obj, types.Typ[types.String])

	got := m.Convert(alias)
	if got != "string" {
		t.Errorf("Convert(MyStr alias) = %q, want %q (no meta → resolve to underlying)", got, "string")
	}
	if len(m.Defs()) != 0 {
		t.Errorf("should produce no defs for unregistered alias")
	}
}

func TestCollisionRename(t *testing.T) {
	authPkg := types.NewPackage("github.com/app/auth", "auth")
	modelsPkg := types.NewPackage("github.com/app/models", "models")

	authTokenField := types.NewField(0, authPkg, "Token", types.Typ[types.String], false)
	authUser := types.NewNamed(
		types.NewTypeName(0, authPkg, "User", nil),
		types.NewStruct([]*types.Var{authTokenField}, []string{`json:"token"`}),
		nil,
	)

	modelsNameField := types.NewField(0, modelsPkg, "Name", types.Typ[types.String], false)
	modelsUser := types.NewNamed(
		types.NewTypeName(0, modelsPkg, "User", nil),
		types.NewStruct([]*types.Var{modelsNameField}, []string{`json:"name"`}),
		nil,
	)

	m := NewMapper(nil)
	m.Convert(authUser)
	m.Convert(modelsUser)

	defs := m.Defs()
	if len(defs) != 2 {
		t.Fatalf("got %d defs, want 2 (both Users should be registered)", len(defs))
	}

	names := map[string]bool{}
	for _, d := range defs {
		if names[d.Name] {
			t.Fatalf("collision not resolved: duplicate name %q", d.Name)
		}
		names[d.Name] = true
	}

	// Expect prefixed names: "AuthUser" and "ModelsUser".
	if !names["AuthUser"] {
		t.Errorf("expected AuthUser in defs, got %v", names)
	}
	if !names["ModelsUser"] {
		t.Errorf("expected ModelsUser in defs, got %v", names)
	}
}

func TestNoCollisionSameOutput(t *testing.T) {
	pkg := types.NewPackage("main", "main")
	nameField := types.NewField(0, pkg, "Name", types.Typ[types.String], false)
	user := types.NewNamed(
		types.NewTypeName(0, pkg, "User", nil),
		types.NewStruct([]*types.Var{nameField}, []string{`json:"name"`}),
		nil,
	)

	m := NewMapper(nil)
	resolved := m.Resolve(m.Convert(user))
	if resolved != "User" {
		t.Errorf("Resolve(Convert(User)) = %q, want %q (no collision → short name)", resolved, "User")
	}

	defs := m.Defs()
	if len(defs) != 1 {
		t.Fatalf("got %d defs, want 1", len(defs))
	}
	if defs[0].Name != "User" {
		t.Errorf("def Name = %q, want %q", defs[0].Name, "User")
	}
}

func TestCollisionInFieldRef(t *testing.T) {
	authPkg := types.NewPackage("github.com/app/auth", "auth")
	modelsPkg := types.NewPackage("github.com/app/models", "models")

	// auth.User struct
	authTokenField := types.NewField(0, authPkg, "Token", types.Typ[types.String], false)
	authUser := types.NewNamed(
		types.NewTypeName(0, authPkg, "User", nil),
		types.NewStruct([]*types.Var{authTokenField}, []string{`json:"token"`}),
		nil,
	)

	// models.User struct
	modelsNameField := types.NewField(0, modelsPkg, "Name", types.Typ[types.String], false)
	modelsUser := types.NewNamed(
		types.NewTypeName(0, modelsPkg, "User", nil),
		types.NewStruct([]*types.Var{modelsNameField}, []string{`json:"name"`}),
		nil,
	)

	m := NewMapper(nil)

	// Convert a slice of auth.User — should produce "AuthUser[]" after resolution.
	sliceType := types.NewSlice(authUser)
	m.Resolve(m.Convert(sliceType))

	// Also convert models.User to trigger collision.
	m.Convert(modelsUser)

	// Re-resolve after both are known.
	resolved := m.Resolve(m.Convert(sliceType))
	if resolved != "AuthUser[]" {
		t.Errorf("Resolve(Convert([]auth.User)) = %q, want %q", resolved, "AuthUser[]")
	}
}

func TestCollisionInGenericRef(t *testing.T) {
	authPkg := types.NewPackage("github.com/app/auth", "auth")
	modelsPkg := types.NewPackage("github.com/app/models", "models")

	// Two "User" structs from different packages.
	authUser := types.NewNamed(
		types.NewTypeName(0, authPkg, "User", nil),
		types.NewStruct(
			[]*types.Var{types.NewField(0, authPkg, "Token", types.Typ[types.String], false)},
			[]string{`json:"token"`},
		),
		nil,
	)
	modelsUser := types.NewNamed(
		types.NewTypeName(0, modelsPkg, "User", nil),
		types.NewStruct(
			[]*types.Var{types.NewField(0, modelsPkg, "Name", types.Typ[types.String], false)},
			[]string{`json:"name"`},
		),
		nil,
	)

	m := NewMapper(nil)

	// Convert a map containing auth.User as value type.
	mapType := types.NewMap(types.Typ[types.String], authUser)
	m.Convert(mapType)
	m.Convert(modelsUser)

	resolved := m.Resolve(m.Convert(mapType))
	if resolved != "Record<string, AuthUser>" {
		t.Errorf("Resolve(Convert(map[string]auth.User)) = %q, want %q", resolved, "Record<string, AuthUser>")
	}
}

func TestCollisionThreePackages(t *testing.T) {
	pkg1 := types.NewPackage("github.com/app/auth", "auth")
	pkg2 := types.NewPackage("github.com/app/models", "models")
	pkg3 := types.NewPackage("github.com/app/admin", "admin")

	mkUser := func(pkg *types.Package, fieldName string) *types.Named {
		return types.NewNamed(
			types.NewTypeName(0, pkg, "User", nil),
			types.NewStruct(
				[]*types.Var{types.NewField(0, pkg, fieldName, types.Typ[types.String], false)},
				[]string{`json:"` + strings.ToLower(fieldName) + `"`},
			),
			nil,
		)
	}

	m := NewMapper(nil)
	m.Convert(mkUser(pkg1, "Token"))
	m.Convert(mkUser(pkg2, "Name"))
	m.Convert(mkUser(pkg3, "Role"))

	defs := m.Defs()
	if len(defs) != 3 {
		t.Fatalf("got %d defs, want 3", len(defs))
	}

	names := map[string]bool{}
	for _, d := range defs {
		if names[d.Name] {
			t.Fatalf("collision not resolved: duplicate name %q", d.Name)
		}
		names[d.Name] = true
	}

	if !names["AuthUser"] || !names["ModelsUser"] || !names["AdminUser"] {
		t.Errorf("expected AuthUser, ModelsUser, AdminUser; got %v", names)
	}
}

func TestExtendsTokenResolution(t *testing.T) {
	pkg := types.NewPackage("github.com/app/models", "models")

	// Base struct: type Base struct { ID string `json:"id"` }
	baseIDField := types.NewField(0, pkg, "ID", types.Typ[types.String], false)
	baseStruct := types.NewStruct([]*types.Var{baseIDField}, []string{`json:"id"`})
	base := types.NewNamed(types.NewTypeName(0, pkg, "Base", nil), baseStruct, nil)

	// User struct with embedded Base using tstype:",extends"
	// type User struct { Base `tstype:",extends"`; Name string `json:"name"` }
	embeddedBase := types.NewField(0, pkg, "Base", base, true)
	nameField := types.NewField(0, pkg, "Name", types.Typ[types.String], false)
	userStruct := types.NewStruct(
		[]*types.Var{embeddedBase, nameField},
		[]string{`tstype:",extends"`, `json:"name"`},
	)
	user := types.NewNamed(types.NewTypeName(0, pkg, "User", nil), userStruct, nil)

	m := NewMapper(nil)
	m.Convert(user)
	defs := m.Defs()

	// Find User def.
	var userDef *TypeDef
	for i := range defs {
		if defs[i].Name == "User" {
			userDef = &defs[i]
			break
		}
	}
	if userDef == nil {
		t.Fatal("User def not found")
	}

	// Extends should be resolved to "Base", not a raw §...§ token.
	if len(userDef.Extends) != 1 {
		t.Fatalf("User.Extends = %v, want 1 entry", userDef.Extends)
	}
	if userDef.Extends[0] != "Base" {
		t.Errorf("User.Extends[0] = %q, want %q", userDef.Extends[0], "Base")
	}

	// User should NOT have the base field (ID) since it's extended, not flattened.
	for _, f := range userDef.Fields {
		if f.Name == "id" {
			t.Error("User should not have 'id' field — extended fields should not be flattened")
		}
	}
}
