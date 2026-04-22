package search

import (
	"testing"

	searchv1 "github.com/swayrider/protos/search/v1"
)

func makeResult(label, street, locality, layer string, confidence, lat, lon float64) *searchv1.Result {
	return &searchv1.Result{
		Label:      label,
		Street:     street,
		Locality:   locality,
		Layer:      layer,
		Confidence: confidence,
		Lat:        lat,
		Lon:        lon,
	}
}

func TestRank_confidenceFirst(t *testing.T) {
	results := []*searchv1.Result{
		makeResult("Low Confidence", "", "", "venue", 0.96, 0, 0),
		makeResult("High Confidence", "", "", "venue", 1.0, 0, 0),
	}
	ranked := Rank(results, "", 0, 0, 5)
	if len(ranked) == 0 {
		t.Fatal("expected results, got empty")
	}
	if ranked[0].Label != "High Confidence" {
		t.Errorf("expected High Confidence first, got %s", ranked[0].Label)
	}
}

func TestRank_distanceTiebreak(t *testing.T) {
	// Both near focus (51.0, 5.0); Near is closer
	focusLat, focusLon := 51.0, 5.0
	results := []*searchv1.Result{
		makeResult("Far", "", "", "venue", 1.0, 51.05, 5.05),
		makeResult("Near", "", "", "venue", 1.0, 51.01, 5.01),
	}
	ranked := Rank(results, "", focusLat, focusLon, 5)
	if len(ranked) == 0 {
		t.Fatal("expected results, got empty")
	}
	if ranked[0].Label != "Near" {
		t.Errorf("expected Near first, got %s", ranked[0].Label)
	}
}

func TestRank_cutoffDisabled(t *testing.T) {
	// With cutoff at 0.0, all results are retained
	results := []*searchv1.Result{
		makeResult("Good", "", "", "venue", 1.0, 0, 0),
		makeResult("Bad", "", "", "venue", 0.5, 0, 0),
	}
	ranked := Rank(results, "", 0, 0, 5)
	if len(ranked) != 2 {
		t.Fatalf("expected 2 results (cutoff disabled), got %d", len(ranked))
	}
}

func TestCollapseAddresses_sameStreetLocality(t *testing.T) {
	results := []*searchv1.Result{
		makeResult("1 Main St, City", "Main St", "City", "address", 0.7, 0, 0),
		makeResult("2 Main St, City", "Main St", "City", "address", 0.9, 1, 1),
	}
	collapsed := CollapseAddresses(results, 0, 0)
	if len(collapsed) != 1 {
		t.Fatalf("expected 1 result, got %d", len(collapsed))
	}
	if collapsed[0].Confidence != 0.9 {
		t.Errorf("expected higher confidence kept, got %f", collapsed[0].Confidence)
	}
}

func TestCollapseAddresses_differentStreet(t *testing.T) {
	results := []*searchv1.Result{
		makeResult("1 Main St, City", "Main St", "City", "address", 0.9, 0, 0),
		makeResult("1 Oak Ave, City", "Oak Ave", "City", "address", 0.9, 1, 1),
	}
	collapsed := CollapseAddresses(results, 0, 0)
	if len(collapsed) != 2 {
		t.Errorf("expected 2 results (different streets), got %d", len(collapsed))
	}
}

func TestCollapseAddresses_differentLocality(t *testing.T) {
	results := []*searchv1.Result{
		makeResult("1 Main St, CityA", "Main St", "CityA", "address", 0.9, 0, 0),
		makeResult("1 Main St, CityB", "Main St", "CityB", "address", 0.9, 1, 1),
	}
	collapsed := CollapseAddresses(results, 0, 0)
	if len(collapsed) != 2 {
		t.Errorf("expected 2 results (different localities), got %d", len(collapsed))
	}
}

func TestCollapseAddresses_nonAddressPassthrough(t *testing.T) {
	results := []*searchv1.Result{
		makeResult("Venue A", "", "City", "venue", 0.9, 0, 0),
		makeResult("Street B", "Street B", "City", "street", 0.8, 1, 1),
	}
	collapsed := CollapseAddresses(results, 0, 0)
	if len(collapsed) != 2 {
		t.Errorf("expected 2 non-address results, got %d", len(collapsed))
	}
}

func TestRank_sizeTruncation(t *testing.T) {
	results := make([]*searchv1.Result, 10)
	for i := range results {
		results[i] = makeResult("R", "", "", "venue", 0.95+float64(i)*0.005, 0, 0)
	}
	ranked := Rank(results, "", 0, 0, 5)
	if len(ranked) != 5 {
		t.Errorf("expected 5, got %d", len(ranked))
	}
}

