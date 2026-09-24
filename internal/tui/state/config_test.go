package state_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent-harness/internal/approval"
	"github.com/viktordanov/uagent-harness/internal/engine"
	"github.com/viktordanov/uagent-harness/internal/models"
	"github.com/viktordanov/uagent-harness/internal/session"
	"github.com/viktordanov/uagent-harness/internal/tui/state"
)

func configValues() map[string]state.ConfigValue {
	return map[string]state.ConfigValue{
		"auto_compact_percent":           {Value: "90", Source: "default"},
		"model_auto_compact_token_limit": {Value: "0", Source: "default"},
		"compact_model":                  {Value: "gpt-6-sol", Source: "default"},
		"model":                          {Value: "gpt-6-sol", Source: "user file"},
		"effort":                         {Value: "high", Source: "default"},
		"fast":                           {Value: "false", Source: "default"},
		"permission_mode":                {Value: "workspace", Source: "default"},
		"tui.details":                    {Value: "false", Source: "default"},
		"tui.mouse":                      {Value: "false", Source: "user file"},
	}
}

// openConfig opens /config with the values and the model list loaded, on
// the row with label.
func openConfig(t *testing.T, s state.State, label string) state.State {
	t.Helper()
	s, _ = apply(s, state.Submit{Text: "/config"},
		state.ConfigLoaded{Path: "/home/me/.config/uagent/config.toml", Values: configValues()},
		state.ModelsLoaded{Catalog: codexCatalog(models.OriginLive)})
	for _, row := range s.ConfigRows() {
		if row.Label == label {
			return s
		}
		s, _ = apply(s, state.ConfigMove{Delta: 1})
	}
	t.Fatalf("no row %q", label)

	return s
}

func TestConfig_OpensAndShowsValuesWithSources(t *testing.T) {
	s, effects := apply(opened(), state.Submit{Text: "/config"})
	assert.Equal(t, []state.Effect{state.EffLoadConfig{}, state.EffLoadModels{Provider: "openai-codex"}}, effects)
	require.NotNil(t, s.Config)

	s, _ = apply(s, state.ConfigLoaded{Path: "/cfg.toml", Values: configValues()})
	rows := s.ConfigRows()
	require.Len(t, rows, 9)
	got := map[string][2]string{}
	for _, r := range rows {
		got[r.Label] = [2]string{r.Value, r.Source}
	}
	assert.Equal(t, [2]string{"on at 90%", "default"}, got["Auto-compact"])
	assert.Equal(t, [2]string{"none", "default"}, got["Auto-compact token limit"])
	assert.Equal(t, [2]string{"session model (gpt-6-sol)", "default"}, got["Compaction model"])
	assert.Equal(t, [2]string{"gpt-6-sol", "user file"}, got["Model"])
	assert.Equal(t, [2]string{"off", "user file"}, got["Mouse"])

	s, _ = apply(s, state.ConfigEsc{})
	assert.Nil(t, s.Config, "esc closes")
}

func TestConfig_TogglesAndAppliesLive(t *testing.T) {
	s := openConfig(t, opened(), "Mouse")
	s, effects := apply(s, state.ConfigChange{Delta: 1})
	assert.Equal(t, []state.Effect{state.EffSaveConfig{Key: "tui.mouse", Value: true}}, effects)
	assert.True(t, s.Mouse, "the TUI reports the mouse at once")
	assert.Equal(t, "on", s.ConfigRows()[8].Value)
	assert.Equal(t, state.SourceUser, s.ConfigRows()[8].Source)

	s, effects = apply(s, state.ConfigSaved{Key: "tui.mouse", Value: true})
	assert.Equal(t, []state.Effect{state.EffLoadConfig{}}, effects, "reload the sources")
	assert.Contains(t, s.Items[len(s.Items)-1].Text, "saved tui.mouse = true to /home/me/.config/uagent/config.toml; applies now")

	s = openConfig(t, opened(), "Details view")
	s, _ = apply(s, state.ConfigEnter{})
	assert.True(t, s.Details)
}

func TestConfig_ModelEffortAndFastChangeTheSession(t *testing.T) {
	s := openConfig(t, opened(), "Effort")
	_, effects := apply(s, state.ConfigChange{Delta: 1})
	next := settings()
	next.Effort = "xhigh"
	assert.Equal(t, []state.Effect{state.EffSaveConfig{Key: "effort", Value: "xhigh"}, state.EffSetSettings{Settings: next}}, effects)

	s = openConfig(t, opened(), "Model")
	_, effects = apply(s, state.ConfigChange{Delta: 1})
	next = settings()
	next.Model = "gpt-6-luna"
	assert.Equal(t, []state.Effect{state.EffSaveConfig{Key: "model", Value: "gpt-6-luna"}, state.EffSetSettings{Settings: next}}, effects, "the next model in the provider's list")

	s = openConfig(t, opened(), "Fast mode")
	_, effects = apply(s, state.ConfigChange{Delta: 1})
	assert.Equal(t, []state.Effect{state.EffSaveConfig{Key: "fast", Value: true}}, effects, "the process engine has no fast mode: saved only")
	s = opened()
	s.Caps = engine.Capabilities{ServiceTier: true}
	s = openConfig(t, s, "Fast mode")
	_, effects = apply(s, state.ConfigChange{Delta: 1})
	next = settings()
	next.ServiceTier = "priority"
	assert.Equal(t, []state.Effect{state.EffSaveConfig{Key: "fast", Value: true}, state.EffSetSettings{Settings: next}}, effects)
}

