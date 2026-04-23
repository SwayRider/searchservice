# searchservice

Geocoding search service for the SwayRider platform. Acts as the single search endpoint for the mobile app — handles JWT authentication, fans out to Pelias geocoding servers across multiple geographic regions, applies confidence-based ranking, and collapses duplicate address results.

## Architecture

The searchservice exposes two server interfaces:

| Interface | Port | Purpose |
| --------- | ---- | ------- |
| REST/HTTP | 8080 | HTTP API via gRPC-gateway |
| gRPC | 8081 | Internal service-to-service communication |

### Dependencies

| Service | Purpose |
| ------- | ------- |
| **authservice** | Fetches JWT public keys for token validation |
| **regionservice** | Resolves which Pelias servers to query for a given map viewport |
| **Pelias** (1..N instances) | Geocoding backends, one per geographic region |

### Search Flow

Search proceeds in 3 phases, with an optional localadmin retry phase if no address results are found:

1. **Phase 1 — Core regions with boundary**: Query Pelias for regions whose core area intersects the viewport, restricted to the viewport bounding box.
2. **Phase 2 — Extended regions with boundary**: Query Pelias for extended-coverage regions not queried in Phase 1, also restricted to the viewport.
3. **Phase 3 — Remaining configured regions with boundary**: Query any other configured Pelias servers not returned by RegionService, still with boundary restriction.
4. **Localadmin retry**: If no address-layer results were found, retry with alternative queries using `localadmin` names extracted from locality results.

The RegionService is called with the viewport expanded by 1× its width and height on each side to find all potentially relevant regions.

### Result Processing

After collecting results from a phase:

1. **Address collapsing**: Address-layer results are grouped by `(street, locality)`. Keep the highest-confidence result (ties broken by shortest housenumber, then nearest to focus point).
2. **Deduplication by ID**: Results with the same Pelias ID are deduplicated, keeping highest confidence (tie: nearest to focus).
3. **Ranking**: Results sorted by composite score descending, then by distance from focus point ascending.
4. **Truncation**: Result list capped at the requested `size` (default 5, max 20).

**Ranking score formula:**
```
score = confidence + textMatchBonus + housenumberBonus - distancePenalty - streetMismatchPenalty
```

- **textMatchBonus**: +0.2 for query tokens appearing in result label
- **housenumberBonus**: +0.5 for exact house number match
- **distancePenalty**: Exponential decay, max 0.5 penalty (~500km half-life)
- **streetMismatchPenalty**: Levenshtein similarity penalty, max 1.0

The confidence field is overwritten with the computed ranking score clamped to [0, 1].

## Configuration

Configuration is provided via environment variables or CLI flags.

### Server Configuration

| Environment Variable | CLI Flag | Default | Description |
| -------------------- | -------- | ------- | ----------- |
| `HTTP_PORT` | `-http-port` | 8080 | REST API port |
| `GRPC_PORT` | `-grpc-port` | 8081 | gRPC port |

### Pelias Configuration

| Environment Variable | CLI Flag | Default | Description |
| -------------------- | -------- | ------- | ----------- |
| `PELIAS_REGIONS` | `-pelias-regions` | | Comma-separated `region=base-url` pairs (see below) |

**Format:** `region1=http://host:port/v1,region2=http://host:port/v1`

The region names must match the values returned by RegionService (e.g. `iberian-peninsula`, `west-europe`). Unknown region names returned by RegionService are silently skipped.

### Service Dependencies

| Environment Variable | CLI Flag | Default | Description |
| -------------------- | -------- | ------- | ----------- |
| `REGIONSERVICE_HOST` | `-regionservice-host` | | RegionService host |
| `REGIONSERVICE_PORT` | `-regionservice-port` | | RegionService gRPC port |
| `AUTHSERVICE_HOST` | `-authservice-host` | | AuthService host |
| `AUTHSERVICE_PORT` | `-authservice-port` | | AuthService gRPC port |

See `.env.example` for a complete configuration example.

## API Reference

The API is defined in `protos/search/v1/search.proto`.

The `/Search` and `/ReverseGeocode` RPCs require a valid JWT (`Authorization: Bearer <token>`). The `/Ping` and `/health` endpoints are public.

---

### Search

