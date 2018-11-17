package watcher

import "path"

func hasPrefixPath(p, prefix string) bool {
	for {
		if p == prefix {
			return true
		}

		if p == "/" {
			break
		}

		p = path.Dir(p)
	}
	return false
}
