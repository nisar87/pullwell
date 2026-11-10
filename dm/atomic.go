package dm

import "sync/atomic"

type atomicBool struct{ v atomic.Bool }

func (a *atomicBool) get() bool          { return a.v.Load() }
func (a *atomicBool) cas(o, n bool) bool { return a.v.CompareAndSwap(o, n) }
func (a *atomicBool) set(b bool)         { a.v.Store(b) }
