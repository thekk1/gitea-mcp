package issue

import (
	"fmt"
	"math/rand"
	"regexp"
	"slices"
	"strings"
	"testing"
)

func regexFind(patterns []*regexp.Regexp, text string) []int {
	found := []int{}
	for i, pattern := range patterns {
		if pattern.MatchString(text) {
			found = append(found, i)
		}
	}
	return found
}

func regexPatterns(terms []string) []*regexp.Regexp {
	patterns := make([]*regexp.Regexp, len(terms))
	for i, term := range terms {
		patterns[i] = regexp.MustCompile(`(?i)(^|[^\p{L}\p{N}])` + regexp.QuoteMeta(term) + `($|[^\p{L}\p{N}])`)
	}
	return patterns
}

func TestTermMatcherWholeWords(t *testing.T) {
	terms := []string{"ABC-12", "foo bar", "#42", "Größe", "x"}
	cases := []struct {
		text string
		want []int
	}{
		{"[abc-12] Titel", []int{0}},
		{"ABC-123 und XABC-12", nil},
		{"ABC-12", []int{0}},
		{"vgl. abc-12, ABC-12.", []int{0}},
		{"ein FOO BAR hier", []int{1}},
		{"foo  bar", nil},
		{"Issue #42 offen", []int{2}},
		{"a#42", nil},
		{"die größe zählt", []int{3}},
		{"größer", nil},
		{"Xäx x", []int{4}},
		{"", nil},
	}
	matcher := newTermMatcher(terms)
	for _, c := range cases {
		got := matcher.find(c.text)
		slices.Sort(got)
		if !slices.Equal(got, c.want) {
			t.Errorf("find(%q) = %v, want %v", c.text, got, c.want)
		}
	}
}

// The matcher has to agree with the regular expression it replaced.
func TestTermMatcherAgreesWithRegex(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	words := []string{"abc-1", "abc-12", "ABC-123", "foo", "bar", "#7", "ä", "Ö-1", "x_y", "(a)", "\n", " ", "-", ".", "é1", "日本"}
	terms := []string{"abc-1", "ABC-12", "abc-123", "foo bar", "#7", "ä", "ö-1", "x_y", "(a)", "日本", "bar."}
	matcher := newTermMatcher(terms)
	patterns := regexPatterns(terms)
	for n := 0; n < 20000; n++ {
		var b strings.Builder
		for i := r.Intn(12); i > 0; i-- {
			b.WriteString(words[r.Intn(len(words))])
			if r.Intn(2) == 0 {
				b.WriteString(" ")
			}
		}
		text := b.String()
		got := matcher.find(text)
		slices.Sort(got)
		want := regexFind(patterns, text)
		if !slices.Equal(append([]int{}, got...), want) {
			t.Fatalf("find(%q) = %v, regex %v", text, got, want)
		}
	}
}

func benchCorpus() ([]string, []string) {
	terms := make([]string, 894)
	for i := range terms {
		terms[i] = fmt.Sprintf("DIFANGEPP2-%d", i*3+100)
	}
	texts := make([]string, 2461)
	body := strings.Repeat("Beschreibung mit normalem Text, https://example.org/browse/DIFANGEPP2-77 und Details. ", 35)
	for i := range texts {
		texts[i] = fmt.Sprintf("[DIFANGEPP2-%d] Titel\n%s", i, body)
	}
	return terms, texts
}

func BenchmarkMatcher(b *testing.B) {
	terms, texts := benchCorpus()
	for b.Loop() {
		matcher := newTermMatcher(terms)
		for _, text := range texts {
			matcher.find(text)
		}
	}
}

func BenchmarkRegex(b *testing.B) {
	terms, texts := benchCorpus()
	patterns := regexPatterns(terms)
	for b.Loop() {
		for _, text := range texts {
			for _, pattern := range patterns {
				pattern.MatchString(text)
			}
		}
	}
}
