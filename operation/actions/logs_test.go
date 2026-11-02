package actions

import (
	"math"
	"regexp"
	"testing"
)

func TestTailByLines(t *testing.T) {
	in := []byte("a\nb\nc\nd\n")
	got := string(tailByLines(in, 2))
	if got != "c\nd\n" {
		t.Fatalf("tailByLines(...,2) = %q", got)
	}
}

func TestLimitBytesKeepsTailFromRuneStart(t *testing.T) {
	for in, want := range map[string]string{"0123456789": "6789", "é123": "123"} {
		if out := limitBytes([]byte(in), 4); string(out) != want {
			t.Errorf("limitBytes(%q, 4) = %q, want %q", in, out, want)
		}
	}
}

func TestGrepLines(t *testing.T) {
	const in = "a\nERR 1\nb\nc\nd\ne\nERR 2\nf\n"
	for _, tc := range []struct {
		in, pattern string
		context     int
		want        string
	}{
		{in, `\d$`, -1, "ERR 1\n--\nERR 2\n"},
		{in, "ERR", 1, "a\nERR 1\nb\n--\ne\nERR 2\nf\n"},
		{in, "ERR", 2, in},
		{in, "ERR", math.MaxInt, in},
		{in, "none", 2, ""},
		{"a\nERR", "ERR", 1, "a\nERR"},
		{"", "ERR", 1, ""},
	} {
		if got, _ := grepLines([]byte(tc.in), regexp.MustCompile(tc.pattern), tc.context, 0); string(got) != tc.want {
			t.Errorf("grepLines(%q, %q, %d) = %q, want %q", tc.in, tc.pattern, tc.context, got, tc.want)
		}
	}
}
