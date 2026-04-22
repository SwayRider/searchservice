package pelias

import "strings"

// labelFormatFunc builds a label for a given country. Returns "" to fall back to Pelias label.
type labelFormatFunc func(name, street, housenumber, localadmin, locality, region, country string) string

var labelFormatters = map[string]labelFormatFunc{
	"BE": beLabel,
}

// isDuplicateName returns true when the name is just the address repeated by Pelias.
func isDuplicateName(name, street, housenumber string) bool {
	if name == "" {
		return true
	}
	n := strings.ToLower(name)
	// "Oosthamsesteenweg 8" with street "Oosthamsesteenweg", hn "8"
	if street != "" && housenumber != "" && n == strings.ToLower(street+" "+housenumber) {
		return true
	}
	// "Oosthamsesteenweg" with street "Oosthamsesteenweg", no hn
	if street != "" && housenumber == "" && n == strings.ToLower(street) {
		return true
	}
	return false
}

func beLabel(name, street, housenumber, localadmin, locality, region, country string) string {
	// Prefix: company name (if meaningful) or street+number
	prefix := street
	if housenumber != "" {
		prefix = prefix + " " + housenumber
	}
	if !isDuplicateName(name, street, housenumber) {
		prefix = name + " " + prefix
	}
	// Suffix: locality, country
	suffix := localadmin
	if suffix == "" {
		suffix = locality
	}
	if country != "" {
		if suffix != "" {
			suffix += ", "
		}
		suffix += country
	}
	if prefix == "" && suffix == "" {
		return ""
	}
	if suffix == "" {
		return prefix
	}
	if prefix == "" {
		return suffix
	}
	return prefix + ", " + suffix
}

// formatLabel resolves a country-specific label.
// Returns empty string if no formatter exists (caller should keep Pelias label).
func formatLabel(countryCode, name, layer, street, housenumber, localadmin, locality, region, country string) string {
	fn, ok := labelFormatters[countryCode]
	if !ok {
		return ""
	}
	return fn(name, street, housenumber, localadmin, locality, region, country)
}
