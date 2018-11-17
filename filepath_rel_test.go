package watcher

import (
	"path/filepath"
	"testing"
)

func TestFilePathRel(t *testing.T) {
	testCases := []struct {
		basepath string
		targpath string
		want     string
	}{
		{`C:\`, `C:\`, `.`},
		{`C:\a`, `C:\`, `..`},
		{`C:\a\b`, `C:\`, `..\..`},
		{`C:\`, `C:\a`, `a`},
		{`C:\`, `C:\a\b`, `a\b`},
		{`C:\a`, `C:\a`, `.`},
		{`C:\a`, `C:\a\b`, `b`},
		{`C:\a`, `C:\a\b\c`, `b\c`},
		{`C:\a`, `C:\b`, `..\b`},
		{`C:\a\b`, `C:\a\c`, `..\c`},
		{`C:\a\b`, `C:\a\bc`, `..\bc`},
		{`C:\a\b`, `C:\a\b\c`, `c`},
		{`C:\a\b\c`, `C:\a\b`, `..`},
		{`C:\a\b\c\d`, `C:\a\b`, `..\..`},
	}
	for i, c := range testCases {
		got, err := filepath.Rel(c.basepath, c.targpath)
		if err != nil {
			t.Errorf("unexpected error for case %d, basepath=%s, targpath=%s, err=%s",
				i, c.basepath, c.targpath, err)
		}
		if got != c.want {
			t.Errorf("unexpected result for case %d, basepath=%s, targpath=%s, got=%s, want=%s",
				i, c.basepath, c.targpath, got, c.want)
		}
	}
}
