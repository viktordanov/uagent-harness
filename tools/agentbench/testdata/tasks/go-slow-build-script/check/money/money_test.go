package money

import "testing"

func TestFormat(t *testing.T) {
	for _, c := range []struct {
		code  string
		minor int64
		want  string
	}{
		{"USD", 1234, "12.34 USD"},
		{"EUR", -5, "-0.05 EUR"},
		{"GBP", 100, "1.00 GBP"},
	} {
		got, err := Format(c.code, c.minor)
		if err != nil || got != c.want {
			t.Errorf("Format(%q, %d) = %q, %v; want %q", c.code, c.minor, got, err, c.want)
		}
	}
}

func TestUnknown(t *testing.T) {
	if _, err := Format("XXX", 1); err == nil {
		t.Error("Format(XXX) did not fail")
	}
}
