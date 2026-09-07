package validationcontract

type CustomAliasInput struct {
	First string `json:"first" check:"bounded" validate:"eq=ignored"`
	Again string `json:"again" check:"matching"`
}

type CustomDiveInput struct {
	Values map[string][]int `json:"values" check:"required,dive,keys,key,endkeys,nonempty,dive,even=2"`
}

type CustomScalarInput struct {
	Text   string  `json:"text" check:"suffix=0x2C0x7C"`
	Float  float32 `json:"float" check:"rounded"`
	Quoted int64   `json:"quoted,string" check:"large"`
	Bool   bool    `json:"bool,string" check:"truth"`
	String string  `json:"string,string" check:"replacement"`
}

type CustomOmitInput struct {
	Value int `json:"value,omitempty" check:"omitempty,even=2"`
	Other int `json:"other,omitempty" check:"even=2"`
}

type CustomStructInput struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

type CustomNestedInput struct {
	Window CustomStructInput `json:"window"`
}

type CustomSafeInput struct {
	Value string `json:"value" check:"safe"`
}

type CustomORInput struct {
	Value int `json:"value" check:"even=2|eq=3"`
}

type CustomCrossORInput struct {
	First int `json:"first"`
	Value int `json:"value" check:"even=2|eqfield=First"`
}

type CustomServerInput struct {
	First  int   `json:"first"`
	Value  int   `json:"value" check:"available|eqfield=First"`
	Other  int   `json:"other" check:"available|eq=3"`
	Quoted int64 `json:"quoted,string" check:"available|eq=3"`
}

type CustomDynamicInput struct {
	Value int `json:"value,omitempty" check:"dynamic"`
}

type CustomNilInput struct {
	Values map[string]int `json:"values,omitempty" check:"nilmap"`
}

type CustomNilORInput struct {
	Values map[string]int `json:"values,omitempty" check:"nilmap|len=0"`
}

type CustomZeroQuotedInput struct {
	Value int64 `json:"value,string,omitempty" check:"zero"`
}

type CustomOptionalArrayInput struct {
	Value [2]int `json:"value,omitempty" check:"arraylength=2"`
}

type CustomOptionalArrayInvalid struct {
	Value [2]int `json:"value,omitempty" check:"arraylength=3"`
}
