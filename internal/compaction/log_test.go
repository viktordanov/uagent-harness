package compaction_test

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent-harness/internal/compaction"
)

func TestLog_SkipsUnreadableLinesAndEndsACutLine(t *testing.T) {
	log := compaction.OpenLog(t.TempDir(), "s1")
	rec, corrupt, err := log.Last()
	require.NoError(t, err)
	assert.Nil(t, rec, "no log yet")
	assert.Zero(t, corrupt)

	first := compaction.Record{Covered: 3, Hash: "h1", Summary: "one", Trigger: compaction.TriggerManual, At: time.Unix(1, 0).UTC()}
	require.NoError(t, log.Append(first))
	f, err := os.OpenFile(log.Path(), os.O_WRONLY|os.O_APPEND, 0o600)
	require.NoError(t, err)
	_, err = f.WriteString("not json\n{\"covered\":0}\n{\"covered\":5,\"hash\":\"h2\",\"summ") // a crash mid-write
	require.NoError(t, err)
	require.NoError(t, f.Close())

	rec, corrupt, err = log.Last()
	require.NoError(t, err)
	assert.Equal(t, 3, corrupt)
	assert.Equal(t, &first, rec, "the last readable record applies")

	second := compaction.Record{Covered: 7, Hash: "h3", Summary: "two", Trigger: compaction.TriggerAuto, At: time.Unix(2, 0).UTC()}
	require.NoError(t, log.Append(second))
	records, corrupt, err := log.Records()
	require.NoError(t, err)
	assert.Equal(t, 3, corrupt, "the cut line was ended, so the new one reads")
	assert.Equal(t, []compaction.Record{first, second}, records)
}
