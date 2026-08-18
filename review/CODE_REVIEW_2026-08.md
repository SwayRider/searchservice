# Code Review — `searchservice`

**Date:** 2026-08
**Scope:** Full review of `searchservice/` — the geocoding search service for the SwayRider platform (Pelias fan-out across regions, confidence-based ranking, address collapsing, localadmin retry).
**Reviewed:** `cmd/searchservice/main.go`, `internal/config/*`, `internal/pelias/*` (client, labels), `internal/search/*` (flow, ranking), `internal/server/*` (server, search, reverse, autocomplete, ping), all test files, `protos/search/v1/search.proto`, `protos/health/v1/health.proto`, `protos/common_types/geo/geo.proto`, `Dockerfile`, `Makefile`, `env.example`, `local.env`, `.github/workflows/ci.yml`, and the shared `swlib` app/security/logger machinery.
**Verification performed:** `go build ./...`, `go vet ./...`, `go test ./... -count=1 -race`, and per-package coverage all run clean. Two suspected bugs were confirmed empirically with throwaway reproduction tests (removed afterwards; no source changes were made — the only uncommitted modifications in the repo, `go.mod`/`go.sum`/`.gitignore`, pre-date this review).

---

## Summary

This is the healthiest of the three services reviewed so far. It has solid test coverage (config 92.3%, search 73.0%, server 74.1%, pelias 69.5%), is race-clean, and — like `routerservice` but unlike `regionservice` — the auth interceptor is **correctly enabled** (`app.AuthInterceptor | app.ClientInfoInterceptor` with a JWT key-fetch loop), so the declared `search:execute` / user-JWT endpoint profiles are actually enforced. The Pelias HTTP client has a proper 10s timeout (a contrast to `routerservice`), the `PeliasSearcher`/`regionSearcher` interface abstraction makes the flow genuinely testable, and the code degrades gracefully: per-region Pelias errors are logged and skipped, partial results are returned on cancellation, and the localadmin retry is a thoughtful UX feature.

There is one dominant problem and a cluster of smaller correctness issues:

1. **A malformed-but-valid request crashes the process.** `Search` dereferences `viewport.bottom_left`/`top_right` without nil checks (they are pointer fields in the generated proto). `{"viewport": {}}` — a perfectly valid proto3 message — panics with a nil-pointer dereference (verified empirically). There is no recover interceptor, so any authenticated user can kill the whole service with a single request.
2. The rest are correctness/robustness gaps: `ReverseGeocode` doesn't clamp `size` (unbounded Pelias responses), the viewport is never validated (inverted boxes, negative extents, unclamped longitude past ±180°), result ordering is nondeterministic on ties, the `minConfidence` cutoff is dead code, and the `Autocomplete` endpoint is entirely untested and undocumented.

---

## High

### 1. `Search` panics on a viewport missing its corner coordinates — remote crash ✅ DONE
`internal/search/flow.go:91-110`

`Search` validates only that `req.Viewport != nil`, then immediately dereferences the corner messages:

```go
vp := req.Viewport
if vp == nil {
    return nil, status.Error(codes.InvalidArgument, "viewport is required")
}
width := vp.TopRight.Lon - vp.BottomLeft.Lon      // flow.go:91 — nil deref if corners unset
height := vp.TopRight.Lat - vp.BottomLeft.Lat
```

`BoundingBox` is defined in `protos/common_types/geo/geo.proto` as message fields (`Coordinate bottom_left = 1; Coordinate top_right = 2;`), so in the generated Go code they are `*Coordinate` pointers. A request such as `{"text":"x","viewport":{}}` (or a viewport with only one corner set) is valid proto3 and reaches the handler with `vp.BottomLeft == nil` / `vp.TopRight == nil`. **Confirmed empirically:**

```
go test ... TestSearch_nilViewportCorners
panic: runtime error: invalid memory address or nil pointer dereference
    search.(*SearchFlow).Search(...)  flow.go:91
```

There is no `recover` interceptor, so the **entire process dies**. Because auth is enforced, the trigger is an authenticated user (any app user) rather than an anonymous caller, but it is still a trivially-crafted remote crash of a shared service. The same class of nil-deref risk exists in `Autocomplete`/`ReverseGeocode` for their `focus_point`/`point` — those two *are* nil-checked (`flow.go:312`, `flow.go:404`), which shows the intended pattern; `Search` just misses it for the viewport corners.

