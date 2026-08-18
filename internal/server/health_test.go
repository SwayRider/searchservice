package server

import (
	"context"
	"errors"
	"testing"
	"time"

	healthv1 "github.com/swayrider/protos/health/v1"
	log "github.com/swayrider/swlib/logger"
)

// =============================================================================
// mockRegionProber — implements regionProber with a configurable function field
// =============================================================================

type mockRegionProber struct {
	checkConnectionFn func() error
}

func (m *mockRegionProber) CheckConnection() error {
	if m.checkConnectionFn != nil {
		return m.checkConnectionFn()
	}
	return nil
}

// =============================================================================
// mockPeliasProber — implements PeliasProber with a configurable function field
// =============================================================================

type mockPeliasProber struct {
	pingFn func(ctx context.Context) error
}

func (m *mockPeliasProber) Ping(ctx context.Context) error {
	if m.pingFn != nil {
		return m.pingFn(ctx)
	}
	return nil
}

func newTestHealthServer(region regionProber, probers []PeliasProber, ttl time.Duration) *HealthServer {
	return NewHealthServer(region, probers, ttl, log.New())
}

// =============================================================================
// Check Tests
// =============================================================================

func TestCheck_UnknownComponent(t *testing.T) {
	h := newTestHealthServer(&mockRegionProber{}, []PeliasProber{&mockPeliasProber{}}, time.Second)

	for _, component := range []string{"unknown", "pelias", "regionservice", "database"} {
		t.Run(component, func(t *testing.T) {
			resp, err := h.Check(context.Background(), &healthv1.HealthRequest{Component: component})
			if err != nil {
				t.Fatalf("Check(%q) returned error: %v", component, err)
			}
			if resp.Status != healthv1.HealthResponse_UNKNOWN {
				t.Errorf("Check(%q).Status = %v, want %v", component, resp.Status, healthv1.HealthResponse_UNKNOWN)
			}
		})
	}
}

func TestCheck_ReportsUpWhenAllDependenciesHealthy(t *testing.T) {
	h := newTestHealthServer(
		&mockRegionProber{},
		[]PeliasProber{&mockPeliasProber{}, &mockPeliasProber{}},
		time.Second,
	)

	for _, component := range []string{"search", "SEARCH", "health", "HEALTH", ""} {
		t.Run(component, func(t *testing.T) {
			resp, err := h.Check(context.Background(), &healthv1.HealthRequest{Component: component})
			if err != nil {
				t.Fatalf("Check(%q) returned error: %v", component, err)
			}
			if resp.Status != healthv1.HealthResponse_UP {
				t.Errorf("Check(%q).Status = %v, want %v", component, resp.Status, healthv1.HealthResponse_UP)
			}
		})
	}
}

func TestCheck_ReportsDownWhenRegionUnreachable(t *testing.T) {
	region := &mockRegionProber{checkConnectionFn: func() error {
		return errors.New("connection refused")
	}}
	h := newTestHealthServer(region, []PeliasProber{&mockPeliasProber{}}, time.Second)

	resp, err := h.Check(context.Background(), &healthv1.HealthRequest{Component: "search"})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if resp.Status != healthv1.HealthResponse_DOWN {
		t.Errorf("Check.Status = %v, want %v", resp.Status, healthv1.HealthResponse_DOWN)
	}
}

func TestCheck_ReportsDownWhenAnyPeliasUnreachable(t *testing.T) {
	unhealthy := &mockPeliasProber{pingFn: func(ctx context.Context) error {
		return errors.New("connection refused")
	}}
	h := newTestHealthServer(
		&mockRegionProber{},
		[]PeliasProber{&mockPeliasProber{}, unhealthy},
		time.Second,
	)

	resp, err := h.Check(context.Background(), &healthv1.HealthRequest{Component: "search"})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if resp.Status != healthv1.HealthResponse_DOWN {
		t.Errorf("Check.Status = %v, want %v", resp.Status, healthv1.HealthResponse_DOWN)
	}
}

func TestCheck_ReportsDownWhenNoPeliasConfigured(t *testing.T) {
	h := newTestHealthServer(&mockRegionProber{}, nil, time.Second)

	resp, err := h.Check(context.Background(), &healthv1.HealthRequest{Component: "search"})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if resp.Status != healthv1.HealthResponse_DOWN {
		t.Errorf("Check.Status = %v, want %v", resp.Status, healthv1.HealthResponse_DOWN)
	}
}

func TestCheck_CachesProbeResultWithinTTL(t *testing.T) {
	var calls int
	p := &mockPeliasProber{pingFn: func(ctx context.Context) error {
		calls++
		return nil
	}}
	// Long TTL: the second Check below must reuse the first probe result
	// rather than pinging again.
	h := newTestHealthServer(&mockRegionProber{}, []PeliasProber{p}, time.Hour)

	if _, err := h.Check(context.Background(), &healthv1.HealthRequest{Component: "search"}); err != nil {
		t.Fatalf("first Check returned error: %v", err)
	}
	if _, err := h.Check(context.Background(), &healthv1.HealthRequest{Component: "search"}); err != nil {
		t.Fatalf("second Check returned error: %v", err)
	}

	if calls != 1 {
		t.Errorf("Ping called %d times, want 1 (second Check should have used the cache)", calls)
	}
}

func TestCheck_ReprobesAfterTTLExpires(t *testing.T) {
	var calls int
	p := &mockPeliasProber{pingFn: func(ctx context.Context) error {
		calls++
		return nil
	}}
	h := newTestHealthServer(&mockRegionProber{}, []PeliasProber{p}, time.Millisecond)

	if _, err := h.Check(context.Background(), &healthv1.HealthRequest{Component: "search"}); err != nil {
		t.Fatalf("first Check returned error: %v", err)
	}

	time.Sleep(5 * time.Millisecond)

	if _, err := h.Check(context.Background(), &healthv1.HealthRequest{Component: "search"}); err != nil {
		t.Fatalf("second Check returned error: %v", err)
	}

	if calls != 2 {
		t.Errorf("Ping called %d times, want 2 (TTL should have expired before the second Check)", calls)
	}
}
