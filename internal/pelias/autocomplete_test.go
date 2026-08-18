package pelias

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAutocomplete_success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/autocomplete" {
			t.Errorf("expected path /autocomplete, got %s", r.URL.Path)
		}
		q := r.URL.Query()
		if q.Get("text") != "plaza" {
			t.Errorf("text: got %q, want %q", q.Get("text"), "plaza")
		}
		if q.Get("focus.point.lat") != "37.984" {
			t.Errorf("focus.point.lat: got %q, want 37.984", q.Get("focus.point.lat"))
		}
		if q.Get("focus.point.lon") != "-1.128" {
			t.Errorf("focus.point.lon: got %q, want -1.128", q.Get("focus.point.lon"))
		}

		resp := map[string]interface{}{
			"features": []interface{}{
				makeFeature("Plaza Sandoval, Murcia, Spain", "ES", "venue", -1.128, 37.984),
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := New(server.URL)
	results, err := client.Autocomplete(context.Background(), "plaza", "", 37.984, -1.128)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Id != "test-gid" {
		t.Errorf("expected id=test-gid, got %s", results[0].Id)
	}
	if results[0].Lat != 37.984 || results[0].Lon != -1.128 {
		t.Errorf("expected coords (37.984, -1.128), got (%f, %f)", results[0].Lat, results[0].Lon)
	}
}

func TestAutocomplete_httpError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := New(server.URL)
	_, err := client.Autocomplete(context.Background(), "plaza", "", 0, 0)
	if err == nil {
		t.Fatal("expected error on HTTP 500")
	}
}

func TestAutocomplete_withLanguage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept-Language") != "es" {
			t.Errorf("expected Accept-Language=es, got %q", r.Header.Get("Accept-Language"))
		}
		resp := map[string]interface{}{"features": []interface{}{}}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := New(server.URL)
	results, err := client.Autocomplete(context.Background(), "plaza", "es", 0, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}
