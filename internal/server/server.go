package server

import (
	"context"
	"sync"
	"time"

	healthv1 "github.com/swayrider/protos/health/v1"
	searchv1 "github.com/swayrider/protos/search/v1"
	log "github.com/swayrider/swlib/logger"
	"github.com/swayrider/swlib/security"
)

func init() {
	security.PublicEndpoint("/health.v1.HealthService/Ping")
	security.SkipRateLimitEndpoint("/health.v1.HealthService/Ping")

	security.PublicEndpoint("/health.v1.HealthService/Check")
	security.SkipRateLimitEndpoint("/health.v1.HealthService/Check")

	security.UserOrServiceEndpoint("/search.v1.SearchService/Search", []string{"search:execute"})
	security.UserOrServiceEndpoint("/search.v1.SearchService/ReverseGeocode", []string{"search:execute"})
	security.UserOrServiceEndpoint("/search.v1.SearchService/Autocomplete", []string{"search:execute"})
}

type searchFlow interface {
	Search(ctx context.Context, req *searchv1.SearchRequest) ([]*searchv1.Result, error)
	ReverseGeocode(ctx context.Context, req *searchv1.ReverseGeocodeRequest) ([]*searchv1.Result, error)
	Autocomplete(ctx context.Context, req *searchv1.AutocompleteRequest) ([]*searchv1.Result, error)
}

// SearchServer implements the SearchService gRPC interface.
type SearchServer struct {
	searchv1.UnimplementedSearchServiceServer
	flow searchFlow
	l    *log.Logger
}

// NewSearchServer creates a new SearchServer.
func NewSearchServer(flow searchFlow, l *log.Logger) *SearchServer {
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

// regionProber reports whether the regionservice dependency is reachable.
// *regionclient.Client satisfies this interface.
type regionProber interface {
	CheckConnection() error
}

// PeliasProber reports whether a single Pelias instance is reachable.
// It is exported so that main.go can build the probe list from the configured
// Pelias clients. *pelias.Client satisfies this interface.
type PeliasProber interface {
	Ping(ctx context.Context) error
}

// HealthServer implements the HealthService gRPC interface.
//
// Check() probes dependency reachability (regionservice plus every configured
// Pelias instance) rather than reporting UP unconditionally, caching the result
// for probeTTL to avoid hammering dependencies on every orchestrator health
// check.
type HealthServer struct {
	healthv1.UnimplementedHealthServiceServer
	regionProber  regionProber   // regionservice gRPC client
	peliasProbers []PeliasProber // Pelias HTTP clients, one per region
	probeTTL      time.Duration  // How long a probe result is reused before re-probing
	l             *log.Logger    // Logger instance

	mu        sync.Mutex
	lastCheck time.Time
	lastUp    bool
}

// NewHealthServer creates a new HealthServer that probes the regionservice and
// the given Pelias instances, caching the aggregate result for probeTTL.
func NewHealthServer(
	region regionProber,
	pelias []PeliasProber,
	probeTTL time.Duration,
	l *log.Logger,
) *HealthServer {
	return &HealthServer{
		regionProber:  region,
		peliasProbers: pelias,
		probeTTL:      probeTTL,
		l: l.Derive(
			log.WithComponent("HealthServer"),
			log.WithFunction("NewHealthServer"),
		),
	}
}

// Logger returns the server's logger instance.
func (s *HealthServer) Logger() *log.Logger {
	return s.l
}
