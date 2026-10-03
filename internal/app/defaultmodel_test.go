package app_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uah/internal/app"
	"github.com/viktordanov/uah/internal/config"
	"github.com/viktordanov/uah/internal/models"
	"github.com/viktordanov/uah/internal/session"
)

// modelsServer is the ChatGPT backend's models endpoint listing slugs.
func modelsServer(t *testing.T, slugs ...string) string {
	t.Helper()
	body := `{"models":[`
	for i, s := range slugs {
		if i > 0 {
			body += ","
		}
		body += `{"slug":"` + s + `","visibility":"list","priority":` + string(rune('1'+i)) + `}`
	}
	body += `]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" || r.URL.Query().Get("client_version") != models.CodexClientVersion {
			http.NotFound(w, r)

			return
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	return srv.URL
}

// TestDefaultModel: on openai-codex without a named model, a session gets
// gpt-6.1-sol when the login's list has it, else gpt-6-sol; a named model
// stays whatever the list says. On openai the fallback is gpt-6-astra.
func TestDefaultModel(t *testing.T) {
	for name, c := range map[string]struct {
		slugs []string
		model string
		want  string
	}{
		"listed":             {slugs: []string{"gpt-6.1-sol", "gpt-6-sol"}, want: "gpt-6.1-sol"},
		"not yet rolled out": {slugs: []string{"gpt-6-astra", "gpt-6-sol", "gpt-6-luna"}, want: "gpt-6-sol"},
		"no list":            {want: "gpt-6-sol"},
		"named":              {slugs: []string{"gpt-6.1-sol"}, model: "gpt-6-luna", want: "gpt-6-luna"},
	} {
		t.Run(name, func(t *testing.T) {
			_, in := setupEnv(t)
			if c.slugs != nil {
				in.BaseURL = modelsServer(t, c.slugs...)
			}
			in.Model = c.model

			res, err := app.Setup(context.Background(), in, io.Discard)

			require.NoError(t, err)
			assert.Equal(t, c.want, res.Options.Settings.Model)
		})
	}
}

// TestEffortNotice: effort ultra on a model whose entry does not list it
// opens the session with a warning; the provider decides.
func TestEffortNotice(t *testing.T) {
	_, in := setupEnv(t)
	in.Model, in.Effort = "gpt-6-luna", "ultra"

	res, err := app.Setup(context.Background(), in, io.Discard)

	require.NoError(t, err)
	assert.Equal(t, "ultra", res.Options.Settings.Effort)
	assert.Contains(t, res.Options.Notices, "gpt-6-luna does not list effort ultra (it lists low, medium, high, xhigh, max); the provider may refuse it")

	in.Model = "gpt-6-sol"
	res, err = app.Setup(context.Background(), in, io.Discard)
	require.NoError(t, err)
	assert.Empty(t, res.Options.Notices)
}

// TestVerbosity: --model-verbosity wins over model_verbosity, and a model
// whose entry does not support verbosity opens with Codex's warning.
func TestVerbosity(t *testing.T) {
	_, in := setupEnv(t)
	in.Model = "gpt-6.1-sol"
	r, err := app.Resolve(in, session.Info{}, config.Config{ModelVerbosity: "high"})
	require.NoError(t, err)
	assert.Equal(t, "high", r.Verbosity)
	in.ModelVerbosity = "medium"
	r, err = app.Resolve(in, session.Info{}, config.Config{ModelVerbosity: "high"})
	require.NoError(t, err)
	assert.Equal(t, "medium", r.Verbosity)

	res, err := app.Setup(context.Background(), in, io.Discard)
	require.NoError(t, err)
	assert.Empty(t, res.Options.Notices)

	in.Model = "gpt-unlisted"
	res, err = app.Setup(context.Background(), in, io.Discard)
	require.NoError(t, err)
	assert.Contains(t, res.Options.Notices, "model_verbosity is set but ignored as the model does not support verbosity: gpt-unlisted")
}

func TestDefaultModelFromCatalog(t *testing.T) {
	t.Parallel()
	live := models.Catalog{Origin: models.OriginLive, Models: []models.Model{{ID: "gpt-6.1-sol"}}}
	assert.Equal(t, app.DefaultCodexModel, app.DefaultModel(app.CodexProvider, live))
	assert.Equal(t, app.DefaultCodexModel, app.DefaultModel(models.ProviderOpenAI, live))
	assert.Equal(t, app.FallbackCodexModel, app.DefaultModel(app.CodexProvider, models.Bundled(models.ProviderCodex)), "the bundled list lists it for every login")
	assert.Equal(t, "gpt-6-astra", app.DefaultModel(models.ProviderOpenAI, models.Bundled(models.ProviderOpenAI)), "the runner's default")

	r := app.Resolved{Settings: session.Settings{Model: "gpt-6-luna"}}
	r.SettleModel(live)
	assert.Equal(t, "gpt-6-luna", r.Settings.Model, "a named model stays")
}
