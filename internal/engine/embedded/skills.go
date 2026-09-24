package embedded

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/unreallabsai/unreal-agent/harness/tool"

	"github.com/viktordanov/uagent-harness/internal/config"
	"github.com/viktordanov/uagent-harness/internal/instructions"
)

// skillRoots are the directories holding <name>/SKILL.md skills, most
// specific first: Codex's .agents/skills from the workspace up to the
// project root, the runner's own .harness/skills, then the user's
// ~/.config/uagent/skills and Codex's $CODEX_HOME/skills.
func skillRoots(workspace string, getenv func(string) string) []string {
	var roots []string
	if dirs, err := instructions.ProjectDirs(workspace, nil); err == nil {
		for _, dir := range slices.Backward(dirs) {
			roots = append(roots, filepath.Join(dir, ".agents", "skills"))
		}
	}
	roots = append(roots, filepath.Join(workspace, ".harness", "skills"), filepath.Join(config.Dir(), "skills"))
	codexHome := strings.TrimSpace(getenv("CODEX_HOME"))
	if codexHome == "" {
		if home, err := os.UserHomeDir(); err == nil {
			codexHome = filepath.Join(home, ".codex")
		}
	}
	if codexHome != "" {
		roots = append(roots, filepath.Join(codexHome, "skills"))
	}

	return roots
}

// discoverSkills reads the skills under every root with the runner's own
// SKILL.md parser; a name found in a more specific root wins.
func discoverSkills(workspace string, getenv func(string) string) ([]tool.Skill, []error) {
	var skills []tool.Skill
	var errs []error
	seen := map[string]bool{}
	for _, root := range skillRoots(workspace, getenv) {
		found, rootErrs := tool.DiscoverSkills(root)
		errs = append(errs, rootErrs...)
		for _, s := range found {
			if !seen[s.Name] {
				seen[s.Name] = true
				skills = append(skills, s)
			}
		}
	}

	return skills, errs
}

// CodexSkills names the skills found in Codex's folders, which only the
// embedded engine loads: every skill root except the runner's own
// .harness/skills.
func CodexSkills(workspace string, getenv func(string) string) []string {
	runners := filepath.Join(workspace, ".harness", "skills")
	var names []string
	for _, root := range skillRoots(workspace, getenv) {
		if root == runners {
			continue
		}
		found, _ := tool.DiscoverSkills(root)
		for _, s := range found {
			if !slices.Contains(names, s.Name) {
				names = append(names, s.Name)
			}
		}
	}

	return names
}
