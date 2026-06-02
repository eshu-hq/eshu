# Searchdecay

## Purpose

`searchdecay` defines bounded decay scoring for selected non-canonical evidence
used in search and retrieval ranking. It gives issue #418 a testable contract
before retrieval adapters, API/MCP surfaces, or runtime telemetry exporters use
decay scores.

## Ownership boundary

This package owns pure policy validation, half-life scoring, canonical-skip
decisions, ineligible-skip decisions, and low-cardinality observation summaries.
It does not query Postgres, call NornicDB, write graph state, expose API/MCP
routes, emit OTEL telemetry, or decide canonical truth.

## Exported surface

See `doc.go` for the godoc-rendered package contract.

- `EvidenceClass` names evidence families that may or may not be decay-scored.
- `Policy` configures policy id, clock, half-life, minimum score, and eligible
  classes.
- `Evidence` is one rankable evidence item.
- `Scorer.Score` validates the policy/input and returns a `Decision`.
- `Outcome` records whether decay was applied, skipped as canonical, skipped as
  ineligible, or rejected as invalid.
- `Observer` and `Observation` expose one low-cardinality decision summary for
  later metrics, spans, or logs.

## Dependencies

`searchdecay` imports `go/internal/searchdocs` for the derived truth label. It
otherwise uses only the Go standard library.

## Telemetry

No OTEL telemetry directly. `Scorer` emits one `Observation` through an optional
observer. Live callers must bridge those summaries to operator-facing counts by
policy id, evidence class, and outcome. Do not add evidence ids, graph handles,
repository ids, or service ids as metric labels.

## Gotchas / invariants

- Canonical graph evidence and durable relationships are never decay-scored.
- Missing truth levels are rejected. Non-derived truth levels are skipped as
  canonical.
- The default eligible classes are CI run evidence, vulnerability observations,
  deployment events, cloud observations, and relationship candidates.
- Scores stay bounded from `0` to `1`; `Policy.MinScore` is also bounded.
- Decay score is ranking metadata only. It must not hide evidence or rewrite
  canonical graph truth.

## Related docs

- `docs/public/reference/search-decay-scoring.md`
- `docs/public/reference/search-benchmark-evidence.md`
- `docs/internal/design/430-nornicdb-graph-search-split.md`
