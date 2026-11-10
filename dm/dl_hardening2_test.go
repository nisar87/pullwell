package dm

import (
	"testing"
	"time"
)

// A server that throttles some connections hard: the download must not wait for them.
func TestSlowConnectionsAreReplaced(t *testing.T) {
	size := int64(96 << 20)
	s := newSim(size)
	defer s.Close()
	s.set(func() { s.slowEvery, s.slowRate = 8, 100<<10 }) // every 8th request crawls at 100 KB/s
	env := newSimEnv(newSimLine("A", "127.0.0.1", 60<<20))
	tk, d, err := runDl(t, env, s.URL+"/file.bin", dlSpec{})
	mustOK(t, tk, err, size)
	t.Logf("96 MB with every 8th connection throttled: %.1fs", d.Seconds())
	if d > 14*time.Second {
		t.Fatalf("throttled connections held the download back: %v", d)
	}
}
