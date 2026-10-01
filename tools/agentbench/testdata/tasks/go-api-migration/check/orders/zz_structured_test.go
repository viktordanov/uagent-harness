package orders

import (
	"bytes"
	"strings"
	"testing"

	"example.com/shop/logx"
)

// Each line must be structured: a constant message and the values as
// key/value pairs, so no formatted numbers inside msg.
func TestStructuredLines(t *testing.T) {
	var b bytes.Buffer
	logx.SetOutput(&b)
	o, _ := Place("zed", 3, 4200)
	Cancel(o, "changed mind")
	for _, line := range strings.Split(strings.TrimSpace(b.String()), "\n") {
		if !strings.Contains(line, "msg=") || strings.Contains(line, "!BADKEY") {
			t.Errorf("not structured: %q", line)
		}
		msg := line[strings.Index(line, "msg="):]
		if i := strings.Index(msg[5:], "\""); i >= 0 {
			msg = msg[:5+i]
		}
		if strings.Contains(msg, "4200") || strings.Contains(msg, "zed") {
			t.Errorf("variable data inside the message: %q", line)
		}
	}
	if !strings.Contains(b.String(), "4200") || !strings.Contains(b.String(), "zed") || !strings.Contains(b.String(), "changed mind") {
		t.Errorf("information lost:\n%s", b.String())
	}
}
