package pelias

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func makeFeature(label, countryCode, layer string, lon, lat float64) map[string]interface{} {
	return map[string]interface{}{
		"properties": map[string]interface{}{
			"gid":          "test-gid",
			"name":         "Test Name",
			"label":        label,
			"street":       "Test Street",
			"housenumber":  "1",
			"localadmin":   "Test City",
			"locality":     "Test Town",
			"region":       "Test Region",
			"country":      "Germany",
			"country_code": countryCode,
			"confidence":   0.9,
			"layer":        layer,
		},
		"geometry": map[string]interface{}{
			"coordinates": []interface{}{lon, lat},
		},
	}
}

func TestSearch_success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search" {
			t.Errorf("expected path /search, got %s", r.URL.Path)
		}
		resp := map[string]interface{}{
			"features": []interface{}{
				makeFeature("Test Street 1, Test City, Germany", "DE", "address", 9.1, 48.7),
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := New(server.URL)
	results, err := client.Search(context.Background(), "Test", "", 0, 0, false, 0, 0, 0, 0, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	r := results[0]
	if r.Id != "test-gid" {
		t.Errorf("id: got %q, want %q", r.Id, "test-gid")
	}
	if r.Layer != "address" {
		t.Errorf("layer: got %q, want %q", r.Layer, "address")
	}
	// GeoJSON coordinates are [lon, lat]; verify the swap
	if r.Lat != 48.7 {
		t.Errorf("lat: got %f, want 48.7", r.Lat)
	}
	if r.Lon != 9.1 {
		t.Errorf("lon: got %f, want 9.1", r.Lon)
	}
	if r.Confidence != 0.9 {
		t.Errorf("confidence: got %f, want 0.9", r.Confidence)
	}
}

func TestSearch_httpError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := New(server.URL)
	_, err := client.Search(context.Background(), "Test", "", 0, 0, false, 0, 0, 0, 0, false)
	if err == nil {
		t.Fatal("expected error on HTTP 500")
	}
}

func TestSearch_emptyResults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{"features": []interface{}{}}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := New(server.URL)
	results, err := client.Search(context.Background(), "Test", "", 0, 0, false, 0, 0, 0, 0, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestSearch_skipsEmptyLabel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"features": []interface{}{
				makeFeature("", "DE", "venue", 9.1, 48.7),
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := New(server.URL)
	results, err := client.Search(context.Background(), "Test", "", 0, 0, false, 0, 0, 0, 0, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results (empty label skipped), got %d", len(results))
	}
}

func TestSearch_skipsInvalidCoordinates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"features": []interface{}{
				map[string]interface{}{
					"properties": map[string]interface{}{
						"gid":   "x",
						"label": "Some Label",
					},
					"geometry": map[string]interface{}{
						"coordinates": []interface{}{9.1}, // only one value — invalid
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := New(server.URL)
	results, err := client.Search(context.Background(), "Test", "", 0, 0, false, 0, 0, 0, 0, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results (invalid coords skipped), got %d", len(results))
	}
}

func TestSearch_withFocus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("focus.point.lat") == "" {
			t.Error("focus.point.lat should be present when hasFocus=true")
		}
		if r.URL.Query().Get("focus.point.lon") == "" {
			t.Error("focus.point.lon should be present when hasFocus=true")
		}
		resp := map[string]interface{}{"features": []interface{}{}}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := New(server.URL)
	_, err := client.Search(context.Background(), "Test", "", 48.7, 9.1, true, 0, 0, 0, 0, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSearch_withoutFocus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Has("focus.point.lat") {
			t.Error("focus.point.lat should be absent when hasFocus=false")
		}
		if r.URL.Query().Has("focus.point.lon") {
			t.Error("focus.point.lon should be absent when hasFocus=false")
		}
		resp := map[string]interface{}{"features": []interface{}{}}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := New(server.URL)
	_, err := client.Search(context.Background(), "Test", "", 0, 0, false, 0, 0, 0, 0, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSearch_withBoundary(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		for _, p := range []string{"boundary.rect.min_lat", "boundary.rect.min_lon", "boundary.rect.max_lat", "boundary.rect.max_lon"} {
			if q.Get(p) == "" {
				t.Errorf("%s should be present when hasBoundary=true", p)
			}
		}
		resp := map[string]interface{}{"features": []interface{}{}}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := New(server.URL)
	_, err := client.Search(context.Background(), "Test", "", 0, 0, false, 47.0, 8.0, 50.0, 12.0, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSearch_withoutBoundary(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		for _, p := range []string{"boundary.rect.min_lat", "boundary.rect.min_lon", "boundary.rect.max_lat", "boundary.rect.max_lon"} {
			if q.Has(p) {
				t.Errorf("%s should be absent when hasBoundary=false", p)
			}
		}
		resp := map[string]interface{}{"features": []interface{}{}}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := New(server.URL)
	_, err := client.Search(context.Background(), "Test", "", 0, 0, false, 0, 0, 0, 0, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSearch_withLanguage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept-Language") != "fr" {
			t.Errorf("expected Accept-Language=fr, got %q", r.Header.Get("Accept-Language"))
		}
		resp := map[string]interface{}{"features": []interface{}{}}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := New(server.URL)
	_, err := client.Search(context.Background(), "Test", "fr", 0, 0, false, 0, 0, 0, 0, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSearch_alwaysSendsTextLayersSize(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("text") != "hello world" {
			t.Errorf("text: got %q, want %q", q.Get("text"), "hello world")
		}
		if q.Get("layers") == "" {
			t.Error("layers should always be present")
		}
		if q.Get("size") != "40" {
			t.Errorf("size: got %q, want 40", q.Get("size"))
		}
		resp := map[string]interface{}{"features": []interface{}{}}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := New(server.URL)
	_, err := client.Search(context.Background(), "hello world", "", 0, 0, false, 0, 0, 0, 0, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
