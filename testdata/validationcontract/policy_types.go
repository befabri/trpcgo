package validationcontract

type PolicyRole string

const PolicyRoleKnown PolicyRole = "known"

type PolicyStatus int8

const PolicyStatusKnown PolicyStatus = 1

type PolicyEnumInput struct {
	Role   PolicyRole            `json:"role"`
	Status PolicyStatus          `json:"status"`
	Keys   map[PolicyRole]string `json:"keys"`
}

type PolicyEnumExplicit struct {
	Role   PolicyRole   `json:"role" validate:"oneof=known"`
	Status PolicyStatus `json:"status" validate:"oneof=1"`
}

type PolicyEnumQuoted struct {
	Role   PolicyRole   `json:"role,string"`
	Status PolicyStatus `json:"status,string"`
}

type PolicyNested struct {
	Value int8 `json:"value"`
}

type PolicyStrictInput struct {
	Nested PolicyNested         `json:"nested"`
	Values map[int]PolicyNested `json:"values"`
}

type PolicyOmitInput struct {
	Name string         `json:"name" validate:"required"`
	ID   string         `json:"id" zod_omit:"true"`
	Keys map[int]string `json:"keys"`
}

type PolicyOmitEmbeddedBase struct {
	Hidden string `json:"hidden" zod_omit:"true"`
	Value  string `json:"value" validate:"required"`
}

type PolicyOmitEmbedded struct{ *PolicyOmitEmbeddedBase }

type PolicyOmitExtended struct {
	*PolicyOmitEmbeddedBase `tstype:",extends"`
}
