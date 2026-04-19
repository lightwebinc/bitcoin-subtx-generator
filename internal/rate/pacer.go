// Package rate implements a simple time-based pacer that emits ticks at
// approximately the requested rate.
//
// For pps <= 1000 we use a standard time.Ticker with a period of 1/pps.
// For higher rates we emit in bursts of burstSize = max(1, pps/1000) every
// millisecond, which keeps syscall overhead low without pinning a CPU.
package rate

import "time"

// Pacer yields at most one token per Wait() call. Stop frees underlying
// timer resources.
type Pacer interface {
	// Wait blocks until the next token is available. Returns false if the
	// pacer has been stopped.
	Wait() bool
	Stop()
}

// New returns a Pacer that targets the given packets-per-second rate.
// A pps of 0 returns a non-blocking pacer (emit as fast as possible).
func New(pps int) Pacer {
	if pps <= 0 {
		return &freePacer{}
	}
	if pps <= 1000 {
		return newSmoothPacer(pps)
	}
	return newBurstPacer(pps)
}

// freePacer never waits.
type freePacer struct{}

func (freePacer) Wait() bool { return true }
func (freePacer) Stop()      {}

// smoothPacer uses a time.Ticker.
type smoothPacer struct {
	t *time.Ticker
}

func newSmoothPacer(pps int) *smoothPacer {
	period := time.Second / time.Duration(pps)
	if period <= 0 {
		period = time.Microsecond
	}
	return &smoothPacer{t: time.NewTicker(period)}
}

func (p *smoothPacer) Wait() bool {
	_, ok := <-p.t.C
	return ok
}

func (p *smoothPacer) Stop() { p.t.Stop() }

// burstPacer emits N tokens per millisecond.
type burstPacer struct {
	t         *time.Ticker
	burst     int
	remaining int
}

func newBurstPacer(pps int) *burstPacer {
	burst := pps / 1000
	if burst < 1 {
		burst = 1
	}
	return &burstPacer{
		t:     time.NewTicker(time.Millisecond),
		burst: burst,
	}
}

func (p *burstPacer) Wait() bool {
	if p.remaining > 0 {
		p.remaining--
		return true
	}
	_, ok := <-p.t.C
	if !ok {
		return false
	}
	p.remaining = p.burst - 1
	return true
}

func (p *burstPacer) Stop() { p.t.Stop() }