Searches for locations matching a text query within or near a map viewport.

- **Endpoint:** `POST /api/v1/search/search`
- **Access:** JWT required (email verified)

```bash
curl --request POST \
  --url http://localhost:8080/api/v1/search/search \
  --header 'Authorization: Bearer <token>' \
  --header 'Content-Type: application/json' \
  --data '{
    "text": "plaza sandoval",
    "viewport": {
      "bottomLeft": { "lat": 37.8, "lon": -1.2 },
      "topRight":   { "lat": 38.2, "lon": -0.8 }
    },
    "focusPoint": { "lat": 37.984, "lon": -1.128 },
    "language": "es",
    "size": 5
  }'
```

**Request fields:**

| Field | Type | Required | Description |
| ----- | ---- | -------- | ----------- |
| `text` | string | yes | Search query |
| `viewport` | BoundingBox | yes | Current map viewport |
| `focusPoint` | Coordinate | no | Reference point for distance ranking; defaults to viewport center |
| `language` | string | no | BCP-47 language tag forwarded to Pelias (e.g. `nl-BE`, `es`) |
| `size` | int32 | no | Max results to return (default: 5, max: 20) |

Response:
```json
{
  "results": [
    {
      "label": "Plaza Sandoval, Murcia",
      "locality": "Murcia",
      "region": "Región de Murcia",
      "country": "Spain",
      "confidence": 0.921,
      "layer": "venue",
      "lat": 37.984,
      "lon": -1.128,
      "street": "Plaza Sandoval"
    }
  ]
}
```

**Result fields:**

| Field | Type | Description |
| ----- | ---- | ----------- |
| `label` | string | Full place label |
| `locality` | string | City/town name |
| `region` | string | Province/state |
| `country` | string | Country name |
| `confidence` | double | Computed ranking score [0, 1] |
| `layer` | string | Pelias layer (`venue`, `address`, `street`, `locality`, …) |
| `lat` | double | Latitude |
| `lon` | double | Longitude |
| `street` | string | Street name (used for address collapsing) |
| `housenumber` | string | House number |
| `id` | string | Pelias result ID (for deduplication) |
| `localadmin` | string | Local admin area (for localadmin retry) |
| `country_code` | string | ISO country code |
| `name` | string | Name (for venue results) |

**Error codes:**

| gRPC code | Condition |
| --------- | --------- |
| `UNAVAILABLE` | RegionService unreachable |
| `UNAVAILABLE` | All configured Pelias servers unreachable |
| `OK` (empty results) | Search succeeded but no results found |

---

### Reverse Geocode

Reverse geocodes a coordinate to address results.

- **Endpoint:** `POST /api/v1/search/reverse`
- **Access:** JWT required (email verified)

```bash
curl --request POST \
  --url http://localhost:8080/api/v1/search/reverse \
  --header 'Authorization: Bearer <token>' \
  --header 'Content-Type: application/json' \
  --data '{
    "point": { "lat": 37.984, "lon": -1.128 },
    "size": 10,
    "language": "es"
  }'
```

**Request fields:**

| Field | Type | Required | Description |
| ----- | ---- | -------- | ----------- |
| `point` | Coordinate | yes | Latitude/longitude to reverse geocode |
| `size` | int32 | no | Max results (default: 10) |
| `language` | string | no | BCP-47 language tag |

Response: Same format as Search response.

**Error codes:**

| gRPC code | Condition |
| --------- | --------- |
| `INVALID_ARGUMENT` | point is required |
| `UNAVAILABLE` | RegionService unreachable |
| `NOT_FOUND` | No region found for coordinate |
| `NOT_FOUND` | No Pelias server configured for region |

---

### Ping

Simple health check.

- **Endpoint:** `GET /api/v1/search/ping` (gRPC: `SearchService/Ping`)
- **Access:** Public

---

### Health

- **Endpoint:** `GET /v1/health/ping`
- **Access:** Public

## Building

```bash
# Generate protobuf code (run from repo root)
make proto

# Build the service
cd backend
go build ./services/searchservice/cmd/searchservice

# Run tests
go test ./services/searchservice/...

# Run the service
go run ./services/searchservice/cmd/searchservice
```

## Docker

```bash
# Build container (from repo root)
make services-searchservice-container
```