# apilib — Slim Proof-Read Extraction (pre-1.0)

Versioned Go module (`github.com/eshu-hq/eshu/go/apilib`, tags
`go/apilib/v0.x.y`) carrying the proof-read surface for the eshu-api
satellite (Refs eshu-hq/eshu-api#456): backend-gated `/readyz` plus the
admin proof reads (tuning-report, work-items/query).

Layout:

- `doc.go`, `readyz.go` — module doc and the readiness mount
  (`MountReadiness`): core-shape `{service, probe: readyz, status,
  error?}` body, two `func(ctx) error` probes (postgres, graph) each
  under its own 2s timeout run concurrently, 200/503 permanent, never
  `Retry-After`, public. Probe errors are served verbatim, so probes
  MUST return only pre-sanitized errors.
- `admin/` — the proof-read family: the 7-method `ReadStore` port with
  row/filter types (`types.go`), the static tuning handler
  (`tuning.go`), the work-items query handler (`workitems.go`), and the
  per-family mounts (`mount.go`).

Audience is internal-only: the eshu-api satellite and core-owned
promotion tooling. The core parent consumes this module via `require` +
a committed `replace => ./apilib`; the external proof consumer pins the
tag with no replace. Do not re-export to external consumers, and do not
widen beyond the proof reads without a new extraction decision (v0: no
compatibility promise, breaking changes land without a window).
