package validationconfig

type Child struct {
	Name string `json:"name,omitempty" binding:"requiredText"`
}

type Input struct {
	Start     int      `json:"start"`
	End       int      `json:"end" binding:"afterStart"`
	Child     Child    `json:"child"`
	Values    []string `json:"values" binding:"nonemptyList"`
	Anonymous struct {
		Name string `json:"name,omitempty" binding:"requiredText"`
	} `json:"anonymous"`
}
