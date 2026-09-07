package validationcontract

import (
	"encoding/json"
	"time"
)

type EmbeddedRequiredBase struct {
	ID    string `json:"id" validate:"required"`
	Email string `json:"email" validate:"email"`
	Count int    `json:"count" validate:"min=1"`
}

type EmbeddedPartialInput struct {
	*EmbeddedRequiredBase `tstype:",extends"`
	Label                 string `json:"label"`
}

type EmbeddedFlatInput struct {
	*EmbeddedRequiredBase
	Label string `json:"label"`
}

type EmbeddedNestedBase struct {
	*EmbeddedRequiredBase `tstype:",extends"`
	Extra                 string `json:"extra" validate:"min=2"`
}

type EmbeddedNestedInput struct {
	*EmbeddedNestedBase `tstype:",extends"`
	Label               string `json:"label"`
}

// Directive fixtures pin validator's traversal semantics that the generator
// once flattened: omitzero's dereferenced zero test, rules before an omission
// tag, structonly/nostructlevel ending a field's rule list, rune-counted
// string lengths, json.Number's string kind, time.Time required, and a nil
// pointer failing dive.
type DirectiveOmitZeroSlice struct {
	Tags []string `json:"tags" validate:"omitzero,min=2"`
}

type DirectiveOmitEmptySlice struct {
	Tags []string `json:"tags" validate:"omitempty,min=2"`
}

type DirectiveOmitZeroMap struct {
	Values map[string]int `json:"values" validate:"omitzero,min=2"`
}

type DirectiveOmitZeroPointer struct {
	N *int `json:"n" validate:"omitzero,min=1"`
}

type DirectiveOmitEmptyPointer struct {
	N *int `json:"n" validate:"omitempty,min=1"`
}

type DirectiveOmitZeroString struct {
	Name string `json:"name" validate:"omitzero,min=3"`
}

type DirectivePrefixString struct {
	Name string `json:"name" validate:"required,omitempty,min=3"`
}

type DirectivePrefixNumber struct {
	Age int8 `json:"age" validate:"min=1,omitempty,max=5"`
}

type DirectivePrefixStarts struct {
	Code string `json:"code" validate:"startswith=a,omitempty,min=3"`
}

type DirectiveStructOnlyScalar struct {
	N int `json:"n" validate:"min=3,structonly,max=1"`
}

type DirectiveNoStructLevelScalar struct {
	N int `json:"n" validate:"min=3,nostructlevel,max=1"`
}

type DirectiveRuneMax struct {
	Title string `json:"title" validate:"max=5"`
}

type DirectiveRuneMin struct {
	Name string `json:"name" validate:"min=3"`
}

type DirectiveNumberOneOf struct {
	Qty json.Number `json:"qty" validate:"oneof=1 2"`
}

type DirectiveNumberMinimum struct {
	Amount json.Number `json:"amount,omitempty" validate:"min=1"`
}

type DirectiveNumberCross struct {
	A json.Number `json:"a" validate:"gtfield=B"`
	B json.Number `json:"b"`
}

type DirectiveTimeRequired struct {
	When time.Time `json:"when" validate:"required"`
}

type DirectivePointerDive struct {
	Values *[]int8 `json:"values" validate:"dive,min=1"`
}

type DirectiveOrInexpressible struct {
	Count int `json:"count" validate:"gtfield=Min|excludesall=!@"`
	Min   int `json:"min"`
}

// Cross-field targets JSON never sets keep their Go zero value.
type DirectiveHiddenUnexported struct {
	Count int `json:"count" validate:"gtefield=max"`
	max   int
}

type DirectiveHiddenSkipped struct {
	Gone   string `json:"gone" validate:"eqfield=Secret"`
	Secret string `json:"-"`
}

// validator enters struct elements only after dive, and never enters the
// struct behind structonly or nostructlevel.
type DirectiveProfile struct {
	Name string `json:"name" validate:"required"`
}

type DirectiveSliceNoDive struct {
	Items []DirectiveProfile `json:"items" validate:"min=1"`
}

type DirectiveSliceDive struct {
	Items []DirectiveProfile `json:"items" validate:"min=1,dive"`
}

type DirectiveMapNoDive struct {
	Items map[string]DirectiveProfile `json:"items" validate:"min=1"`
}

type DirectiveStructOnlyField struct {
	Profile DirectiveProfile `json:"profile" validate:"structonly"`
}

type DirectiveNoStructLevelField struct {
	Profile *DirectiveProfile `json:"profile" validate:"required,nostructlevel"`
}

type DirectiveNestedField struct {
	Profile DirectiveProfile `json:"profile"`
}
