// Package config provides configuration parsing utilities for the search service.
package config

import (
	"fmt"
	"strings"
)

// ParsePeliasRegions parses "region1=url1,region2=url2" into map[string]string.
// Splits on comma, then splits each token on the first '=' only.
func ParsePeliasRegions(val string) (map[string]string, error) {
	result := make(map[string]string)
	if val == "" {
		return result, nil
	}
	for _, token := range strings.Split(val, ",") {
		idx := strings.IndexByte(token, '=')
		if idx < 0 {
			return nil, fmt.Errorf("invalid PELIAS_REGIONS token %q: missing '='", token)
		}
		region := token[:idx]
		url := token[idx+1:]
		if region == "" {
			return nil, fmt.Errorf("invalid PELIAS_REGIONS token %q: empty region name", token)
		}
		result[region] = url
	}
	return result, nil
}
