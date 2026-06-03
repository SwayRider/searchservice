package search

import (
	"context"
	"errors"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"github.com/swayrider/grpcclients/regionclient"
	pbgeo "github.com/swayrider/protos/common_types/geo"
	searchv1 "github.com/swayrider/protos/search/v1"
)

func makeReverseReq(lat, lon float64) *searchv1.ReverseGeocodeRequest {
	return &searchv1.ReverseGeocodeRequest{
		Point: &pbgeo.Coordinate{Lat: lat, Lon: lon},
	}
}

func TestFlowReverse_nilPoint(t *testing.T) {
	flow := NewSearchFlow(map[string]PeliasSearcher{}, &fakeRegionSearcher{}, testLogger())

	_, err := flow.ReverseGeocode(context.Background(), &searchv1.ReverseGeocodeRequest{})
	if err == nil {
		t.Fatal("expected error for nil point")
	}
	st, ok := status.FromError(err)
	if !ok || st.Code() != codes.InvalidArgument {
		t.Errorf("expected InvalidArgument, got %v", err)
	}
}

func TestFlowReverse_regionServiceUnavailable(t *testing.T) {
	flow := NewSearchFlow(
		map[string]PeliasSearcher{},
		&fakeRegionSearcher{err: errors.New("connection refused")},
		testLogger(),
	)

	_, err := flow.ReverseGeocode(context.Background(), makeReverseReq(51.0, 4.0))
	if err == nil {
		t.Fatal("expected error")
	}
	st, ok := status.FromError(err)
	if !ok || st.Code() != codes.Unavailable {
		t.Errorf("expected Unavailable, got %v", err)
	}
}

func TestFlowReverse_noRegions(t *testing.T) {
	flow := NewSearchFlow(
		map[string]PeliasSearcher{},
		&fakeRegionSearcher{list: regionclient.RegionList{}},
		testLogger(),
	)

	_, err := flow.ReverseGeocode(context.Background(), makeReverseReq(51.0, 4.0))
	if err == nil {
		t.Fatal("expected error for empty region list")
	}
	st, ok := status.FromError(err)
	if !ok || st.Code() != codes.NotFound {
		t.Errorf("expected NotFound, got %v", err)
	}
}

func TestFlowReverse_noClientForRegion(t *testing.T) {
	flow := NewSearchFlow(
		map[string]PeliasSearcher{},
		&fakeRegionSearcher{list: regionclient.RegionList{
			CoreRegions: []string{"west-europe"},
		}},
		testLogger(),
	)

	_, err := flow.ReverseGeocode(context.Background(), makeReverseReq(51.0, 4.0))
	if err == nil {
		t.Fatal("expected error when no pelias client configured for region")
	}
	st, ok := status.FromError(err)
	if !ok || st.Code() != codes.NotFound {
		t.Errorf("expected NotFound, got %v", err)
	}
}

func TestFlowReverse_peliasError(t *testing.T) {
	searcher := &fakePeliasSearcher{err: errors.New("timeout")}
	flow := NewSearchFlow(
		map[string]PeliasSearcher{"west-europe": searcher},
		&fakeRegionSearcher{list: regionclient.RegionList{
			CoreRegions: []string{"west-europe"},
		}},
		testLogger(),
	)

	_, err := flow.ReverseGeocode(context.Background(), makeReverseReq(51.0, 4.0))
	if err == nil {
		t.Fatal("expected error")
	}
	st, ok := status.FromError(err)
	if !ok || st.Code() != codes.Internal {
		t.Errorf("expected Internal, got %v", err)
	}
}

func TestFlowReverse_successCoreRegion(t *testing.T) {
	expected := []*searchv1.Result{
		{Id: "r1", Label: "Grote Markt 1, Antwerp", Layer: "address", Confidence: 0.95},
	}
	searcher := &fakePeliasSearcher{results: expected}
	flow := NewSearchFlow(
		map[string]PeliasSearcher{"west-europe": searcher},
		&fakeRegionSearcher{list: regionclient.RegionList{
			CoreRegions: []string{"west-europe"},
		}},
		testLogger(),
	)

	results, err := flow.ReverseGeocode(context.Background(), makeReverseReq(51.22, 4.40))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Id != "r1" {
		t.Errorf("expected id=r1, got %q", results[0].Id)
	}
	if searcher.callCount != 1 {
		t.Errorf("expected 1 pelias call, got %d", searcher.callCount)
	}
}

func TestFlowReverse_fallbackToExtended(t *testing.T) {
	extended := &fakePeliasSearcher{results: []*searchv1.Result{
		{Id: "ext1", Label: "Some Place", Layer: "venue", Confidence: 0.8},
	}}
	flow := NewSearchFlow(
		map[string]PeliasSearcher{"extended-region": extended},
		&fakeRegionSearcher{list: regionclient.RegionList{
			CoreRegions:     []string{},
			ExtendedRegions: []string{"extended-region"},
		}},
		testLogger(),
	)

	results, err := flow.ReverseGeocode(context.Background(), makeReverseReq(51.0, 4.0))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Id != "ext1" {
		t.Errorf("expected id=ext1, got %q", results[0].Id)
	}
	if extended.callCount != 1 {
		t.Errorf("expected 1 pelias call, got %d", extended.callCount)
	}
}
