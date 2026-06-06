# Searchdecaytelemetry

## Purpose

`searchdecaytelemetry` bridges `searchdecay.Observer` decisions into Eshu's
central telemetry instruments. It lets live search or evaluation callers record
which decay policy affected which bounded evidence class without putting OTEL
dependencies inside the pure scoring package.

## Ownership boundary

This package owns only the observer adapter from `go/internal/searchdecay` to
`go/internal/telemetry`. It does not score evidence, query Postgres, call
NornicDB, write graph state, expose API/MCP routes, or decide canonical truth.

## Exported surface

- `NewObserver` builds an observer backed by `telemetry.Instruments`.
- `Observer.ObserveDecay` records
  `eshu_dp_search_decay_policy_applications_total` with `policy_id`,
  `evidence_class`, and `outcome`.

## Telemetry

The counter labels are intentionally low-cardinality. Policy ids are configured
tokens, evidence classes are the closed `searchdecay` class set, and outcomes
are the closed decision enum. Evidence ids, graph handles, repository ids, and
service ids are never metric labels.

## Gotchas / invariants

- Nil instruments are a no-op so tests and non-telemetry eval paths can reuse
  the observer safely.
- Decay telemetry is ranking metadata only. It must not hide required evidence
  or claim canonical graph truth.

## Related docs

- `go/internal/searchdecay/README.md`
- `go/internal/telemetry/README.md`
- `docs/public/reference/search-decay-scoring.md`
