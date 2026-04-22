package pelias

import "testing"

func TestFormatLabel_address(t *testing.T) {
	// Address with name matching street+number → no duplication
	label := formatLabel("BE", "Oosthamsesteenweg 8", "address", "Oosthamsesteenweg", "8", "Balen", "Kwaadmechelen", "Antwerpen", "België")
	expected := "Oosthamsesteenweg 8, Balen, België"
	if label != expected {
		t.Errorf("got %q, want %q", label, expected)
	}
}

func TestFormatLabel_venue(t *testing.T) {
	// Venue with meaningful company name
	label := formatLabel("BE", "Vitalita", "venue", "Oosthamsesteenweg", "8", "Balen", "Kwaadmechelen", "Antwerpen", "België")
	expected := "Vitalita Oosthamsesteenweg 8, Balen, België"
	if label != expected {
		t.Errorf("got %q, want %q", label, expected)
	}
}

func TestFormatLabel_venueNameIsAddress(t *testing.T) {
	// Venue where name == street + housenumber → deduplicate
	label := formatLabel("BE", "Oosthamsesteenweg 8", "venue", "Oosthamsesteenweg", "8", "Balen", "Kwaadmechelen", "Antwerpen", "België")
	expected := "Oosthamsesteenweg 8, Balen, België"
	if label != expected {
		t.Errorf("got %q, want %q", label, expected)
	}
}

func TestFormatLabel_noTemplate(t *testing.T) {
	label := formatLabel("ES", "", "address", "Calle Mayor", "5", "", "Madrid", "Madrid", "España")
	if label != "" {
		t.Errorf("expected empty for unknown country, got %q", label)
	}
}

func TestFormatLabel_emptyLocaladmin(t *testing.T) {
	// localadmin empty → falls back to locality
	label := formatLabel("BE", "", "address", "Oosthamsesteenweg", "8", "", "Kwaadmechelen", "Antwerpen", "België")
	expected := "Oosthamsesteenweg 8, Kwaadmechelen, België"
	if label != expected {
		t.Errorf("got %q, want %q", label, expected)
	}
}

func TestFormatLabel_emptyHousenumber(t *testing.T) {
	label := formatLabel("BE", "", "address", "Oosthamsesteenweg", "", "Balen", "Kwaadmechelen", "Antwerpen", "België")
	expected := "Oosthamsesteenweg, Balen, België"
	if label != expected {
		t.Errorf("got %q, want %q", label, expected)
	}
}

func TestFormatLabel_allEmpty(t *testing.T) {
	label := formatLabel("BE", "", "", "", "", "", "", "", "")
	if label != "" {
		t.Errorf("got %q, want empty", label)
	}
}

func TestIsDuplicateName(t *testing.T) {
	tests := []struct {
		name, street, hn string
		want             bool
	}{
		{"Oosthamsesteenweg 8", "Oosthamsesteenweg", "8", true},
		{"Oosthamsesteenweg", "Oosthamsesteenweg", "", true},
		{"Vitalita", "Oosthamsesteenweg", "8", false},
		{"", "Street", "1", true},
		{"Some Place", "Other Street", "5", false},
	}
	for _, tc := range tests {
		got := isDuplicateName(tc.name, tc.street, tc.hn)
		if got != tc.want {
			t.Errorf("isDuplicateName(%q, %q, %q) = %v, want %v", tc.name, tc.street, tc.hn, got, tc.want)
		}
	}
}
