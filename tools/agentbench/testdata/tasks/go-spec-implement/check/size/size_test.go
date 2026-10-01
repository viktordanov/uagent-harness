package size

import (
	"errors"
	"testing"
)

func TestParseSize(t *testing.T) {
	for _, c := range []struct {
		in   string
		want int64
	}{
		{"0", 0}, {"42", 42}, {"42B", 42}, {"42 B", 42}, {"  7 kB\n", 7000},
		{"10 KiB", 10240}, {"1.5MiB", 1572864}, {"1.5 B", 1}, {"0.001 kB", 1}, {"0.0009 kB", 0},
		{"2 GB", 2000000000}, {"3 TiB", 3 * 1024 * 1024 * 1024 * 1024}, {"1.25 GiB", 1342177280},
		{"9223372036854775807", 9223372036854775807}, {"8388607.99999 TiB", 9223372036843780691},
		{"007 MB", 7000000},
	} {
		got, err := ParseSize(c.in)
		if err != nil || got != c.want {
			t.Errorf("ParseSize(%q) = %d, %v; want %d", c.in, got, err, c.want)
		}
	}
}

func TestParseSizeErrors(t *testing.T) {
	for _, c := range []struct {
		in   string
		want error
	}{
		{"", ErrSyntax}, {"   ", ErrSyntax}, {"-1", ErrSyntax}, {"+1", ErrSyntax}, {".5", ErrSyntax}, {"5.", ErrSyntax},
		{"1..5", ErrSyntax}, {"5  B", ErrSyntax}, {"5 \tB", ErrSyntax}, {"1,5 kB", ErrSyntax}, {"kB", ErrSyntax},
		{"5 kb", ErrUnit}, {"5 KB", ErrUnit}, {"5 PiB", ErrUnit}, {"5 bytes", ErrUnit}, {"5kib", ErrUnit},
		{"9223372036854775808", ErrRange}, {"8388608 TiB", ErrRange}, {"99999999999999999999999", ErrRange},
	} {
		_, err := ParseSize(c.in)
		if !errors.Is(err, c.want) {
			t.Errorf("ParseSize(%q) error = %v; want %v", c.in, err, c.want)
		}
	}
}

func TestFormatSize(t *testing.T) {
	for _, c := range []struct {
		in   int64
		want string
	}{
		{0, "0 B"}, {1023, "1023 B"}, {1024, "1 KiB"}, {1536, "1.5 KiB"}, {1535, "1.4 KiB"},
		{1048575, "1023.9 KiB"}, {1048576, "1 MiB"}, {5 << 30, "5 GiB"}, {3 << 40, "3 TiB"},
		{1 << 50, "1024 TiB"}, {-1536, "-1.5 KiB"}, {-5, "-5 B"},
	} {
		if got := FormatSize(c.in); got != c.want {
			t.Errorf("FormatSize(%d) = %q; want %q", c.in, got, c.want)
		}
	}
}
