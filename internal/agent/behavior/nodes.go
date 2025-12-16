package behavior

import (
	"context"
)

// ActionNode is a helper for simple function-based nodes
type ActionNode struct {
	Action func(ctx context.Context, bb *Blackboard) Status
}

func (n *ActionNode) Tick(ctx context.Context, bb *Blackboard) Status {
	return n.Action(ctx, bb)
}

// ConditionNode is a helper for simple boolean checks
type ConditionNode struct {
	Condition func(ctx context.Context, bb *Blackboard) bool
}

func (n *ConditionNode) Tick(ctx context.Context, bb *Blackboard) Status {
	if n.Condition(ctx, bb) {
		return StatusSuccess
	}
	return StatusFailure
}
