package embedded

import (
	"encoding/json/v2"
	"fmt"
	"maps"
	"time"

	"github.com/viktordanov/unreal-agent/harness/coordinator"
	"github.com/viktordanov/unreal-agent/harness/llm"
	"github.com/viktordanov/unreal-agent/harness/tool"
	"github.com/viktordanov/unreal-agent/harness/tool/bash"
)

const (
	// wakeDebounce is how long wake-debounce holds a result while calls run.
	wakeDebounce = 2 * time.Second
	// foregroundYield is how long wake-foreground waits for a Bash call:
	// the longest yield_time_ms Codex's exec_command allows. Its default is
	// 10 s, but a yielded call costs uah a turn.
	foregroundYield = 30 * time.Second
	// longForegroundYield is wake-foreground-long's: the longest wait of
	// Codex's write_stdin on a running command.
	longForegroundYield = 5 * time.Minute
)

// bashForegroundDescription is Bash's description under wake-foreground,
// with the yield.
const bashForegroundDescription = "Execute a shell command. The turn waits for the command to finish, up to %s; a command still running then continues in the background, you get its output so far, and its result arrives in a later turn. Set background to true only for a command you must not wait on, such as a dev server. Independent commands may be issued as parallel tool calls in one turn. Command child processes are killed when the shell exits."

// wakePolicy is the coordinator's wake policy for the wake experiments; it
// is the default when none is on.
func (x experiments) wakePolicy() coordinator.WakePolicy {
	var p coordinator.WakePolicy
	p.Batch, p.AllDone = x.wakeNoPlaceholder, x.wakeAllDone
	if x.wakeDebounce {
		p.Debounce = wakeDebounce
	}
	if x.wakeForeground > 0 {
		p.Yield, p.Progress = bashYield(x.wakeForeground), bash.Progress
	}

	return p
}

// bashYield returns how long a turn waits for a call under wake-foreground:
// yield for a Bash call that is not in the background; other tools do not
// hold the turn. A first test offered Codex's yield_time_ms instead, and the
// model set 1000 on 40% of its commands, long tests included, as it does
// with Codex, which polls them; so the switch is a boolean.
func bashYield(yield time.Duration) func(llm.ToolCall) time.Duration {
	return func(call llm.ToolCall) time.Duration {
		if call.Name != tool.BashName {
			return 0
		}
		var args struct {
			Background bool `json:"background"`
		}
		if json.Unmarshal([]byte(call.Arguments), &args) == nil && args.Background {
			return 0
		}

		return yield
	}
}

// foregroundRegistry offers Bash with background under wake-foreground.
type foregroundRegistry struct {
	tool.Registry

	yield time.Duration
}

func (r foregroundRegistry) StaticDefinitions() []tool.Definition {
	defs := r.Registry.StaticDefinitions()
	for i, d := range defs {
		if d.Tool.Name == tool.BashName {
			defs[i].Tool = bashForeground(d.Tool, r.yield)
		}
	}

	return defs
}

// bashForeground is Bash with the background argument and a description of
// the wait.
func bashForeground(base llm.Tool, yield time.Duration) llm.Tool {
	params := maps.Clone(base.Parameters)
	props, _ := params["properties"].(map[string]any)
	props = maps.Clone(props)
	props["background"] = property("boolean",
		"Return at once and deliver the result in a later turn, instead of waiting up to "+waitText(yield)+".")
	params["properties"] = props
	base.Parameters, base.Description = params, fmt.Sprintf(bashForegroundDescription, waitText(yield))

	return base
}

// waitText is a yield in words, such as "30 seconds" or "5 minutes".
func waitText(d time.Duration) string {
	if d%time.Minute == 0 {
		return fmt.Sprintf("%d minutes", d/time.Minute)
	}

	return fmt.Sprintf("%d seconds", d/time.Second)
}
