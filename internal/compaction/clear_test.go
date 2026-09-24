package compaction_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/unreallabsai/unreal-agent/harness/llm"

	"github.com/viktordanov/uagent-harness/internal/compaction"
)

func TestClear_DropsEverythingBeforeIt(t *testing.T) {
	history := []llm.Item{msg(llm.RoleSystem, "sys"), msg(llm.RoleUser, "first"), call("a"), result("a", "x"), msg(llm.RoleUser, "new")}
	rec, err := compaction.NewClear(history, time.Now())
	require.NoError(t, err)
	assert.Equal(t, compaction.TriggerClear, rec.Trigger)
	assert.Equal(t, rec.Covered, rec.Floor)
	got, err := compaction.Apply(history, rec)
	require.NoError(t, err)
	assert.Equal(t, []string{"system: sys", "user: new"}, texts(got), "no user message and no summary from before the clear")
}

func TestClear_ALaterCompactionKeepsOnlyMessagesAfterIt(t *testing.T) {
	history := []llm.Item{msg(llm.RoleSystem, "sys"), msg(llm.RoleUser, "before"), msg(llm.RoleAssistant, "ok")}
	clear, err := compaction.NewClear(history, time.Now())
	require.NoError(t, err)
	later := append(history, msg(llm.RoleUser, "after"), call("b"), result("b", "y"))
	rec, err := compaction.NewRecord(later, "s", compaction.TriggerManual, "m", time.Now())
	require.NoError(t, err)
	rec.Floor = clear.Floor
	got, err := compaction.Apply(later, rec)
	require.NoError(t, err)
	assert.Equal(t, []string{"system: sys", "user: after", "user: " + compaction.SummaryPrefix + "\ns"}, texts(got))
}
