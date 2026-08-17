package server

import (
	"context"
	"errors"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	healthv1 "github.com/swayrider/protos/health/v1"
	pbgeo "github.com/swayrider/protos/common_types/geo"
	searchv1 "github.com/swayrider/protos/search/v1"
	log "github.com/swayrider/swlib/logger"
)

type mockSearchFlow struct {
	searchFn         func(context.Context, *searchv1.SearchRequest) ([]*searchv1.Result, error)
	reverseGeocodeFn func(context.Context, *searchv1.ReverseGeocodeRequest) ([]*searchv1.Result, error)
	autocompleteFn   func(context.Context, *searchv1.AutocompleteRequest) ([]*searchv1.Result, error)
}

func (m *mockSearchFlow) Search(ctx context.Context, req *searchv1.SearchRequest) ([]*searchv1.Result, error) {
	if m.searchFn != nil {
		return m.searchFn(ctx, req)
	}
	return nil, nil
}

func (m *mockSearchFlow) ReverseGeocode(ctx context.Context, req *searchv1.ReverseGeocodeRequest) ([]*searchv1.Result, error) {
	if m.reverseGeocodeFn != nil {
		return m.reverseGeocodeFn(ctx, req)
	}
	return nil, nil
}

func (m *mockSearchFlow) Autocomplete(ctx context.Context, req *searchv1.AutocompleteRequest) ([]*searchv1.Result, error) {
	if m.autocompleteFn != nil {
		return m.autocompleteFn(ctx, req)
	}
	return nil, nil
}

func newTestSearchServer(flow searchFlow) *SearchServer {
	return NewSearchServer(flow, log.New())
}

func TestPing_returnsEmpty(t *testing.T) {
	srv := NewHealthServer(log.New())
	resp, err := srv.Ping(context.Background(), &healthv1.PingRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
}

func TestSearch_success(t *testing.T) {
	want := []*searchv1.Result{
		{Id: "r1", Label: "Test Street 1, City", Layer: "address", Confidence: 0.9},
	}
	srv := newTestSearchServer(&mockSearchFlow{
		searchFn: func(_ context.Context, _ *searchv1.SearchRequest) ([]*searchv1.Result, error) {
			return want, nil
		},
	})

	resp, err := srv.Search(context.Background(), &searchv1.SearchRequest{Text: "Test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(resp.Results))
	}
	if resp.Results[0].Id != "r1" {
		t.Errorf("expected id=r1, got %q", resp.Results[0].Id)
	}
}

func TestSearch_propagatesError(t *testing.T) {
	grpcErr := status.Error(codes.Unavailable, "all pelias servers unavailable")
	srv := newTestSearchServer(&mockSearchFlow{
		searchFn: func(_ context.Context, _ *searchv1.SearchRequest) ([]*searchv1.Result, error) {
			return nil, grpcErr
		},
	})

	_, err := srv.Search(context.Background(), &searchv1.SearchRequest{Text: "Test"})
	if err == nil {
		t.Fatal("expected error")
	}
	st, ok := status.FromError(err)
	if !ok || st.Code() != codes.Unavailable {
		t.Errorf("expected Unavailable, got %v", err)
	}
}

func TestReverseGeocode_success(t *testing.T) {
	want := []*searchv1.Result{
		{Id: "geo1", Label: "Grote Markt 1, Antwerp", Layer: "address", Confidence: 0.95},
	}
	srv := newTestSearchServer(&mockSearchFlow{
		reverseGeocodeFn: func(_ context.Context, _ *searchv1.ReverseGeocodeRequest) ([]*searchv1.Result, error) {
			return want, nil
		},
	})

	req := &searchv1.ReverseGeocodeRequest{Point: &pbgeo.Coordinate{Lat: 51.22, Lon: 4.40}}
	resp, err := srv.ReverseGeocode(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(resp.Results))
	}
	if resp.Results[0].Id != "geo1" {
		t.Errorf("expected id=geo1, got %q", resp.Results[0].Id)
	}
}

func TestReverseGeocode_propagatesError(t *testing.T) {
	srv := newTestSearchServer(&mockSearchFlow{
		reverseGeocodeFn: func(_ context.Context, _ *searchv1.ReverseGeocodeRequest) ([]*searchv1.Result, error) {
			return nil, errors.New("internal error")
		},
	})

	req := &searchv1.ReverseGeocodeRequest{Point: &pbgeo.Coordinate{Lat: 51.22, Lon: 4.40}}
	_, err := srv.ReverseGeocode(context.Background(), req)
	if err == nil {
		t.Fatal("expected error")
	}
}
