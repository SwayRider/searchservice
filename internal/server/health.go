// health.go implements the health check endpoint.
//
// The health service provides a simple UP/DOWN status for load balancers and
// orchestration systems. For "search" (and "health"/""), status reflects real
// dependency reachability — the regionservice and every configured Pelias
// instance — rather than unconditionally reporting UP.

package server

import (
	"context"
	"strings"
	"time"

	healthv1 "github.com/swayrider/protos/health/v1"
)

// probeTimeout bounds a single dependency-probe round (region service plus all
// Pelias instances).
const probeTimeout = 3 * time.Second

// Check returns the health status of the specified component.
// Returns UP/DOWN based on dependency reachability for "search", "health", or
// empty component name; UNKNOWN otherwise.
func (h *HealthServer) Check(
	ctx context.Context,
	req *healthv1.HealthRequest,
) (*healthv1.HealthResponse, error) {
	switch strings.ToLower(req.Component) {
	case "search", "health", "":
		status := healthv1.HealthResponse_DOWN
		if h.probeDependencies(ctx) {
			status = healthv1.HealthResponse_UP
		}
		return &healthv1.HealthResponse{Status: status}, nil
	default:
		return &healthv1.HealthResponse{
			Status: healthv1.HealthResponse_UNKNOWN,
		}, nil
	}
}

// probeDependencies reports whether the regionservice and all configured Pelias
// instances are reachable, reusing the last probe result while it is younger
// than h.probeTTL to avoid hammering dependencies on every health check.
func (h *HealthServer) probeDependencies(ctx context.Context) bool {
	h.mu.Lock()
	if time.Since(h.lastCheck) < h.probeTTL {
		up := h.lastUp
		h.mu.Unlock()
		return up
	}
	h.mu.Unlock()

	probeCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	up := false
	switch {
	case h.regionProber == nil:
		h.l.Warnln("regionservice prober not configured")
	case len(h.peliasProbers) == 0:
		h.l.Warnln("no pelias instances configured")
	case h.regionProber.CheckConnection() != nil:
		h.l.Warnln("regionservice health probe failed")
	default:
		up = true
		for _, p := range h.peliasProbers {
			if err := p.Ping(probeCtx); err != nil {
				h.l.Warnf("pelias health probe failed: %v", err)
				up = false
				break
			}
		}
	}

	h.mu.Lock()
	h.lastCheck = time.Now()
	h.lastUp = up
	h.mu.Unlock()

	return up
}
