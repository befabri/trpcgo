package schemaissues

import "time"

type Tag struct {
	Name string `json:"name" validate:"min=1"`
}

type Tags = []Tag
type Links = map[string]*Node

type Node struct {
	Child  *Node  `json:"child,omitempty"`
	Date   string `json:"date" tstype:"Date"`
	Tags   Tags   `json:"tags"`
	Links  Links  `json:"links,omitempty"`
	Secret string `json:"secret" zod_omit:"true"`
	Inline struct {
		Z    int   `json:"z"`
		Next *Node `json:"next,omitempty"`
	} `json:"inline"`
}

type NonStructBase struct {
	time.Time `tstype:",extends"`
}
