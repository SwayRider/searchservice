package server

import (
	"context"

	searchv1 "github.com/swayrider/protos/search/v1"
	log "github.com/swayrider/swlib/logger"
)

// ReverseGeocode handles the ReverseGeocode RPC — reverse geocoding with JWT auth.
func (s *SearchServer) ReverseGeocode(
	ctx context.Context,
	req *searchv1.ReverseGeocodeRequest,
) (*searchv1.ReverseGeocodeResponse, error) {
	lg := s.Logger().Derive(log.WithFunction("ReverseGeocode"))
	lg.Debugf("reverse geocode request: point=(%f, %f)", req.Point.Lat, req.Point.Lon)

	results, err := s.flow.ReverseGeocode(ctx, req)
	if err != nil {
		return nil, err
	}

	return &searchv1.ReverseGeocodeResponse{Results: results}, nil
}
