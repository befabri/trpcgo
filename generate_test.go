package trpcgo_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/befabri/trpcgo"
)

type GenPage[T any] struct {
	Items []T `json:"items"`
	Total int `json:"total"`
	Page  int `json:"page"`
}

type GenPair[A any, B any] struct {
	First  A `json:"first"`
	Second B `json:"second"`
}

type GenAlpha struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type GenBeta struct {
	ID    string `json:"id"`
	Score int    `json:"score"`
}

type GenGamma struct {
	ID   string `json:"id"`
	Flag bool   `json:"flag"`
}

type WithRawJSON struct {
	Data json.RawMessage `json:"data"`
	Name string          `json:"name"`
}

type WithAnyField struct {
	Payload any    `json:"payload"`
	Label   string `json:"label"`
}

type WithFixedArray struct {
	Coords [3]float64 `json:"coords"`
	Tags   [2]string  `json:"tags"`
}

type WithIntKeyMap struct {
	Lookup map[int]string `json:"lookup"`
}

type WithDoublePtr struct {
	Value **string `json:"value"`
}

type WithBytes struct {
	Photo []byte `json:"photo"`
	Name  string `json:"name"`
}

type CgAddress struct {
	Street string `json:"street"`
	City   string `json:"city"`
}

type WithNested struct {
	Name    string    `json:"name"`
	Address CgAddress `json:"address"`
}

type TreeNode struct {
	Label    string     `json:"label"`
	Children []TreeNode `json:"children"`
}

type WithTSSkip struct {
	Visible string `json:"visible"`
	Hidden  string `json:"hidden" tstype:"-"`
}

type WithTSOverride struct {
	Raw  string `json:"raw" tstype:"Record<string, unknown>"`
	Name string `json:"name"`
}

type WithReadonly struct {
	ID   string `json:"id" tstype:",readonly"`
	Name string `json:"name"`
}

type WithRequired struct {
	Avatar *string `json:"avatar" tstype:",required"`
	Bio    *string `json:"bio"`
}

type WithJSONSkip struct {
	Public string `json:"public"`
	Secret string `json:"-"`
}

type PtrBase struct {
	ID        string `json:"id"`
	CreatedAt string `json:"createdAt"`
}

type WithPtrEmbed struct {
	*PtrBase
	Name string `json:"name"`
}

type WithUnexported struct {
	Public  string `json:"public"`
	private string //lint:ignore U1000 intentionally unexported for test
}

type AllNumerics struct {
	I   int     `json:"i"`
	I8  int8    `json:"i8"`
	I16 int16   `json:"i16"`
	I32 int32   `json:"i32"`
	I64 int64   `json:"i64"`
	U   uint    `json:"u"`
	U8  uint8   `json:"u8"`
	U16 uint16  `json:"u16"`
	U32 uint32  `json:"u32"`
	U64 uint64  `json:"u64"`
	F32 float32 `json:"f32"`
	F64 float64 `json:"f64"`
}

type WithBool struct {
	Active  bool  `json:"active"`
	Deleted *bool `json:"deleted"`
}

type CgInner struct {
	Value string `json:"value"`
}

type CgMiddle struct {
	Inner CgInner `json:"inner"`
	Count int     `json:"count"`
}

type CgOuter struct {
	Middle CgMiddle `json:"middle"`
	Name   string   `json:"name"`
}

type NestedMapConfig struct {
	Settings map[string]map[string]int `json:"settings"`
}

type KeyboardKey struct {
	Key string `json:"key"`
}

type Keyboard struct {
	Keys [][]KeyboardKey `json:"keys"`
}

type Endpoint struct {
	URL    string `json:"url"`
	Method string `json:"method"`
}

type APIConfig struct {
	Endpoints map[string]Endpoint `json:"endpoints"`
}

type BatchResult struct {
	Results []map[string]int `json:"results"`
}

type GroupedTags struct {
	Groups map[string][]string `json:"groups"`
}

type WithOmitzero struct {
	Name      string `json:"name"`
	CreatedAt string `json:"created_at,omitzero"`
}

type MultiOptional struct {
	Required    string  `json:"required"`
	OmitEmpty   string  `json:"omitEmpty,omitempty"`
	OmitZero    string  `json:"omitZero,omitzero"`
	Pointer     *string `json:"pointer"`
	PtrOmit     *string `json:"ptrOmit,omitempty"`
	PtrRequired *string `json:"ptrRequired" tstype:",required"`
}

type DeeplyNestedMap struct {
	Data map[string]map[string][]int `json:"data"`
}

