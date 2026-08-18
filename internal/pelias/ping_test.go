package pelias

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPing_returnsNilOn2xx(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := New(server.URL)
	if err := client.Ping(context.Background()); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

func TestPing_returnsErrorOn5xx(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := New(server.URL)
	if err := client.Ping(context.Background()); err == nil {
		t.Fatal("expected error on HTTP 500")
	}
}

func TestPing_returnsErrorOnConnectionRefused(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := server.URL
	server.Close() // close immediately to simulate an unreachable server

	client := New(url)
	if err := client.Ping(context.Background()); err == nil {
		t.Fatal("expected error on unreachable server")
	}
}
