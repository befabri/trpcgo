package embedding

type StringValue struct {
	Value string `json:"value"`
}
type IntValue struct {
	Value int `json:"value"`
}
type OuterBefore struct {
	Value int `json:"value"`
	StringValue
}
type OuterAfter struct {
	StringValue
	Value int `json:"value"`
}
type Ambiguous struct {
	StringValue
	IntValue
	Keep bool `json:"keep"`
}
type Tagged struct {
	Value string `json:"Value"`
}
type Untagged struct{ Value int }
type TagWins struct {
	Tagged
	Untagged
}
type EmptyTag struct {
	Value int `json:",omitempty"`
}
type EmptyTagAmbiguous struct {
	EmptyTag
	Untagged
}
type Deep struct{ StringValue }
type DepthWins struct {
	Deep
	IntValue
}
type Left struct{ StringValue }
type Right struct{ StringValue }
type Diamond struct {
	Left
	Right
}

type SharedIntermediate struct{ StringValue }
type IntermediateLeft struct{ SharedIntermediate }
type IntermediateRight struct{ SharedIntermediate }
type IntermediateDiamond struct {
	IntermediateLeft
	IntermediateRight
}

type hiddenBase struct {
	Value string `json:"value"`
}
type TaggedHiddenEmbed struct {
	hiddenBase `json:"base"`
}
type Recursive struct {
	*Recursive
	Value int `json:"value"`
}
type PointerShadow struct {
	*StringValue
	Value int `json:"value"`
}
type Alias = StringValue
type AliasShadow struct {
	Alias
	Value int `json:"value"`
}
type ExtendedShadow struct {
	StringValue `tstype:",extends"`
	Value       int `json:"value"`
}
type ExtendedAmbiguous struct {
	StringValue `tstype:",extends"`
	IntValue    `tstype:",extends"`
}
type ExtendedRecursive struct {
	*ExtendedRecursive `tstype:",extends"`
	Value              int `json:"value"`
}

// Cases are marshalled with encoding/json to give each generator's field set
// a reference.
var Cases = []any{
	OuterBefore{}, OuterAfter{}, Ambiguous{}, TagWins{}, EmptyTagAmbiguous{},
	DepthWins{}, Diamond{}, IntermediateDiamond{}, Recursive{}, PointerShadow{StringValue: &StringValue{}},
	AliasShadow{}, ExtendedShadow{}, ExtendedAmbiguous{}, ExtendedRecursive{},
}