Fix direction: validate `vp.BottomLeft != nil && vp.TopRight != nil` (reject with `InvalidArgument`, or default missing corners), and add a regression test.

---

## Medium

### 2. `ReverseGeocode` does not clamp `size` — unbounded Pelias response ✅ DONE
`internal/search/flow.go:399-468`

```go
size := 10
if req.Size != nil {
    size = int(*req.Size)
}
...
results, err := clnt.Reverse(ctx, lat, lon, size, language)
```

`Search` and `Autocomplete` funnel through `Rank`, which clamps `size` to `[defaultSize, maxSize=20]`. `ReverseGeocode` passes the raw request `size` straight to Pelias (`internal/pelias/client.go` `Reverse`: `if size > 0 { params.Set("size", ...) }`). A request with `"size": 100000` asks Pelias for up to 100,000 results, which are then decoded into memory and returned — an easy amplification vector for any authenticated user. Fix direction: clamp `size` to `maxSize` (or validate it) in `ReverseGeocode`, matching the other two endpoints.

### 3. No validation of the viewport / coordinates ✅ DONE
`internal/search/flow.go:82-110`

`Search` never validates the viewport geometry before deriving the expanded box that is sent to both regionservice and every Pelias instance as `boundary.rect`:
- **Inverted viewport** (`top_right.lat < bottom_left.lat` or `top_right.lon < bottom_left.lon`) yields a negative `width`/`height`, and the expanded box collapses or inverts — regionservice and Pelias then get garbage bounds and return wrong/empty results with no error.
- **Longitude is never clamped or wrapped.** `clampLat` clamps latitude only; after expansion, `bottomLeft.Lon - width` can go below −180 (or above +180), producing an invalid `boundary.rect` (the antimeridian case — e.g. a viewport at lon 179/−179 — yields a ~358°-wide box, admitting nearly everything).
- No `NaN`/`Inf` or out-of-range checks on any coordinate.

Fix direction: validate corner ordering and coordinate ranges (reject or normalize), clamp/wrap longitude consistently, and add tests for inverted and dateline-crossing viewports.

### 4. Non-deterministic result ordering on ties ✅ DONE
`internal/search/ranking.go:278`, `internal/search/flow.go:201,293`, `internal/search/ranking.go:207,255`

`Rank` uses `sort.Slice`, which is **not stable**, and the inputs are assembled from map iterations (`for region, clnt := range f.peliasClients` in Phase 3 and the localadmin retry; `for _, r := range addressMap` / `byID` in `CollapseAddresses`/`DeduplicateByID`). For results with equal composite scores — common in geocoding, where many candidates share confidence 1.0 and similar distance — the returned order depends on Go's random map iteration and is therefore **different between requests for the same query**. For a geocoding API the top result should be stable and reproducible. Fix direction: use `sort.SliceStable` and add a deterministic final tiebreak (e.g. by label or id), or sort the map keys.

### 5. `Autocomplete` is entirely untested and undocumented ✅ DONE
`internal/search/flow.go:307`, `internal/server/autocomplete.go`, `routerservice`… `searchservice/README.md`

- **0% test coverage**: no flow test, no handler test, and no Pelias-client test exercises `Autocomplete` (the `fakePeliasSearcher.Autocomplete` and `queryBasedSearcher.Autocomplete` stubs are never driven).
- **Undocumented**: the README's authorization table lists only `Search` and `ReverseGeocode`, and the API reference omits `Autocomplete` entirely — yet `server.go`'s `init()` registers it as `UserOrServiceEndpoint(..., "search:execute")` and it is routed at `POST /api/v1/search/autocomplete`. The endpoint exists, is auth-protected, but is invisible in the docs and has zero test coverage.

Fix direction: add flow/handler/client tests for `Autocomplete` and update the README (auth table + API reference).

### 6. The `minConfidence` cutoff is dead code ✅ DONE
`internal/search/ranking.go:22,302`, `routerservice/README.md`

```go
minConfidence = 0.0 // drop results below this confidence
...
if r.Confidence >= minConfidence {   // always true — confidence is clamped to >= 0
```

`Rank` clamps every computed score to `[0, 1]` before this filter, so with `minConfidence = 0.0` the filter never drops anything (the test `TestRank_cutoffDisabled` explicitly documents this). The README's "Result Processing" section claims low-confidence results are dropped, which is not the case. Either set a meaningful cutoff (e.g. 0.3) or remove the dead filter and the misleading README claim.

