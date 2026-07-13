# replay/schedulereplay

The Layer 3 (ordering) **schedule replay** for the deterministic replay
framework (epic #4102, issue #4122, R-13). It drives recorded projection work
through the **real reducer service loop** using a deterministic in-memory work
source that delivers intents in a *scripted order*, and asserts the converged
graph truth is **independent of delivery order**.

## What it proves

Given one fixed set of recorded work items, delivering them in any order
(in-order, adversarial reverse, rotated, or with duplicates) must converge on a
byte-identical canonical graph snapshot. That snapshot is the offline,
credential-free analog of the B-12 graph-truth snapshot.

The acceptance scenarios (`scenario_test.go`):

- **Order invariance** — the same items in ≥3 scripted orders, including a
  duplicate-delivery order, drained through the real reducer loop, produce one
  identical snapshot.
- **Concurrency invariance** — the real concurrent batch claim path
  (`BatchWorkSource.ClaimBatch`, 4 workers competing on the shared conflict
  domain) converges on the same snapshot as the deterministic sequential run.
- **Teeth** — a deliberately order-sensitive applier (it drops a `CONTAINS`
  edge when the parent node has not been applied yet — the #4019
  child-before-parent class) produces *different* snapshots for in-order vs
  reverse delivery, proving the gate detects ordering bugs.

## How it relates to the R-5 offline tier (`replay/offlinetier`)

These two gates are complementary and test **different properties**:

| | `offlinetier` (R-5) | `schedulereplay` (R-13) |
|---|---|---|
| Property | projection is **correct** on the real engine | converged truth is **order-independent** |
| Backend | **real** NornicDB (env-gated; skips without one) | in-memory canonical model, every PR |
| Guards | backend-specific bugs (#4019 on NornicDB, MERGE races) | delivery-ordering / replay-ordering races |

`schedulereplay` deliberately uses an in-memory canonical graph because the
property under test — *does the accumulation depend on delivery order?* — is
backend-agnostic, and the issue mandates it run credential-free with no
Postgres on every PR. It is **not** a stand-in for the real backend:
backend-specific projection correctness stays owned by `offlinetier`'s
real-NornicDB live tier. The in-memory graph is the *subject* of the
order-invariance assertion, not a fake of production projection.

## Inputs

Work items come from the committed cassette
`testdata/cassettes/replayoffline/nested-directory-tree.json` through the real
`cassette.Source` → `offlinetier.MaterializationFromGeneration` seam, so the
inputs are recorded facts, not synthetic toys. Each materialization is split
into per-entity work items (one repository item, one per directory) whose edges
reference the parent item's node, so reordering exercises a genuine
conflict-key ordering scenario.

## Shared-conflict-key projection ordering (C-14, #4367)

The nested-directory-tree scenario above proves ordering within one projection
hook. Two reducer projections are shared-conflict-key: their `reducer_domain`
is written by **more than one** distinct `projection_hook` in
`specs/fact-kind-registry.v1.yaml`, so two independently-scheduled fact
families contend on the same reducer conflict key:

- `projection:incident_repository_correlation` — hooks
  `incident_context_read_model` (`change.record`, `incident.lifecycle_event`,
  `incident.record`) and `work_item_evidence_read_model` (`work_item.record`,
  `work_item.external_link`, ...).
- `projection:supply_chain_impact` — hooks `supply_chain_impact`
  (`scanner_worker.analysis`/`.warning`), `vulnerability_source_state`
  (`vulnerability.cve`, `vulnerability.affected_package`, ...), and
  `vulnerability_suppression_admission` (`vulnerability.suppression`).

`projection_ordering_scenario_test.go` proves both are delivery-order
independent under their real `reducer.Domain` constant
(`Config.Domain`, wired through `RunScheduleReport`) using two dedicated
cassettes:

- `testdata/cassettes/replayschedule/incident-repository-correlation.json`
- `testdata/cassettes/replayschedule/supply-chain-impact.json`

`workitem_projection.go` maps each cassette's recorded facts to WorkItems
through the `cassette.Source` seam (there is no offlinetier materializer for
these two domains). Each projection hook owns distinct node labels (Incident
vs. WorkItem; Vulnerability/Package vs. Finding vs. Suppression); the edges
that cross between two hooks' nodes (`HAS_WORK_ITEM`, `DETECTS`,
`TARGETS_PACKAGE`, `SUPPRESSES`) are what make this a genuine cross-hook
ordering proof rather than two independent single-hook scenarios glued
together. The teeth test
(`TestProjectionScheduleReplayCatchesCrossHookOrderingBug`) proves the harness
catches a cross-hook ordering bug on the incident cassette.

See `evidence-4367-projection-ordering.md` for the no-regression and
no-observability-change evidence for this addition.

## Verifying a change

```bash
export GOCACHE="$(git rev-parse --show-toplevel)/.gocache"
cd go && go test -race ./internal/replay/schedulereplay/ -count=1
```

No Docker, no Postgres, no graph backend — the gate runs in the default
`go test` pass.

## Performance & observability evidence

- **No-Regression Evidence:** This package is additive replay/gate
  infrastructure. It imports and drives the existing `reducer.Service` claim
  loop unchanged; it adds no code to any production runtime path (no edit under
  `go/internal/reducer`, `go/internal/storage`, `go/internal/queue`, or the
  service binaries). Conflict domain: a single in-memory canonical graph keyed
  by node identity and `(from, rel, to)` edge tuple, mutated under one mutex by
  the reducer executor; worker settings exercised are `Workers=1` (sequential)
  and `Workers=4, BatchClaimSize=4` (concurrent batch). Input shape: 4 work
  items (one repository, three directories) from the committed
  `nested-directory-tree.json` cassette; terminal state is a fixed 4-node /
  3-edge snapshot, identical across every scripted delivery order. Wall time is
  not asserted (ordering correctness, not throughput); `go test -race` for the
  package completes in ~1.6s. Because no production path changed, there is no
  reducer throughput, queue-depth, or row-count regression to measure.
- **No-Observability-Change:** No telemetry instruments, spans, logs, or status
  fields are added or modified. The gate asserts the canonical graph-truth
  snapshot directly (`Graph.Canonical()`), not a runtime metric, and the
  reducer's existing claim/queue instrumentation is untouched.
