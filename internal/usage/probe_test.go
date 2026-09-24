//go:build probe

package usage_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/unreallabsai/unreal-agent/harness/llm"
	"github.com/unreallabsai/unreal-agent/harness/llm/clients/openaicodex"
	"github.com/unreallabsai/unreal-agent/harness/llm/responsesapi"
	"github.com/unreallabsai/unreal-agent/harness/primitives"

	"github.com/viktordanov/uagent-harness/internal/engine/codexauth"
	"github.com/viktordanov/uagent-harness/internal/llmcall"
	"github.com/viktordanov/uagent-harness/internal/usage"
)

// The probes read the Codex sign-in the way the engine does and never print
// a token or an ID. Run them by hand:
//
//	go test -tags probe -run TestProbe -v ./internal/usage/
func probeCreds(t *testing.T) codexauth.Creds {
	t.Helper()
	config, err := openaicodex.EnvironmentConfig(os.Getenv)
	require.NoError(t, err)
	creds, err := codexauth.Load(config)
	require.NoError(t, err)

	return creds
}

// bodyRecorder keeps the last response body so the probe can log its shape.
type bodyRecorder struct{ body []byte }

func (r *bodyRecorder) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := http.DefaultTransport.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	r.body, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	resp.Body = io.NopCloser(bytes.NewReader(r.body))

	return resp, nil
}

// TestProbeFetch makes one GET /wham/usage and logs the snapshot and the
// body's shape with identifiers redacted.
func TestProbeFetch(t *testing.T) {
	rec := &bodyRecorder{}
	s, err := usage.Fetch(context.Background(), probeCreds(t), usage.Options{Client: &http.Client{
		Transport:     rec,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}})
	var shape any
	_ = json.Unmarshal(rec.body, &shape)
	out, _ := json.MarshalIndent(redact("", shape), "", "  ")
	t.Logf("shape:\n%s", out)
	require.NoError(t, err)
	logSnapshot(t, s)
}

// TestProbeHeaders makes one tiny /responses call through usage.Transport
// and logs the rate-limit headers it saw.
func TestProbeHeaders(t *testing.T) {
	creds := probeCreds(t)
	var got []usage.Snapshot
	var names []string
	remote := primitives.NewRemoteClientWithHTTPClient(&http.Client{
		Transport:     namesRecorder{names: &names, next: usage.Transport{Observe: func(s usage.Snapshot) { got = append(got, s) }}},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	})
	one := 1
	adapter, err := responsesapi.NewAdapter(remote, responsesapi.Config{
		Endpoint: openaicodex.BaseURL + "/responses",
		Headers: map[string][]string{
			"Authorization": {"Bearer " + creds.AccessToken}, "ChatGPT-Account-ID": {creds.AccountID},
			"Content-Type": {"application/json"}, "originator": {"unreal-agent"}, "User-Agent": {"unreal-agent"},
		},
		CacheKeyPlacement: responsesapi.CacheKeyPlacement{UsePromptCacheKeyField: true, Header: "session-id"},
		MaxAttempts:       &one,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = remote.Close() })
	model := os.Getenv("UAH_PROBE_MODEL")
	if model == "" {
		model = "gpt-5.6-luna"
	}
	_, err = llmcall.Call(context.Background(), adapter, llmcall.Request{
		Model: model, Effort: llm.ReasoningEffortLow, Instructions: "Answer with one word.",
		Input: []llm.Item{llmcall.Message(llm.RoleUser, "Say OK.")},
	})
	require.NoError(t, err)
	slices.Sort(names)
	t.Logf("x-codex-* header names: %v", names)
	require.NotEmpty(t, got)
	for _, s := range got {
		logSnapshot(t, s)
	}
}

type namesRecorder struct {
	names *[]string
	next  http.RoundTripper
}

func (n namesRecorder) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := n.next.RoundTrip(req)
	if err == nil {
		for k := range resp.Header {
			if strings.HasPrefix(strings.ToLower(k), "x-codex") {
				*n.names = append(*n.names, strings.ToLower(k))
			}
		}
	}

	return resp, err
}

func logSnapshot(t *testing.T, s usage.Snapshot) {
	t.Helper()
	now := time.Now()
	t.Logf("plan=%q reached=%q credits=%+v stale=%v", s.Plan, s.ReachedType, s.Credits, s.Stale(now))
	for _, l := range s.Limits {
		line := "limit " + l.ID + " " + l.Name + ":"
		if l.Primary != nil {
			line += " " + l.Primary.String(false, now) + " [" + time.Duration(l.Primary.Minutes*int64(time.Minute)).String() + "]"
		}
		if l.Secondary != nil {
			line += "; " + l.Secondary.String(true, now) + " [" + time.Duration(l.Secondary.Minutes*int64(time.Minute)).String() + "]"
		}
		t.Log(line)
	}
}

// redact replaces strings under identifying keys; numbers and booleans stay.
func redact(key string, v any) any {
	switch x := v.(type) {
	case map[string]any:
		for k, e := range x {
			x[k] = redact(k, e)
		}

		return x
	case []any:
		for i, e := range x {
			x[i] = redact(key, e)
		}

		return x
	case string:
		k := strings.ToLower(key)
		if strings.Contains(k, "id") || strings.Contains(k, "email") || strings.Contains(k, "user") || strings.Contains(k, "account") {
			return "<redacted>"
		}

		return x
	default:
		return x
	}
}
