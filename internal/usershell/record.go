package usershell

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// The tags around a command the user ran, as Codex's UserShellCommand
// context fragment (codex-rs/core/src/context/user_shell_command.rs).
const (
	openTag  = "<user_shell_command>"
	closeTag = "</user_shell_command>"
)

// Record is what the model sees of a command the user ran: the command,
// its exit code, how long it took, and its output, bounded.
type Record struct {
	Command  string
	ExitCode int
	Duration time.Duration
	Output   string
}

// Text is the record as a user message, in Codex's format:
//
//	<user_shell_command>
//	<command>
//	echo hi
//	</command>
//	<result>
//	Exit code: 0
//	Duration: 1.0000 seconds
//	Output:
//	hi
//	</result>
//	</user_shell_command>
func (r Record) Text() string {
	return fmt.Sprintf("%s\n<command>\n%s\n</command>\n<result>\nExit code: %d\nDuration: %.4f seconds\nOutput:\n%s\n</result>\n%s",
		openTag, r.Command, r.ExitCode, r.Duration.Seconds(), r.Output, closeTag)
}

// Parse reads a message that Text wrote, such as the runner's echo of it
// or a message in a saved run, so a resumed transcript shows it as a
// command. ok is false for any other message.
func Parse(text string) (Record, bool) {
	body, ok := strings.CutPrefix(text, openTag+"\n<command>\n")
	if !ok {
		return Record{}, false
	}
	if body, ok = strings.CutSuffix(body, "\n</result>\n"+closeTag); !ok {
		return Record{}, false
	}
	command, rest, ok := strings.Cut(body, "\n</command>\n<result>\nExit code: ")
	if !ok {
		return Record{}, false
	}
	code, rest, ok := strings.Cut(rest, "\nDuration: ")
	if !ok {
		return Record{}, false
	}
	seconds, output, ok := strings.Cut(rest, " seconds\nOutput:\n")
	if !ok {
		return Record{}, false
	}
	exit, err := strconv.Atoi(code)
	if err != nil {
		return Record{}, false
	}
	secs, err := strconv.ParseFloat(seconds, 64)
	if err != nil {
		return Record{}, false
	}

	return Record{Command: command, ExitCode: exit, Duration: time.Duration(secs * float64(time.Second)), Output: output}, true
}
