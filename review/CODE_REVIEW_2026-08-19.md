# Code Review — 2026-08-19

Follow-up security audit of the full current codebase (not diff-based), cross-checked against [`review/CODE_REVIEW_2026-08.md`](CODE_REVIEW_2026-08.md). See [`Docs/REVIEW.md`](../../Docs/REVIEW.md) for how findings in this file are tracked.

**Verification of prior review:** all 15 previously-tracked findings are marked done and were verified against current code with no regressions — `Search` nil-guards viewport corners (`flow.go:90-96`), `ReverseGeocode` clamps `size` to `maxSize=20` (`flow.go:521-530`), viewport/coordinate validation and antimeridian wrapping are present (`flow.go:97-122`), `Rank` uses `sort.SliceStable` with a deterministic tiebreak (`ranking.go:282-300`), the Dockerfile is pinned/hardened with a `.dockerignore`, and `HealthService.Check` is implemented.

Confirmed non-issues: no SSRF via user input (Pelias base URLs come solely from the operator-configured `PELIAS_REGIONS` env var; user input is only ever a properly-encoded query parameter), no Elasticsearch query injection (user input never reaches a raw ES query body), authorization is correctly gated (`Search`/`ReverseGeocode`/`Autocomplete` all require the `search:execute` scope via `UserOrServiceEndpoint`; only health RPCs are public), and secrets handling is clean (`env.example`/`local.env` contain no credentials, `local.env` is gitignored).

### 1. Unbounded `text` field length on `Search` and `Autocomplete`

Neither `internal/search/flow.go:172` (`Search`) nor `flow.go:376` (`Autocomplete`) caps the length of `req.Text` before forwarding it as a Pelias/Elasticsearch query string. An authenticated client sending a very large `text` (bounded only by the gRPC/HTTP max message size) causes an expensive query fanned out to every configured Pelias instance simultaneously (up to 3 phases × N regions, plus localadmin retries). Not exploitable pre-auth and bounded by transport message-size limits, but a cheap amplification vector worth a modest length cap (e.g. 200 chars). Severity: Low.

### 2. Pelias non-2xx response body is embedded in the internal Go error

`internal/pelias/client.go:191-211`'s `doRequest` builds `fmt.Errorf("pelias returned status %d: %s", ..., body)`, which could contain internal Elasticsearch stack traces or index details. Not a live leak today — every caller in `flow.go` only logs this error and returns a generic `status.Error` code to the gRPC client — but the raw body is one refactor away from being surfaced if a future change threads the error message through to a client response. Severity: Info.
