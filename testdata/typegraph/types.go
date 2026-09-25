package typegraph

type RecursiveMap map[string]RecursiveMap
type RecursiveSlice []RecursiveSlice

type MutualMap map[string]MutualSlice
type MutualSlice []MutualMap

type RecursiveInput struct {
	Map    RecursiveMap   `json:"map"`
	Slice  RecursiveSlice `json:"slice"`
	Mutual MutualMap      `json:"mutual"`
}

type Generic[T any] struct {
	Value  T           `json:"value" validate:"required"`
	Values []T         `json:"values" validate:"dive,required"`
	Next   *Generic[T] `json:"next,omitempty"`
}

type Collection[T any] []T

type GenericInput struct {
	Text   Generic[string]  `json:"text"`
	Wide   Generic[int16]   `json:"wide"`
	Number Generic[int8]    `json:"number"`
	Items  Collection[int8] `json:"items" validate:"dive,min=1"`
}

type GenericInlineNode[T any] struct {
	Value  T `json:"value"`
	Inline struct {
		Next *GenericInlineNode[T] `json:"next,omitempty"`
	} `json:"inline"`
	Children []struct {
		Next *GenericInlineNode[T] `json:"next,omitempty"`
	} `json:"children,omitempty"`
	ByName map[string]struct {
		Next *GenericInlineNode[T] `json:"next,omitempty"`
	} `json:"byName,omitempty"`
}

type GenericInlineRecursiveInput struct {
	Narrow GenericInlineNode[int8]  `json:"narrow"`
	Wide   GenericInlineNode[int16] `json:"wide"`
}

type InlineInput struct {
	OptionalName string `json:"optionalName" validate:"omitempty,min=2"`
	Details      struct {
		Count  int8   `json:"count" validate:"min=1"`
		Name   string `json:"name" validate:"min=2"`
		Secret string `json:"secret" zod_omit:"true"`
		Low    int    `json:"low"`
		High   int    `json:"high" validate:"gtefield=Low"`
	} `json:"details"`
	Items []struct {
		Name string `json:"name" validate:"min=2"`
	} `json:"items" validate:"dive"`
	Map map[string]struct {
		Count int8 `json:"count" validate:"min=1"`
	} `json:"map" validate:"dive"`
	Next *InlineInput `json:"next,omitempty"`
}

type State string

const (
	StateReady State = "ready"
	StateDone  State = "done"
)

type Level int8

const (
	LevelLow  Level = 1
	LevelHigh Level = 2
)

type Fraction float32

const (
	FractionTenth Fraction = 0.1
	FractionHalf  Fraction = 0.5
)

type Ratio float64

const (
	RatioEighth Ratio = 0.125
	RatioOne    Ratio = 1
)

type Flag bool

const (
	FlagOn  Flag = true
	FlagOff Flag = false
)

// FlagInput places named constant types of two Go kinds where raw JSON
// decoding checks the wire kind before the schema runs.
type FlagInput struct {
	Flag  Flag  `json:"flag"`
	Level Level `json:"level"`
}

type EnumInput struct {
	State    State            `json:"state" validate:"required"`
	Sparse   map[State]int8   `json:"sparse"`
	Numeric  map[Level]string `json:"numeric"`
	Fraction Fraction         `json:"fraction,omitempty,string"`
	Ratio    Ratio            `json:"ratio,omitempty,string"`
}

type NarrowInput = Generic[int8]
type WideInput = Generic[int16]

type GenericBase[T any] struct {
	Value T `json:"value" validate:"min=1"`
}

type GenericDerived[T any] struct {
	GenericBase[T] `tstype:",extends"`
	Label          string `json:"label"`
}

type GenericExtendedInput struct {
	Narrow GenericDerived[int8]  `json:"narrow"`
	Wide   GenericDerived[int16] `json:"wide"`
}

type DiagnosticInput struct {
	Invalid    int              `json:"invalid" validate:"email"`
	ValidMap   map[string]int   `json:"validMap" validate:"dive,keys,email,endkeys,min=1"`
	NestedMaps []map[string]int `json:"nestedMaps" validate:"dive,dive,keys,email,endkeys,min=1"`
}

