package codereview

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Output is the reviewer's answer, Codex's ReviewOutputEvent: the
// findings, a verdict ("patch is correct" or "patch is incorrect"), its
// explanation, and the reviewer's confidence.
type Output struct {
	Findings               []Finding `json:"findings"`
	OverallCorrectness     string    `json:"overall_correctness"`
	OverallExplanation     string    `json:"overall_explanation"`
	OverallConfidenceScore float64   `json:"overall_confidence_score"`
}

// Finding is one issue: a title (which the rubric starts with "[P1]"), a
// Markdown body, the reviewer's confidence, a priority from 0 (P0) to 3,
// and where it is.
type Finding struct {
	Title           string   `json:"title"`
	Body            string   `json:"body"`
	ConfidenceScore float64  `json:"confidence_score"`
	Priority        *int     `json:"priority"`
	CodeLocation    Location `json:"code_location"`
}

// Location is a finding's file and its lines, inclusive.
type Location struct {
	AbsoluteFilePath string    `json:"absolute_file_path"`
	LineRange        LineRange `json:"line_range"`
}

// LineRange is a finding's first and last line.
type LineRange struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// FallbackMessage is Codex's text for a review with nothing to show.
const FallbackMessage = "Reviewer failed to output a response."

// Parse reads the reviewer's last message as Codex does: the whole text as
// JSON, else the text from its first "{" to its last "}" (a fenced or
// explained answer), else the whole text as the explanation, without
// findings. Unlike Codex, a finding without a priority still parses: the
// rubric allows leaving it out.
func Parse(text string) Output {
	var out Output
	if json.Unmarshal([]byte(text), &out) == nil {
		return out
	}
	if i, j := strings.Index(text, "{"), strings.LastIndex(text, "}"); i >= 0 && j > i {
		if json.Unmarshal([]byte(text[i:j+1]), &out) == nil {
			return out
		}
	}

	return Output{OverallExplanation: text}
}

// Location is "path:start-end", Codex's form.
func (f Finding) Location() string {
	l := f.CodeLocation

	return fmt.Sprintf("%s:%d-%d", l.AbsoluteFilePath, l.LineRange.Start, l.LineRange.End)
}

// Text is the review as Codex shows it and hands it to the main agent
// (render_review_output_text): the explanation, then the findings block,
// or FallbackMessage when both are empty.
func (o Output) Text() string {
	var sections []string
	if e := strings.TrimSpace(o.OverallExplanation); e != "" {
		sections = append(sections, e)
	}
	if len(o.Findings) > 0 {
		sections = append(sections, strings.TrimSpace(findingsBlock(o.Findings)))
	}
	if len(sections) == 0 {
		return FallbackMessage
	}

	return strings.Join(sections, "\n\n")
}

// findingsBlock is Codex's format_review_findings_block without a
// selection: a header, then "- title — path:start-end" and the body
// indented under it, for each finding.
func findingsBlock(findings []Finding) string {
	lines := []string{""}
	if len(findings) > 1 {
		lines = append(lines, "Full review comments:")
	} else {
		lines = append(lines, "Review comment:")
	}
	for _, f := range findings {
		lines = append(lines, "", fmt.Sprintf("- %s — %s", f.Title, f.Location()))
		for line := range strings.Lines(f.Body) {
			lines = append(lines, "  "+strings.TrimRight(line, "\r\n"))
		}
	}

	return strings.Join(lines, "\n")
}

// ExitMessage is what the main agent gets after a review, as Codex records
// it in the thread: the review's results in Codex's <user_action> message,
// or the interrupted form. It goes with the user's next message.
func ExitMessage(o Output, interrupted bool) string {
	if interrupted {
		return exitInterrupted
	}
	results := strings.TrimSpace(o.OverallExplanation)
	if len(o.Findings) > 0 {
		results += "\n" + findingsBlock(o.Findings)
	}

	return strings.Replace(exitSuccess, "{{results}}", results, 1)
}

// IsExitMessage reports whether a user message is ExitMessage's, so the
// transcript shows it as a note instead of a message the user wrote.
func IsExitMessage(text string) bool {
	return strings.HasPrefix(strings.TrimSpace(text), "<user_action>") && strings.Contains(text, "<action>review</action>")
}
