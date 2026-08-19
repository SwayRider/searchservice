package search

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/swayrider/grpcclients/regionclient"
	pbgeo "github.com/swayrider/protos/common_types/geo"
	searchv1 "github.com/swayrider/protos/search/v1"
	log "github.com/swayrider/swlib/logger"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// fakePeliasSearcher records calls and returns configured responses.
type fakePeliasSearcher struct {
	results      []*searchv1.Result
	err          error
	callCount    int
	lastSize     int
	lastFocusLat float64
	lastFocusLon float64
}

func (f *fakePeliasSearcher) Search(
	ctx context.Context,
	text, language string,
	focusLat, focusLon float64,
	hasFocus bool,
	minLat, minLon, maxLat, maxLon float64,
	hasBoundary bool,
) ([]*searchv1.Result, error) {
	f.callCount++
	f.lastFocusLat = focusLat
	f.lastFocusLon = focusLon
	return f.results, f.err
}

func (f *fakePeliasSearcher) Autocomplete(
	ctx context.Context,
	text, language string,
	focusLat, focusLon float64,
) ([]*searchv1.Result, error) {
	f.callCount++
	f.lastFocusLat = focusLat
	f.lastFocusLon = focusLon
	return f.results, f.err
}

func (f *fakePeliasSearcher) Reverse(
	ctx context.Context,
	lat, lon float64,
	size int,
	language string,
) ([]*searchv1.Result, error) {
	f.callCount++
	f.lastSize = size
	return f.results, f.err
}

// fakeRegionSearcher returns configured region lists.
type fakeRegionSearcher struct {
	list    regionclient.RegionList
	err     error
	lastBox regionclient.BoundingBox
}

func (f *fakeRegionSearcher) SearchBox(_ context.Context, _ string, bb regionclient.BoundingBox, includeExtended bool) (regionclient.RegionList, error) {
	f.lastBox = bb
	return f.list, f.err
}

func testLogger() *log.Logger {
	return log.New()
}

func makeSearchReq(text string) *searchv1.SearchRequest {
	return &searchv1.SearchRequest{
		Text: text,
		Viewport: &pbgeo.BoundingBox{
			BottomLeft: &pbgeo.Coordinate{Lat: 37.8, Lon: -1.2},
			TopRight:   &pbgeo.Coordinate{Lat: 38.2, Lon: -0.8},
		},
	}
}

