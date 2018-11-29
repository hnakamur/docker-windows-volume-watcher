package watcher

import (
	"testing"
)

func TestHasPrefixFilePath(t *testing.T) {
	testCases := []struct {
		p      string
		prefix string
		want   bool
	}{
		{`C:\`, `C:\`, true},
		{`C:\a`, `C:\`, true},
		{`C:\a\b`, `C:\`, true},
		{`C:\`, `C:\a`, false},
		{`C:\a`, `C:\b`, false},
		{`C:\a\b`, `C:\a\c`, false},
		{`C:\a\b`, `C:\a\bc`, false},
		{`C:\a\b`, `C:\a\b\c`, false},
		{`C:\a\b\c`, `C:\a\b`, true},
		{`C:\a\b\c\d`, `C:\a\b`, true},
		{`a`, `b`, false},
		{`a`, `a`, true},
		{`a\b`, `a`, true},
		{`a\b`, `a\b`, true},
		{`a\b`, `a\c`, false},
		{`a\b`, `b\a`, false},
		{`a\b\c\d`, `a\b`, true},
	}

	for i, c := range testCases {
		got := hasPrefixFilePath(c.p, c.prefix)
		if got != c.want {
			t.Errorf("unexpected result for case %d, p=%s, prefix=%s, got=%v, want=%v",
				i, c.p, c.prefix, got, c.want)
		}
	}
}
