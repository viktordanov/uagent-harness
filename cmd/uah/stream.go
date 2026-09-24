package main

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/viktordanov/uagent/core"
	"github.com/viktordanov/uagent/stream"

	"github.com/viktordanov/uagent-harness/internal/engine"
	"github.com/viktordanov/uagent-harness/internal/session"
)

// jsonlWriter writes run events in uagent's stream schema and session events
// in the same shape: {"v":1,"type":...,"at":...,...}.
type jsonlWriter struct {
	enc *json.Encoder
	err error
}

func newJSONLWriter(w io.Writer) *jsonlWriter {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)

	return &jsonlWriter{enc: enc}
}

func (j *jsonlWriter) write(event core.Event) {
	if j.err != nil {
		return
	}
	dto, ok := stream.EventToDTO(event)
	if !ok {
		dto, ok = sessionEventDTO(event)
	}
	if !ok {
		dto, ok = engineEventDTO(event)
	}
	if !ok {
		return
	}
	if err := j.enc.Encode(dto); err != nil {
		j.err = fmt.Errorf("failed to write event: %w", err)
	}
}

type sessionHeader struct {
	V    int       `json:"v"`
	Type string    `json:"type"`
	At   time.Time `json:"at"`
}

func header(eventType string, at time.Time) sessionHeader {
	return sessionHeader{V: stream.SchemaVersion, Type: eventType, At: at.UTC()}
}

type settingsDTO struct {
	Provider    string `json:"provider"`
	Model       string `json:"model"`
	Effort      string `json:"effort"`
	ServiceTier string `json:"service_tier,omitempty"`
	Workspace   string `json:"workspace"`
}

func toSettingsDTO(s session.Settings) settingsDTO {
	return settingsDTO{Provider: s.Provider, Model: s.Model, Effort: s.Effort, ServiceTier: s.ServiceTier, Workspace: s.Workspace}
}

func sessionEventDTO(event core.Event) (any, bool) {
	switch e := event.(type) {
	case session.SessionOpened:
		return struct {
			sessionHeader

			ID       string      `json:"id"`
			Resumed  bool        `json:"resumed"`
			Engine   string      `json:"engine"`
			Settings settingsDTO `json:"settings"`
		}{header("session_opened", e.At), e.ID, e.Resumed, e.Engine, toSettingsDTO(e.Settings)}, true
	case session.InstructionsLoaded:
		return struct {
			sessionHeader

			Files     []string `json:"files"`
			Bytes     int      `json:"bytes"`
			Truncated bool     `json:"truncated"`
		}{header("instructions_loaded", e.At), e.Files, e.Bytes, e.Truncated}, true
	case session.InputQueued:
		return struct {
			sessionHeader

			ID   string `json:"id"`
			Text string `json:"text"`
		}{header("input_queued", e.At), e.Input.ID, e.Input.Text}, true
	case session.InputSent:
		return struct {
			sessionHeader

			IDs []string `json:"ids"`
		}{header("input_sent", e.At), e.IDs}, true
	case session.InputDelivered:
		return struct {
			sessionHeader

			ID string `json:"id"`
		}{header("input_delivered", e.At), e.ID}, true
	case session.InputFailed:
		return struct {
			sessionHeader

			IDs    []string `json:"ids"`
			Reason string   `json:"reason"`
		}{header("input_failed", e.At), e.IDs, e.Reason}, true
	case session.InputWithdrawn:
		return struct {
			sessionHeader

			ID string `json:"id"`
		}{header("input_withdrawn", e.At), e.ID}, true
	case session.SettingsChanged:
		return struct {
			sessionHeader

			Settings settingsDTO `json:"settings"`
			Applied  string      `json:"applied"`
		}{header("settings_changed", e.At), toSettingsDTO(e.Settings), string(e.Applied)}, true
	case session.Idle:
		return header("idle", e.At), true
	case session.Notice:
		return struct {
			sessionHeader

			Level   string `json:"level"`
			Message string `json:"message"`
		}{header("notice", e.At), e.Level, e.Message}, true
	}

	return nil, false
}

// engineEventDTO is the stream shape of the embedded engine's own events.
func engineEventDTO(event core.Event) (any, bool) {
	switch e := event.(type) {
	case engine.CompactionStarted:
		return struct {
			sessionHeader

			Trigger string `json:"trigger"`
			Tokens  int64  `json:"tokens"`
		}{header("compaction_started", e.At), string(e.Trigger), e.Tokens}, true
	case engine.Compacted:
		return struct {
			sessionHeader

			Trigger     string `json:"trigger"`
			Summary     string `json:"summary,omitempty"`
			Error       string `json:"error,omitempty"`
			Interrupted bool   `json:"interrupted,omitempty"`
			Warning     string `json:"warning,omitempty"`
		}{header("compacted", e.At), string(e.Trigger), e.Summary, e.Err, e.Interrupted, e.Warning}, true
	}

	return nil, false
}
