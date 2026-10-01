package money

import "testing"

func TestFormatHidden(t *testing.T) {
	for _, c := range []struct {
		code  string
		minor int64
		want  string
	}{
		{"JPY", 1234, "1234 JPY"},
		{"JPY", -7, "-7 JPY"},
		{"KWD", 1234, "1.234 KWD"},
		{"KWD", 5, "0.005 KWD"},
		{"USD", 1234, "12.34 USD"},
	} {
		got, err := Format(c.code, c.minor)
		if err != nil || got != c.want {
			t.Errorf("Format(%q, %d) = %q, %v; want %q", c.code, c.minor, got, err, c.want)
		}
	}
	for code, want := range map[string]string{"JPY": "Japanese yen", "KWD": "Kuwaiti dinar"} {
		if got, err := Name(code); err != nil || got != want {
			t.Errorf("Name(%s) = %q, %v; want %q", code, got, err, want)
		}
	}
}