### 7. `ParsePeliasRegions` does not trim whitespace ✅ DONE
`internal/config/config.go:16-27`

```go
for _, token := range strings.Split(val, ",") {
    idx := strings.IndexByte(token, '=')
    region := token[:idx]
    ...
    result[region] = url
}
```

A config like `"region1=http://x, region2=http://y"` (space after the comma) produces region names with a leading space (`" region2"`), which never match regionservice's region names — the region is silently never queried. Fix direction: `strings.TrimSpace` on the token/region (and validate the URL).

### 8. `fuzzyStreetPenalty` uses byte-as-rune checks and byte-length normalization ✅ DONE
`internal/search/ranking.go:146-177`

```go
if unicode.IsLetter(rune(t[0])) && len(t) > len(longest) {   // t[0] is a UTF-8 byte, not a rune
...
maxLen := max(len(longest), len(streetLower))                // byte lengths, not rune lengths
```

`rune(t[0])` treats the first *byte* of a token as a code point. For most Western-European accented characters this happens to work only because UTF-8 leading bytes (0xC2–0xF4) fall in the Latin-1 letter ranges (U+00C0–U+00F4); the correct check is `unicode.IsLetter([]rune(t)[0])`. The similarity normalization also uses byte lengths, which skews the ratio for multi-byte text. This is fragile rather than actively wrong for the current (Latin-script) data, but it should be corrected to rune-based logic — and non-ASCII street matching deserves a test.

---

## Low

### 9. Triplicated Pelias feature-parsing code ✅ DONE
`internal/pelias/client.go` — `Search` (lines ~70-150), `Autocomplete` (~155-235), and `Reverse` (~240-320) each contain an identical ~40-line block (build query params → GET → decode `geoJSONResponse` → loop features, skip invalid coords/empty labels, call `formatLabel`, map to `searchv1.Result`). Extract a shared `parseFeatures`/`doRequest` helper to remove ~120 duplicated lines and the risk of the three copies drifting.

### 10. `bootstrapFn` logs fatal and has a dead return; config parsed twice ✅ DONE
`cmd/searchservice/main.go:120-129,219-224`

```go
_, err := config.ParsePeliasRegions(peliasRegions)
if err != nil {
    lg.Fatalf("invalid PELIAS_REGIONS: %v", err)   // os.Exit — skips graceful shutdown
}
return nil                                          // dead code
```

`grpcSearchRegistrar` then parses the same `PELIAS_REGIONS` string a second time. Return the error and let the caller decide, and parse once (e.g. store the parsed map as app data).

### 11. No validation of empty `text` ✅ DONE
`internal/search/flow.go:82`, `flow.go:307`

`Search` and `Autocomplete` send an empty `text` to Pelias if the client omits it, rather than rejecting with `InvalidArgument`. Minor, but a cheap validation.

### 12. `labelFormatters` only covers `BE`; `region` is a dead parameter ✅ DONE
`internal/pelias/labels.go`

Only Belgium has a custom label formatter — everything else falls back to the raw Pelias label (fine, but worth documenting). `beLabel`'s signature accepts `region` but never uses it.

### 13. Dockerfile hardening ✅ DONE
`Dockerfile`

- `FROM golang:latest` and `FROM debian:bookworm-slim` — unpinned, mutable base tags; builds are not reproducible.
- `COPY . .` with **no `.dockerignore`** — the build context ships `.git/`, `local.env` (machine-specific paths), `.DS_Store`. Add a `.dockerignore`.
- `CGO_ENABLED=1` with cross-gcc toolchains for both arches — verified unnecessary: `CGO_ENABLED=0 go build ./cmd/searchservice/` succeeds, so the whole cross-compiler block can be dropped for a static binary.
- No `HEALTHCHECK` (the service exposes a `Ping` RPC that could drive one).

### 14. `HealthService.Check` is unimplemented ✅ DONE
`internal/server/server.go:64-83`, `internal/server/ping.go`, `protos/health/v1/health.proto`

The proto defines a `Check` RPC (`GET /api/v1/health`), but `HealthServer` embeds `UnimplementedHealthServiceServer` and implements only `Ping` — `Check` returns `codes.Unimplemented`. Implement a trivial always-UP `Check` or remove it from the proto.

### 15. README boundary description is inaccurate ✅ DONE
`routerservice/README.md` (Search Flow)

