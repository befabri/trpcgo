package validationcontract

// Keep these input declarations separate from the runtime registrations so the
// same types can be loaded by both static analysis and reflection.
type ScalarRequiredInteger struct {
	Value int `json:"value" validate:"required"`
}

type ScalarRequiredBoolean struct {
	Value bool `json:"value" validate:"required"`
}

type ScalarNumericString struct {
	Value string `json:"value" validate:"numeric"`
}

type ScalarAlphaString struct {
	Value string `json:"value" validate:"alpha"`
}

type ScalarLowercase struct {
	Value string `json:"value" validate:"lowercase"`
}

type ScalarUUID struct {
	Value string `json:"value" validate:"uuid"`
}

type ScalarIP struct {
	Value string `json:"value" validate:"ip"`
}

type ScalarHexadecimal struct {
	Value string `json:"value" validate:"hexadecimal"`
}

type ScalarBase64URL struct {
	Value string `json:"value" validate:"base64url"`
}

type ScalarMAC struct {
	Value string `json:"value" validate:"mac"`
}

type ScalarHostname struct {
	Value string `json:"value" validate:"hostname"`
}

type ScalarCIDRv4 struct {
	Value string `json:"value" validate:"cidrv4"`
}

type ScalarEscapedComma struct {
	Value string `json:"value" validate:"contains=0x2C"`
}

type ScalarEscapedPipe struct {
	Value string `json:"value" validate:"contains=0x7C"`
}

type ScalarOrPrefix struct {
	Value string `json:"value" validate:"startswith=a|startswith=b"`
}

type ScalarFormatOneof struct {
	Value string `json:"value" validate:"email,oneof=admin@example.com"`
}

type ScalarRequiredBeforeOmit struct {
	Value string `json:"value" validate:"required,omitempty"`
}

type ScalarMinBeforeOmit struct {
	Value string `json:"value" validate:"min=2,omitempty"`
}

type ScalarMinAfterOmit struct {
	Value string `json:"value" validate:"omitempty,min=2"`
}

type ScalarTwoOneof struct {
	Value string `json:"value" validate:"oneof=a b,oneof=b c"`
}

type ScalarOptionalEmail struct {
	Value string `json:"value,omitempty" validate:"email"`
}

// An absent property decodes to the zero value, which min rejects: the schema
// has to refuse the omission that the json tag appears to allow.
type ScalarOptionalMinLength struct {
	Value string `json:"value,omitempty" validate:"min=1"`
}

// The mirror of ScalarOptionalMinLength: max accepts the zero value, so the
// property may be omitted.
type ScalarOptionalMaxLength struct {
	Value string `json:"value,omitempty" validate:"max=8"`
}

type ScalarPointerMinimum struct {
	Value *int `json:"value,omitempty" validate:"min=1"`
}

type ScalarPointerOmit struct {
	Value *int `json:"value,omitempty" validate:"omitempty,min=1"`
}

type ScalarPointerRequired struct {
	Value *int `json:"value" validate:"required"`
}

type ScalarOutOfRangeOneof struct {
	Value int8 `json:"value" validate:"oneof=128"`
}

type ScalarBytesLength struct {
	Value []byte `json:"value" validate:"len=2"`
}

type ScalarUppercase struct {
	Value string `json:"value" validate:"uppercase"`
}

type ScalarNegativeLength struct {
	Value string `json:"value" validate:"max=-1"`
}

type ScalarPureText struct {
	Value string `json:"value" validate:"ascii,containsany=ab,excludes=z,startsnotwith=x,endsnotwith=y,ne=bad"`
}

type ScalarEscapedControl struct {
	Value string `json:"value" validate:"contains=\a"`
}

type ScalarUniqueStrings struct {
	Value []string `json:"value" validate:"unique"`
}

type ScalarUniqueNumbers struct {
	Value []int `json:"value" validate:"unique"`
}

type ScalarUniquePointers struct {
	Value []*int `json:"value" validate:"unique"`
}

type ScalarUniqueMap struct {
	Value map[string]string `json:"value" validate:"unique"`
}

type ScalarUniqueFloat32 struct {
	Value []float32 `json:"value" validate:"unique"`
}

type Float32Equal struct {
	Value float32 `json:"value" validate:"eq=1"`
}

type Float32Greater struct {
	Value float32 `json:"value" validate:"gt=1"`
}

type Float32Bounds struct {
	Value float32 `json:"value" validate:"min=0.1,max=0.1"`
}

type Float32Required struct {
	Value float32 `json:"value" validate:"required"`
}

type Float32Omit struct {
	Value float32 `json:"value" validate:"omitempty,gt=1"`
}

type Float32PointerOmit struct {
	Value *float32 `json:"value" validate:"omitempty,gt=1"`
}

type Float32OR struct {
	Value float32 `json:"value" validate:"eq=0.1|gt=1"`
}

type Float32Wire struct {
	Value float32 `json:"value,string" validate:"eq=0.1"`
}

type Float32Dive struct {
	Value []float32 `json:"value" validate:"dive,gt=1"`
}

type Float32Range struct {
	Value float32 `json:"value"`
}

type ScalarOmitNilInt struct {
	Value int `json:"value,omitempty" validate:"omitnil,min=1"`
}

type ScalarOmitNilString struct {
	Value string `json:"value,omitempty" validate:"omitnil,min=1"`
}

type ScalarOmitNilBool struct {
	Value bool `json:"value,omitempty" validate:"omitnil,eq=true"`
}

type ScalarOmitNilPointer struct {
	Value *int `json:"value,omitempty" validate:"omitnil,min=1"`
}

type ScalarFloat32Minimum struct {
	Value float32 `json:"value" validate:"min=1"`
}

type ScalarQuotedFloat struct {
	Value float64 `json:"value,string"`
}

type ScalarStringEqual struct {
	Value string `json:"value" validate:"eq=�"`
}

type ScalarStringNotEqual struct {
	Value string `json:"value" validate:"ne=�"`
}

type ScalarQuotedStringEqual struct {
	Value string `json:"value,string" validate:"eq=�"`
}

type ScalarQuotedFloat32Greater struct {
	Value float32 `json:"value,string" validate:"gt=1"`
}

type ScalarQuotedFloat64Greater struct {
	Value float64 `json:"value,string" validate:"gt=1"`
}

type ScalarQuotedFloatPositive struct {
	Value float64 `json:"value,string" validate:"gt=0"`
}

type ScalarQuotedFloatPointer struct {
	Value *float64 `json:"value,string" validate:"omitnil,gt=0"`
}

type ScalarStringOneof struct {
	Value string `json:"value" validate:"oneof=� ok"`
}

type ScalarStringContains struct {
	Value string `json:"value" validate:"contains=�"`
}

type ScalarStringBoundary struct {
	Value string `json:"value" validate:"startswith=�,endswith=�"`
}

type ScalarStringExcludes struct {
	Value string `json:"value" validate:"excludes=�,excludesall=�"`
}

type ScalarCrossQuotedFloat struct {
	A float64 `json:"a,string"`
	B float64 `json:"b,string" validate:"eqfield=A"`
}

type ScalarCrossQuotedFloatPointer struct {
	A float64  `json:"a,string"`
	B *float64 `json:"b,string" validate:"omitnil,eqfield=A"`
}

type ScalarCrossQuotedFloat32 struct {
	A float32 `json:"a,string"`
	B float32 `json:"b,string" validate:"eqfield=A"`
}
