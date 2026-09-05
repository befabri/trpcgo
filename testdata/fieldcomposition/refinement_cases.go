package fieldcomposition

// RefinementCase is a JSON input for Type with the validity that both the Go
// validator and the generated schema must report.
type RefinementCase struct {
	Type  string `json:"type"`
	JSON  string `json:"json"`
	Valid bool   `json:"valid"`
}

var RefinementCases = []RefinementCase{
	{"OptionalBoundsInput", `{"label":1}`, true},
	{"OptionalBoundsInput", `{"label":1,"min":1,"max":2}`, true},
	{"OptionalBoundsInput", `{"label":1,"min":2,"max":1}`, false},
	{"OptionalBoundsInput", `{"label":1,"max":2}`, true},
	{"OptionalBoundsInput", `{"label":1,"min":2}`, false},
	{"OptionalBoundsInput", `{"label":1,"max":-1}`, false},
	{"OptionalBoundsInput", `{"label":1,"min":-1}`, true},
	{"OptionalBoundsInput", `{"label":1,"min":0,"max":0}`, true},
	{"OptionalStrictBoundsInput", `{"label":1}`, true},
	{"OptionalStrictBoundsInput", `{"label":1,"marker":"allocated"}`, false},
	{"OptionalStrictBoundsInput", `{"label":1,"max":1}`, true},
	{"OptionalStrictBoundsInput", `{"label":1,"min":1,"max":2}`, true},
	{"OptionalStrictBoundsInput", `{"label":1,"min":0,"max":0}`, false},
	{"OptionalBoundsWithoutCollision", `{}`, true},
	{"OptionalBoundsWithoutCollision", `{"max":2}`, true},
	{"OptionalBoundsWithoutCollision", `{"min":2}`, false},
	{"MultipleRefinedBases", `{"min":1,"max":2,"label":1,"low":3,"high":4}`, true},
	{"MultipleRefinedBases", `{"min":1,"max":0,"label":1,"low":3,"high":4}`, false},
	{"MultipleRefinedBases", `{"min":1,"max":2,"label":1,"low":3,"high":1}`, false},
	{"MultipleOptionalRefinedBases", `{"min":1,"max":2,"label":1}`, true},
	{"MultipleOptionalRefinedBases", `{"min":1,"max":2,"label":1,"high":2}`, true},
	{"MultipleOptionalRefinedBases", `{"min":1,"max":2,"label":1,"low":2}`, false},
	{"InheritedOmittedBoundInput", `{"max":1,"total":2}`, true},
	{"InheritedOmittedBoundInput", `{"max":2,"total":1}`, false},
	{"NestedOmittedBoundInput", `{"max":1,"total":2}`, true},
	{"NestedOmittedBoundInput", `{"max":2,"total":1}`, false},
	{"OptionalNestedBoundsInput", `{"label":1}`, true},
	{"OptionalNestedBoundsInput", `{"label":1,"outerMarker":"allocated"}`, true},
	{"OptionalNestedBoundsInput", `{"label":1,"marker":"allocated"}`, false},
	{"OptionalNestedBoundsInput", `{"label":1,"min":1,"max":2}`, true},
	{"OptionalTargetInput", `{"ceiling":1,"marker":"allocated"}`, true},
	{"OptionalTargetInput", `{"ceiling":1,"min":2,"max":3}`, false},
	{"OptionalTargetInput", `{"ceiling":3,"min":2,"max":3}`, true},
}

var ElementValidationCases = []RefinementCase{
	{"ElementOmitemptyInput", `{"emails":{"a":""},"list":[""],"nested":{"a":[""]}}`, true},
	{"ElementOmitemptyInput", `{"emails":{"a":"alice@example.com"},"list":["alice@example.com"],"nested":{"a":["alice@example.com"]}}`, true},
	{"ElementOmitemptyInput", `{"emails":{},"list":[],"nested":{}}`, true},
	{"ElementOmitemptyInput", `{"emails":{"a":"invalid"},"list":[],"nested":{}}`, false},
	{"ElementOmitemptyInput", `{"emails":{},"list":["invalid"],"nested":{}}`, false},
	{"ElementOmitemptyInput", `{"emails":{},"list":[],"nested":{"a":["invalid"]}}`, false},
	{"PointerElementOmitemptyInput", `{"emails":{"a":""},"list":[],"nested":{}}`, false},
	{"PointerElementOmitemptyInput", `{"emails":{},"list":[""],"nested":{}}`, false},
	{"PointerElementOmitemptyInput", `{"emails":{},"list":[],"nested":{"a":[""]}}`, false},
	{"PointerElementOmitemptyInput", `{"emails":{"a":"alice@example.com"},"list":["alice@example.com"],"nested":{"a":["alice@example.com"]}}`, true},
}

