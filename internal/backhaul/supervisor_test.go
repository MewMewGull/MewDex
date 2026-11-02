package backhaul

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"mewdex/internal/startup"
)

type fakeCarrier struct {
	done  chan struct{}
	once  sync.Once
	kills atomic.Int32
}

func newFakeCarrier() *fakeCarrier           { return &fakeCarrier{done: make(chan struct{})} }
func (p *fakeCarrier) Done() <-chan struct{} { return p.done }
func (p *fakeCarrier) exit()                 { p.once.Do(func() { close(p.done) }) }
func (p *fakeCarrier) Wait() error           { <-p.done; return nil }
func (p *fakeCarrier) Kill() error           { p.kills.Add(1); p.exit(); return nil }

func awaitState(t *testing.T, states <-chan State, want string) State {
	t.Helper()
	timeout := time.NewTimer(2 * time.Second)
	defer timeout.Stop()
	for {
		select {
		case state := <-states:
			if state.State == want {
				return state
			}
		case <-timeout.C:
			t.Fatal("backhaul state did not arrive", want)
		}
	}
}

func testSupervisor(t *testing.T) (*supervisor, chan State) {
	t.Helper()
	states := make(chan State, 100)
	r := newSupervisor(t.TempDir(), Config{ID: "fixture", LocalPort: 47830, PublicURL: "https://fixture.invalid"}, startup.Binding{}, func(state State) error { states <- state; return nil })
	r.checkCurrent = func() error { return nil }
	r.probe = func(context.Context, string) error { return nil }
	r.interval, r.minimumBackoff, r.maximumBackoff = 5*time.Millisecond, time.Millisecond, 8*time.Millisecond
	return r, states
}

func TestReconnectWaitsForThePreviousOwnedCarrierAndCancellationStopsTheNewOne(t *testing.T) {
	r, states := testSupervisor(t)
	first, second := newFakeCarrier(), newFakeCarrier()
	var launches atomic.Int32
	r.launch = func(context.Context) (carrier, error) {
		n := launches.Add(1)
		if n == 1 {
			return first, nil
		}
		if n == 2 && first.kills.Load() > 0 {
			return second, nil
		}
		return nil, errors.New("retry ran before closing the old owned carrier")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- r.run(ctx) }()
	awaitState(t, states, "connected")
	first.exit()
	if state := awaitState(t, states, "retrying"); state.Attempt != 1 || state.RetryAt == "" {
		t.Fatal(state)
	}
	if state := awaitState(t, states, "connected"); state.Attempt != 2 {
		t.Fatal(state)
	}
	cancel()
	if err := <-done; err != nil || launches.Load() != 2 || first.kills.Load() == 0 || second.kills.Load() == 0 {
		t.Fatal("owned connection was replayed or left running", err, launches.Load())
	}
}

func TestHealthFailureKeepsOneConnectionAndProfileChangeSuspendsIt(t *testing.T) {
	r, states := testSupervisor(t)
	p := newFakeCarrier()
	var launches atomic.Int32
	var healthy, changed atomic.Bool
	r.launch = func(context.Context) (carrier, error) { launches.Add(1); return p, nil }
	r.probe = func(_ context.Context, target string) error {
		if target == r.config.PublicURL && !healthy.Load() {
			return errors.New("public ingress unavailable")
		}
		return nil
	}
	r.checkCurrent = func() error {
		if changed.Load() {
			return ErrProfileChanged
		}
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- r.run(ctx) }()
	awaitState(t, states, "verifying")
	healthy.Store(true)
	awaitState(t, states, "connected")
	changed.Store(true)
	awaitState(t, states, "suspended")
	if err := <-done; !errors.Is(err, ErrProfileChanged) || launches.Load() != 1 || p.kills.Load() != 1 {
		t.Fatal("configuration change reopened or leaked a connection", err, launches.Load(), p.kills.Load())
	}
}

func TestPublicHealthRejectsRedirectsAndOtherApplications(t *testing.T) {
	var mode atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch mode.Load() {
		case 0:
			_, _ = w.Write([]byte(`{"name":"MewDex","protocolVersion":1}`))
		case 1:
			http.Redirect(w, r, "/other", http.StatusFound)
		case 2:
			_, _ = w.Write([]byte(`{"name":"another application","protocolVersion":1}`))
		}
	}))
	defer server.Close()
	if err := probe(context.Background(), server.URL); err != nil {
		t.Fatal(err)
	}
	for _, value := range []int32{1, 2} {
		mode.Store(value)
		if err := probe(context.Background(), server.URL); err == nil {
			t.Fatal("public ingress was incorrectly marked healthy", value)
		}
	}
}
