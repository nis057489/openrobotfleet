package behavior

import "context"

type Status int

const (
	StatusSuccess Status = iota
	StatusFailure
	StatusRunning
)

func (s Status) String() string {
	switch s {
	case StatusSuccess:
		return "SUCCESS"
	case StatusFailure:
		return "FAILURE"
	case StatusRunning:
		return "RUNNING"
	default:
		return "UNKNOWN"
	}
}

type Node interface {
	Tick(ctx context.Context, bb *Blackboard) Status
}