The README says results are "restricted to the viewport bounding box", but the code sends the **expanded** box (`extBox`, 1× width/height on each side) as `boundary.rect` to Pelias in every phase. Results can therefore come from up to ~3× the viewport area. Either send the original viewport as the boundary (with the expanded box only for the regionservice query) or correct the README.

---

## Positive observations

- **Auth is correctly enforced** — `main.go` passes `app.AuthInterceptor | app.ClientInfoInterceptor` with a real `JWTPublicKeysFn` (background key-fetch loop), and `server.go`'s `init()` declares `PublicEndpoint(Ping)` / `UserOrServiceEndpoint(Search, ReverseGeocode, Autocomplete, ["search:execute"])`. This is the right pattern.
- **Strong test culture** — config 92.3%, search 73.0%, server 74.1%, pelias 69.5%, all race-clean. The `PeliasSearcher`/`regionSearcher` interfaces make the flow genuinely mockable, and the ranking tests (confidence, distance tiebreak, housenumber match, fuzzy street penalty, dedup) are meaningful.
- **Proper timeouts** — the Pelias HTTP client sets a 10s timeout (contrast: `routerservice` has none).
- **Graceful degradation** — per-region Pelias failures are logged and skipped; results from healthy regions are still returned; the `ctx.Err()` checks between phases return partial results or the correct `DeadlineExceeded`/`Canceled` code instead of hanging.
- **Thoughtful ranking** — the documented composite score (text match + housenumber bonus − distance decay − street mismatch) is a sensible, testable design, and the localadmin retry is a genuinely useful feature for low-coverage address areas.
- **Clean config parsing** — `ParsePeliasRegions` correctly splits on the *first* `=` (handling URLs with query params) and is well tested.
- **Build/vet/tests clean** — `go build`, `go vet`, and the full suite (including `-race`) all pass.

---

## Test-coverage gaps

Measured with `go test -cover`:

| Package | Coverage | Notes |
| ------- | -------- | ----- |
| `internal/config` | 92.3% | Good. |
| `internal/search` | 73.0% | `Search` 73.8%, `ReverseGeocode` 93.5%, ranking near-complete — but **`Autocomplete` is 0%**. |
| `internal/server` | 74.1% | `Search`/`ReverseGeocode` handlers covered; **`Autocomplete` handler untested**. |
| `internal/pelias` | 69.5% | `Search`/`Reverse` client covered; **`Autocomplete` client untested**. |
| `cmd/searchservice` | 0% | No startup/bootstrap/auth test. |

Specific gaps:

- **`Autocomplete` is 0% covered** across flow, handler, and client (finding #5) — the only endpoint with zero tests.
- **No malformed-input tests** — no test for a viewport with missing corners (#1, the crash), inverted viewports, out-of-range/`NaN` coordinates, or negative/oversized `size` (#2, #3).
- **No determinism test** — nothing asserts that repeated `Rank` calls (or repeated `Search` calls) return results in a stable order (#4).
- **No non-ASCII ranking test** — the fuzzy-street and tokenization paths are only exercised with ASCII input (#8).
- **No `Autocomplete` README coverage** — the endpoint is undocumented (#5).
- **`incomingToken` and `clampLat` are only partially covered** (50% / 60%) — the token-forwarding and clamp edge cases (lat ±90) aren't pinned.

---

## Recommended fix order

1. **#1 (high)** — nil-guard `viewport.bottom_left`/`top_right` in `Search`; add a regression test. This is the process-crash bug.
2. **#2 (medium)** — clamp `size` in `ReverseGeocode` to `maxSize` (matching `Search`/`Autocomplete`).
3. **#3 (medium)** — validate viewport ordering and coordinate ranges, and clamp/wrap longitude consistently (antimeridian).
4. **#4 (medium)** — make result ordering deterministic (`sort.SliceStable` + a stable tiebreak).
5. **#5 (medium)** — add `Autocomplete` tests (flow, handler, client) and document it in the README.
6. **#6–#8 (medium)** — either enable or remove the `minConfidence` cutoff and fix the README, trim whitespace in `ParsePeliasRegions`, and correct the byte-as-rune logic in `fuzzyStreetPenalty`.
7. **#9–#15 (low)** — deduplicate the Pelias client, fix `bootstrapFn`/double-parse, add `text` validation, add `.dockerignore`, pin base images, drop needless CGO, and implement or remove `HealthService.Check`.

Item #1 is the priority: it is a trivially-reachable process crash from any authenticated user, and it is the one bug in this codebase that can take down the whole service.
