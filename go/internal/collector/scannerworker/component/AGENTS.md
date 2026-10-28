# AGENTS.md - scannerworker/component guidance

Read these before editing this package:

1. `README.md`
2. `doc.go`
3. `go/internal/collector/scannerworker/AGENTS.md`
4. `go/internal/component/README.md`

## Invariants

- This package executes analyzers; it never owns claims. Do not add
  workflow writes, store handles, heartbeats, or claim loops here.
- Never interpolate claim values (scope IDs, image references, paths,
  digests, cursors aside) into errors, logs, metrics, or statuses. The
  cursor and component IDs are safe; everything else names the defect, not
  the data.
- Stdout carries exactly one JSON result document. Anything else breaks the
  host's strict decode.
- Mirror discipline: the protocol string, page envelope, and bound
  arithmetic mirror `sdk/go/collector` and the host until the
  eshu-sdk/collector pin carries pagination. Reconcile all three when the
  pin advances; the bound and cursor tests pin the values.
- Manifests must declare every kind the analyzer can emit, including
  degraded-path warnings, or the host rejects real output at admission.
- Keep target payloads privacy-safe per the parent `AGENTS.md`.

## Common changes

- A new analyzer needs its component ID, manifest kinds, target decoding,
  and construction in `Runner`, plus parity and admission tests proving
  host-admitted output identical to in-tree truth.
- Changing the request/response shape needs the host-side mirror updated in
  the same PR (or an explicit follow-up issue): the two sides decode each
  other strictly.
