package validationcontract

import (
	"time"
)

type CompositeMapMinimum struct {
	Values map[string]string `json:"values" validate:"min=2"`
}

type CompositeArrayGreaterThan struct {
	Values []string `json:"values" validate:"gt=1"`
}

type CompositeMapKeys struct {
	Values map[string]string `json:"values" validate:"dive,keys,email,endkeys,required"`
}

type CompositeAnonymousNested struct {
	Nested struct {
		Email string `json:"email" validate:"required,email"`
	} `json:"nested"`
}

type CompositeStringGreaterThan struct {
	A string `json:"a"`
	B string `json:"b" validate:"gtfield=A"`
}

type CompositeSliceEqual struct {
	A []int `json:"a"`
	B []int `json:"b" validate:"eqfield=A"`
}

type CompositeMapEqual struct {
	A map[string]int `json:"a"`
	B map[string]int `json:"b" validate:"eqfield=A"`
}

type CompositeTimeEqual struct {
	A time.Time `json:"a"`
	B time.Time `json:"b" validate:"eqfield=A"`
}

type CrossStringEqual struct {
	A string `json:"a"`
	B string `json:"b" validate:"eqfield=A"`
}

type CrossStringNotEqual struct {
	A string `json:"a"`
	B string `json:"b" validate:"nefield=A"`
}

type CrossStringGreaterEqual struct {
	A string `json:"a"`
	B string `json:"b" validate:"gtefield=A"`
}

type CrossStringLess struct {
	A string `json:"a"`
	B string `json:"b" validate:"ltfield=A"`
}

type CrossStringLessEqual struct {
	A string `json:"a"`
	B string `json:"b" validate:"ltefield=A"`
}

type CrossSliceNotEqual struct {
	A []int `json:"a"`
	B []int `json:"b" validate:"nefield=A"`
}

type CrossOptionalSliceEqual struct {
	A []int `json:"a,omitempty"`
	B []int `json:"b,omitempty" validate:"eqfield=A"`
}

type CrossOptionalMapEqual struct {
	A map[string]int `json:"a,omitempty"`
	B map[string]int `json:"b,omitempty" validate:"eqfield=A"`
}

type CrossArrayEqual struct {
	A [2]int `json:"a"`
	B [2]int `json:"b" validate:"eqfield=A"`
}

type CrossByteSliceEqual struct {
	A []byte `json:"a"`
	B []byte `json:"b" validate:"eqfield=A"`
}

type CrossSliceOrdered struct {
	A []int    `json:"a"`
	B []string `json:"b" validate:"gtfield=A"`
}

type CrossBoolEqual struct {
	A bool `json:"a"`
	B bool `json:"b" validate:"eqfield=A"`
}

type CrossDifferentKindsEqual struct {
	A int   `json:"a"`
	B int64 `json:"b" validate:"eqfield=A"`
}

type CrossDifferentKindsNotEqual struct {
	A int   `json:"a"`
	B int64 `json:"b" validate:"nefield=A"`
}

type CrossFloat32Equal struct {
	A float32 `json:"a"`
	B float32 `json:"b" validate:"eqfield=A"`
}

type CrossPointerEqual struct {
	A *int `json:"a,omitempty"`
	B *int `json:"b,omitempty" validate:"eqfield=A"`
}

type CrossPointerNotEqual struct {
	A *int `json:"a,omitempty"`
	B *int `json:"b,omitempty" validate:"nefield=A"`
}

type CrossPointerOmitEqual struct {
	A *int `json:"a,omitempty"`
	B *int `json:"b,omitempty" validate:"omitempty,eqfield=A"`
}

type CrossOmitBefore struct {
	A int `json:"a"`
	B int `json:"b,omitempty" validate:"omitempty,gtfield=A"`
}

type CrossOmitAfter struct {
	A int `json:"a"`
	B int `json:"b,omitempty" validate:"gtfield=A,omitempty"`
}

type CrossMissingEqual struct {
	A int `json:"a"`
	B int `json:"b" validate:"eqfield=a"`
}

