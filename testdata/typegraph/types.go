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
