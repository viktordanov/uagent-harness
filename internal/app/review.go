package app

import (
	"fmt"
	"time"

	"github.com/unreallabsai/unreal-agent/harness/llm"

	"github.com/viktordanov/uagent-harness/internal/config"
	"github.com/viktordanov/uagent-harness/internal/review"
	"github.com/viktordanov/uagent-harness/internal/session"
)

// pickReview checks approvals_reviewer (auto_review by default, the
// owner's decision S8) and [review]: the model defaults to
// codex-auto-review on openai-codex and to the session model elsewhere, at
// low effort, with Codex's 90s timeout.
func pickReview(cfg config.Config, s session.Settings) (string, review.Config, error) {
	who := first(cfg.ApprovalsReviewer, review.ReviewerAuto)
	if who != review.ReviewerAuto && who != review.ReviewerUser {
		return "", review.Config{}, usage(fmt.Errorf("invalid approvals_reviewer %q (want auto_review or user)", who))
	}
	effort := llm.ReasoningEffort(first(cfg.Review.Effort, string(review.DefaultEffort)))
	if !effort.Valid() {
		return "", review.Config{}, usage(fmt.Errorf("invalid review.effort %q", cfg.Review.Effort))
	}
	timeout := review.DefaultTimeout
	if cfg.Review.Timeout != "" {
		d, err := time.ParseDuration(cfg.Review.Timeout)
		if err != nil || d <= 0 {
			return "", review.Config{}, usage(fmt.Errorf("invalid review.timeout %q", cfg.Review.Timeout))
		}
		timeout = d
	}

	return who, review.Config{
		Model:  first(cfg.Review.Model, review.DefaultModel(s.Provider, s.Model)),
		Effort: effort, Timeout: timeout,
	}, nil
}