func TestFlow_coreAndExtendedRegionsQueried(t *testing.T) {
	iber := &fakePeliasSearcher{results: []*searchv1.Result{
		{Label: "Result", Layer: "venue", Confidence: 1.0, Lat: 38.0, Lon: -1.0},
	}}
	we := &fakePeliasSearcher{results: []*searchv1.Result{}}

	flow := NewSearchFlow(
		map[string]PeliasSearcher{
			"iberian-peninsula": iber,
			"west-europe":       we,
		},
		&fakeRegionSearcher{list: regionclient.RegionList{
			CoreRegions:     []string{"iberian-peninsula"},
			ExtendedRegions: []string{"west-europe"},
		}},
		testLogger(),
	)

	results, err := flow.Search(context.Background(), makeSearchReq("test"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected results")
	}
	// Both core and extended regions are queried exactly once each
	if iber.callCount != 1 {
		t.Errorf("iber call count: expected 1, got %d", iber.callCount)
	}
	if we.callCount != 1 {
		t.Errorf("we call count: expected 1, got %d", we.callCount)
	}
}

func TestFlow_regionNotInServiceQueried(t *testing.T) {
	// Region not returned by region service but present in configured clients is still queried
	iber := &fakePeliasSearcher{results: []*searchv1.Result{}}
	extra := &fakePeliasSearcher{results: []*searchv1.Result{
		{Label: "Extra", Layer: "venue", Confidence: 1.0, Lat: 38.0, Lon: -1.0},
	}}

	flow := NewSearchFlow(
		map[string]PeliasSearcher{
			"iberian-peninsula": iber,
			"extra-region":      extra,
		},
		&fakeRegionSearcher{list: regionclient.RegionList{
			CoreRegions: []string{"iberian-peninsula"},
		}},
		testLogger(),
	)

	results, err := flow.Search(context.Background(), makeSearchReq("test"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected results from extra region")
	}
	if extra.callCount != 1 {
		t.Errorf("extra call count: expected 1, got %d", extra.callCount)
	}
}

func TestFlow_allPhasesEmpty_returnsEmpty(t *testing.T) {
	iber := &fakePeliasSearcher{results: []*searchv1.Result{}}

	flow := NewSearchFlow(
		map[string]PeliasSearcher{"iberian-peninsula": iber},
		&fakeRegionSearcher{list: regionclient.RegionList{
			CoreRegions: []string{"iberian-peninsula"},
		}},
		testLogger(),
	)

	results, err := flow.Search(context.Background(), makeSearchReq("nothing"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected empty, got %d results", len(results))
	}
}

func TestFlow_unknownRegionsSilentlySkipped(t *testing.T) {
	iber := &fakePeliasSearcher{results: []*searchv1.Result{
		{Label: "Result", Layer: "venue", Confidence: 1.0, Lat: 38.0, Lon: -1.0},
	}}

	flow := NewSearchFlow(
		map[string]PeliasSearcher{"iberian-peninsula": iber},
		&fakeRegionSearcher{list: regionclient.RegionList{
			CoreRegions: []string{"unknown-region", "iberian-peninsula"},
		}},
		testLogger(),
	)

	results, err := flow.Search(context.Background(), makeSearchReq("test"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected results from known region")
	}
}

func TestFlow_regionServiceUnavailable_returnsUnavailable(t *testing.T) {
	flow := NewSearchFlow(
		map[string]PeliasSearcher{},
		&fakeRegionSearcher{err: errors.New("connection refused")},
		testLogger(),
	)

	_, err := flow.Search(context.Background(), makeSearchReq("test"))
	if err == nil {
		t.Fatal("expected error")
	}
	st, ok := status.FromError(err)
	if !ok || st.Code() != codes.Unavailable {
		t.Errorf("expected Unavailable, got %v", err)
	}
}

func TestFlow_allPeliasError_returnsUnavailable(t *testing.T) {
	errSearcher := &fakePeliasSearcher{err: errors.New("network timeout")}

	flow := NewSearchFlow(
		map[string]PeliasSearcher{"iberian-peninsula": errSearcher},
		&fakeRegionSearcher{list: regionclient.RegionList{
			CoreRegions: []string{"iberian-peninsula"},
		}},
		testLogger(),
	)

	_, err := flow.Search(context.Background(), makeSearchReq("test"))
	if err == nil {
		t.Fatal("expected error when all pelias error")
	}
	st, ok := status.FromError(err)
	if !ok || st.Code() != codes.Unavailable {
		t.Errorf("expected Unavailable, got %v", err)
	}
}

func TestFlow_onePeliasErrors_othersUsed(t *testing.T) {
	errSearcher := &fakePeliasSearcher{err: errors.New("network timeout")}
	okSearcher := &fakePeliasSearcher{results: []*searchv1.Result{
		{Label: "Result", Layer: "venue", Confidence: 1.0, Lat: 38.0, Lon: -1.0},
	}}

	flow := NewSearchFlow(
		map[string]PeliasSearcher{
			"region-a": errSearcher,
			"region-b": okSearcher,
		},
		&fakeRegionSearcher{list: regionclient.RegionList{
			CoreRegions: []string{"region-a", "region-b"},
		}},
		testLogger(),
	)

	results, err := flow.Search(context.Background(), makeSearchReq("test"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected results from working server")
	}
}

// queryBasedSearcher returns different results based on query text.
type queryBasedSearcher struct {
	responses map[string][]*searchv1.Result
	callCount int
	lastQuery string
}

func (f *queryBasedSearcher) Search(
	ctx context.Context,
	text, language string,
	focusLat, focusLon float64,
	hasFocus bool,
	minLat, minLon, maxLat, maxLon float64,
	hasBoundary bool,
) ([]*searchv1.Result, error) {
	f.callCount++
	f.lastQuery = text
	if res, ok := f.responses[text]; ok {
		return res, nil
	}
	return nil, nil
}

func (f *queryBasedSearcher) Autocomplete(
	ctx context.Context,
	text, language string,
	focusLat, focusLon float64,
) ([]*searchv1.Result, error) {
	return nil, nil
}

func (f *queryBasedSearcher) Reverse(
	ctx context.Context,
	lat, lon float64,
	size int,
	language string,
) ([]*searchv1.Result, error) {
	return nil, nil
}

func TestFlow_localadminRetry(t *testing.T) {
	// First query "street 8, olmen" returns only locality result.
	// Retry with "street 8, balen" returns address result.
	searcher := &queryBasedSearcher{
		responses: map[string][]*searchv1.Result{
			"oosthamsesteenweg 8, olmen": {
				{Label: "Olmen, AN, België", Layer: "locality", Localadmin: "Balen", Confidence: 0.6},
			},
			"oosthamsesteenweg 8, Balen": {
				{Label: "Oosthamsesteenweg 8, Balen, België", Layer: "address", Street: "Oosthamsesteenweg", Housenumber: "8", Confidence: 1.0, Lat: 51.13, Lon: 5.15},
			},
		},
	}

	flow := NewSearchFlow(
		map[string]PeliasSearcher{"west-europe": searcher},
		&fakeRegionSearcher{list: regionclient.RegionList{
			CoreRegions: []string{"west-europe"},
		}},
		testLogger(),
	)

	req := makeSearchReq("oosthamsesteenweg 8, olmen")
	results, err := flow.Search(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected results after localadmin retry")
	}
	// The address result should be ranked first (confidence 1.0 > 0.6)
	if results[0].Layer != "address" {
		t.Errorf("expected address result first, got layer %q", results[0].Layer)
	}
	if searcher.callCount < 2 {
		t.Errorf("expected at least 2 calls (original + retry), got %d", searcher.callCount)
	}
}

func assertInvalidArgument(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected InvalidArgument error, got nil")
	}
	st, ok := status.FromError(err)
	if !ok || st.Code() != codes.InvalidArgument {
		t.Errorf("expected InvalidArgument, got %v", err)
	}
}

func withinEpsilon(a, b float64) bool {
	return math.Abs(a-b) < 1e-6
}

func TestFlow_nilViewport_returnsInvalidArgument(t *testing.T) {
	flow := NewSearchFlow(map[string]PeliasSearcher{}, &fakeRegionSearcher{}, testLogger())
	_, err := flow.Search(context.Background(), &searchv1.SearchRequest{Text: "test"})
	assertInvalidArgument(t, err)
}

func TestFlow_viewportMissingBothCorners_returnsInvalidArgument(t *testing.T) {
	flow := NewSearchFlow(map[string]PeliasSearcher{}, &fakeRegionSearcher{}, testLogger())
	req := &searchv1.SearchRequest{Text: "test", Viewport: &pbgeo.BoundingBox{}}
	_, err := flow.Search(context.Background(), req)
	assertInvalidArgument(t, err)
}

func TestFlow_viewportMissingOneCorner_returnsInvalidArgument(t *testing.T) {
	flow := NewSearchFlow(map[string]PeliasSearcher{}, &fakeRegionSearcher{}, testLogger())
	req := &searchv1.SearchRequest{
		Text: "test",
		Viewport: &pbgeo.BoundingBox{
			BottomLeft: &pbgeo.Coordinate{Lat: 37.8, Lon: -1.2},
		},
	}
	_, err := flow.Search(context.Background(), req)
	assertInvalidArgument(t, err)
}

func TestFlow_invertedLatitude_returnsInvalidArgument(t *testing.T) {
	flow := NewSearchFlow(map[string]PeliasSearcher{}, &fakeRegionSearcher{}, testLogger())
	req := &searchv1.SearchRequest{
		Text: "test",
		Viewport: &pbgeo.BoundingBox{
			BottomLeft: &pbgeo.Coordinate{Lat: 38.2, Lon: -1.2},
			TopRight:   &pbgeo.Coordinate{Lat: 37.8, Lon: -0.8},
		},
	}
	_, err := flow.Search(context.Background(), req)
	assertInvalidArgument(t, err)
}

func TestFlow_outOfRangeLatitude_returnsInvalidArgument(t *testing.T) {
	flow := NewSearchFlow(map[string]PeliasSearcher{}, &fakeRegionSearcher{}, testLogger())
	req := &searchv1.SearchRequest{
		Text: "test",
		Viewport: &pbgeo.BoundingBox{
			BottomLeft: &pbgeo.Coordinate{Lat: 37.8, Lon: -1.2},
			TopRight:   &pbgeo.Coordinate{Lat: 91, Lon: -0.8},
		},
	}
	_, err := flow.Search(context.Background(), req)
	assertInvalidArgument(t, err)
}

func TestFlow_outOfRangeLongitude_returnsInvalidArgument(t *testing.T) {
	flow := NewSearchFlow(map[string]PeliasSearcher{}, &fakeRegionSearcher{}, testLogger())
	req := &searchv1.SearchRequest{
		Text: "test",
		Viewport: &pbgeo.BoundingBox{
			BottomLeft: &pbgeo.Coordinate{Lat: 37.8, Lon: -1.2},
			TopRight:   &pbgeo.Coordinate{Lat: 38.2, Lon: 181},
		},
	}
	_, err := flow.Search(context.Background(), req)
	assertInvalidArgument(t, err)
}

func TestFlow_NaNCoordinate_returnsInvalidArgument(t *testing.T) {
	flow := NewSearchFlow(map[string]PeliasSearcher{}, &fakeRegionSearcher{}, testLogger())
	req := &searchv1.SearchRequest{
		Text: "test",
		Viewport: &pbgeo.BoundingBox{
			BottomLeft: &pbgeo.Coordinate{Lat: 37.8, Lon: -1.2},
			TopRight:   &pbgeo.Coordinate{Lat: math.NaN(), Lon: -0.8},
		},
	}
	_, err := flow.Search(context.Background(), req)
	assertInvalidArgument(t, err)
}

func TestFlow_focusPointOutOfRange_returnsInvalidArgument(t *testing.T) {
	flow := NewSearchFlow(map[string]PeliasSearcher{}, &fakeRegionSearcher{}, testLogger())
	req := makeSearchReq("test")
	req.FocusPoint = &pbgeo.Coordinate{Lat: 91, Lon: 0}
	_, err := flow.Search(context.Background(), req)
	assertInvalidArgument(t, err)
}

func TestFlow_focusPointNaN_returnsInvalidArgument(t *testing.T) {
	flow := NewSearchFlow(map[string]PeliasSearcher{}, &fakeRegionSearcher{}, testLogger())
	req := makeSearchReq("test")
	req.FocusPoint = &pbgeo.Coordinate{Lat: 0, Lon: math.NaN()}
	_, err := flow.Search(context.Background(), req)
	assertInvalidArgument(t, err)
}

func TestFlow_datelineCrossingViewport_wrapsBoxAndFocus(t *testing.T) {
	searcher := &fakePeliasSearcher{results: []*searchv1.Result{
		{Label: "Result", Layer: "venue", Confidence: 1.0, Lat: 0, Lon: 180},
	}}
	regionSearcher := &fakeRegionSearcher{list: regionclient.RegionList{
		CoreRegions: []string{"pacific"},
	}}
	flow := NewSearchFlow(
		map[string]PeliasSearcher{"pacific": searcher},
		regionSearcher,
		testLogger(),
	)

	req := &searchv1.SearchRequest{
		Text: "test",
		Viewport: &pbgeo.BoundingBox{
			BottomLeft: &pbgeo.Coordinate{Lat: -10, Lon: 179},
			TopRight:   &pbgeo.Coordinate{Lat: 10, Lon: -179},
		},
	}
	results, err := flow.Search(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected results")
	}
	if !withinEpsilon(regionSearcher.lastBox.BottomLeft.Longitude, 177) {
		t.Errorf("bottom_left lon: got %v, want 177", regionSearcher.lastBox.BottomLeft.Longitude)
	}
	if !withinEpsilon(regionSearcher.lastBox.TopRight.Longitude, -177) {
		t.Errorf("top_right lon: got %v, want -177", regionSearcher.lastBox.TopRight.Longitude)
	}
	if !withinEpsilon(math.Abs(searcher.lastFocusLon), 180) {
		t.Errorf("focus lon: got %v, want ±180", searcher.lastFocusLon)
	}
}

func TestFlow_nearEdgeViewport_clampsExpandedBox(t *testing.T) {
	searcher := &fakePeliasSearcher{results: []*searchv1.Result{
		{Label: "Result", Layer: "venue", Confidence: 1.0, Lat: 0, Lon: 175},
	}}
	regionSearcher := &fakeRegionSearcher{list: regionclient.RegionList{
		CoreRegions: []string{"pacific"},
	}}
	flow := NewSearchFlow(
		map[string]PeliasSearcher{"pacific": searcher},
		regionSearcher,
		testLogger(),
	)

	req := &searchv1.SearchRequest{
		Text: "test",
		Viewport: &pbgeo.BoundingBox{
			BottomLeft: &pbgeo.Coordinate{Lat: -10, Lon: 170},
			TopRight:   &pbgeo.Coordinate{Lat: 10, Lon: 180},
		},
	}
	results, err := flow.Search(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected results")
	}
	if !withinEpsilon(regionSearcher.lastBox.BottomLeft.Longitude, 160) {
		t.Errorf("bottom_left lon: got %v, want 160", regionSearcher.lastBox.BottomLeft.Longitude)
	}
	if !withinEpsilon(regionSearcher.lastBox.TopRight.Longitude, 180) {
		t.Errorf("top_right lon: got %v, want 180", regionSearcher.lastBox.TopRight.Longitude)
	}
}

func TestFlowAutocomplete_datelineFocusPoint_wrapsBox(t *testing.T) {
	searcher := &fakePeliasSearcher{results: []*searchv1.Result{
		{Label: "Result", Layer: "venue", Confidence: 1.0, Lat: 0, Lon: 179.9},
	}}
	regionSearcher := &fakeRegionSearcher{list: regionclient.RegionList{
		CoreRegions: []string{"pacific"},
	}}
	flow := NewSearchFlow(
		map[string]PeliasSearcher{"pacific": searcher},
		regionSearcher,
		testLogger(),
	)

	_, err := flow.Autocomplete(context.Background(), &searchv1.AutocompleteRequest{
		Text:       "test",
		FocusPoint: &pbgeo.Coordinate{Lat: 0, Lon: 179.9},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !withinEpsilon(regionSearcher.lastBox.BottomLeft.Longitude, 179.4) {
		t.Errorf("bottom_left lon: got %v, want ~179.4", regionSearcher.lastBox.BottomLeft.Longitude)
	}
	if !withinEpsilon(regionSearcher.lastBox.TopRight.Longitude, -179.6) {
		t.Errorf("top_right lon: got %v, want ~-179.6", regionSearcher.lastBox.TopRight.Longitude)
	}
}

func TestFlowAutocomplete_outOfRangeFocusPoint_returnsInvalidArgument(t *testing.T) {
	flow := NewSearchFlow(map[string]PeliasSearcher{}, &fakeRegionSearcher{}, testLogger())
	_, err := flow.Autocomplete(context.Background(), &searchv1.AutocompleteRequest{
		Text:       "test",
		FocusPoint: &pbgeo.Coordinate{Lat: 91, Lon: 0},
	})
	assertInvalidArgument(t, err)
}

func TestFlowAutocomplete_NaNFocusPoint_returnsInvalidArgument(t *testing.T) {
	flow := NewSearchFlow(map[string]PeliasSearcher{}, &fakeRegionSearcher{}, testLogger())
	_, err := flow.Autocomplete(context.Background(), &searchv1.AutocompleteRequest{
		Text:       "test",
		FocusPoint: &pbgeo.Coordinate{Lat: 0, Lon: math.NaN()},
	})
	assertInvalidArgument(t, err)
}

func TestFlowAutocomplete_nilFocusPoint_returnsInvalidArgument(t *testing.T) {
	flow := NewSearchFlow(map[string]PeliasSearcher{}, &fakeRegionSearcher{}, testLogger())
	_, err := flow.Autocomplete(context.Background(), &searchv1.AutocompleteRequest{Text: "test"})
	assertInvalidArgument(t, err)
}

func TestFlowAutocomplete_success(t *testing.T) {
	searcher := &fakePeliasSearcher{results: []*searchv1.Result{
		{Label: "Result", Layer: "venue", Confidence: 1.0, Lat: 51.0, Lon: 4.0},
	}}
	flow := NewSearchFlow(
		map[string]PeliasSearcher{"west-europe": searcher},
		&fakeRegionSearcher{list: regionclient.RegionList{CoreRegions: []string{"west-europe"}}},
		testLogger(),
	)

	results, err := flow.Autocomplete(context.Background(), &searchv1.AutocompleteRequest{
		Text:       "test",
		FocusPoint: &pbgeo.Coordinate{Lat: 51.0, Lon: 4.0},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected results")
	}
	if searcher.callCount != 1 {
		t.Errorf("expected 1 pelias call, got %d", searcher.callCount)
	}
}

func TestFlowAutocomplete_regionServiceUnavailable(t *testing.T) {
	flow := NewSearchFlow(
		map[string]PeliasSearcher{},
		&fakeRegionSearcher{err: errors.New("connection refused")},
		testLogger(),
	)

	_, err := flow.Autocomplete(context.Background(), &searchv1.AutocompleteRequest{
		Text:       "test",
		FocusPoint: &pbgeo.Coordinate{Lat: 51.0, Lon: 4.0},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	st, ok := status.FromError(err)
	if !ok || st.Code() != codes.Unavailable {
		t.Errorf("expected Unavailable, got %v", err)
	}
}

func TestFlowAutocomplete_allPeliasError(t *testing.T) {
	errSearcher := &fakePeliasSearcher{err: errors.New("timeout")}
	flow := NewSearchFlow(
		map[string]PeliasSearcher{"west-europe": errSearcher},
		&fakeRegionSearcher{list: regionclient.RegionList{CoreRegions: []string{"west-europe"}}},
		testLogger(),
	)

	_, err := flow.Autocomplete(context.Background(), &searchv1.AutocompleteRequest{
		Text:       "test",
		FocusPoint: &pbgeo.Coordinate{Lat: 51.0, Lon: 4.0},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	st, ok := status.FromError(err)
	if !ok || st.Code() != codes.Unavailable {
		t.Errorf("expected Unavailable, got %v", err)
	}
}

func TestFlow_emptyText_returnsInvalidArgument(t *testing.T) {
	flow := NewSearchFlow(map[string]PeliasSearcher{}, &fakeRegionSearcher{}, testLogger())
	_, err := flow.Search(context.Background(), makeSearchReq(""))
	assertInvalidArgument(t, err)
}

func TestFlow_whitespaceText_returnsInvalidArgument(t *testing.T) {
	flow := NewSearchFlow(map[string]PeliasSearcher{}, &fakeRegionSearcher{}, testLogger())
	_, err := flow.Search(context.Background(), makeSearchReq("   "))
	assertInvalidArgument(t, err)
}

func TestFlowAutocomplete_emptyText_returnsInvalidArgument(t *testing.T) {
	flow := NewSearchFlow(map[string]PeliasSearcher{}, &fakeRegionSearcher{}, testLogger())
	_, err := flow.Autocomplete(context.Background(), &searchv1.AutocompleteRequest{
		Text:       "",
		FocusPoint: &pbgeo.Coordinate{Lat: 0, Lon: 0},
	})
	assertInvalidArgument(t, err)
}