type RequiredOmitInput struct {
	Text    string         `json:"text,omitempty" validate:"omitempty,min=2" tstype:",required"`
	Pointer *string        `json:"pointer,omitempty" validate:"omitempty,email" tstype:",required"`
	Map     map[string]int `json:"map,omitempty" validate:"omitempty,dive,min=1" tstype:",required"`
	Details struct {
		Value string `json:"value,omitempty" validate:"omitempty,min=2" tstype:",required"`
	} `json:"details"`
	Next *RequiredOmitInput `json:"next,omitempty" validate:"omitempty"`
}

type RecursiveIntegerMap map[int8]RecursiveIntegerMap

type IntegerMapInput struct {
	Values    map[int8]int8          `json:"values"`
	Nested    map[int8]map[int8]int8 `json:"nested"`
	Recursive RecursiveIntegerMap    `json:"recursive"`
	Next      *IntegerMapInput       `json:"next,omitempty"`
}

type Replacement string

const (
	ReplacementRune Replacement = "�"
	ReplacementOK   Replacement = "ok"
)

type FloatChoice float64

const (
	FloatOne FloatChoice = 1
	FloatTwo FloatChoice = 2
)

type NarrowChoice float32

const (
	NarrowTenth NarrowChoice = 0.1
	NarrowNext  NarrowChoice = 1.0000001192092896
)

// DecodedEnumInput places named constants in plain, quoted, and map-key
// positions so decoded values are checked against the underlying scalar.
type DecodedEnumInput struct {
	Value    Replacement          `json:"value"`
	Quoted   Replacement          `json:"quoted,string"`
	Float    FloatChoice          `json:"float,string"`
	Narrow   NarrowChoice         `json:"narrow,string"`
	Sparse   map[Replacement]int8 `json:"sparse"`
	Optional *FloatChoice         `json:"optional,omitempty,string" validate:"omitnil,gt=0"`
}

// Cycles through anonymous structs pass no struct definition, so only the
// named type in the cycle can stop the mappers.
type AnonymousMenu []struct {
	Label    string        `json:"label" validate:"required"`
	Children AnonymousMenu `json:"children" validate:"dive"`
}

type AnonymousTree map[string]struct {
	Size int8          `json:"size" validate:"min=1"`
	Kids AnonymousTree `json:"kids"`
}

type AnonymousChain []*struct {
	Next AnonymousChain `json:"next,omitempty"`
}

type AnonymousList[T any] []struct {
	Value T                `json:"value"`
	Next  AnonymousList[T] `json:"next"`
}

// AnonymousLink has no definition to reference, so its recursive occurrence
// is untyped.
type AnonymousLink *struct {
	Next AnonymousLink `json:"next,omitempty"`
}

type AnonymousRecursionInput struct {
	Menu  AnonymousMenu       `json:"menu"`
	Tree  AnonymousTree       `json:"tree"`
	Chain AnonymousChain      `json:"chain"`
	List  AnonymousList[int8] `json:"list"`
	Link  AnonymousLink       `json:"link,omitempty"`
}

// Named scalars stay open, with or without constants, but oneof narrows a
// field of such a type to its values.
type OneofRole string

const (
	OneofRoleAdmin  OneofRole = "admin"
	OneofRoleEditor OneofRole = "editor"
)

type OneofLevel int8

const (
	OneofLevelLow  OneofLevel = 1
	OneofLevelHigh OneofLevel = 2
)

type OneofCode string

type OneofNamedInput struct {
	Role     OneofRole  `json:"role" validate:"oneof=admin editor"`
	Optional OneofRole  `json:"optional,omitempty" validate:"omitempty,oneof=admin editor"`
	Level    OneofLevel `json:"level" validate:"oneof=1 2"`
	Code     OneofCode  `json:"code" validate:"oneof=x y"`
	Open     OneofRole  `json:"open"`
}

// Generic map declarations keep the key constraint TypeScript needs, so every
// instantiation type-checks against the generic declaration.
type StringKeyed[K ~string, V any] map[K]V
type NumberKeyed[K ~int8 | ~int16, V any] map[K]V
type ComparableKeyed[K comparable, V any] map[K]V
type ComparableList[T comparable] []T

type Region string

type GenericMapInput struct {
	Names   StringKeyed[string, int8]     `json:"names"`
	Regions StringKeyed[Region, bool]     `json:"regions"`
	Counts  NumberKeyed[int8, string]     `json:"counts"`
	Lookup  ComparableKeyed[string, int8] `json:"lookup"`
	Ids     ComparableKeyed[int8, string] `json:"ids"`
	Listed  ComparableList[Region]        `json:"listed"`
}
