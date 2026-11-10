package dm

import (
	"path/filepath"
	"strings"
)

// dlLongPath lets Windows open paths longer than 260 characters (torrents can have deep folders).
func dlLongPath(p string) string {
	if len(p) < 240 || strings.HasPrefix(p, `\\?\`) {
		return p
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	if strings.HasPrefix(abs, `\\`) {
		return `\\?\UNC\` + abs[2:]
	}
	return `\\?\` + abs
}
