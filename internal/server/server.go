package server

import (
	"context"

	healthv1 "github.com/swayrider/protos/health/v1"
	searchv1 "github.com/swayrider/protos/search/v1"
	log "github.com/swayrider/swlib/logger"
	"github.com/swayrider/swlib/security"
)

func init() {
	security.PublicEndpoint("/health.v1.HealthService/Ping")
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
