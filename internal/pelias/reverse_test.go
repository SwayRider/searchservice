package pelias

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	searchv1 "github.com/swayrider/protos/search/v1"
)

func TestReverse_success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/reverse" {
			t.Errorf("expected path /reverse, got %s", r.URL.Path)
		}
		if r.URL.Query().Get("point.lat") != "51.13" {
			t.Errorf("expected point.lat=51.13, got %s", r.URL.Query().Get("point.lat"))
		}
		if r.URL.Query().Get("point.lon") != "5.15" {
			t.Errorf("expected point.lon=5.15, got %s", r.URL.Query().Get("point.lon"))
		}
		if r.URL.Query().Get("size") != "10" {
			t.Errorf("expected size=10, got %s", r.URL.Query().Get("size"))
		}

		resp := map[string]interface{}{
			"features": []interface{}{
				map[string]interface{}{
					"properties": map[string]interface{}{
						"gid":          "test-id",
						"name":         "Test Street 5",
						"label":        "Test Street 5, Test City, Belgium",
						"street":       "Test Street",
						"housenumber":  "5",
						"localadmin":   "Test City",
						"locality":     "Test Town",
						"region":       "Test Region",
						"country":      "Belgium",
						"country_code": "BE",
						"confidence":   0.95,
						"layer":        "address",
					},
					"geometry": map[string]interface{}{
						"coordinates": []interface{}{5.15, 51.13},
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := New(server.URL)
	results, err := client.Reverse(context.Background(), 51.13, 5.15, 10, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Id != "test-id" {
		t.Errorf("expected id=test-id, got %s", results[0].Id)
	}
	if results[0].Label != "Test Street 5, Test City, Belgium" {
		t.Errorf("expected label, got %s", results[0].Label)
	}
}

func TestReverse_emptyResults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{"features": []interface{}{}}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := New(server.URL)
	results, err := client.Reverse(context.Background(), 51.13, 5.15, 10, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestReverse_httpError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := New(server.URL)
	_, err := client.Reverse(context.Background(), 51.13, 5.15, 10, "")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestReverse_optionalSizeAndLanguage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// When size and language are not provided, they should not be in the URL
		if r.URL.Query().Has("size") {
			t.Error("size should not be present when not provided")
		}
		if r.URL.Query().Has("lang") {
			t.Error("lang should not be present when not provided")
		}

		resp := map[string]interface{}{"features": []interface{}{}}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := New(server.URL)
	results, err := client.Reverse(context.Background(), 51.13, 5.15, 0, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestReverse_withLanguage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("lang") != "nl" {
			t.Errorf("expected lang=nl, got %s", r.URL.Query().Get("lang"))
		}
		if r.Header.Get("Accept-Language") != "nl" {
			t.Errorf("expected Accept-Language=nl, got %s", r.Header.Get("Accept-Language"))
		}

		resp := map[string]interface{}{"features": []interface{}{}}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := New(server.URL)
	results, err := client.Reverse(context.Background(), 51.13, 5.15, 10, "nl")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestReverse_skipsEmptyLabel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"features": []interface{}{
				map[string]interface{}{
					"properties": map[string]interface{}{
						"gid":     "test-id",
						"name":    "Test",
						"label":   "", // empty label should be skipped
						"country": "Belgium",
					},
					"geometry": map[string]interface{}{
						"coordinates": []interface{}{5.15, 51.13},
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := New(server.URL)
	results, err := client.Reverse(context.Background(), 51.13, 5.15, 10, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Empty label should be skipped
	if len(results) != 0 {
		t.Errorf("expected 0 results (empty label skipped), got %d", len(results))
	}
}

func TestReverse_skipsInvalidCoordinates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"features": []interface{}{
				map[string]interface{}{
					"properties": map[string]interface{}{
						"gid":     "test-id",
						"name":    "Test",
						"label":   "Test Label",
						"country": "Belgium",
					},
					"geometry": map[string]interface{}{
						"coordinates": []interface{}{5.15}, // only one coordinate - invalid
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := New(server.URL)
	results, err := client.Reverse(context.Background(), 51.13, 5.15, 10, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Invalid coordinates should be skipped
	if len(results) != 0 {
		t.Errorf("expected 0 results (invalid coords skipped), got %d", len(results))
	}
}

var _ = searchv1.Result{} // ensure import is used
