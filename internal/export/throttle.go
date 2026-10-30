package export

import (
	"context"
	"sync"
	"time"
)

// Throttle holds the speed of a running export and lets the user change it
// at any time. A slower speed takes effect as running downloads finish; a
// faster one starts waiting work immediately.
type Throttle struct {
	mu        sync.Mutex
	speed     Speed
	downloads *gate
	pdfs      *gate
	setRate   func(perSecond float64) // the Confluence client's rate limit
}

// NewThrottle starts at the given speed.
func NewThrottle(s Speed) *Throttle {
	th := s.throttle()
	return &Throttle{speed: s, downloads: newGate(th.downloads), pdfs: newGate(th.pdfWorkers)}
}

// Speed returns the current speed.
func (t *Throttle) Speed() Speed {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.speed
}

// SetSpeed changes the speed of the running export.
func (t *Throttle) SetSpeed(s Speed) {
	t.mu.Lock()
	t.speed = s
	setRate := t.setRate
	t.mu.Unlock()
	th := s.throttle()
	t.downloads.setLimit(th.downloads)
	t.pdfs.setLimit(th.pdfWorkers)
	if setRate != nil {
		setRate(th.requestRate)
	}
}

// bindRate connects the request rate limit and applies the current speed.
func (t *Throttle) bindRate(setRate func(float64)) {
	t.mu.Lock()
	t.setRate = setRate
	s := t.speed
	t.mu.Unlock()
	setRate(s.throttle().requestRate)
}

// Maximum workers ever needed: the Fast speed's numbers.
func maxDownloads() int  { return SpeedFast.throttle().downloads }
func maxPDFWorkers() int { return SpeedFast.throttle().pdfWorkers }

// pdfThrottle adapts the Throttle to the pdf package.
type pdfThrottle struct{ t *Throttle }

func (p pdfThrottle) Acquire(ctx context.Context) error { return p.t.pdfs.acquire(ctx) }
func (p pdfThrottle) Release()                          { p.t.pdfs.release() }
func (p pdfThrottle) Pause() time.Duration              { return p.t.Speed().throttle().pdfPause }

// gate lets at most limit holders in at a time; the limit can change while
// holders wait.
type gate struct {
	mu     sync.Mutex
	cond   *sync.Cond
	limit  int
	active int
}

func newGate(limit int) *gate {
	g := &gate{limit: max(limit, 1)}
	g.cond = sync.NewCond(&g.mu)
	return g
}

func (g *gate) acquire(ctx context.Context) error {
	// Wake waiters when the export is canceled.
	stop := context.AfterFunc(ctx, func() {
		g.mu.Lock()
		g.cond.Broadcast()
		g.mu.Unlock()
	})
	defer stop()
	g.mu.Lock()
	defer g.mu.Unlock()
	for g.active >= g.limit {
		if err := ctx.Err(); err != nil {
			return err
		}
		g.cond.Wait()
	}
	g.active++
	return nil
}

func (g *gate) release() {
	g.mu.Lock()
	g.active--
	g.cond.Broadcast()
	g.mu.Unlock()
}

func (g *gate) setLimit(n int) {
	g.mu.Lock()
	g.limit = max(n, 1)
	g.cond.Broadcast()
	g.mu.Unlock()
}