func TestRank_defaultSize(t *testing.T) {
	results := make([]*searchv1.Result, 10)
	for i := range results {
		results[i] = makeResult("R", "", "", "venue", 0.95+float64(i)*0.005, 0, 0)
	}
	ranked := Rank(results, "", 0, 0, 0) // 0 → default (5)
	if len(ranked) != 5 {
		t.Errorf("expected 5 (default), got %d", len(ranked))
	}
}

func TestRank_maxSize(t *testing.T) {
	results := make([]*searchv1.Result, 30)
	for i := range results {
		results[i] = makeResult("R", "", "", "venue", 0.95+float64(i)*0.002, 0, 0)
	}
	ranked := Rank(results, "", 0, 0, 50) // 50 → capped at 20
	if len(ranked) != 20 {
		t.Errorf("expected 20 (max), got %d", len(ranked))
	}
}

func TestRank_textScoreBoostsQueryMatch(t *testing.T) {
	focusLat, focusLon := 37.4, -5.9
	results := []*searchv1.Result{
		makeResult("Plaza Sandoval, Seville, Spain", "Plaza Sandoval", "Seville", "address", 1.0, 37.4, -5.9),
		makeResult("Plaza Sandoval 7, Murcia, Spain", "Plaza Sandoval", "Murcia", "address", 1.0, 37.4, -5.0),
	}
	ranked := Rank(results, "plaza sandoval, murcia", focusLat, focusLon, 5)
	if len(ranked) == 0 {
		t.Fatal("expected results, got empty")
	}
	if ranked[0].Locality != "Murcia" {
		t.Errorf("expected Murcia result first (text score), got %s", ranked[0].Label)
	}
}

func makeResultWithID(id, label, street, housenumber, locality, layer string, confidence, lat, lon float64) *searchv1.Result {
	return &searchv1.Result{
		Id:          id,
		Label:       label,
		Street:      street,
		Housenumber: housenumber,
		Locality:    locality,
		Layer:       layer,
		Confidence:  confidence,
		Lat:         lat,
		Lon:         lon,
	}
}

func TestRank_housenumberExactMatch(t *testing.T) {
	results := []*searchv1.Result{
		makeResultWithID("addr-62", "Oosthamsesteenweg 62_8, Belgium", "Oosthamsesteenweg", "62_8", "Kwaadmechelen", "address", 1.0, 51.124, 5.159),
		makeResultWithID("addr-8", "Oosthamsesteenweg 8, Belgium", "Oosthamsesteenweg", "8", "Kwaadmechelen", "address", 1.0, 51.133, 5.156),
	}
	ranked := Rank(results, "oosthamsesteenweg 8", 51.1, 5.1, 5)
	if ranked[0].Housenumber != "8" {
		t.Errorf("expected housenumber 8 first, got %s", ranked[0].Housenumber)
	}
}

func TestDeduplicateByID_removesDuplicates(t *testing.T) {
	results := []*searchv1.Result{
		makeResultWithID("dup-1", "Balen, Belgium", "", "", "", "locality", 0.6, 51.17, 5.17),
		makeResultWithID("dup-1", "Balen, Belgium", "", "", "", "locality", 0.6, 51.17, 5.17),
		makeResultWithID("dup-2", "Other, Belgium", "", "", "", "locality", 0.6, 51.0, 5.0),
	}
	deduped := DeduplicateByID(results, 51.1, 5.1)
	if len(deduped) != 2 {
		t.Errorf("expected 2 results after dedup, got %d", len(deduped))
	}
}

func TestDeduplicateByID_keepsHigherConfidence(t *testing.T) {
	results := []*searchv1.Result{
		makeResultWithID("dup-1", "Low", "", "", "", "locality", 0.5, 51.17, 5.17),
		makeResultWithID("dup-1", "High", "", "", "", "locality", 0.8, 51.17, 5.17),
	}
	deduped := DeduplicateByID(results, 51.1, 5.1)
	if len(deduped) != 1 {
		t.Fatalf("expected 1 result, got %d", len(deduped))
	}
	if deduped[0].Label != "High" {
		t.Errorf("expected High confidence kept, got %s", deduped[0].Label)
	}
}

