# AGENTS.md — internal/telemetry/contract/observability guidance for LLM assistants

## Read first

1. `go/internal/telemetry/contract/observability/README.md`
2. `go/internal/telemetry/contract/AGENTS.md` — sibling package rules; the
   same declarations-only / no-registration / leaf-import invariants apply
   here
3. `go/internal/telemetry/AGENTS.md` — root package contract rules

## Invariants this package enforces

- **Declarations only** — one file per hosted-observability source, each
  declaring only that source's `Observe`/`Fetch` span constants. No
  `func init()`, no registration.
- **Leaf contract** — no `go/internal/*` imports, including root package
  `telemetry`.

## How to add a new hosted-observability source

1. Add a new file here following the existing `grafana.go`/`loki.go`
   pattern: `SpanXxxObserve`/`SpanXxxFetch` constants.
2. Add the spans to `registerVulnerabilityIntelligence` in root
   `go/internal/telemetry/registration_steps.go` (the source-collector spans
   for every hosted-observability and incident source are registered
   together there) — or add a new register step if the source does not
   belong in that group.
3. Add compat aliases in root `compat_observability.go`.
4. Update the expected list in root `contract_test.go`'s `TestSpanNames`.
5. Run `go test ./internal/telemetry/... -count=1` from `go/`.

## What NOT to change without discussion

- The registration order — see the load-bearing-order comment in root
  `registration.go`.
