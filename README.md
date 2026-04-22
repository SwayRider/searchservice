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

Search proceeds in up to four phases, stopping early as soon as results are found:

1. **Phase 1 — Core regions with boundary**: Query Pelias for regions whose core area intersects the viewport, restricted to the viewport bounding box.
2. **Phase 2 — Extended regions with boundary**: Query Pelias for extended-coverage regions not queried in Phase 1, also restricted to the viewport.
3. **Phase 3 — Core + extended without boundary**: Re-query all servers from Phases 1 & 2, without geographic restriction.
4. **Phase 4 — Remaining regions without boundary**: Query any configured Pelias servers not yet queried, without restriction.

The RegionService is called with the viewport expanded by 1× its width and height on each side to find all potentially relevant regions.

### Result Processing

After collecting results from a phase:

1. **Address collapsing**: Address-layer results are grouped by `(street, locality)`. Only the highest-confidence result per group is kept (ties broken by distance to the focus point).
2. **Ranking**: Results sorted by `confidence` descending, then by distance from the focus point ascending (equirectangular approximation).
3. **Truncation**: Result list capped at the requested `size` (default 5, max 20).

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
| `REGION_SERVICE_HOST` | `-region-service-host` | | RegionService host |
| `REGION_SERVICE_PORT` | `-region-service-port` | | RegionService gRPC port |
| `AUTH_SERVICE_HOST` | `-auth-service-host` | | AuthService host |
| `AUTH_SERVICE_PORT` | `-auth-service-port` | | AuthService gRPC port |

See `.env.example` for a complete configuration example.

## API Reference

The API is defined in `backend/protos/searchservice/v1/searchservice.proto`.

The `/Search` RPC requires a valid JWT (`Authorization: Bearer <token>`). The `/Ping` and `/health` endpoints are public.

---

### Search

Searches for locations matching a text query within or near a map viewport.

- **Endpoint:** `POST /api/v1/search`
- **Access:** JWT required (email verified)

```bash
curl --request POST \
  --url http://localhost:8080/api/v1/search \
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
| `confidence` | double | Pelias confidence score [0, 1] |
| `layer` | string | Pelias layer (`venue`, `address`, `street`, `locality`, …) |
| `lat` | double | Latitude |
| `lon` | double | Longitude |
| `street` | string | Street name (used for address collapsing) |

**Error codes:**

| gRPC code | Condition |
| --------- | --------- |
| `UNAVAILABLE` | RegionService unreachable |
| `UNAVAILABLE` | All configured Pelias servers unreachable |
| `OK` (empty results) | Search succeeded but no results found |

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

## Development

For local development with Docker Compose infrastructure:

1. Start base infrastructure: `cd infra/dev/layer-00 && docker-compose up -d`
2. Start SwayRider services: `cd infra/dev/layer-20 && docker-compose up -d`

Development ports:
- REST API: 34006
- gRPC: 34106
