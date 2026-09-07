package validationcontract

type ArrayZeroRequiredNumbers struct {
	Values [2]int `json:"values" validate:"required"`
}

type ArrayZeroRequiredFloats struct {
	Values [1]float32 `json:"values" validate:"required"`
}

type ArrayZeroRequiredPointers struct {
	Values [2]*int `json:"values" validate:"required"`
}

type ArrayZeroRequiredMaps struct {
	Values [2]map[string]int `json:"values" validate:"required"`
}

type ArrayZeroRequiredSlices struct {
	Values [2][]int `json:"values" validate:"required"`
}

type ArrayZeroRequiredNested struct {
	Values [2][2]int `json:"values" validate:"required"`
}

type ArrayZeroRequiredEmpty struct {
	Values [0]int `json:"values" validate:"required"`
}

type ArrayZeroItem struct {
	Number  int            `json:"number"`
	Pointer *int           `json:"pointer"`
	Lookup  map[string]int `json:"lookup"`
	Nested  [2]int         `json:"nested"`
}

type ArrayZeroRequiredStructs struct {
	Values [1]ArrayZeroItem `json:"values" validate:"required"`
}

type ArrayZeroOmitNumbers struct {
	Values [2]int `json:"values" validate:"omitempty,dive,min=1"`
}

type ArrayZeroRequiredBeforeOmit struct {
	Values [2]int `json:"values" validate:"required,omitempty,dive,min=1"`
}

type ArrayZeroLengthBeforeOmit struct {
	Values [2]int `json:"values" validate:"max=1,omitempty,dive,min=1"`
}

type ArrayZeroOmitBeforeRequired struct {
	Values [2]int `json:"values" validate:"omitempty,required,dive,min=1"`
}

type ArrayZeroORRequired struct {
	Values [2]int `json:"values" validate:"required|len=2"`
}

type ArrayZeroQuotedItem struct {
	Number  int64   `json:"number,string"`
	Boolean bool    `json:"boolean,string"`
	Text    string  `json:"text,string"`
	Float   float32 `json:"float,string"`
}

type ArrayZeroRequiredQuoted struct {
	Values [1]ArrayZeroQuotedItem `json:"values" validate:"required"`
}

type ArrayZeroTaggedItem struct {
	Number int `json:"number" validate:"min=1"`
}

type ArrayZeroOmitStructs struct {
	Values [1]ArrayZeroTaggedItem `json:"values" validate:"omitempty,dive"`
}

type ArrayZeroRequiredPointerArray struct {
	Values *[2]int `json:"values" validate:"required"`
}

type ArrayZeroOmitPointerElements struct {
	Values [1]*int `json:"values" validate:"omitempty,dive,min=1"`
}

type ArrayZeroOmitRequiredPointers struct {
	Values [2]*int `json:"values" validate:"omitempty,dive,required"`
}

type ArrayZeroCrossRequired struct {
	A [2]int `json:"a,omitempty" validate:"required|eqfield=B"`
	B [3]int `json:"b,omitempty"`
}

type ArrayZeroCrossOmit struct {
	A [2]int `json:"a,omitempty" validate:"omitempty,eqfield=B"`
	B [3]int `json:"b,omitempty"`
}

type ArrayZeroCrossEqualLength struct {
	A [2]int `json:"a,omitempty" validate:"eqfield=B"`
	B [2]int `json:"b,omitempty"`
}

type ArrayZeroCrossDifferentLength struct {
	A [2]int `json:"a,omitempty" validate:"eqfield=B"`
	B [3]int `json:"b,omitempty"`
}

type ArrayZeroCrossFloatOmit struct {
	A [1]float32 `json:"a,omitempty" validate:"omitempty,eqfield=B"`
	B [2]float32 `json:"b,omitempty"`
}

type FixedArrayNumbers struct {
	Values [2]int8 `json:"values" validate:"len=2,dive,gte=0"`
}

type FixedArrayPositive struct {
	Values [2]int8 `json:"values" validate:"dive,min=1"`
}

type FixedArrayEmpty struct {
	Values [0]int8 `json:"values" validate:"len=0"`
}

type FixedArrayNested struct {
	Values [2][2]int8 `json:"values" validate:"dive,dive,gte=0"`
}

type FixedArrayBytes struct {
	Values [2]byte `json:"values"`
}

type FixedArrayItem struct {
	Name  string `json:"name"`
	Count int8   `json:"count" validate:"gte=0"`
}

type FixedArrayStructs struct {
	Values [2]FixedArrayItem `json:"values" validate:"dive"`
}

type FixedArrayNamed [2]int8

type FixedArrayAliases struct {
	Values FixedArrayNamed `json:"values" validate:"dive,gte=0"`
}

