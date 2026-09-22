# AGENTS.md — internal/telemetry/contract/thirdparty guidance for LLM assistants

## Read first

1. `go/internal/telemetry/contract/thirdparty/README.md`
2. `go/internal/telemetry/contract/AGENTS.md` — sibling package rules; the
   same declarations-only / no-registration / leaf-import invariants apply
   here
3. `go/internal/telemetry/AGENTS.md` — root package contract rules,
   including the no-high-cardinality-metric-label rule

## Invariants this package enforces

- **Declarations only** — one file per hosted third-party source. No
  `func init()`, no registration.
- **Leaf contract** — no `go/internal/*` imports, including root package
  `telemetry`.
- **High-cardinality values stay span attributes, never metric labels** —
  Jira issue keys, user identifiers, summaries, custom-field IDs, and URLs
  must be `SpanAttrJira*` span attributes, not new metric dimensions.

## How to add a new hosted third-party source

1. Add a new file here following the existing `jira.go`/`pagerduty.go`
   pattern.
2. Add the spans to `registerVulnerabilityIntelligence` in root
   `go/internal/telemetry/registration_steps.go`, or a new register step if
   the source does not belong in that group.
3. Add compat aliases in root `compat_thirdparty.go`.
4. Update the expected list in root `contract_test.go`'s `TestSpanNames`
   (and `TestMetricDimensionKeys` for a new bounded dimension).
5. Run `go test ./internal/telemetry/... -count=1` from `go/`.

## What NOT to change without discussion

- The registration order — see the load-bearing-order comment in root
  `registration.go`.