type CrossMissingNotEqual struct {
	A int `json:"a"`
	B int `json:"b" validate:"nefield=a"`
}

type CrossTimeNotEqual struct {
	A time.Time `json:"a"`
	B time.Time `json:"b" validate:"nefield=A"`
}

type CrossTimeGreater struct {
	A time.Time `json:"a"`
	B time.Time `json:"b" validate:"gtfield=A"`
}

type CrossTimeGreaterEqual struct {
	A time.Time `json:"a"`
	B time.Time `json:"b" validate:"gtefield=A"`
}

type CrossTimeLess struct {
	A time.Time `json:"a"`
	B time.Time `json:"b" validate:"ltfield=A"`
}

type CrossTimeLessEqual struct {
	A time.Time `json:"a"`
	B time.Time `json:"b" validate:"ltefield=A"`
}

type CrossTimeOptionalEqual struct {
	A time.Time `json:"a,omitempty"`
	B time.Time `json:"b,omitempty" validate:"omitempty,eqfield=A"`
}

type CrossJSONStringGreater struct {
	A int `json:"a,string"`
	B int `json:"b,string" validate:"gtfield=A"`
}

type CrossJSONStringEqual struct {
	A string `json:"a,string"`
	B string `json:"b,string" validate:"eqfield=A"`
}

type CrossStructValue struct {
	X int `json:"x"`
}

type CrossStructEqual struct {
	A CrossStructValue `json:"a"`
	B CrossStructValue `json:"b" validate:"eqfield=A"`
}

type CrossORFields struct {
	A string `json:"a"`
	B string `json:"b"`
	V string `json:"v" validate:"eqfield=A|eqfield=B"`
}

type CrossORMixed struct {
	A string `json:"a"`
	V string `json:"v" validate:"email|eqfield=A"`
}

type CrossORReverseScalar struct {
	A int `json:"a"`
	V int `json:"v" validate:"eqfield=A|eq=0"`
}

type CrossOROmit struct {
	A int `json:"a"`
	V int `json:"v,omitempty" validate:"omitempty,gtfield=A|eq=-1"`
}

type CrossORBase struct {
	A int `json:"a"`
	B int `json:"b" validate:"eqfield=A|eq=0"`
}

type CrossOROptionalBase struct {
	*CrossORBase `tstype:",extends"`
}

type CrossORQuoted struct {
	A int64 `json:"a,string"`
	V int64 `json:"v,string" validate:"eqfield=A|eq=9007199254740993"`
}

type CrossQuotedInt64Equal struct {
	A int64 `json:"a,string"`
	B int64 `json:"b,string" validate:"eqfield=A"`
}

type CrossQuotedInt64Greater struct {
	A int64 `json:"a,string"`
	B int64 `json:"b,string" validate:"gtfield=A"`
}

type CrossQuotedMixedEqual struct {
	A int64 `json:"a"`
	B int64 `json:"b,string" validate:"eqfield=A"`
}

type CrossArrayOmitEqual struct {
	A [1]int `json:"a"`
	B [2]int `json:"b" validate:"omitempty,eqfield=A"`
}

type CrossTimeOptionalStrictEqual struct {
	A time.Time `json:"a,omitempty"`
	B time.Time `json:"b,omitempty" validate:"eqfield=A"`
}

type CrossTimeNamedValues map[string]struct {
	A time.Time `json:"a"`
	B time.Time `json:"b" validate:"eqfield=A"`
}

type CrossTimeNamedContainer struct {
	V CrossTimeNamedValues `json:"v" validate:"dive"`
}

// Omission is intentionally supplied by validate alone. Adding JSON omitempty
// would mask the distinction between public type and validation optionality.
type CrossOmitString struct {
	A string `json:"a"`
	B string `json:"b" validate:"omitempty,eqfield=A"`
}

type CrossOmitNumber struct {
	A int `json:"a"`
	B int `json:"b" validate:"omitempty,eqfield=A"`
}

type CrossOmitTarget struct {
	A int `json:"a" validate:"omitempty"`
	B int `json:"b" validate:"eqfield=A"`
}
