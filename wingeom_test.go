package main

import "testing"

func TestFixWindowRect(t *testing.T) {
	primary := winRect{0, 0, 1920, 1040}         // 1080p minus a 40 px taskbar
	right := winRect{1920, 0, 1920 + 2560, 1400} // a second, larger monitor to the right
	both := []winRect{primary, right}

	cases := []struct {
		name     string
		saved    winRect
		monitors []winRect
		want     winRect
	}{
		{"never saved: default size, centred", winRect{}, both, winRect{410, 140, 1510, 900}},
		{"saved on the main monitor: kept", winRect{100, 50, 1300, 850}, both, winRect{100, 50, 1300, 850}},
		{"saved on the second monitor: kept", winRect{2200, 100, 3400, 900}, both, winRect{2200, 100, 3400, 900}},
		{"second monitor unplugged: centred on main", winRect{2200, 100, 3400, 900}, []winRect{primary}, winRect{360, 120, 1560, 920}},
		{"title bar above the screen: centred", winRect{100, -500, 1300, 300}, both, winRect{360, 120, 1560, 920}},
		{"too small: grown to the minimum", winRect{100, 100, 300, 200}, both, winRect{100, 100, 100 + winMinW, 100 + winMinH}},
		{"too large: shrunk to the monitor", winRect{0, 0, 5000, 3000}, []winRect{primary}, winRect{0, 0, 1920, 1040}},
		{"hangs off the right edge: pulled in", winRect{1500, 100, 2600, 900}, []winRect{primary}, winRect{820, 100, 1920, 900}},
		{"hangs off the bottom: pulled up", winRect{100, 600, 1200, 1400}, []winRect{primary}, winRect{100, 240, 1200, 1040}},
		{"impossible numbers: default", winRect{0, 0, -5, 999999}, both, winRect{410, 140, 1510, 900}},
		{"no monitor information", winRect{}, nil, winRect{90, 0, 1190, 720}},
	}
	for _, c := range cases {
		mons := c.monitors
		prim := primary
		if mons == nil {
			prim = winRect{}
		}
		if got := fixWindowRect(c.saved, mons, prim); got != c.want {
			t.Errorf("%s: got %+v, want %+v", c.name, got, c.want)
		}
	}
}

func TestFixWindowRectSmallScreen(t *testing.T) {
	tiny := winRect{0, 0, 640, 440} // smaller than the minimum window
	got := fixWindowRect(winRect{}, []winRect{tiny}, tiny)
	if got.w() != winMinW || got.h() != winMinH || got.L != 0 || got.T != 0 {
		t.Fatalf("got %+v: want the minimum size with the title bar on screen", got)
	}
}

func TestFixWindowRectScaled(t *testing.T) {
	work := winRect{0, 0, 2560, 1400} // 150 % scaling on a 1440p screen
	got := fixWindowRectScaled(winRect{}, []winRect{work}, work, 1.5)
	if got.w() != 1650 || got.h() != 1140 {
		t.Fatalf("default size at 150%%: %+v (%dx%d), want 1650x1140", got, got.w(), got.h())
	}
	if r := fixWindowRectScaled(winRect{}, []winRect{work}, work, 0); r.w() != winDefW { // nonsense scale = 1
		t.Fatalf("scale 0 gave width %d", r.w())
	}
}
