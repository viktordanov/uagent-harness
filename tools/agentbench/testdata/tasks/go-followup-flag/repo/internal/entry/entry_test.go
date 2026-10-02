package entry

import (
	"strings"
	"testing"
	"time"
)

func TestReadAll(t *testing.T) {
	in := `{"time":"2026-03-01T10:00:00Z","level":"info","msg":"a"}
not json
{"time":"2026-03-01T10:00:05Z","level":"WARNING","msg":"b","fields":{"k":1}}
`
	var warn strings.Builder
	es, err := ReadAll(strings.NewReader(in), &warn)
	if err != nil {
		t.Fatal(err)
	}
	if len(es) != 2 || es[1].Level != Warn || !es[1].Time.Equal(time.Date(2026, 3, 1, 10, 0, 5, 0, time.UTC)) {
		t.Fatalf("got %+v", es)
	}
	if !strings.Contains(warn.String(), "line 2") {
		t.Errorf("warning %q does not name line 2", warn.String())
	}
}
