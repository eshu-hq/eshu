# AGENTS.md — internal/exposure guidance for LLM assistants

## Read first

1. `go/internal/exposure/README.md` — capability framing, sink table, honesty
   contract, content-hash discipline.
2. `go/internal/exposure/doc.go` — package contract paragraph.
3. `go/internal/exposure/sink_catalog.go` — the curated catalog, the recognizer
   (`MatchSink`), and the content hash.
4. `go/internal/reducer/iam_escalation_catalog.go` — the closed-catalog pattern
   this package mirrors.

## What this package is

The declarative half of the code-to-cloud reachability taint capability (epic
#2704, Level 1). Pure analysis: it declares **what counts as a source/sink** and
recognizes edges. It does NOT read the graph, run Cypher, or write nodes — the
bounded tracer (a later slice, in `internal/query`) consumes these catalogs.

## Invariants this package enforces

- **Closed vocabulary** — a sink is exactly one of the `SinkKind` constants. Do
  not add open-ended or string-built kinds.
- **Declarative recognition** — `MatchSink` matches only a declared
  (relationship, target label, predicates) tuple. No regex/heuristic matching of
  node content.
- **No fabrication (honesty contract)** — a sink kind with no materialized graph
  fact MUST be `GraphBacked: false`, declare no relationship/target, and cite the
  follow-up that will materialize it. `MatchSink` never returns such a spec. The
  tracer reports it `unresolved`.
- **Conservative predicates** — a missing target property fails the predicate
  (`predicatesSatisfied`). Never treat absence as a match.
- **Provenance required** — every graph-backed spec cites the reducer/graph file
  that authors its edge, verified against the real materializer.
- **Deterministic content hash** — `SinkCatalogVersion` sorts before hashing so
  reordering equivalent entries does not churn the version, but any field change
  does. `sinkCatalogVersionGolden` is pinned; bump it deliberately.
- **No internal Eshu imports** — keep this a leaf analysis package (stdlib only).
  Do not import `internal/graph`, `internal/reducer`, `internal/query`, or
  storage packages.

## Common changes and how to scope them

- **Add a sink kind** →
  1. Add the `SinkKind` constant with a doc comment.
  2. Add one or more `SinkSpec` entries; verify the relationship + target label
     against the authoring reducer/graph file and cite it in `Provenance`.
  3. If the edge is not materialized yet, set `GraphBacked: false`, leave
     relationship/target empty, and open a follow-up issue cited in `Provenance`.
  4. Extend the closed-vocabulary coverage test.
  5. Run the catalog tests; the version golden will fail — update it deliberately.
  Run `cd go && go test ./internal/exposure -count=1`.

- **Change a recognition rule** → update the spec AND its `Provenance`, then
  re-pin `sinkCatalogVersionGolden`. Confirm the change matches the real edge in
  the reducer/graph materializer; a drifted rule silently stops recognizing a
  sink.

## Failure modes and how to debug

- Symptom: a real sink edge is not recognized → the spec's relationship or target
  label drifted from the materializer. Re-check the authoring reducer file cited
  in `Provenance`.
- Symptom: version golden test fails unexpectedly → the catalog changed (possibly
  via a merge). Re-run, confirm the diff is intended, then re-pin the golden.
- Symptom: an internet CIDR sink matches a non-internet block → a predicate was
  dropped; `is_internet=true` must stay on the internet endpoint spec.

## What NOT to change without an ADR / docs update

- `SinkKind` string values — they appear in API/MCP responses; renaming is a
  breaking wire change. Update `docs/public/reference/http-api.md` in lockstep.
- The honesty contract (non-graph-backed kinds never matched) — this is the
  product's correctness guarantee, not a convenience.
