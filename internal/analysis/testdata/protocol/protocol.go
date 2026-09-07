// Package protocol is a wire contract with no Go server: no procedures are
// registered here, and nothing imports trpcgo.
package protocol

// JobState is the lifecycle stage of a job.
type JobState string

const (
	JobPending JobState = "pending"
	JobRunning JobState = "running"
	JobExited  JobState = "exited"
)

// JobID identifies a job across the relay.
type JobID = string

// Envelope is a job the relay routes without reading it.
type Envelope struct {
	ID JobID `json:"id"`
	// State advances only forward.
	State    JobState `json:"state"`
	Payload  []byte   `json:"payload"`
	Attempts int      `json:"attempts" validate:"min=0,max=5"`
	Route    routing  `json:"route"`
	Runner   *Runner  `json:"runner,omitempty"`
}

// Runner executes jobs it is assigned.
type Runner struct {
	Name string   `json:"name" validate:"required"`
	Tags []string `json:"tags"`
}

// Heartbeat is pushed by a runner; nothing requests it.
type Heartbeat struct {
	Runner  string `json:"runner"`
	Pending int    `json:"pending"`
}

// Batch is generic: TypeScript takes a type parameter, a wire schema cannot.
type Batch[T any] struct {
	Items []T `json:"items"`
	Count int `json:"count"`
}

// Heartbeats instantiates Batch concretely.
type Heartbeats = Batch[Heartbeat]

// Dispatch has no TypeScript representation.
type Dispatch func(Envelope) error

// routing is unexported: it reaches the output only through Envelope.
type routing struct {
	Hop int `json:"hop"`
}
