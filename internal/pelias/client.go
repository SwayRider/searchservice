// Package pelias provides an HTTP client for the Pelias geocoding API.
package pelias

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	searchv1 "github.com/swayrider/protos/search/v1"
)

const (
	peliasLayers  = "venue,address,neighbourhood,localadmin,locality,county,macrocounty,region,macroregion,country"
	peliasSize    = 40
	clientTimeout = 10 * time.Second
)

// Client makes HTTP requests to a Pelias geocoding server.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// New creates a Client for the given base URL (e.g. "http://host:3100/v1").
func New(baseURL string) *Client {
	return &Client{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: clientTimeout},
	}
}

type geoJSONResponse struct {
	Features []peliasFeature `json:"features"`
}

type peliasFeature struct {
	Properties peliasProperties `json:"properties"`
	Geometry   peliasGeometry   `json:"geometry"`
}

type peliasProperties struct {
	ID          string  `json:"gid"`
	Name        string  `json:"name"`
	Label       string  `json:"label"`
	Street      string  `json:"street"`
	Housenumber string  `json:"housenumber"`
	Localadmin  string  `json:"localadmin"`
	Locality    string  `json:"locality"`
	Region      string  `json:"region"`
	Country     string  `json:"country"`
	CountryCode string  `json:"country_code"`
	Confidence  float64 `json:"confidence"`
	Layer       string  `json:"layer"`
}

type peliasGeometry struct {
	Coordinates []float64 `json:"coordinates"`
}

// Search queries Pelias for the given text.
// If hasFocus is true, focus.point params are sent so Pelias biases its scoring.
// If hasBoundary is true, boundary.rect params are sent to geographically filter results.
// Returns (nil, error) on network or HTTP error; caller should log and skip.
func (c *Client) Search(
	ctx context.Context,
	text string,
	language string,
	focusLat, focusLon float64,
	hasFocus bool,
	minLat, minLon, maxLat, maxLon float64,
	hasBoundary bool,
) ([]*searchv1.Result, error) {
	params := url.Values{}
	params.Set("text", text)
	params.Set("layers", peliasLayers)
	params.Set("size", strconv.Itoa(peliasSize))
	if hasFocus {
		params.Set("focus.point.lat", strconv.FormatFloat(focusLat, 'f', -1, 64))
		params.Set("focus.point.lon", strconv.FormatFloat(focusLon, 'f', -1, 64))
	}
	if hasBoundary {
		params.Set("boundary.rect.min_lat", strconv.FormatFloat(minLat, 'f', -1, 64))
		params.Set("boundary.rect.min_lon", strconv.FormatFloat(minLon, 'f', -1, 64))
		params.Set("boundary.rect.max_lat", strconv.FormatFloat(maxLat, 'f', -1, 64))
		params.Set("boundary.rect.max_lon", strconv.FormatFloat(maxLon, 'f', -1, 64))
	}

	endpoint := fmt.Sprintf("%s/search?%s", c.baseURL, strings.ReplaceAll(params.Encode(), "+", "%20"))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	if language != "" {
		req.Header.Set("Accept-Language", language)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("pelias returned status %d: %s", resp.StatusCode, body)
	}

	var geoResp geoJSONResponse
	if err := json.NewDecoder(resp.Body).Decode(&geoResp); err != nil {
		return nil, err
	}

	results := make([]*searchv1.Result, 0, len(geoResp.Features))
	for _, f := range geoResp.Features {
		if len(f.Geometry.Coordinates) < 2 {
			continue
		}
		if f.Properties.Label == "" {
			continue
		}

		label := f.Properties.Label
		if formatted := formatLabel(
			f.Properties.CountryCode,
			f.Properties.Name,
			f.Properties.Layer,
			f.Properties.Street,
			f.Properties.Housenumber,
			f.Properties.Localadmin,
			f.Properties.Locality,
			f.Properties.Region,
			f.Properties.Country,
		); formatted != "" {
			label = formatted
		}

		results = append(results, &searchv1.Result{
			Id:          f.Properties.ID,
			Name:        f.Properties.Name,
			Label:       label,
			Street:      f.Properties.Street,
			Housenumber: f.Properties.Housenumber,
			Localadmin:  f.Properties.Localadmin,
			Locality:    f.Properties.Locality,
			Region:      f.Properties.Region,
			Country:     f.Properties.Country,
			CountryCode: f.Properties.CountryCode,
			Confidence:  f.Properties.Confidence,
			Layer:       f.Properties.Layer,
			Lat:         f.Geometry.Coordinates[1], // GeoJSON: [lon, lat]
			Lon:         f.Geometry.Coordinates[0],
		})
	}
	return results, nil
}

// Response is an alias for the result slice, used by the Reverse method.
type Response = []*searchv1.Result

// Reverse queries Pelias for the given coordinates (reverse geocoding).
// Returns (nil, error) on network or HTTP error; caller should log and skip.
func (c *Client) Reverse(
	ctx context.Context,
	lat, lon float64,
	size int,
	language string,
) ([]*searchv1.Result, error) {
	params := url.Values{}
	params.Set("point.lat", strconv.FormatFloat(lat, 'f', -1, 64))
	params.Set("point.lon", strconv.FormatFloat(lon, 'f', -1, 64))
	if size > 0 {
		params.Set("size", strconv.Itoa(size))
	}
	if language != "" {
		params.Set("lang", language)
	}

	endpoint := fmt.Sprintf("%s/reverse?%s", c.baseURL, strings.ReplaceAll(params.Encode(), "+", "%20"))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	if language != "" {
		req.Header.Set("Accept-Language", language)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("pelias returned status %d: %s", resp.StatusCode, body)
	}

	var geoResp geoJSONResponse
	if err := json.NewDecoder(resp.Body).Decode(&geoResp); err != nil {
		return nil, err
	}

	results := make([]*searchv1.Result, 0, len(geoResp.Features))
	for _, f := range geoResp.Features {
		if len(f.Geometry.Coordinates) < 2 {
			continue
		}
		if f.Properties.Label == "" {
			continue
		}

		label := f.Properties.Label
		if formatted := formatLabel(
			f.Properties.CountryCode,
			f.Properties.Name,
			f.Properties.Layer,
			f.Properties.Street,
			f.Properties.Housenumber,
			f.Properties.Localadmin,
			f.Properties.Locality,
			f.Properties.Region,
			f.Properties.Country,
		); formatted != "" {
			label = formatted
		}

		results = append(results, &searchv1.Result{
			Id:          f.Properties.ID,
			Name:        f.Properties.Name,
			Label:       label,
			Street:      f.Properties.Street,
			Housenumber: f.Properties.Housenumber,
			Localadmin:  f.Properties.Localadmin,
			Locality:    f.Properties.Locality,
			Region:      f.Properties.Region,
			Country:     f.Properties.Country,
			CountryCode: f.Properties.CountryCode,
			Confidence:  f.Properties.Confidence,
			Layer:       f.Properties.Layer,
			Lat:         f.Geometry.Coordinates[1],
			Lon:         f.Geometry.Coordinates[0],
		})
	}
	return results, nil
}