type Timestamps struct {
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

type Metadata struct {
	Version int    `json:"version"`
	Source  string `json:"source"`
}

type FullEntity struct {
	Timestamps
	Metadata
	Name string `json:"name"`
}

type NoJSONTags struct {
	PublicName  string
	PublicCount int
	privateVal  string //lint:ignore U1000 intentionally unexported for test
}

type OptionalVariants struct {
	Always    string  `json:"always"`
	OmitE     string  `json:"omitE,omitempty"`
	OmitZ     string  `json:"omitZ,omitzero"`
	Ptr       *string `json:"ptr"`
	PtrOmitE  *string `json:"ptrOmitE,omitempty"`
	ForcedReq *string `json:"forcedReq" tstype:",required"`
}

type ExBase struct {
	ID string `json:"id"`
}

type ExUser struct {
	ExBase `tstype:",extends"`
	Name   string `json:"name"`
}

type ExWithPtrExtends struct {
	*ExBase `tstype:",extends"`
	Name    string `json:"name"`
}

type ExWithPtrReqExtends struct {
	*ExBase `tstype:",extends,required"`
	Name    string `json:"name"`
}

type ExAuditFields struct {
	CreatedBy string `json:"createdBy"`
	UpdatedBy string `json:"updatedBy"`
}

type ExMultiExtends struct {
	ExBase        `tstype:",extends"`
	ExAuditFields `tstype:",extends"`
	Title         string `json:"title"`
}

type WithTSDoc struct {
	Host string `json:"host" ts_doc:"The hostname to connect to"`
	Port int    `json:"port" ts_doc:"Port number (1-65535)"`
	Name string `json:"name"`
}

type WithJSONNumber struct {
	Value json.Number `json:"value"`
	Name  string      `json:"name"`
}

type ZodLoginInput struct {
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required,min=8,max=128"`
}

type ZodCreateItemInput struct {
	Name  string   `json:"name" validate:"required,min=1,max=100"`
	Tags  []string `json:"tags" validate:"max=10,dive,min=1,max=50"`
	Count int      `json:"count" validate:"gte=0,lte=1000"`
}

type ZodOptionalInput struct {
	Query  string `json:"query,omitempty"`
	Limit  *int   `json:"limit"`
	Offset int    `json:"offset"`
}

type ZodBase struct {
	ID string `json:"id" validate:"required"`
}

type ZodDerived struct {
	ZodBase `tstype:",extends"`
	Name    string `json:"name" validate:"required,min=1"`
}

type ZodMultiBase struct {
	ZodBase       `tstype:",extends"`
	ExAuditFields `tstype:",extends"`
	Title         string `json:"title" validate:"required"`
}

type ZodPtrExtends struct {
	*ZodBase `tstype:",extends"`
	Label    string `json:"label"`
}

type ZodCyclicNode struct {
	ZodBase  `tstype:",extends"`
	Children []ZodCyclicNode `json:"children"`
}

type ZodUnsupportedInput struct {
	MinVal float64 `json:"minVal" validate:"required,gte=0"`
	MaxVal float64 `json:"maxVal" validate:"required,gte=0,gtefield=MinVal"`
	Label  string  `json:"label" validate:"required,custom_check"`
}

type ZodCrossFieldInput struct {
	MinVal   int32 `json:"min_val" validate:"min=1"`
	MaxVal   int32 `json:"max_val" validate:"min=1,gtefield=MinVal"`
	StartVal int32 `json:"start_val" validate:"min=1"`
	EndVal   int32 `json:"end_val" validate:"min=1,gtefield=StartVal"`
}

type ZodCrossFieldAllOps struct {
	A int32 `json:"a"`
	B int32 `json:"b" validate:"gtefield=A"`
	C int32 `json:"c" validate:"ltefield=A"`
	D int32 `json:"d" validate:"gtfield=A"`
	E int32 `json:"e" validate:"ltfield=A"`
	F int32 `json:"f" validate:"eqfield=A"`
	G int32 `json:"g" validate:"nefield=A"`
}

type ZodOmitInput struct {
	ID     string `json:"id" zod_omit:"true"`
	Name   string `json:"name" validate:"required,min=1"`
	Active bool   `json:"active"`
}

type ZodOmitWithRefine struct {
	ID     int32 `json:"id" zod_omit:"true"`
	MinVal int32 `json:"min_val" validate:"min=1,gtefield=ID"`
	MaxVal int32 `json:"max_val" validate:"min=1,gtefield=MinVal"`
}

type ZodInt64Input struct {
	BigSigned   int64  `json:"bigSigned"`
	BigUnsigned uint64 `json:"bigUnsigned"`
	NormalInt   int32  `json:"normalInt"`
}

type ZodNewTagsInput struct {
	Host    string `json:"host" validate:"hostname"`
	Token   string `json:"token" validate:"base64url"`
	Hash    string `json:"hash" validate:"hexadecimal,min=64,max=64"`
	ID      string `json:"id" validate:"ulid"`
	Mac     string `json:"mac" validate:"mac"`
	Subnet  string `json:"subnet" validate:"cidrv4"`
	Code    string `json:"code" validate:"uppercase"`
	Website string `json:"website" validate:"startswith=https://,min=10"`
	File    string `json:"file" validate:"endswith=.json"`
	Path    string `json:"path" validate:"contains=/api/"`
}

type DriftPrivateUser struct {
	ID          string `json:"id"`
	SecretToken string `json:"secretToken"`
}

type DriftPublicUser struct {
	ID string `json:"id"`
}

func generateTS(t *testing.T, r *trpcgo.Router) string {
	t.Helper()
	dir := t.TempDir()
	out := filepath.Join(dir, "trpc.ts")
	if err := r.GenerateTS(out); err != nil {
		t.Fatalf("GenerateTS: %v", err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}
	return string(data)
}

func generateZod(t *testing.T, r *trpcgo.Router) string {
	t.Helper()
	dir := t.TempDir()
	out := filepath.Join(dir, "zod.ts")
	if err := r.GenerateZod(out); err != nil {
		t.Fatalf("GenerateZod: %v", err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}
	return string(data)
}

func countPattern(s, pattern string) int {
	re := regexp.MustCompile(pattern)
	return len(re.FindAllStringIndex(s, -1))
}

func symlinkNodeModules(t *testing.T, dir string) {
	t.Helper()
	src, err := filepath.Abs(filepath.Join("examples", "start-trpc", "web", "node_modules"))
	if err != nil {
		t.Fatalf("abs path: %v", err)
	}
	if _, err := os.Stat(src); err != nil {
		t.Skip("node_modules not installed in examples/start-trpc/web")
	}
	if err := os.Symlink(src, filepath.Join(dir, "node_modules")); err != nil {
		t.Fatalf("symlink node_modules: %v", err)
	}
}
