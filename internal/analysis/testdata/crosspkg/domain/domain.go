// Package domain holds enum types defined outside the API package, mirroring a
// real project where status/role constants live in a shared internal package
// that the trpcgo scan pattern does not match directly.
package domain

import "example.com/crosspkg/workflow"

// Status is a recording status. It is referenced by an API response struct in
// the parent package but declared here, so its const group is only reachable
// through the import closure.
type Status string

const (
	StatusPending Status = "PENDING"
	StatusRunning Status = "RUNNING"
	StatusDone    Status = "DONE"
	StatusFailed  Status = "FAILED"
)

// Job references an enum from another same-module package. The root API package
// imports domain, but not workflow, so this exercises recursive import walking.
type Job struct {
	Phase workflow.Phase `json:"phase"`
}
