package approval_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/viktordanov/uah/internal/approval"
)

// TestDecide_ConcurrentDontAskAgain pins that "don't ask again" on one
// prompt settles a concurrent prompt for a command the new rule allows:
// its ask ends with ErrNowAllowed, and the command runs unasked, as it
// would have if the two were asked one after another.
func TestDecide_ConcurrentDontAskAgain(t *testing.T) {
	a := approval.New(approval.Config{})
	req := approval.Request{Command: "touch /tmp/x", Escalated: true}
	open := make(chan struct{})
	causes := make(chan error, 1)
	waiting := func(ctx context.Context, _ approval.Prompt) approval.Answer {
		close(open)
		<-ctx.Done()
		causes <- context.Cause(ctx)

		return approval.Decline
	}
	second := make(chan approval.Decision, 1)
	go func() { second <- a.Decide(t.Context(), req, waiting) }()
	<-open

	first := a.Decide(t.Context(), req, func(context.Context, approval.Prompt) approval.Answer { return approval.ApprovePrefix })

	assert.Equal(t, approval.Decision{Run: approval.Unsandboxed}, first)
	assert.Equal(t, approval.Decision{Run: approval.Unsandboxed}, <-second)
	assert.ErrorIs(t, <-causes, approval.ErrNowAllowed)
}

// TestAskUnless pins that an ask nothing settles returns its answer, and a
// nil Changes asks as is.
func TestAskUnless(t *testing.T) {
	ask := func(context.Context, approval.Prompt) approval.Answer { return approval.Approve }
	never := func() bool { return false }

	answer, settled := approval.AskUnless(t.Context(), ask, approval.Prompt{}, &approval.Changes{}, never)
	assert.Equal(t, approval.Approve, answer)
	assert.False(t, settled)

	answer, settled = approval.AskUnless(t.Context(), ask, approval.Prompt{}, nil, never)
	assert.Equal(t, approval.Approve, answer)
	assert.False(t, settled)

	_, settled = approval.AskUnless(t.Context(), ask, approval.Prompt{}, &approval.Changes{}, func() bool { return true })
	assert.True(t, settled, "a prompt already settled is not asked")
}
