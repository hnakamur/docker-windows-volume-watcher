package watcher

import (
	"testing"
)

func TestHasPrefixPath(t *testing.T) {
	testCases := []struct {
		p      string
		prefix string
		want   bool
	}{
		{"/", "/", true},
		{"/a", "/", true},
		{"/a/b", "/", true},
		{"/", "/a", false},
		{"/a", "/b", false},
		{"/a/b", "/a/c", false},
		{"/a/b", "/a/bc", false},
		{"/a/b", "/a/b/c", false},
		{"/a/b/c", "/a/b", true},
		{"/a/b/c/d", "/a/b", true},
	}

	for i, c := range testCases {
		got := hasPrefixPath(c.p, c.prefix)
		if got != c.want {
			t.Errorf("unexpected result for case %d, p=%s, prefix=%s, got=%v, want=%v",
				i, c.p, c.prefix, got, c.want)
		}
	}
}
