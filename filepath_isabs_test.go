package watcher

import (
	"path/filepath"
	"testing"
)

func TestFilePathIsAbs(t *testing.T) {
	testCases := []struct {
		p    string
		want bool
	}{
		{`C:\`, true},
		{`\`, false},
		{`C:\a`, true},
		{`C:\a\b`, true},
		{`a`, false},
		{`a\b`, false},
	}
	for i, c := range testCases {
		got := filepath.IsAbs(c.p)
		if got != c.want {
			t.Errorf("unexpected result for case %d, p=%s, got=%v, want=%v",
				i, c.p, got, c.want)
		}
	}
}
