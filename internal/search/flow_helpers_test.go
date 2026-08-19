package search

import (
	"context"
	"math"
	"testing"

	searchv1 "github.com/swayrider/protos/search/v1"
	"google.golang.org/grpc/metadata"
)

func TestIncomingToken(t *testing.T) {
	tests := []struct {
		name string
		ctx  context.Context
		want string
	}{
		{
			name: "no metadata",
			ctx:  context.Background(),
			want: "",
		},
		{
			name: "no authorization header",
			ctx:  metadata.NewIncomingContext(context.Background(), metadata.Pairs("other", "value")),
			want: "",
		},
		{
			name: "bearer token",
			ctx:  metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Bearer abc123")),
			want: "abc123",
		},
		{
			name: "plain token without bearer prefix",
			ctx:  metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "abc123")),
			want: "abc123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := incomingToken(tt.ctx); got != tt.want {
				t.Errorf("incomingToken() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestClampLat(t *testing.T) {
	tests := []struct {
		in   float64
		want float64
	}{
		{-91, -90},
		{-90, -90},
		{-0.5, -0.5},
		{0, 0},
		{45, 45},
		{90, 90},
		{91, 90},
	}

	for _, tt := range tests {
		if got := clampLat(tt.in); got != tt.want {
			t.Errorf("clampLat(%v) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestClampLon(t *testing.T) {
	tests := []struct {
		in   float64
		want float64
	}{
		{-181, -180},
		{-180, -180},
		{-0.5, -0.5},
		{0, 0},
		{45, 45},
		{180, 180},
		{181, 180},
	}

	for _, tt := range tests {
		if got := clampLon(tt.in); got != tt.want {
			t.Errorf("clampLon(%v) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestWrapLon(t *testing.T) {
	tests := []struct {
		in   float64
		want float64
	}{
		{181, -179},
		{-181, 179},
		{360, 0},
		{720, 0},
		{179.4, 179.4},
		{-179.4, -179.4},
		{180, 180},
		{-180, -180},
	}

	for _, tt := range tests {
		if got := wrapLon(tt.in); got != tt.want {
			t.Errorf("wrapLon(%v) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestValidCoordinate(t *testing.T) {
	tests := []struct {
		lat, lon float64
		want     bool
	}{
		{0, 0, true},
		{90, 180, true},
		{-90, -180, true},
		{90.1, 0, false},
		{-90.1, 0, false},
		{0, 180.1, false},
		{0, -180.1, false},
		{math.NaN(), 0, false},
		{0, math.NaN(), false},
		{math.Inf(1), 0, false},
		{0, math.Inf(-1), false},
	}

	for _, tt := range tests {
		if got := validCoordinate(tt.lat, tt.lon); got != tt.want {
			t.Errorf("validCoordinate(%v, %v) = %v, want %v", tt.lat, tt.lon, got, tt.want)
		}
	}
}

func TestHasLayerResult(t *testing.T) {
	results := []*searchv1.Result{
		{Layer: "address"},
		{Layer: "locality"},
	}

	if !hasLayerResult(results, "address") {
		t.Error("expected address layer to be found")
	}
	if !hasLayerResult(results, "locality") {
		t.Error("expected locality layer to be found")
	}
	if hasLayerResult(results, "venue") {
		t.Error("expected venue layer not to be found")
	}
	if hasLayerResult(nil, "address") {
		t.Error("expected false for empty results")
	}
}
