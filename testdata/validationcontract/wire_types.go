package validationcontract

import (
	"encoding/json"
	"time"
)

type WireNumbers struct {
	Tiny     int8   `json:"tiny"`
	Small    uint16 `json:"small"`
	Big      int64  `json:"big"`
	Unsigned uint64 `json:"unsigned"`
}

type WireTimestamp struct {
	At time.Time `json:"at"`
}

type WireQuotedNumber struct {
	Count int `json:"count,string"`
}

type WireInt32 struct {
	Value int32 `json:"value"`
}

type WireUint32 struct {
	Value uint32 `json:"value"`
}

type WireRuneLength struct {
	Value string `json:"value" validate:"len=1"`
}

type WireQuotedInt64 struct {
	Value int64 `json:"value,string"`
}

type WireQuotedUint64 struct {
	Value uint64 `json:"value,string"`
}

type WireQuotedInt64Oneof struct {
	Value int64 `json:"value,string" validate:"oneof=9007199254740992 9007199254740993"`
}

type WireQuotedInt64Bound struct {
	Value int64 `json:"value,string" validate:"min=9007199254740993,max=9007199254740993"`
}

type WireQuotedInt64Ordered struct {
	Value int64 `json:"value,string" validate:"min=1,omitempty"`
}

type WireQuotedInt64Omit struct {
	Value int64 `json:"value,string" validate:"omitempty,min=1"`
}

type WireQuotedBool struct {
	Value bool `json:"value,string"`
}

type WireQuotedRequiredBool struct {
	Value bool `json:"value,string" validate:"required"`
}

type WireQuotedString struct {
	Value string `json:"value,string" validate:"email,oneof=admin@example.com"`
}

// encoding/json stores a quoted json.Number payload that starts like a number
// as the Number's text, unquotes a nested string literal that must then be a
// valid number, and leaves the zero Number for the quoted null spelling.
type WireQuotedJSONNumber struct {
	Value json.Number `json:"value,string"`
}

type WireQuotedJSONNumberRules struct {
	Value json.Number `json:"value,string" validate:"required,numeric,min=2"`
}

type WireQuotedJSONNumberPointer struct {
	Value *json.Number `json:"value,string" validate:"omitempty,min=2"`
}

type WireQuotedJSONNumberOmit struct {
	Value json.Number `json:"value,string,omitempty" validate:"min=2"`
}

type DuplicateMap struct {
	Values map[int]int8 `json:"values" validate:"min=2,dive,min=1"`
}

type DuplicateDetails struct {
	Values map[int]int8 `json:"values" validate:"min=2,dive,min=1"`
	Label  string       `json:"label" validate:"min=1"`
}

type DuplicateNested struct {
	Details DuplicateDetails `json:"details"`
}

type DuplicateScalar struct {
	Value  int8         `json:"value" validate:"min=1"`
	Values map[int]int8 `json:"values,omitempty"`
}

type DuplicateEntry struct {
	A int8 `json:"a" validate:"min=1"`
	B int8 `json:"b" validate:"min=1"`
}

type DuplicateStructMap struct {
	Values map[int]DuplicateEntry `json:"values" validate:"min=1,dive"`
}

// Each map entry decodes into a fresh value, but the fields of that value
// still merge repeated keys and match case-insensitively.
type DuplicateEntryValues struct {
	Details map[string]DuplicateDetails `json:"details,omitempty" validate:"dive"`
	Lists   map[string][]DuplicateEntry `json:"lists,omitempty" validate:"dive,dive"`
}

type DuplicateQuotedFloat struct {
	Value  float64      `json:"value,string" validate:"eq=1"`
	Values map[int]int8 `json:"values,omitempty"`
}

type DuplicateQuotedFloatPointer struct {
	Value  *float64     `json:"value,string" validate:"omitnil,eq=1"`
	Values map[int]int8 `json:"values,omitempty"`
}

type DuplicateStructSlice struct {
	Values []DuplicateEntry `json:"values" validate:"min=1,dive"`
	Anchor map[int]int8     `json:"anchor,omitempty"`
}

type DuplicateScalarSlice struct {
	Values []int8       `json:"values" validate:"min=1,dive,min=1"`
	Anchor map[int]int8 `json:"anchor,omitempty"`
}