func TestDeduplicateByID_passesThroughEmptyID(t *testing.T) {
	results := []*searchv1.Result{
		makeResultWithID("", "No ID 1", "", "", "", "locality", 0.6, 51.17, 5.17),
		makeResultWithID("", "No ID 2", "", "", "", "locality", 0.6, 51.17, 5.17),
	}
	deduped := DeduplicateByID(results, 51.1, 5.1)
	if len(deduped) != 2 {
		t.Errorf("expected 2 results (empty ids pass through), got %d", len(deduped))
	}
}

func TestRank_distancePenaltyDemotesDistantResults(t *testing.T) {
	results := []*searchv1.Result{
		makeResultWithID("far", "Balen, Switzerland", "", "", "Valens", "neighbourhood", 1.0, 48.0, 7.0),
		makeResultWithID("near", "Balen, Belgium", "", "", "", "locality", 1.0, 51.17, 5.17),
	}
	ranked := Rank(results, "balen", 51.1, 5.1, 5)
	if len(ranked) == 0 {
		t.Fatal("expected results, got empty")
	}
	if ranked[0].Id != "near" {
		t.Errorf("expected near result first due to distance penalty, got %s (label=%s)", ranked[0].Id, ranked[0].Label)
	}
}

func TestRank_distancePenaltyPreservesHighConfidence(t *testing.T) {
	results := []*searchv1.Result{
		makeResultWithID("far", "Far Address", "Main St", "1", "FarCity", "address", 1.0, 51.5, 5.5),
		makeResultWithID("near", "Near Locality", "", "", "", "locality", 1.0, 51.17, 5.17),
	}
	ranked := Rank(results, "main st 1", 51.1, 5.1, 5)
	if ranked[0].Id != "far" {
		t.Errorf("expected high-confidence far result first, got %s (label=%s)", ranked[0].Id, ranked[0].Label)
	}
}

func TestFuzzyStreetPenalty_mismatchedStreet(t *testing.T) {
	results := []*searchv1.Result{
		makeResultWithID("de", "Engelbertstraße 8, Germany", "Engelbertstraße", "8", "Selfkant", "address", 1.0, 51.04, 5.88),
		makeResultWithID("be", "Oosthamsesteenweg 8, Balen, Belgium", "Oosthamsesteenweg", "8", "Balen", "address", 1.0, 51.13, 5.16),
	}
	ranked := Rank(results, "oosthamsesteenweg 8", 51.1, 5.1, 5)
	if len(ranked) == 0 {
		t.Fatal("expected at least 1 result")
	}
	if ranked[0].Id != "be" {
		t.Errorf("expected Belgian result first, got %s", ranked[0].Id)
	}
	// German result should be filtered out by cutoff (fuzzy penalty → score < 0.95)
	for _, r := range ranked {
		if r.CountryCode == "DE" {
			t.Errorf("expected German result filtered out, but found: %s", r.Label)
		}
	}
}

func TestFuzzyStreetPenalty_noPenaltyWithoutStreet(t *testing.T) {
	results := []*searchv1.Result{
		makeResultWithID("locality", "Balen, Belgium", "", "", "Balen", "locality", 1.0, 51.17, 5.17),
	}
	ranked := Rank(results, "oosthamsesteenweg 8", 51.1, 5.1, 5)
	if len(ranked) != 1 || ranked[0].Id != "locality" {
		t.Errorf("expected locality result to pass through without penalty")
	}
}

func TestFuzzyStreetPenalty_partialMatch(t *testing.T) {
	// "oosthamsest" is a substring of "oosthamsesteenweg" → no penalty
	// "kirchplatz" is completely different → high penalty
	results := []*searchv1.Result{
		makeResultWithID("partial", "Oosthamsesteenweg 8, Belgium", "Oosthamsesteenweg", "8", "SomePlace", "address", 1.0, 51.1, 5.1),
		makeResultWithID("unrelated", "Kirchplatz 8, Germany", "Kirchplatz", "8", "Selfkant", "address", 1.0, 51.04, 5.88),
	}
	ranked := Rank(results, "oosthamsesteenweg 8", 51.1, 5.1, 5)
	if len(ranked) == 0 {
		t.Fatal("expected at least 1 result")
	}
	if ranked[0].Id != "partial" {
		t.Errorf("expected matching street first, got %s", ranked[0].Id)
	}
}

func TestEditDistance(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"abc", "", 3},
		{"", "abc", 3},
		{"abc", "abc", 0},
		{"kitten", "sitting", 3},
		{"oosthamsesteenweg", "oosthamsesteenweg", 0},
	}
	for _, tc := range cases {
		got := editDistance(tc.a, tc.b)
		if got != tc.want {
			t.Errorf("editDistance(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}
