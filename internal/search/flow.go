package search

import (
	"context"
	"errors"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"github.com/swayrider/grpcclients/regionclient"
	searchv1 "github.com/swayrider/protos/search/v1"
	log "github.com/swayrider/swlib/logger"
)

// PeliasSearcher is the interface satisfied by *pelias.Client.
// It is exported so that main.go can build the map with the concrete client.
type PeliasSearcher interface {
	Search(
		ctx context.Context,
		text string,
		language string,
		focusLat, focusLon float64,
		hasFocus bool,
		minLat, minLon, maxLat, maxLon float64,
		hasBoundary bool,
	) ([]*searchv1.Result, error)
	Reverse(
		ctx context.Context,
		lat, lon float64,
		size int,
		language string,
	) ([]*searchv1.Result, error)
}

// regionSearcher is the interface satisfied by *regionclient.Client for testability.
type regionSearcher interface {
	SearchBox(boundingBox regionclient.BoundingBox, includeExtended bool) (regionclient.RegionList, error)
}

// SearchFlow orchestrates the 4-phase Pelias search strategy.
type SearchFlow struct {
	peliasClients map[string]PeliasSearcher
	regionClient  regionSearcher
	logger        *log.Logger
}

// NewSearchFlow creates a SearchFlow with the given pelias clients and region client.
func NewSearchFlow(
	peliasClients map[string]PeliasSearcher,
	regionClient regionSearcher,
	logger *log.Logger,
) *SearchFlow {
	return &SearchFlow{
		peliasClients: peliasClients,
		regionClient:  regionClient,
		logger:        logger.Derive(log.WithComponent("SearchFlow")),
	}
}

// Search executes the search and returns ranked results.
// Queries core regions first, then extended, then any remaining configured regions.
// The expanded viewport is passed to Pelias as boundary.rect to filter out distant results.
// Focus point biases Pelias scoring; distance decay penalizes far-away results in ranking.
func (f *SearchFlow) Search(ctx context.Context, req *searchv1.SearchRequest) ([]*searchv1.Result, error) {
	lg := f.logger.Derive(log.WithFunction("Search"))

	vp := req.Viewport
	if vp == nil {
		return nil, status.Error(codes.InvalidArgument, "viewport is required")
	}

	// Expand viewport by 1× width/height on each side for region service query
	width := vp.TopRight.Lon - vp.BottomLeft.Lon
	height := vp.TopRight.Lat - vp.BottomLeft.Lat
	extBox := regionclient.BoundingBox{
		BottomLeft: regionclient.Coordinate{
			Latitude:  clampLat(vp.BottomLeft.Lat - height),
			Longitude: vp.BottomLeft.Lon - width,
		},
		TopRight: regionclient.Coordinate{
			Latitude:  clampLat(vp.TopRight.Lat + height),
			Longitude: vp.TopRight.Lon + width,
		},
	}
	minLat := extBox.BottomLeft.Latitude
	minLon := extBox.BottomLeft.Longitude
	maxLat := extBox.TopRight.Latitude
	maxLon := extBox.TopRight.Longitude

	// Determine focus point
	hasFocus := req.FocusPoint != nil
	focusLat := (vp.BottomLeft.Lat + vp.TopRight.Lat) / 2
	focusLon := (vp.BottomLeft.Lon + vp.TopRight.Lon) / 2
	if hasFocus {
		focusLat = req.FocusPoint.Lat
		focusLon = req.FocusPoint.Lon
	}

	// Determine result size
	size := defaultSize
	if req.Size != nil {
		size = int(*req.Size)
	}

	language := ""
	if req.Language != nil {
		language = *req.Language
	}

	// Query region service to determine which Pelias instances to call
	regionList, err := f.regionClient.SearchBox(extBox, true)
	if err != nil {
		lg.Warnf("region service unavailable: %v", err)
		return nil, status.Error(codes.Unavailable, "region service unavailable")
	}

	queriedNames := make(map[string]bool)
	text := req.Text
	successCount := 0
	allResults := make([]*searchv1.Result, 0)

	// Phase 1: core regions
	for _, region := range regionList.CoreRegions {
		if queriedNames[region] {
			continue
		}
		queriedNames[region] = true
		clnt, ok := f.peliasClients[region]
		if !ok {
			continue
		}
		res, err := clnt.Search(ctx, text, language, focusLat, focusLon, hasFocus, minLat, minLon, maxLat, maxLon, true)
		if err != nil {
			lg.Warnf("pelias error for region %s (phase 1): %v", region, err)
			continue
		}
		successCount++
		allResults = append(allResults, res...)
	}

	// Bail out early if context is already cancelled/expired
	if err := ctx.Err(); err != nil {
		if successCount > 0 {
			return Rank(allResults, text, focusLat, focusLon, size), nil
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, status.Error(codes.DeadlineExceeded, "context deadline exceeded")
		}
		return nil, status.Error(codes.Canceled, "context canceled")
	}

	// Phase 2: extended regions not already queried
	for _, region := range regionList.ExtendedRegions {
		if queriedNames[region] {
			continue
		}
		queriedNames[region] = true
		clnt, ok := f.peliasClients[region]
		if !ok {
			continue
		}
		res, err := clnt.Search(ctx, text, language, focusLat, focusLon, hasFocus, minLat, minLon, maxLat, maxLon, true)
		if err != nil {
			lg.Warnf("pelias error for region %s (phase 2): %v", region, err)
			continue
		}
		successCount++
		allResults = append(allResults, res...)
	}

	// Bail out early if context is already cancelled/expired
	if err := ctx.Err(); err != nil {
		if successCount > 0 {
			return Rank(allResults, text, focusLat, focusLon, size), nil
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, status.Error(codes.DeadlineExceeded, "context deadline exceeded")
		}
		return nil, status.Error(codes.Canceled, "context canceled")
	}

	// Phase 3: any remaining configured regions not returned by region service
	for region, clnt := range f.peliasClients {
		if queriedNames[region] {
			continue
		}
		res, err := clnt.Search(ctx, text, language, focusLat, focusLon, hasFocus, minLat, minLon, maxLat, maxLon, true)
		if err != nil {
			lg.Warnf("pelias error for region %s (phase 3): %v", region, err)
			continue
		}
		successCount++
		allResults = append(allResults, res...)
	}

	if successCount == 0 {
		return nil, status.Error(codes.Unavailable, "all pelias servers unavailable")
	}

	// If no address results, try retrying with localadmin names from locality results.
	// Skip retry if context is already expired — it would just waste time on doomed calls.
	if !hasLayerResult(allResults, "address") && ctx.Err() == nil {
		if retryResults := f.retryWithLocaladmin(ctx, allResults, text, language, focusLat, focusLon, hasFocus, minLat, minLon, maxLat, maxLon, lg); len(retryResults) > 0 {
			allResults = append(allResults, retryResults...)
		}
	}

	return Rank(allResults, text, focusLat, focusLon, size), nil
}

