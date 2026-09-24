package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/unreallabsai/unreal-agent/harness/llm"

	"github.com/viktordanov/uagent-harness/internal/compaction"
	"github.com/viktordanov/uagent-harness/internal/config"
	"github.com/viktordanov/uagent-harness/internal/session"
)

// pickCompaction checks the compaction keys: the automatic limit (Codex's
// 90% by default, lowered by model_auto_compact_token_limit), the summary
// model, effort, and prompt, the kept-message cap, and the context window
// override, which goes into the settings for the context meter. It returns
// the prompt file Setup reads when compact_prompt is not set.
func pickCompaction(cfg config.Config, s *session.Settings) (compaction.Settings, string, error) {
	c := compaction.Settings{
		Percent: compaction.DefaultAutoPercent, TokenLimit: cfg.ModelAutoCompactTokenLimit,
		Model: cfg.CompactModel, Effort: llm.ReasoningEffort(cfg.CompactEffort),
		Prompt: strings.TrimSpace(cfg.CompactPrompt), UserMessageMaxTokens: cfg.CompactUserMessageMaxTokens,
	}
	if cfg.AutoCompactPercent != nil {
		c.Percent = *cfg.AutoCompactPercent
	}
	var err error
	switch {
	case c.Percent < 0 || c.Percent > 100:
		err = fmt.Errorf("invalid auto_compact_percent %d (want 0 to 100)", c.Percent)
	case cfg.ModelContextWindow < 0:
		err = fmt.Errorf("invalid model_context_window %d", cfg.ModelContextWindow)
	case c.TokenLimit < 0:
		err = fmt.Errorf("invalid model_auto_compact_token_limit %d", c.TokenLimit)
	case c.UserMessageMaxTokens < 0:
		err = fmt.Errorf("invalid compact_user_message_max_tokens %d", c.UserMessageMaxTokens)
	case c.Effort != "" && !c.Effort.Valid():
		err = fmt.Errorf("invalid compact_effort %q (want %s)", cfg.CompactEffort, strings.Join(session.Efforts, ", "))
	}
	if err != nil {
		return c, "", usage(err)
	}
	var file string
	if c.Prompt == "" {
		if file, err = promptPath("experimental_compact_prompt_file", cfg.ExperimentalCompactPromptFile); err != nil {
			return c, "", err
		}
	}
	s.ContextWindow = cfg.ModelContextWindow

	return c, file, nil
}

// promptPath checks a prompt file key: an absolute path or one under ~/,
// with ~/ expanded. Empty stays empty.
func promptPath(key, value string) (string, error) {
	if value == "" {
		return "", nil
	}
	path, err := expandHome(value)
	if err != nil || !filepath.IsAbs(path) {
		return "", usage(fmt.Errorf("invalid %s %q (want an absolute path or one under ~/)", key, value))
	}

	return path, nil
}

// readPrompt reads a prompt file. As in Codex, a missing or empty file is
// an error.
func readPrompt(key, path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", usage(fmt.Errorf("failed to read %s: %w", key, err))
	}
	text := strings.TrimSpace(string(data))
	if text == "" {
		return "", usage(errors.New(key + " " + path + " is empty"))
	}

	return text, nil
}

// readCompactPrompt reads experimental_compact_prompt_file into the
// settings when compact_prompt is not set. As in Codex, a missing or empty
// file is an error.
func readCompactPrompt(r *Resolved) error {
	if r.CompactPromptFile == "" || r.Compaction.Prompt != "" {
		return nil
	}
	text, err := readPrompt("experimental_compact_prompt_file", r.CompactPromptFile)
	r.Compaction.Prompt = text

	return err
}

// expandHome replaces a leading ~/ with the home directory.
func expandHome(path string) (string, error) {
	rest, ok := strings.CutPrefix(path, "~/")
	if !ok {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to find the home directory: %w", err)
	}

	return filepath.Join(home, rest), nil
}

// compactionSettings explain the compaction keys. The summary model and
// effort show the session's when they are not set.
func compactionSettings(l config.Layers, r Resolved, cfg config.Config) []Setting {
	c := r.Compaction
	prompt := "Codex's"
	switch {
	case c.Prompt != "":
		prompt = fmt.Sprintf("%d characters", len(c.Prompt))
	case r.CompactPromptFile != "":
		prompt = "from experimental_compact_prompt_file"
	}

	return []Setting{
		overridden(l, "auto_compact_percent", c.Percent, func(c config.Config) any { return c.AutoCompactPercent }),
		overridden(l, "model_auto_compact_token_limit", c.TokenLimit, func(c config.Config) any { return c.ModelAutoCompactTokenLimit }),
		overridden(l, "compact_model", first(c.Model, r.Settings.Model), func(c config.Config) any { return c.CompactModel }),
		overridden(l, "compact_effort", first(cfg.CompactEffort, r.Settings.Effort), func(c config.Config) any { return c.CompactEffort }),
		overridden(l, "compact_prompt", prompt, func(c config.Config) any { return c.CompactPrompt }),
		overridden(l, "experimental_compact_prompt_file", cfg.ExperimentalCompactPromptFile, func(c config.Config) any { return c.ExperimentalCompactPromptFile }),
		overridden(l, "compact_user_message_max_tokens", c.KeepTokens(), func(c config.Config) any { return c.CompactUserMessageMaxTokens }),
	}
}
