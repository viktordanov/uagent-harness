package sandbox_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/viktordanov/uagent-harness/internal/sandbox"
)

func TestEnvPolicyApply(t *testing.T) {
	environ := []string{
		"PATH=/usr/bin",
		"HOME=/home/u",
		"OPENAI_API_KEY=sk-1",
		"GITHUB_TOKEN=gh",
		"AWS_SECRET_ACCESS_KEY=aws",
		"EDITOR=vi",
		"lang=C",
		"EMPTY=",
		"WEIRD=a=b",
	}
	no := false
	yes := true
	cases := []struct {
		name   string
		policy sandbox.EnvPolicy
		want   []string
	}{
		{"default inherits everything", sandbox.EnvPolicy{}, environ},
		{"explicit all", sandbox.EnvPolicy{Inherit: sandbox.InheritAll, IgnoreDefaultExcludes: &yes}, environ},
		{"core", sandbox.EnvPolicy{Inherit: sandbox.InheritCore}, []string{"PATH=/usr/bin", "HOME=/home/u", "lang=C"}},
		{"none", sandbox.EnvPolicy{Inherit: sandbox.InheritNone}, []string{}},
		{
			"default excludes",
			sandbox.EnvPolicy{IgnoreDefaultExcludes: &no},
			[]string{"PATH=/usr/bin", "HOME=/home/u", "EDITOR=vi", "lang=C", "EMPTY=", "WEIRD=a=b"},
		},
		{
			"exclude is case-insensitive",
			sandbox.EnvPolicy{Exclude: []string{"*_key", "ed?tor", "LANG"}},
			[]string{"PATH=/usr/bin", "HOME=/home/u", "GITHUB_TOKEN=gh", "EMPTY=", "WEIRD=a=b"},
		},
		{
			"set overrides and adds",
			sandbox.EnvPolicy{Inherit: sandbox.InheritCore, Set: map[string]string{"PATH": "/bin", "CI": "1", "A": "2"}},
			[]string{"PATH=/bin", "HOME=/home/u", "lang=C", "A=2", "CI=1"},
		},
		{
			"set survives exclude but not include_only",
			sandbox.EnvPolicy{Exclude: []string{"*"}, Set: map[string]string{"CI": "1", "X_TOKEN": "t"}, IncludeOnly: []string{"ci", "path"}},
			[]string{"CI=1"},
		},
		{
			"include_only",
			sandbox.EnvPolicy{IncludeOnly: []string{"PATH", "*_TOKEN"}},
			[]string{"PATH=/usr/bin", "GITHUB_TOKEN=gh"},
		},
		{
			"set runs after default excludes",
			sandbox.EnvPolicy{Inherit: sandbox.InheritNone, IgnoreDefaultExcludes: &no, Set: map[string]string{"MY_TOKEN": "x"}},
			[]string{"MY_TOKEN=x"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, c.policy.Apply(environ))
		})
	}
}
