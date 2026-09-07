package validationcontract

type ContainerMapBounds struct {
	Values map[string]int `json:"values" validate:"min=1,max=2"`
}

type ContainerArrayBounds struct {
	Values []int `json:"values" validate:"gt=1,lte=3"`
}

type ContainerRequired struct {
	Values map[string]string `json:"values" validate:"required"`
	List   []string          `json:"list" validate:"required"`
}

type ContainerOptionalMap struct {
	Values map[string]string `json:"values,omitempty" validate:"omitempty,min=1,dive,required"`
}

type ContainerNestedKeys struct {
	Values map[string][]string `json:"values" validate:"dive,keys,startswith=team_,endkeys,min=1,dive,required,email"`
}

type ContainerIntegerKeys struct {
	Values map[int8]int `json:"values" validate:"dive,keys,min=-2,max=2,endkeys,gt=0"`
}

type ContainerNamedMap map[string]int

type ContainerNamedValues struct {
	Values ContainerNamedMap `json:"values" validate:"min=1,dive,keys,startswith=key_,endkeys,gt=0"`
}

type ContainerBytes struct {
	Value []byte `json:"value" validate:"min=2,max=3,dive,gte=1"`
}

type UniquePoint struct {
	Label string  `json:"label,omitempty"`
	Count float32 `json:"count,omitempty"`
}

type UniqueStructs struct {
	Values []UniquePoint `json:"values" validate:"unique,dive"`
}

type UniqueArrays struct {
	Values [][2]int8 `json:"values" validate:"unique"`
}

type UniqueRecords struct {
	Values []UniqueRecord `json:"values" validate:"unique=ID,dive"`
}

type UniqueRecord struct {
	ID      int8     `json:"wireID,omitempty"`
	Payload []string `json:"payload"`
}

type UniquePointerRecords struct {
	Values []UniquePointerRecord `json:"values" validate:"unique=ID,dive"`
}

type UniquePointerRecord struct {
	ID    *int8  `json:"id,omitempty"`
	Label string `json:"label"`
}

type UniqueQuotedRecords struct {
	Values []UniqueQuotedRecord `json:"values" validate:"unique=ID,dive"`
}

type UniqueQuotedRecord struct {
	ID    int64  `json:"id,string"`
	Label string `json:"label"`
}

type UniqueFloatRecords struct {
	Values []UniqueFloatRecord `json:"values" validate:"unique=ID,dive"`
}

type UniqueFloatRecord struct {
	ID    float32 `json:"id,string"`
	Label string  `json:"label"`
}

type UniqueMapStructs struct {
	Values map[string]UniquePoint `json:"values" validate:"unique=Ignored"`
}

type UniqueEmbeddedID struct {
	ID string `json:"identifier"`
}

type UniqueEmbeddedRecord struct {
	UniqueEmbeddedID
	Label string `json:"label"`
}

type UniqueEmbeddedRecords struct {
	Values []UniqueEmbeddedRecord `json:"values" validate:"unique=ID,dive"`
}

type UniqueSelectedArrays struct {
	Values []UniqueSelectedArray `json:"values" validate:"unique=Pair,dive"`
}

type UniqueSelectedArray struct {
	Pair  [2]int8 `json:"pair,omitempty"`
	Label string  `json:"label"`
}

type UniqueStructPointers struct {
	Values []*UniquePoint `json:"values" validate:"unique,dive"`
}

type UniqueNestedValues struct {
	Values [][]UniquePoint `json:"values" validate:"dive,unique,dive"`
}

type UniqueShadowedID struct {
	ID int8 `json:"innerID"`
}

type UniqueShadowedRecord struct {
	UniqueShadowedID
	ID int8 `json:"outerID"`
}

type UniqueShadowedRecords struct {
	Values []UniqueShadowedRecord `json:"values" validate:"unique=ID,dive"`
}

type MapMinimum struct {
	Values map[int]int `json:"values" validate:"min=2"`
}

type MapUnique struct {
	Values map[int]int `json:"values" validate:"unique"`
}

type MapDive struct {
	Values map[int]int `json:"values" validate:"dive,min=1"`
}

type MapRoundtrip struct {
	Values map[int]int `json:"values"`
}

type MapNarrow struct {
	Values map[int8]int8 `json:"values" validate:"dive,min=1"`
}

type MapStructValue struct {
	Count int8 `json:"count" validate:"min=1"`
}

type MapStruct struct {
	Values map[int]MapStructValue `json:"values" validate:"dive"`
}

type MapNested struct {
	Values map[int]map[int]int `json:"values" validate:"dive,min=2,dive,min=1"`
}

type MapKeyRules struct {
	Values map[int]int `json:"values" validate:"dive,keys,min=1,max=2,endkeys,min=1"`
}
