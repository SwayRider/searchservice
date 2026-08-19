package pelias

import "strings"

// labelFormatFunc builds a label for a given country. Returns "" to fall back to Pelias label.
type labelFormatFunc func(name, street, housenumber, localadmin, locality, country string) string

// labelFormatters maps an ISO 3166-1 alpha-2 country code to a label formatter.
//
// The generic formatter below covers the dev-mini regions (benelux, france,
// germany). Countries whose addresses put the house number after the street
// name use streetNumberLabel; countries that put it before use
// numberStreetLabel. When expanding beyond dev-mini, add an entry here for any
// new country whose Pelias labels need correcting.
var labelFormatters = map[string]labelFormatFunc{
	"BE": streetNumberLabel, // "Oosthamsesteenweg 8, Balen, België"
	"NL": streetNumberLabel, // "Dorpsstraat 8, Amsterdam, Nederland"
	"DE": streetNumberLabel, // "Musterstraße 8, Berlin, Deutschland"
	"LU": numberStreetLabel, // "10 Rue de la Poste, Luxembourg, Luxembourg"
	"FR": numberStreetLabel, // "8 Rue de la Paix, Paris, France"
}

// streetNumberLabel formats "street + housenumber" addresses (BE, NL, DE, …).
func streetNumberLabel(name, street, housenumber, localadmin, locality, country string) string {
	return genericLabel(name, street, housenumber, localadmin, locality, country, false)
}

// numberStreetLabel formats "housenumber + street" addresses (FR, LU, …).
func numberStreetLabel(name, street, housenumber, localadmin, locality, country string) string {
	return genericLabel(name, street, housenumber, localadmin, locality, country, true)
}

// genericLabel builds "[name] street housenumber, locality, country", or with
// the housenumber before the street when numberFirst is true.
func genericLabel(name, street, housenumber, localadmin, locality, country string, numberFirst bool) string {
	// Prefix: company name (if meaningful) plus street and housenumber.
	var prefix string
	if numberFirst {
		prefix = join(" ", housenumber, street)
	} else {
		prefix = join(" ", street, housenumber)
	}
	if !isDuplicateName(name, street, housenumber) {
		prefix = join(" ", name, prefix)
	}

	// Suffix: locality, country.
	suffix := join(", ", firstNonEmpty(localadmin, locality), country)

	return join(", ", prefix, suffix)
}

// isDuplicateName returns true when the name is just the address repeated by Pelias.
func isDuplicateName(name, street, housenumber string) bool {
	if name == "" {
		return true
	}
	n := strings.ToLower(name)
	// "Oosthamsesteenweg 8" with street "Oosthamsesteenweg", hn "8" — and the
	// number-first form "8 Oosthamsesteenweg" used in FR/LU.
	if street != "" && housenumber != "" {
		if n == strings.ToLower(street+" "+housenumber) || n == strings.ToLower(housenumber+" "+street) {
			return true
		}
	}
	// "Oosthamsesteenweg" with street "Oosthamsesteenweg", no hn.
	if street != "" && housenumber == "" && n == strings.ToLower(street) {
		return true
	}
	return false
}

// join concatenates the non-empty parts with sep.
func join(sep string, parts ...string) string {
	out := ""
	for _, p := range parts {
		if p == "" {
			continue
		}
		if out != "" {
			out += sep
		}
		out += p
	}
	return out
}

// firstNonEmpty returns the first non-empty string, or "".
func firstNonEmpty(parts ...string) string {
	for _, p := range parts {
		if p != "" {
			return p
		}
	}
	return ""
}

// formatLabel resolves a country-specific label.
// Returns empty string if no formatter exists (caller should keep Pelias label).
func formatLabel(countryCode, name, street, housenumber, localadmin, locality, country string) string {
	fn, ok := labelFormatters[countryCode]
	if !ok {
		return ""
	}
	return fn(name, street, housenumber, localadmin, locality, country)
}
