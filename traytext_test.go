package main

import (
	"math"
	"testing"
)

func TestTrayTipText(t *testing.T) {
	for _, c := range []struct {
		active, queued int
		speed          float64
		want           string
	}{
		{0, 0, 0, AppName + " - nothing downloading"},
		{0, 3, 0, AppName + " - 3 waiting"},
		{2, 0, 4.2e6, AppName + " - 2 downloading, 4.2 MB/s"},
		{1, 5, 950e3, AppName + " - 1 downloading, 950 KB/s, 5 waiting"},
		{1, 0, 1.5e9, AppName + " - 1 downloading, 1.50 GB/s"},
		{1, 0, math.NaN(), AppName + " - 1 downloading, 0 KB/s"},
		{1, 0, -5, AppName + " - 1 downloading, 0 KB/s"},
		{1, 0, 4e15, AppName + " - 1 downloading, 0 KB/s"}, // impossible: a broken counter, not a speed
	} {
		if got := trayTipText(c.active, c.queued, c.speed); got != c.want {
			t.Errorf("trayTipText(%d, %d, %v) = %q, want %q", c.active, c.queued, c.speed, got, c.want)
		}
	}
	if s := trayTipText(1<<40, 1<<40, 1); len(s) > 127 {
		t.Errorf("tooltip is %d characters; Windows allows 127", len(s))
	}
}
