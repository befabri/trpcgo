// Package workflow holds enum types behind a transitive same-module import.
package workflow

type Phase string

const (
	PhaseQueued     Phase = "queued"
	PhaseProcessing Phase = "processing"
	PhaseCompleted  Phase = "completed"
)
