package behavior

import "context"

// Sequence runs children until one fails or returns running.
type Sequence struct {
	Children []Node
}

func (s *Sequence) Tick(ctx context.Context, bb *Blackboard) Status {
	for _, child := range s.Children {
		status := child.Tick(ctx, bb)
		if status != StatusSuccess {
			return status
		}
	}
	return StatusSuccess
}

// Selector runs children until one succeeds or returns running.
type Selector struct {
	Children []Node
}

func (s *Selector) Tick(ctx context.Context, bb *Blackboard) Status {
	for _, child := range s.Children {
		status := child.Tick(ctx, bb)
		if status != StatusFailure {
			return status
		}
	}
	return StatusFailure
}

// Parallel runs all children.
// SuccessPolicy: RequireAll (default for this simple impl)
// FailurePolicy: RequireOne
type Parallel struct {
	Children []Node
}

func (p *Parallel) Tick(ctx context.Context, bb *Blackboard) Status {
	successCount := 0
	runningCount := 0

	for _, child := range p.Children {
		status := child.Tick(ctx, bb)
		if status == StatusFailure {
			return StatusFailure
		}
		if status == StatusSuccess {
			successCount++
		}
		if status == StatusRunning {
			runningCount++
		}
	}

	if runningCount > 0 {
		return StatusRunning
	}
	return StatusSuccess
}
