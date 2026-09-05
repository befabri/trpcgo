// Package fieldcomposition contains intentionally overlapping JSON field fixtures.
package fieldcomposition

type PromotionValue struct {
	X string `json:"x"`
}
type PromotionIntermediate struct{ PromotionValue }
type PromotionLeft struct{ PromotionIntermediate }
type PromotionRight struct{ PromotionIntermediate }
type hiddenPromotionBase struct {
	Y int `json:"y"`
}
type PromotionInput struct {
	PromotionLeft
	PromotionRight
	hiddenPromotionBase `json:"base"`
}

type CompositionRecursiveBase struct {
	Value string                    `json:"value"`
	Next  *CompositionRecursiveBase `json:"next,omitempty"`
}

type CompositionRecursiveDerived struct {
	CompositionRecursiveBase `tstype:",extends"`
	Extra                    string `json:"extra"`
}

type CompositionRecursivePointer struct {
	*CompositionRecursiveBase `tstype:",extends"`
	Extra                     string `json:"extra"`
}

type CompositionRecursiveAudit struct {
	Audit int `json:"audit"`
}

type CompositionRecursiveMulti struct {
	CompositionRecursiveBase  `tstype:",extends"`
	CompositionRecursiveAudit `tstype:",extends"`
	Extra                     string `json:"extra"`
}

type CompositionForwardBase struct {
	Children []CompositionForwardDerived `json:"children"`
}

type CompositionForwardDerived struct {
	CompositionForwardBase `tstype:",extends"`
	Value                  string `json:"value"`
}

type CompositionExtendsA struct {
	*CompositionExtendsB `tstype:",extends"`
	A                    string `json:"a"`
}

type CompositionExtendsB struct {
	*CompositionExtendsA `tstype:",extends"`
	B                    int `json:"b"`
}

type CompositionExtendsWrapper struct {
	CompositionExtendsA `tstype:",extends"`
	C                   bool `json:"c"`
}

type CompositionHiddenBase struct {
	Value string `json:"value"`
}

type CompositionHiddenDerived struct {
	CompositionHiddenBase
	Value int `json:"value" tstype:"-"`
}

type CompositionHiddenExtended struct {
	CompositionHiddenBase `tstype:",extends"`
	Value                 int `json:"value" tstype:"-"`
}

type CompositionHiddenDeep struct {
	CompositionHiddenBase
}

type CompositionHiddenEmbedding struct {
	CompositionHiddenBase `tstype:"-"`
	CompositionHiddenDeep
}

type CompositionNumericBase struct {
	Value int `json:"value"`
}

type CompositionHiddenAmbiguous struct {
	CompositionHiddenBase `tstype:"-"`
	CompositionNumericBase
}

type CompositionJSONExcluded struct {
	CompositionHiddenBase
	Value int `json:"-"`
}

type CompositionRangeBase struct {
	Min   int    `json:"min"`
	Max   int    `json:"max" validate:"gtefield=Min"`
	Label string `json:"label"`
}

type CompositionRangeDerived struct {
	CompositionRangeBase `tstype:",extends"`
	Label                int `json:"label"`
}

type CompositionRangeShadowedTarget struct {
	CompositionRangeBase `tstype:",extends"`
	Min                  string `json:"min"`
	Label                int    `json:"label"`
}

type CompositionLowerBound struct {
	Lower int `json:"lower-bound"`
}

type CompositionScopedRange struct {
	CompositionLowerBound
	Upper int    `json:"upper-bound" validate:"gtefield=Lower"`
	Label string `json:"label"`
}

type CompositionNestedRange struct {
	CompositionScopedRange `tstype:",extends"`
	Label                  int `json:"label"`
}

type CompositionCaseEnum struct {
	Value    string   `json:"value" validate:"oneof=good BAD,lowercase"`
	Upper    string   `json:"upper" validate:"uppercase,oneof=GOOD bad"`
	Bounded  string   `json:"bounded" validate:"oneof=go good toolong BAD,lowercase,min=4,max=4"`
	Optional string   `json:"optional,omitempty" validate:"omitempty,oneof=good BAD,lowercase"`
	Values   []string `json:"values" validate:"dive,oneof=good BAD,lowercase"`
	Both     string   `json:"both" validate:"oneof=123 good BAD,lowercase,uppercase"`
}

