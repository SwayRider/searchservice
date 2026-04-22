// Package server implements the gRPC server for the search service.
//
// # Endpoints
//
// The search service provides:
//   - Search: Geocoding search with JWT auth required
//   - Ping: Connectivity check (public)
//
// The Search endpoint uses the zero-value EndpointProfile (the secure default),
// which requires a valid JWT token with email verified.
// Ping and health endpoints are registered as public.
package server

import (
	healthv1 "github.com/swayrider/protos/health/v1"
	searchv1 "github.com/swayrider/protos/search/v1"
	"github.com/swayrider/searchservice/internal/search"
	log "github.com/swayrider/swlib/logger"
	"github.com/swayrider/swlib/security"
)

// init registers security profiles for all endpoints.
// Search is NOT registered here — it uses the zero-value EndpointProfile (secure default),
// which requires a valid JWT token with email verified.
func init() {
	security.PublicEndpoint("/health.v1.HealthService/Ping")
}

// SearchServer implements the SearchService gRPC interface.
type SearchServer struct {
	searchv1.UnimplementedSearchServiceServer
	flow *search.SearchFlow
	l    *log.Logger
}

// NewSearchServer creates a new SearchServer.
func NewSearchServer(flow *search.SearchFlow, l *log.Logger) *SearchServer {
	return &SearchServer{
		flow: flow,
		l: l.Derive(
			log.WithComponent("SearchServer"),
			log.WithFunction("NewSearchServer"),
		),
	}
}

// Logger returns the server's logger instance.
func (s SearchServer) Logger() *log.Logger {
	return s.l
}

// HealthServer implements the HealthService gRPC interface.
type HealthServer struct {
	healthv1.UnimplementedHealthServiceServer
	l *log.Logger
}

// NewHealthServer creates a new HealthServer.
func NewHealthServer(l *log.Logger) *HealthServer {
	return &HealthServer{
		l: l.Derive(
			log.WithComponent("HealthServer"),
			log.WithFunction("NewHealthServer"),
		),
	}
}

// Logger returns the server's logger instance.
func (s HealthServer) Logger() *log.Logger {
	return s.l
}
