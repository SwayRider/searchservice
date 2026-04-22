package config

import (
	"testing"
)

func TestParsePeliasRegions_valid(t *testing.T) {
	m, err := ParsePeliasRegions("iberian-peninsula=http://pelias-iberian-peninsula-api:3100/v1,west-europe=http://pelias-west-europe-api:3100/v1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m["iberian-peninsula"] != "http://pelias-iberian-peninsula-api:3100/v1" {
		t.Errorf("iberian-peninsula: got %q", m["iberian-peninsula"])
	}
	if m["west-europe"] != "http://pelias-west-europe-api:3100/v1" {
		t.Errorf("west-europe: got %q", m["west-europe"])
	}
}

func TestParsePeliasRegions_urlWithEquals(t *testing.T) {
	// URL containing '=' (e.g. query param)
	m, err := ParsePeliasRegions("myregion=http://host/path?key=value")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m["myregion"] != "http://host/path?key=value" {
		t.Errorf("got %q", m["myregion"])
	}
}

func TestParsePeliasRegions_empty(t *testing.T) {
	m, err := ParsePeliasRegions("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m) != 0 {
		t.Errorf("expected empty map, got %v", m)
	}
}

func TestParsePeliasRegions_missingEquals(t *testing.T) {
	_, err := ParsePeliasRegions("regionnourl")
	if err == nil {
		t.Fatal("expected error for token without '='")
	}
}
