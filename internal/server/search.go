package server

import (
	"context"

	searchv1 "github.com/swayrider/protos/search/v1"
	log "github.com/swayrider/swlib/logger"
)

// Search handles the Search RPC — geocoding search with JWT auth.
func (s *SearchServer) Search(
	ctx context.Context,
	req *searchv1.SearchRequest,
) (*searchv1.SearchResponse, error) {
	lg := s.Logger().Derive(log.WithFunction("Search"))
	lg.Debugf("search request: text=%q", req.Text)

	results, err := s.flow.Search(ctx, req)
	if err != nil {
		return nil, err
	}

	return &searchv1.SearchResponse{Results: results}, nil
}
