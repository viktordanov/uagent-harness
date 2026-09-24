// Package modelstest serves model catalogs to tests without the network.
package modelstest

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/viktordanov/uagent-harness/internal/models"
)

// Source is a models.Source that returns a fixed list, or Err.
type Source struct {
	ID     string
	Models []models.Model
	Err    error
	// Calls counts Fetch calls.
	Calls *atomic.Int32
}

func (s Source) Identity() string { return s.ID }

func (s Source) Fetch(context.Context, string) (models.Fetched, error) {
	if s.Calls != nil {
		s.Calls.Add(1)
	}
	if s.Err != nil {
		return models.Fetched{}, s.Err
	}

	return models.Fetched{Models: s.Models}, nil
}

// Manager is a manager for provider whose every source is src, cached in a
// temporary directory.
func Manager(tb testing.TB, provider string, src Source) *models.Manager {
	tb.Helper()
	if src.ID == "" {
		src.ID = "test-identity"
	}

	return models.New(models.Options{
		Dir: tb.TempDir(), Provider: provider,
		NewSource: func(string, string, func(string) string) (models.Source, error) { return src, nil },
	})
}

// IDs are models with only IDs.
func IDs(ids ...string) []models.Model {
	out := make([]models.Model, 0, len(ids))
	for _, id := range ids {
		out = append(out, models.Model{ID: id})
	}

	return out
}