func toSet(s []string) map[string]bool {
	m := make(map[string]bool, len(s))
	for _, v := range s {
		m[v] = true
	}
	return m
}

func clampLat(lat float64) float64 {
	if lat < -90 {
		return -90
	}
	if lat > 90 {
		return 90
	}
	return lat
}

// hasLayerResult reports whether any result has the given layer.
func hasLayerResult(results []*searchv1.Result, layer string) bool {
	for _, r := range results {
		if r.Layer == layer {
			return true
		}
	}
	return false
}

// retryWithLocaladmin builds alternative queries using localadmin names
// extracted from locality results and re-runs the 3-phase Pelias search.
// For example, "oosthamsesteenweg 8, olmen" → localadmin "Balen" → retry with
// "oosthamsesteenweg 8, balen".
func (f *SearchFlow) retryWithLocaladmin(
	ctx context.Context,
	originalResults []*searchv1.Result,
	originalText string,
	language string,
	focusLat, focusLon float64,
	hasFocus bool,
	minLat, minLon, maxLat, maxLon float64,
	lg *log.Logger,
) []*searchv1.Result {
	// Extract unique localadmin names from locality results.
	admins := make(map[string]bool)
	for _, r := range originalResults {
		if r.Layer == "locality" && r.Localadmin != "" {
			admins[r.Localadmin] = true
		}
	}
	if len(admins) == 0 {
		return nil
	}

	// Build alternative query: replace the city part with localadmin name.
	// Heuristic: split on last comma, replace the city part.
	altTexts := make([]string, 0, len(admins))
	trimmed := strings.TrimSpace(originalText)
	if idx := strings.LastIndex(trimmed, ","); idx >= 0 {
		prefix := strings.TrimSpace(trimmed[:idx])
		for admin := range admins {
			altTexts = append(altTexts, prefix+", "+admin)
		}
	} else {
		for admin := range admins {
			altTexts = append(altTexts, trimmed+", "+admin)
		}
	}

	// Query all configured Pelias instances for each alternative text.
	var results []*searchv1.Result
	for _, altText := range altTexts {
		lg.Debugf("localadmin retry: %q", altText)
		for region, clnt := range f.peliasClients {
			res, err := clnt.Search(ctx, altText, language, focusLat, focusLon, hasFocus, minLat, minLon, maxLat, maxLon, true)
			if err != nil {
				lg.Warnf("pelias error for region %s (localadmin retry): %v", region, err)
				continue
			}
			results = append(results, res...)
		}
	}
	return results
}

// ReverseGeocode executes reverse geocoding for the given coordinate.
// Returns results from the Pelias instance of the region containing the coordinate.
func (f *SearchFlow) ReverseGeocode(ctx context.Context, req *searchv1.ReverseGeocodeRequest) ([]*searchv1.Result, error) {
	lg := f.logger.Derive(log.WithFunction("ReverseGeocode"))

	if req.Point == nil {
		return nil, status.Error(codes.InvalidArgument, "point is required")
	}

	lat := req.Point.Lat
	lon := req.Point.Lon

	bb := regionclient.BoundingBox{
		BottomLeft: regionclient.Coordinate{
			Latitude:  lat - 0.001,
			Longitude: lon - 0.001,
		},
		TopRight: regionclient.Coordinate{
			Latitude:  lat + 0.001,
			Longitude: lon + 0.001,
		},
	}

	regionList, err := f.regionClient.SearchBox(bb, false)
	if err != nil {
		lg.Warnf("region service unavailable: %v", err)
		return nil, status.Error(codes.Unavailable, "region service unavailable")
	}

	if len(regionList.CoreRegions) == 0 && len(regionList.ExtendedRegions) == 0 {
		return nil, status.Error(codes.NotFound, "no region found for coordinate")
	}

	var region string
	if len(regionList.CoreRegions) > 0 {
		region = regionList.CoreRegions[0]
	} else {
		region = regionList.ExtendedRegions[0]
	}

	clnt, ok := f.peliasClients[region]
	if !ok {
		lg.Warnf("no pelias client for region %s", region)
		return nil, status.Error(codes.NotFound, "no pelias server configured for region")
	}

	size := 10
	if req.Size != nil {
		size = int(*req.Size)
	}

	language := ""
	if req.Language != nil {
		language = *req.Language
	}

	results, err := clnt.Reverse(ctx, lat, lon, size, language)
	if err != nil {
		lg.Warnf("pelias reverse error for region %s: %v", region, err)
		return nil, status.Error(codes.Internal, "pelias request failed")
	}

	return results, nil
}
