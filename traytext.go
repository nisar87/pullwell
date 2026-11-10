package main

import "fmt"

// appRoot is the folder that holds the data folder (set at start).
var appRoot string

// trayTipText is the tray icon's tooltip. Windows shows at most 127 characters.
func trayTipText(active, queued int, speed float64) string {
	var s string
	switch {
	case active > 0:
		s = fmt.Sprintf("%s - %d downloading, %s/s", AppName, active, speedText(speed))
		if queued > 0 {
			s += fmt.Sprintf(", %d waiting", queued)
		}
	case queued > 0:
		s = fmt.Sprintf("%s - %d waiting", AppName, queued)
	default:
		s = AppName + " - nothing downloading"
	}
	if len(s) > 127 {
		s = s[:127]
	}
	return s
}

// speedText: bytes per second in the units the page uses. Impossible values show as 0 (rule 4.5.5).
func speedText(b float64) string {
	switch {
	case !(b > 0) || b > 1e12: // also catches NaN
		return "0 KB"
	case b >= 1e9:
		return fmt.Sprintf("%.2f GB", b/1e9)
	case b >= 1e6:
		return fmt.Sprintf("%.1f MB", b/1e6)
	}
	return fmt.Sprintf("%.0f KB", b/1e3)
}

// Set from the command line (main.go).
var (
	uiMode    string // "auto", "webview2", "app" or "browser"
	debugMode bool
)