type FixedArrayNilPointers struct {
	Values [2]*int `json:"values" validate:"dive,omitnil,min=1"`
}

type FixedArrayRequiredPointers struct {
	Values [2]*int `json:"values" validate:"dive,required,omitnil,min=1"`
}

type FixedArrayMinimumPointers struct {
	Values [2]*int `json:"values" validate:"dive,min=1,omitnil"`
}

type FixedArrayNilMaps struct {
	Values [2]map[string]int `json:"values" validate:"dive,len=0"`
}

type FixedArrayRequiredMaps struct {
	Values [2]map[string]int `json:"values" validate:"dive,required"`
}

type FixedArrayNilSlices struct {
	Values [2][]int `json:"values" validate:"dive,max=1,dive,min=1"`
}

type FixedArrayNilBytes struct {
	Values [2][]byte `json:"values"`
}

type FixedArrayNilItem struct {
	Pointer *int           `json:"pointer" validate:"omitnil,min=1"`
	Items   []int          `json:"items" validate:"max=1,dive,min=1"`
	Lookup  map[string]int `json:"lookup" validate:"max=1,dive,min=1"`
	Bytes   []byte         `json:"bytes"`
}

type FixedArrayNilStructs struct {
	Values [2]FixedArrayNilItem `json:"values" validate:"dive"`
}

type FixedArrayRequiredItem struct {
	Items []int `json:"items" validate:"required"`
}

type FixedArrayRequiredStructs struct {
	Values [1]FixedArrayRequiredItem `json:"values" validate:"dive"`
}

type FixedArrayNilNode struct {
	Value    int                 `json:"value" validate:"gte=0"`
	Next     *FixedArrayNilNode  `json:"next" validate:"omitnil"`
	Children []FixedArrayNilNode `json:"children" validate:"dive"`
}

type FixedArrayNilRecursive struct {
	Values [1]FixedArrayNilNode `json:"values" validate:"dive"`
}

type FixedArrayNilSlice []int

type FixedArrayNilAliases struct {
	Values [2]FixedArrayNilSlice `json:"values" validate:"dive,max=1,dive,min=1"`
}

type FixedArrayEmbeddedBase struct {
	Name  string `json:"name" validate:"required"`
	Count int    `json:"count" validate:"gte=0"`
	Items []int  `json:"items" validate:"dive,min=1"`
}

type FixedArrayEmbeddedItem struct {
	*FixedArrayEmbeddedBase
	Label string `json:"label"`
}

type FixedArrayEmbeddedPointers struct {
	Values [1]FixedArrayEmbeddedItem `json:"values" validate:"dive"`
}

type FixedArrayEmbeddedMiddle struct {
	*FixedArrayEmbeddedBase
	Marker string `json:"marker"`
}

type FixedArrayEmbeddedOuter struct {
	*FixedArrayEmbeddedMiddle
	Label string `json:"label"`
}

type FixedArrayNestedEmbeddedPointers struct {
	Values [1]FixedArrayEmbeddedOuter `json:"values" validate:"dive"`
}

type FixedArrayOptionalTooLong struct {
	Values [2]int `json:"values,omitempty" validate:"min=3"`
}

type FixedArrayOptionalLength struct {
	Values [2]int `json:"values,omitempty" validate:"len=2"`
}

type FixedArrayOptionalDive struct {
	Values [2]int `json:"values,omitempty" validate:"dive,min=1"`
}

type FixedArrayOptionalSkipDive struct {
	Values [2]int `json:"values,omitempty" validate:"omitempty,dive,min=1"`
}

type FixedArrayOptionalInner struct {
	Name string `json:"name" validate:"required"`
}

type FixedArrayOptionalNoDive struct {
	Values [1]FixedArrayOptionalInner `json:"values,omitempty" validate:"len=1"`
}

type FixedArrayEmbeddedArrayBase struct {
	Values [2]int `json:"values,omitempty" validate:"min=3"`
	Name   string `json:"name"`
}

type FixedArrayEmbeddedOptional struct {
	*FixedArrayEmbeddedArrayBase
	Label string `json:"label"`
}

// These instantiations erase to the same public TypeScript Box<number>, but
// their fixed arrays materialize different Go zero values.
type ArrayClientBox[T any] struct {
	Values [1]T `json:"values"`
}

type ArrayClientGenericInput struct {
	Embedded ArrayClientEmbedded  `json:"embedded"`
	Pointer  ArrayClientBox[*int] `json:"pointer"`
	Scalar   ArrayClientBox[int]  `json:"scalar"`
}

type ArrayClientOverride struct {
	Values [1]*int `json:"values" tstype:"string"`
}

type ArrayClientEmbedded struct {
	ArrayClientBox[*int] `tstype:",extends"`
}
