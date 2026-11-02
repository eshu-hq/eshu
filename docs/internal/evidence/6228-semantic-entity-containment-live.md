# Semantic Entity Containment Live Drive Evidence (#6228)

The semantic_entity_containment family ODU carries 10 facts (1 repository +
3 file + 6 content_entity) in the single fixture scope
`scope-ifa-semantic-entity-containment-family`
(`testdata/cassettes/semanticentitycontainment/ifa-semantic-entity-containment-family.json`).
The four admitted rows are the Go Function with semantic metadata
(`pkg/handle.go`), the Python async Function (`app/fetch.py`), the Python
Annotation (`app/fetch.py`), and the Elixir module-attribute Variable
(`lib/worker.ex`); the plain Python Function and the Go Class admit
nothing.

## Measured stamped population: 4

The first live drive recorded the actual stamped population on NornicDB
before any reconciliation (arbiter D6). The live fixture-scope set is
exactly 4 CONTAINS edges:

- 3 stamped `projector/canonical` (Go Function, Python Function,
  Annotation — canonical-owned rows whose nodes the canonical writer
  commits and whose edges it owns; the semantic writer's
  canonical-node-rows mode MATCHes those nodes and merges no edge there)
- 1 stamped `parser/semantic-entities` (Elixir Variable — the only
  semantic-owned row)

Two independent reads agree. `eshu-ifa assert-edges -domain
semantic_entity_containment` matched the 4-edge exact set in all six
shared-cell asserts (pre/post-delta × N=1/2/4). A direct census of the
kept canonical dumps found exactly 1 `parser/semantic-entities`-stamped
CONTAINS edge graph-wide at each of N=1,2,4 (no leakage from any other
scope's content entities), with the remaining 137 CONTAINS edges stamped
`projector/canonical` across all driven scopes.

The committed `.nornicdb.json` fixture already named exactly these 4
(source, target) pairs with these stamps, so reconciliation to the
measured record required no byte change; the record is this document plus
the commit text quoting the count. Hand-editing a fixture to green
without the recorded population is the rejected proof; this lane did the
opposite (measured first, fixture already exact).

## Determinism (shared N={1,2,4} cell, registry row 22)

`scripts/verify-ifa-determinism.sh` green (exit 0,
project `eshu-ifa-det-semcont`, postgres:15636). Post-delta digests
byte-identical across workers:

- N=1, N=2, N=4 digest
  `0f723ad7693bdae07bcac58d0d6e1d024c48ae674511dfeb16b1e9b2c88e1d3d`
  (cell walls 190s / 215s / 217s)

## Fault trio (cells 88-90, shard 8/8)

`scripts/verify-ifa-fault-injection.sh --shard 8/8` green (exit 0,
project `eshu-ifa-fault-semcont`, postgres:15642). All three cells assert
the same 4-edge exact set with zero dead letters:

- baseline (22s): retry-attempt baseline 0 for
  `semantic_entity_materialization`; digest
  `e0835fea223ca8fcd2071257ad2823b24652bebfd07e8b71c4d4bad18301a0fe`
- kill-worker-after-claim (79s): 1 blocked claimed/running row observed
  under the `fact_records` ACCESS EXCLUSIVE hold (the handler blocks
  deterministically in `LoadFactsForKinds`; no readiness gate precedes
  it), kill -9, release, replacement drain converges to the identical
  digest `e0835fea...` with attempts above the 0 baseline
- fail-graph-write-once-then-succeed (82s): the once-fired marker names
  the targeted `MERGE (f)-[rel:CONTAINS]->(n)` upsert (fault decorator,
  not the reducer log), the full set converges with work-item window
  1|1, digest identical `e0835fea...`

Recovery is byte-identical across the trio: all three digests equal.

## No-Regression Evidence (#6228)

`go test ./internal/ifa/...` green (guard, cassette drift lockstep,
coverage lockstep, trigger stems); `bash
scripts/test-verify-ifa-fault-injection.sh` green (106 cells, exact
cover, shard budget busiest 897s of 1500s); `bash
scripts/test-verify-ifa-determinism.sh` green (37 families'
CASSETTE/EXPECTED vars resolve); `bash
scripts/test-verify-ci-gates-registry.sh` green;
`TestIfaDeterminismWorkflowPathsCoverRegistry` green. CI waves are the
remaining required proof before merge.

No-Observability-Change: the change adds no metric, span, log line,
queue, lease, worker, route, or graph query. The MATCH-miss
attempted/matched telemetry the #8359 change landed is untouched.
