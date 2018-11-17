package watcher

import (
	"path/filepath"
	"strings"
)

func hasPrefixFilePath(p, prefix string) bool {
	rel, err := filepath.Rel(prefix, p)
	return err == nil && !strings.HasPrefix(rel, "..")
}