var NumericEnumValidationCases = []RefinementCase{
	{"NumericEnumOmitemptyInput", `{"values":{"a":0}}`, true},
	{"NumericEnumOmitemptyInput", `{"values":{"a":1,"b":2}}`, true},
	{"NumericEnumOmitemptyInput", `{"values":{"a":3}}`, false},
	{"NumericEnumOmitemptyInput", `{"values":{"a":-1}}`, false},
	{"NumericEnumOmitemptyInput", `{"values":{}}`, true},
	{"NumericEnumOmitemptyInput", `{"values":{},"single":[0,2]}`, true},
	{"NumericEnumOmitemptyInput", `{"values":{},"single":[1]}`, false},
	{"NumericEnumOmitemptyInput", `{"values":{},"nested":{"a":[0,1,2]}}`, true},
	{"NumericEnumOmitemptyInput", `{"values":{},"nested":{"a":[3]}}`, false},
	{"NumericEnumOmitemptyInput", `{"values":{},"withZero":[0,1]}`, true},
	{"NumericEnumOmitemptyInput", `{"values":{},"withZero":[2]}`, false},
	{"NumericEnumOmitemptyInput", `{"values":{},"strict":{"a":0}}`, false},
	{"NumericEnumOmitemptyInput", `{"values":{},"strict":{"a":1,"b":2}}`, true},
	{"NumericEnumOmitemptyInput", `{"values":{},"pointers":{"a":0}}`, false},
	{"NumericEnumOmitemptyInput", `{"values":{},"pointers":{"a":1,"b":2}}`, true},
	{"NumericEnumOmitemptyInput", `{"values":{},"pointers":{"a":3}}`, false},
	{"NumericEnumOmitemptyInput", `{"values":{},"scalar":0}`, true},
	{"NumericEnumOmitemptyInput", `{"values":{},"scalar":1}`, true},
	{"NumericEnumOmitemptyInput", `{"values":{},"scalar":3}`, false},
	{"NumericEnumOmitemptyInput", `{"values":{},"bounded":[0,2]}`, true},
	{"NumericEnumOmitemptyInput", `{"values":{},"bounded":[1]}`, false},
	{"NumericEnumOmitemptyInput", `{"values":{},"singleBounded":[0,2]}`, true},
	{"NumericEnumOmitemptyInput", `{"values":{},"singleBounded":[1]}`, false},
}

func NewRefinementInput(name string) any {
	switch name {
	case "OptionalBoundsInput":
		return new(OptionalBoundsInput)
	case "OptionalStrictBoundsInput":
		return new(OptionalStrictBoundsInput)
	case "OptionalBoundsWithoutCollision":
		return new(OptionalBoundsWithoutCollision)
	case "MultipleRefinedBases":
		return new(MultipleRefinedBases)
	case "MultipleOptionalRefinedBases":
		return new(MultipleOptionalRefinedBases)
	case "InheritedOmittedBoundInput":
		return new(InheritedOmittedBoundInput)
	case "NestedOmittedBoundInput":
		return new(NestedOmittedBoundInput)
	case "OptionalNestedBoundsInput":
		return new(OptionalNestedBoundsInput)
	case "OptionalTargetInput":
		return new(OptionalTargetInput)
	case "ElementOmitemptyInput":
		return new(ElementOmitemptyInput)
	case "PointerElementOmitemptyInput":
		return new(PointerElementOmitemptyInput)
	case "NumericEnumOmitemptyInput":
		return new(NumericEnumOmitemptyInput)
	default:
		panic("unknown refinement input: " + name)
	}
}
