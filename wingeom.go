package main

// Saved window size and position (design section 4.3). The saved values are checked against the monitors that exist
// now: a monitor may have been unplugged, and a damaged file may hold impossible numbers (rule 4.5.5).

type winRect struct{ L, T, R, B int }

func (r winRect) w() int { return r.R - r.L }
func (r winRect) h() int { return r.B - r.T }

const (
	winMinW, winMinH         = 720, 480
	winDefW, winDefH         = 1100, 760
	winVisibleW, winVisibleH = 160, 48 // this much of the title bar must be on a monitor to count as reachable
)

// fixWindowRect returns where to open the window. monitors are the work areas of all monitors (the screen minus the
// taskbar), primary is the main one. A zero saved rect means "never saved".
func fixWindowRect(saved winRect, monitors []winRect, primary winRect) winRect {
	return fixWindowRectScaled(saved, monitors, primary, 1)
}

// fixWindowRectScaled is fixWindowRect for a screen with scale times the normal pixel density (1.5 at 144 DPI): the
// default and minimum sizes grow with it. Coordinates are physical pixels.
func fixWindowRectScaled(saved winRect, monitors []winRect, primary winRect, scale float64) winRect {
	if !(scale >= 1) || scale > 4 {
		scale = 1
	}
	minW, minH := int(float64(winMinW)*scale), int(float64(winMinH)*scale)
	defW, defH := int(float64(winDefW)*scale), int(float64(winDefH)*scale)
	if primary.w() <= 0 || primary.h() <= 0 {
		primary = winRect{0, 0, 1280, 720} // no monitor information: a safe guess
	}
	w, h := saved.w(), saved.h()
	if saved == (winRect{}) || w <= 0 || h <= 0 || w > 20000 || h > 20000 {
		w, h = defW, defH
	}
	if !onAMonitor(saved, monitors) {
		return centred(clampSize(w, h, minW, minH, primary), primary)
	}
	// Keep the position, but shrink it to fit the monitor it is on.
	mon := monitorOf(saved, monitors)
	r := clampSize(w, h, minW, minH, mon)
	r = winRect{saved.L, saved.T, saved.L + r.w(), saved.T + r.h()}
	if r.R > mon.R {
		r.L, r.R = mon.R-r.w(), mon.R
	}
	if r.B > mon.B {
		r.T, r.B = mon.B-r.h(), mon.B
	}
	if r.L < mon.L {
		r.L, r.R = mon.L, mon.L+r.w()
	}
	if r.T < mon.T {
		r.T, r.B = mon.T, mon.T+r.h()
	}
	return r
}

// clampSize gives a rect of the wanted size, at least the minimum and at most the monitor (the minimum wins on a
// monitor smaller than the minimum).
func clampSize(w, h, minW, minH int, mon winRect) winRect {
	if w > mon.w() {
		w = mon.w()
	}
	if h > mon.h() {
		h = mon.h()
	}
	if w < minW {
		w = minW
	}
	if h < minH {
		h = minH
	}
	return winRect{0, 0, w, h}
}

func centred(size winRect, mon winRect) winRect {
	l := mon.L + (mon.w()-size.w())/2
	t := mon.T + (mon.h()-size.h())/2
	if t < mon.T {
		t = mon.T // the title bar must stay reachable
	}
	if l < mon.L {
		l = mon.L
	}
	return winRect{l, t, l + size.w(), t + size.h()}
}

// onAMonitor: enough of the title bar strip (the top 48 pixels) is on one monitor for the person to grab it.
func onAMonitor(r winRect, monitors []winRect) bool {
	if r.w() <= 0 || r.h() <= 0 {
		return false
	}
	bar := winRect{r.L, r.T, r.R, r.T + winVisibleH}
	for _, m := range monitors {
		if overlapW(bar, m) >= winVisibleW && bar.T >= m.T && bar.B <= m.B {
			return true
		}
	}
	return false
}

func monitorOf(r winRect, monitors []winRect) winRect {
	best, bestArea := winRect{}, -1
	for _, m := range monitors {
		if a := overlapW(r, m) * overlapH(r, m); a > bestArea {
			best, bestArea = m, a
		}
	}
	return best
}

func overlapW(a, b winRect) int { return max(0, min(a.R, b.R)-max(a.L, b.L)) }
func overlapH(a, b winRect) int { return max(0, min(a.B, b.B)-max(a.T, b.T)) }
