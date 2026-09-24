package app_test

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/unreallabsai/unreal-agent/harness/llm"

	"github.com/viktordanov/uagent-harness/internal/app"
	"github.com/viktordanov/uagent-harness/internal/approval"
	"github.com/viktordanov/uagent-harness/internal/config"
	"github.com/viktordanov/uagent-harness/internal/review"
	"github.com/viktordanov/uagent-harness/internal/rules"
	"github.com/viktordanov/uagent-harness/internal/sandbox"
	"github.com/viktordanov/uagent-harness/internal/session"
)

// flagDefaults are the inputs the CLI passes when no flag is given.
func flagDefaults() app.Inputs {
	return app.Inputs{Workspace: "/ws", Timeout: 30 * time.Minute, MaxDisk: "5G"}
}

func TestResolve(t *testing.T) {
	resumed := session.Info{Provider: "openai", Model: "gpt-resumed", Effort: "low", Workspace: "/resumed"}
	configured := config.Config{Provider: "openrouter", Model: "cfg-model", Effort: "medium"}

	tests := []struct {
		name    string
		in      func(*app.Inputs)
		resumed session.Info
		cfg     config.Config
		want    func(*app.Resolved)
	}{
		{
			name: "defaults: codex, its model, high effort, embedded",
			want: func(r *app.Resolved) {},
		},
		{
			name: "the config file beats the defaults",
			cfg:  configured,
			want: func(r *app.Resolved) {
				r.Settings.Provider, r.Settings.Model, r.Settings.Effort = "openrouter", "cfg-model", "medium"
			},
		},
		{
			name:    "the resumed session beats the config file",
			resumed: resumed,
			cfg:     configured,
			want: func(r *app.Resolved) {
				r.Settings.Provider, r.Settings.Model, r.Settings.Effort = "openai", "gpt-resumed", "low"
			},
		},
		{
			name:    "flags beat the resumed session",
			in:      func(in *app.Inputs) { in.Provider, in.Model, in.Effort = "openai", "gpt-flag", "max" },
			resumed: resumed,
			cfg:     configured,
			want: func(r *app.Resolved) {
				r.Settings.Provider, r.Settings.Model, r.Settings.Effort = "openai", "gpt-flag", "max"
			},
		},
		{
			name:    "a provider change drops the resumed and configured models",
			in:      func(in *app.Inputs) { in.Provider = "ollama" },
			resumed: resumed,
			cfg:     configured,
			want: func(r *app.Resolved) {
				r.Settings.Provider, r.Settings.Model, r.Settings.Effort = "ollama", "", "low"
			},
		},
		{
			name:    "a provider change to codex gets the codex default model",
			in:      func(in *app.Inputs) { in.Provider = app.CodexProvider },
			resumed: resumed,
			want:    func(r *app.Resolved) { r.Settings.Effort = "low" },
		},
		{
			name:    "the same provider by flag keeps the resumed model",
			in:      func(in *app.Inputs) { in.Provider = "openai" },
			resumed: resumed,
			want: func(r *app.Resolved) {
				r.Settings.Provider, r.Settings.Model, r.Settings.Effort = "openai", "gpt-resumed", "low"
			},
		},
		{
			name: "another provider has no default model",
			cfg:  config.Config{Provider: "openai"},
			want: func(r *app.Resolved) { r.Settings.Provider, r.Settings.Model = "openai", "" },
		},
		{
			name:    "the resumed workspace when no flag is given",
			in:      func(in *app.Inputs) { in.Workspace = "" },
			resumed: session.Info{Workspace: "/resumed"},
			want:    func(r *app.Resolved) { r.Settings.Workspace = "/resumed" },
		},
		{
			name: "the current directory when nothing sets the workspace",
			in:   func(in *app.Inputs) { in.Workspace = "" },
			want: func(r *app.Resolved) { r.Settings.Workspace = "." },
		},
		{
			name: "timeout from the config file",
			cfg:  config.Config{Timeout: "5m"},
			want: func(r *app.Resolved) { r.Settings.Timeout = 5 * time.Minute },
		},
		{
			name: "a timeout flag beats the config file",
			in:   func(in *app.Inputs) { in.Timeout, in.TimeoutSet = 0, true },
			cfg:  config.Config{Timeout: "5m"},
			want: func(r *app.Resolved) { r.Settings.Timeout = 0 },
		},
		{
			name: "fast by flag",
			in:   func(in *app.Inputs) { in.Fast, in.FastSet = true, true },
			want: func(r *app.Resolved) { r.Settings.ServiceTier = "priority" },
		},
		{
			name: "fast from the config file",
			cfg:  config.Config{Fast: true},
			want: func(r *app.Resolved) { r.Settings.ServiceTier = "priority" },
		},
		{
			name: "--fast=false beats the config file",
			in:   func(in *app.Inputs) { in.FastSet = true },
			cfg:  config.Config{Fast: true},
			want: func(r *app.Resolved) {},
		},
		{
			name: "engine from the config file",
			cfg:  config.Config{Engine: app.EngineProcess},
			want: func(r *app.Resolved) { r.Engine = app.EngineProcess },
		},
		{
			name: "an engine flag beats the config file",
			in:   func(in *app.Inputs) { in.Engine = app.EngineEmbedded },
			cfg:  config.Config{Engine: app.EngineProcess},
			want: func(r *app.Resolved) {},
		},
		{
			name: "max disk from the config file",
			cfg:  config.Config{MaxDisk: "500M"},
			want: func(r *app.Resolved) { r.MaxDisk = 500 << 20 },
		},
		{
			name: "a max disk flag beats the config file",
			in:   func(in *app.Inputs) { in.MaxDisk, in.MaxDiskSet = "0", true },
			cfg:  config.Config{MaxDisk: "500M"},
			want: func(r *app.Resolved) { r.MaxDisk = 0 },
		},
		{
			name: "instructions off by flag",
			in:   func(in *app.Inputs) { in.NoInstructions = true },
			want: func(r *app.Resolved) { r.Instructions = false },
		},
		{
			name: "instructions off in the config file",
			cfg:  config.Config{Instructions: config.Instructions{Enabled: new(false)}},
			want: func(r *app.Resolved) { r.Instructions = false },
		},
		{
			name: "the config file sets the sandbox",
			cfg: config.Config{SandboxMode: "read-only", SandboxWorkspaceWrite: config.SandboxWorkspaceWrite{
				NetworkAccess: true, WritableRoots: []string{"~/.cache"},
			}},
			want: func(r *app.Resolved) {
				r.Settings.Sandbox = "read-only"
				r.Sandbox = sandbox.Policy{Mode: sandbox.ReadOnly, WritableRoots: []string{"~/.cache"}, Network: true}
			},
		},
		{
			name: "--sandbox beats the config file",
			in:   func(in *app.Inputs) { in.Sandbox = "danger-full-access" },
			cfg:  config.Config{SandboxMode: "read-only"},
			want: func(r *app.Resolved) {
				r.Settings.Sandbox = "danger-full-access"
				r.Sandbox.Mode = sandbox.FullAccess
			},
		},
		{
			name: "the config file sets the approval policy and approvals",
			cfg:  config.Config{ApprovalPolicy: "never", Approvals: config.Approvals{Allow: []string{"git status"}, Forbid: []string{"rm -rf"}}},
			want: func(r *app.Resolved) {
				r.Approval = approval.Never
				r.Rules = []rules.Rule{
					{Pattern: [][]string{{"git"}, {"status"}}, Decision: rules.Allow, Source: "[approvals] allow"},
					{Pattern: [][]string{{"rm"}, {"-rf"}}, Decision: rules.Forbidden, Source: "[approvals] forbid"},
				}
			},
		},
		{
			name: "--ask beats the config file",
			in:   func(in *app.Inputs) { in.Ask = "on-request" },
			cfg:  config.Config{ApprovalPolicy: "never"},
			want: func(*app.Resolved) {},
		},
		{
			name: "base URL and dotenv pass through",
			in:   func(in *app.Inputs) { in.BaseURL, in.AllowDotenv = "http://llm", true },
			want: func(r *app.Resolved) { r.Settings.BaseURL, r.Settings.AllowDotenv = "http://llm", true },
		},
		{
			name: "the reviewer uses the session model off openai-codex",
			in:   func(in *app.Inputs) { in.Provider, in.Model = "openai", "gpt-x" },
			want: func(r *app.Resolved) {
				r.Settings.Provider, r.Settings.Model = "openai", "gpt-x"
				r.Review.Model = "gpt-x"
			},
		},
		{
			name: "reviewer keys",
			cfg: config.Config{ApprovalsReviewer: "user", Review: config.Review{
				Model: "rev", Effort: "medium", Timeout: "30s",
			}},
			want: func(r *app.Resolved) {
				r.ApprovalsReviewer = review.ReviewerUser
				r.Review = review.Config{Model: "rev", Effort: llm.ReasoningEffortMedium, Timeout: 30 * time.Second}
			},
		},
		{
			name: "agent keys",
			cfg: config.Config{Agents: config.Agents{
				Enabled: new(false), MaxThreads: new(2), MaxDepth: new(2), DefaultSubagentModel: "small", DefaultSubagentReasoningEffort: "low",
			}},
			want: func(r *app.Resolved) {
				r.Agents = app.Agents{MaxThreads: 2, MaxDepth: 2, Model: "small", Effort: "low"}
			},
		},
		{
			name: "max_concurrent_threads_per_session beats Codex's alias",
			cfg:  config.Config{Agents: config.Agents{MaxConcurrentThreadsPerSession: new(8), MaxThreads: new(2)}},
			want: func(r *app.Resolved) { r.Agents.MaxThreads = 8 },
		},
		{
			name: "compaction keys",
			cfg:  config.Config{AutoCompactPercent: new(0), ModelContextWindow: 128_000},
			want: func(r *app.Resolved) { r.AutoCompactPercent, r.Settings.ContextWindow = 0, 128_000 },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := flagDefaults()
			if tt.in != nil {
				tt.in(&in)
			}
			want := app.Resolved{
				Settings: session.Settings{
					Provider: app.CodexProvider, Model: app.DefaultCodexModel, Effort: app.DefaultEffort,
					Workspace: "/ws", Timeout: 30 * time.Minute, Sandbox: string(sandbox.WorkspaceWrite),
				},
				Engine: app.EngineEmbedded, MaxDisk: 5 << 30, Instructions: true,
				Sandbox: sandbox.Policy{Mode: sandbox.WorkspaceWrite}, AutoCompactPercent: 90, Approval: approval.OnRequest,
				ApprovalsReviewer: review.ReviewerAuto,
				Review:            review.Config{Model: review.CodexModel, Effort: llm.ReasoningEffortLow, Timeout: review.DefaultTimeout},
				Agents:            app.Agents{Enabled: true, MaxThreads: 4, MaxDepth: 1},
			}
			tt.want(&want)
			want.Sandbox.Workspace = want.Settings.Workspace
			if want.Review.Model == review.CodexModel && want.Settings.Provider != app.CodexProvider {
				want.Review.Model = want.Settings.Model // off openai-codex, the session model reviews
			}

			got, err := app.Resolve(in, tt.resumed, tt.cfg)

			require.NoError(t, err)
			assert.Equal(t, want, got)
		})
	}
}

