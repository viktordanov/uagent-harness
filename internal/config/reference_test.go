package config_test

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent-harness/internal/config"
)

// TestReferenceListsEveryKey keeps docs/configuration.md complete: every
// TOML key of Config, including nested tables and MCP servers, must appear
// there in backticks, alone or as a table header.
func TestReferenceListsEveryKey(t *testing.T) {
	doc, err := os.ReadFile("../../docs/configuration.md")
	require.NoError(t, err)

	keys := tomlKeys(reflect.TypeFor[config.Config](), "")
	require.Greater(t, len(keys), 50, "the walk found the nested keys")
	text := string(doc)
	for key, leaf := range keys {
		// A table may appear as its header: `[tui]`, `[[hooks.<Event>]]`.
		mentioned := strings.Contains(text, "`"+leaf+"`") || strings.Contains(text, "`["+leaf) ||
			strings.Contains(text, "`[["+leaf) || strings.Contains(text, "."+leaf+"`")
		assert.True(t, mentioned, "docs/configuration.md does not mention %s in backticks", key)
	}
}

// tomlKeys maps each key's dotted path to its own name, walking structs
// behind pointers, slices, and maps.
func tomlKeys(typ reflect.Type, prefix string) map[string]string {
	keys := map[string]string{}
	for field := range typ.Fields() {
		name, _, _ := strings.Cut(field.Tag.Get("toml"), ",")
		if name == "" || name == "-" {
			continue
		}
		path := prefix + name
		keys[path] = name
		inner := field.Type
		for inner.Kind() == reflect.Pointer || inner.Kind() == reflect.Slice || inner.Kind() == reflect.Map {
			if inner.Kind() == reflect.Map {
				path += ".<name>"
			}
			inner = inner.Elem()
		}
		if inner.Kind() == reflect.Struct {
			for k, v := range tomlKeys(inner, path+".") {
				keys[k] = v
			}
		}
	}

	return keys
}