func TestConfig_CyclesThePermissionModeAsShiftTab(t *testing.T) {
	s := openConfig(t, opened(), "Permission mode")
	s, effects := apply(s, state.ConfigChange{Delta: 1})
	assert.Equal(t, []state.Effect{
		state.EffSaveConfig{Key: "permission_mode", Value: "auto"},
		state.EffSetSettings{Settings: settings().WithMode(approval.ModeAuto)},
	}, effects)
	_, effects = apply(s, state.ConfigChange{Delta: 1})
	assert.Equal(t, state.EffSaveConfig{Key: "permission_mode", Value: "read-only"}, effects[0], "full access is not in the cycle")
}

func TestConfig_CyclesAutoCompactAndTheCompactionModel(t *testing.T) {
	s := openConfig(t, opened(), "Auto-compact")
	s, effects := apply(s, state.ConfigChange{Delta: 1})
	assert.Equal(t, []state.Effect{state.EffSaveConfig{Key: "auto_compact_percent", Value: 95}}, effects)
	assert.Equal(t, "on at 95%", s.ConfigRows()[0].Value, "shown before the save returns")
	s, effects = apply(s, state.ConfigChange{Delta: 1})
	assert.Equal(t, []state.Effect{state.EffSaveConfig{Key: "auto_compact_percent", Value: 0}}, effects, "after 95%, off")
	s, effects = apply(s, state.ConfigChange{Delta: -1})
	assert.Equal(t, []state.Effect{state.EffSaveConfig{Key: "auto_compact_percent", Value: 95}}, effects, "← goes back")

	s = openConfig(t, opened(), "Compaction model")
	s, effects = apply(s, state.ConfigChange{Delta: 1})
	assert.Equal(t, []state.Effect{state.EffSaveConfig{Key: "compact_model", Value: "gpt-6-sol"}}, effects, "from the session's model to the first listed")
	s.Config.Values["compact_model"] = state.ConfigValue{Value: "gpt-5.6-luna", Source: "user file"}
	_, effects = apply(s, state.ConfigChange{Delta: 1})
	assert.Equal(t, []state.Effect{state.EffSaveConfig{Key: "compact_model", Value: nil}}, effects, "after the last model, the session's again: the key is removed")
}

func TestConfig_TypesTheTokenLimit(t *testing.T) {
	s := openConfig(t, opened(), "Auto-compact token limit")
	s, effects := apply(s, state.ConfigEnter{})
	assert.Empty(t, effects)
	require.True(t, s.Config.Editing)
	assert.Empty(t, s.Config.Input, "0 is shown as none, so typing starts empty")

	s, effects = apply(s, state.ConfigType{Text: "5"}, state.ConfigType{Text: "x"}, state.ConfigEnter{})
	assert.Empty(t, effects)
	assert.Contains(t, s.Items[len(s.Items)-1].Text, `wants a number of tokens, not "5x"`)
	s, effects = apply(s, state.ConfigType{Text: "\b"}, state.ConfigType{Text: "0000"}, state.ConfigEnter{})
	assert.Equal(t, []state.Effect{state.EffSaveConfig{Key: "model_auto_compact_token_limit", Value: int64(50000)}}, effects)
	assert.False(t, s.Config.Editing)

	s, _ = apply(s, state.ConfigEnter{}, state.ConfigType{Text: "1"}, state.ConfigEsc{})
	assert.False(t, s.Config.Editing, "esc stops typing")
	assert.NotNil(t, s.Config, "and keeps the panel")
}

func TestConfig_TypesAModelWithoutAList(t *testing.T) {
	s, _ := apply(opened(), state.Submit{Text: "/config"}, state.ConfigLoaded{Path: "/cfg.toml", Values: configValues()})
	for range 3 {
		s, _ = apply(s, state.ConfigMove{Delta: 1})
	}
	s, _ = apply(s, state.ConfigChange{Delta: 1})
	require.True(t, s.Config.Editing)
	assert.Equal(t, "gpt-6-sol", s.Config.Input)
	_, effects := apply(s, state.ConfigType{Text: "\b"}, state.ConfigType{Text: "\b"}, state.ConfigType{Text: "\b"}, state.ConfigType{Text: "luna"}, state.ConfigEnter{})
	next := settings()
	next.Model = "gpt-6-luna"
	assert.Equal(t, []state.Effect{state.EffSaveConfig{Key: "model", Value: "gpt-6-luna"}, state.EffSetSettings{Settings: next}}, effects)
}

func TestConfig_WarnsWhenAnotherSourceWins(t *testing.T) {
	s := openConfig(t, opened(), "Auto-compact")
	s, _ = apply(s, state.ConfigChange{Delta: 1}, state.ConfigSaved{Key: "auto_compact_percent", Value: 95})
	values := configValues()
	values["auto_compact_percent"] = state.ConfigValue{Value: "80", Source: "project file"}
	s, _ = apply(s, state.ConfigLoaded{Path: "/cfg.toml", Values: values})
	assert.Equal(t, session.LevelWarning, s.Items[len(s.Items)-1].Level)
	assert.Contains(t, s.Items[len(s.Items)-1].Text, "auto_compact_percent still comes from the project file")
	assert.Contains(t, s.Items[len(s.Items)-2].Text, "applies to sessions opened from now on")

	s, _ = apply(s, state.ConfigSaved{Key: "fast", Value: true, Err: assert.AnError})
	assert.Equal(t, session.LevelError, s.Items[len(s.Items)-1].Level)
}
