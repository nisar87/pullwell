package main

import "strconv"

// restartArgs: the command line for the new version after an update. The old run's own update flags are dropped,
// everything else (for example -root) is kept; the new copy waits for this one (-wait-pid), knows which version it
// replaced (-updated) and starts in the notification area unless the window was open.
func restartArgs(old []string, pid int, showWindow bool) []string {
	var out []string
	for i := 0; i < len(old); i++ {
		a := old[i]
		switch {
		case a == "-tray" || a == "--tray" || a == "-tray=true" || a == "-tray=false":
			continue
		case a == "-wait-pid" || a == "--wait-pid" || a == "-updated" || a == "--updated":
			i++ // and its value
			continue
		case hasPrefix(a, "-wait-pid=") || hasPrefix(a, "--wait-pid=") || hasPrefix(a, "-updated=") || hasPrefix(a, "--updated="):
			continue
		}
		out = append(out, a)
	}
	out = append(out, "-wait-pid="+strconv.Itoa(pid), "-updated="+appVersion+"+"+appBuild)
	if !showWindow {
		out = append(out, "-tray")
	}
	return out
}

func hasPrefix(s, p string) bool { return len(s) >= len(p) && s[:len(p)] == p }
