package export

import (
	"context"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kaitoz11/confluence-exporter/internal/confluence"
	"github.com/kaitoz11/confluence-exporter/internal/fakeconfluence"
)

func TestGateLimitChangesLive(t *testing.T) {
	g := newGate(1)
	var active, peak atomic.Int32
	release := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Go(func() {
			if err := g.acquire(context.Background()); err != nil {
				t.Error(err)
				return
			}
			n := active.Add(1)
			for {
				p := peak.Load()
				if n <= p || peak.CompareAndSwap(p, n) {
					break
				}
			}
			<-release
			active.Add(-1)
			g.release()
		})
	}
	time.Sleep(50 * time.Millisecond)
	if n := active.Load(); n != 1 {
		t.Fatalf("limit 1 but %d running", n)
	}
	g.setLimit(3) // speeding up lets waiting work start at once
	time.Sleep(50 * time.Millisecond)
	if n := active.Load(); n != 3 {
		t.Fatalf("limit 3 but %d running", n)
	}
	close(release)
	wg.Wait()
	if peak.Load() > 3 {
		t.Fatalf("peak %d exceeded the limit", peak.Load())
	}
}

func TestGateCancelWakesWaiters(t *testing.T) {
	g := newGate(1)
	if err := g.acquire(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error)
	go func() { done <- g.acquire(ctx) }()
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("acquire succeeded after cancel")
		}
	case <-time.After(time.Second):
		t.Fatal("waiter was not woken by cancel")
	}
}

// rateRecorder records the rate limits the throttle sets on the client.
type rateRecorder struct {
	confluence.Client
	mu    sync.Mutex
	rates []float64
}

func (c *rateRecorder) SetRateLimit(r float64) {
	c.mu.Lock()
	c.rates = append(c.rates, r)
	c.mu.Unlock()
	c.Client.SetRateLimit(r)
}

func TestChangeSpeedDuringExport(t *testing.T) {
	srv := httptest.NewServer(fakeconfluence.New(20).Cloud())
	defer srv.Close()
	client := &rateRecorder{Client: confluence.New(confluence.Site{Base: srv.URL, Cloud: true},
		confluence.Credentials{Email: fakeconfluence.Email, Token: fakeconfluence.Token})}
	ctx := context.Background()
	spaces, err := client.ListSpaces(ctx)
	if err != nil {
		t.Fatal(err)
	}
	pages, err := client.ListPages(ctx, spaces[0], nil)
	if err != nil {
		t.Fatal(err)
	}
	tree := BuildTree(spaces[0], pages)

	speed := NewThrottle(SpeedGentle)
	var once sync.Once
	start := time.Now()
	res := Run(ctx, client, tree, Options{OutputDir: t.TempDir(), Attachments: true, Throttle: speed}, func(e Event) {
		// After the first page, the user switches to Fast.
		if e.Phase == PhasePages && e.Done == 1 {
			once.Do(func() { speed.SetSpeed(SpeedFast) })
		}
	})
	if len(res.Warnings) > 0 || res.Pages != 25 {
		t.Fatalf("result = %+v", res)
	}
	client.mu.Lock()
	rates := client.rates
	client.mu.Unlock()
	if len(rates) < 2 || rates[0] != 4 || rates[len(rates)-1] != 0 {
		t.Fatalf("rate limits set = %v, want Gentle (4/s) then Fast (unlimited)", rates)
	}
	// At a steady 4 requests per second the 50+ requests would take over 12 s.
	if d := time.Since(start); d > 8*time.Second {
		t.Errorf("speeding up had no effect: took %s", d)
	}
	if speed.Speed() != SpeedFast {
		t.Errorf("speed = %v", speed.Speed())
	}
}