type OptionalBounds struct {
	Min    int    `json:"min"`
	Max    int    `json:"max" validate:"gtefield=Min"`
	Marker string `json:"marker"`
	Label  string `json:"label"`
}

type OptionalBoundsInput struct {
	*OptionalBounds `tstype:",extends"`
	Label           int `json:"label"`
}

type OptionalBoundsWithoutCollision struct {
	*OptionalBounds `tstype:",extends"`
}

type StrictBounds struct {
	Min    int    `json:"min"`
	Max    int    `json:"max" validate:"gtfield=Min"`
	Marker string `json:"marker"`
	Label  string `json:"label"`
}

type OptionalStrictBoundsInput struct {
	*StrictBounds `tstype:",extends"`
	Label         int `json:"label"`
}

type OtherBounds struct {
	Low  int `json:"low"`
	High int `json:"high" validate:"gtefield=Low"`
}

type MultipleRefinedBases struct {
	CompositionRangeDerived `tstype:",extends"`
	OtherBounds             `tstype:",extends"`
}

type MultipleOptionalRefinedBases struct {
	CompositionRangeDerived `tstype:",extends"`
	*OtherBounds            `tstype:",extends"`
}

type OmittedBound struct {
	Min int `json:"min" zod_omit:"true"`
}

type InheritedOmittedBoundInput struct {
	OmittedBound `tstype:",extends"`
	Max          int `json:"max" validate:"gtefield=Min"`
	Total        int `json:"total" validate:"gtefield=Max"`
}

type NestedOmittedBound struct {
	OmittedBound `tstype:",extends"`
}

type NestedOmittedBoundInput struct {
	NestedOmittedBound `tstype:",extends"`
	Max                int `json:"max" validate:"gtefield=Min"`
	Total              int `json:"total" validate:"gtefield=Max"`
}

type OptionalNestedBounds struct {
	*StrictBounds `tstype:",extends"`
	OuterMarker   string `json:"outerMarker"`
}

type OptionalNestedBoundsInput struct {
	*OptionalNestedBounds `tstype:",extends"`
	Label                 int `json:"label"`
}

type OptionalTargetInput struct {
	*OptionalBounds `tstype:",extends"`
	Ceiling         int `json:"ceiling" validate:"gtefield=Min"`
}

type ElementOmitemptyInput struct {
	Emails map[string]string   `json:"emails" validate:"dive,omitempty,email"`
	List   []string            `json:"list" validate:"dive,omitempty,email"`
	Nested map[string][]string `json:"nested" validate:"dive,dive,omitempty,email"`
}

type PointerElementOmitemptyInput struct {
	Emails map[string]*string   `json:"emails" validate:"dive,omitempty,email"`
	List   []*string            `json:"list" validate:"dive,omitempty,email"`
	Nested map[string][]*string `json:"nested" validate:"dive,dive,omitempty,email"`
}

type NumericEnumOmitemptyInput struct {
	Values        map[string]int   `json:"values" validate:"dive,omitempty,oneof=1 2"`
	Single        []uint           `json:"single,omitempty" validate:"dive,omitempty,oneof=2"`
	Nested        map[string][]int `json:"nested,omitempty" validate:"dive,dive,omitempty,oneof=1 2"`
	WithZero      []int            `json:"withZero,omitempty" validate:"dive,omitempty,oneof=0 1"`
	Strict        map[string]int   `json:"strict,omitempty" validate:"dive,oneof=1 2"`
	Pointers      map[string]*int  `json:"pointers,omitempty" validate:"dive,omitempty,oneof=1 2"`
	Scalar        int              `json:"scalar,omitempty" validate:"omitempty,oneof=1 2"`
	Bounded       []int            `json:"bounded,omitempty" validate:"dive,omitempty,oneof=0 1 2,gt=1"`
	SingleBounded []uint           `json:"singleBounded,omitempty" validate:"dive,omitempty,oneof=2,gt=1"`
}