func TestResolveUsageErrors(t *testing.T) {
	tests := []struct {
		name string
		in   func(*app.Inputs)
		cfg  config.Config
		want string
	}{
		{name: "invalid config timeout", cfg: config.Config{Timeout: "soon"}, want: `invalid timeout "soon"`},
		{name: "invalid config provider", cfg: config.Config{Provider: "acme"}, want: `invalid provider "acme"`},
		{name: "invalid config effort", cfg: config.Config{Effort: "huge"}, want: `invalid effort "huge"`},
		{name: "invalid model", in: func(in *app.Inputs) { in.Model = "-x" }, want: "starts with a dash"},
		{name: "invalid config max disk", cfg: config.Config{MaxDisk: "lots"}, want: `max_disk: invalid size "LOTS"`},
		{name: "invalid config engine", cfg: config.Config{Engine: "turbo"}, want: "invalid engine turbo (want embedded or process)"},
		{name: "invalid auto_compact_percent", cfg: config.Config{AutoCompactPercent: new(101)}, want: "invalid auto_compact_percent 101"},
		{name: "invalid model_context_window", cfg: config.Config{ModelContextWindow: -1}, want: "invalid model_context_window -1"},
		{name: "invalid approvals_reviewer", cfg: config.Config{ApprovalsReviewer: "robot"}, want: `invalid approvals_reviewer "robot"`},
		{name: "invalid review effort", cfg: config.Config{Review: config.Review{Effort: "huge"}}, want: `invalid review.effort "huge"`},
		{name: "invalid review timeout", cfg: config.Config{Review: config.Review{Timeout: "-1s"}}, want: `invalid review.timeout "-1s"`},
		{name: "invalid agents.max_concurrent_threads_per_session", cfg: config.Config{Agents: config.Agents{MaxConcurrentThreadsPerSession: new(0)}}, want: "invalid agents.max_concurrent_threads_per_session 0"},
		{name: "invalid agents.max_depth", cfg: config.Config{Agents: config.Agents{MaxDepth: new(-1)}}, want: "invalid agents.max_depth -1"},
		{name: "invalid agents effort", cfg: config.Config{Agents: config.Agents{DefaultSubagentReasoningEffort: "huge"}}, want: `invalid agents.default_subagent_reasoning_effort "huge"`},
		{name: "invalid sandbox mode", cfg: config.Config{SandboxMode: "yolo"}, want: `invalid sandbox mode "yolo"`},
		{name: "invalid approval policy", in: func(in *app.Inputs) { in.Ask = "untrusted" }, want: `invalid approval policy "untrusted"`},
		{name: "invalid approval prefix", cfg: config.Config{Approvals: config.Approvals{Allow: []string{"echo $HOME"}}}, want: "not a simple command prefix"},
		{
			name: "fast on the process engine",
			in:   func(in *app.Inputs) { in.Fast, in.FastSet, in.Engine = true, true, app.EngineProcess },
			want: "--fast needs the embedded engine",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := flagDefaults()
			if tt.in != nil {
				tt.in(&in)
			}

			_, err := app.Resolve(in, session.Info{}, tt.cfg)

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
			var usage *app.UsageError
			assert.True(t, errors.As(err, &usage), "a usage error")
		})
	}
}

func TestParseSize(t *testing.T) {
	tests := []struct {
		in      string
		want    int64
		wantErr bool
	}{
		{in: "1024", want: 1024},
		{in: "0", want: 0},
		{in: "2k", want: 2 << 10},
		{in: "500M", want: 500 << 20},
		{in: "500MB", want: 500 << 20},
		{in: " 1.5G ", want: 3 << 29},
		{in: "", wantErr: true},
		{in: "-1", wantErr: true},
		{in: "5T", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := app.ParseSize(tt.in)
			if tt.wantErr {
				assert.Error(t, err)

				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
