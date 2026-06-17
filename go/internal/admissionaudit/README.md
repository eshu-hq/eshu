# Admission Audit

## Purpose

`internal/admissionaudit` compares independent fixture intent with reducer
admission decisions, canonical graph observations, and API/MCP readbacks. It
gives correlation work a reusable golden audit that catches graph/query drift,
missing explanations, accidental canonical writes, duplicate delivery, and
stale replay before a product-truth claim moves forward.

## Ownership boundary

This package owns pure comparison only. It does not collect facts, evaluate
correlation rules, write reducer decisions, query Postgres, read a graph
backend, dispatch MCP calls, or decide admission policy.

## Exported surface

See `doc.go` for the godoc contract.

- `Suite`, `FixtureIntent`, and `LoadSuite` describe and load fixture intent.
- `Observation`, `Decision`, `GraphFact`, and `ReadbackDecision` describe the
  reducer, graph, API, and MCP snapshots supplied by callers.
- `Audit` returns a deterministic `Report`.
- `Report.Pass` and `Report.Summary` support focused tests and script output.

## Dependencies

The package depends only on the Go standard library. Reducer, query, MCP,
compose, and dogfood scripts may import or feed it, but `admissionaudit` must
not import those parents.

## Telemetry

None. The audit package emits no metrics, spans, or logs. Runtime telemetry
stays with the reducer, query handlers, MCP dispatcher, graph backend, and
Postgres adapters that collect the observations.

## Gotchas / invariants

Golden fixture intent must not be produced by serializing Eshu output back into
expected truth. Each `FixtureIntent` needs a human-readable `fixture_intent`
that cites the exact public-safe case it proves.

Non-admitted decisions (`rejected`, `ambiguous`, `stale`, `missing_evidence`,
`permission_hidden`, `unsupported`, and `unsafe`) must not have canonical graph
facts or written canonical targets in the audit snapshot.

Every decision must explain itself. A decision with no source handles, no
evidence count, and no explanation text is an explainability failure even when
it is rejected or ambiguous, because an operator must be able to ask why the
candidate did not become canonical truth.

Admitted decisions must carry explanation evidence and matching graph/readback
truth. Duplicate decision IDs and stale admitted decisions are failures because
they hide replay or upsert bugs. Two decisions that share the same logical
identity (case, domain, scope, generation) but carry different row IDs are a
`logical_duplicate_decisions` failure; the audit selects the lowest row ID so
its output stays deterministic regardless of input order.

`LoadSuite` rejects any `expected_state` outside the closed admission-state
vocabulary so a fixture typo surfaces as a load error instead of a confusing
state disagreement.

## Related docs

- `tests/fixtures/product_truth/README.md`
- `docs/public/guides/coding-with-agents.md`
- `docs/internal/agent-guide.md`
- `go/internal/reducer/README.md`
- `go/internal/query/README.md`
