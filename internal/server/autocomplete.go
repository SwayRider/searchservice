package server

import (
	"context"

	searchv1 "github.com/swayrider/protos/search/v1"
	log "github.com/swayrider/swlib/logger"
)

// Autocomplete handles the Autocomplete RPC — partial-text geocoding suggestions.
func (s *SearchServer) Autocomplete(
	ctx context.Context,
	req *searchv1.AutocompleteRequest,
) (*searchv1.AutocompleteResponse, error) {
	lg := s.Logger().Derive(log.WithFunction("Autocomplete"))
	lg.Debugf("autocomplete request: text=%q", req.Text)

	results, err := s.flow.Autocomplete(ctx, req)
	if err != nil {
		return nil, err
	}

	return &searchv1.AutocompleteResponse{Results: results}, nil
}
