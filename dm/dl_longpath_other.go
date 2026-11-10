//go:build !windows

package dm

func dlLongPath(p string) string { return p }
