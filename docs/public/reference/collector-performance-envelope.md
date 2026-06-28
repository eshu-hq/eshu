# Collector Performance Envelope

This page defines the per-collector performance envelope: one row per collector
kind, with corpus-size reference, per-phase budgets (claim, ingest, emit,
project), and a wall-clock target. It is the answer to "we slow down" — a fixed,
evidence-backed budget an operator can hold each collector to.

The git collector's full-corpus measurement — a **896-repository, 3.5M-fact**
remote Compose run with per-phase wall-clock budgets (see [Gold
Points](#gold-points)) — is the **template**, not the only number. Every other
collector has its own corpus shape (cloud accounts, registries, alert streams,
runtime endpoints), so each row anchors its budget on the corpus that collector
actually ingests, expressed in the same claim / ingest / emit / project /
wall-clock shape.

Related: [Local Eshu Service Performance Envelope](local-performance-envelope.md)
covers the laptop-scale query/start envelope and the dogfood tiers this page
reuses; [Reducer Claim-Latency Gate](reducer-claim-latency-gate.md) and
[Cypher Performance](cypher-performance.md) own the downstream projection budget
each collector feeds.

## Pipeline Phases

Every collector follows the same flow; the four budget columns map to it
directly:

```text
claim  -> ingest        -> emit        -> project
(lease   (sync/discover/  (fact records   (reducer queue -> graph/content
 a scope) parse a scope)   for the scope)  projection -> query surface)
```

- **claim** — lease a `(scope, generation)` work item. Bounded by queue claim
  latency, not corpus size; the reducer claim-latency gate owns this budget.
- **ingest** — sync, discover, and parse one scope's inputs. Corpus-size
  dominated; this is the per-collector long pole.
- **emit** — turn parsed inputs into `fact_records`. Measured by `BenchmarkEmit`
  (Tier-0 synthetic, see Methodology). The benchmark times one full emit
  operation over a scope's whole fact batch, so `ns/op` is per-emit-operation
  (batch) cost, **not** per fact; the table reports the raw `ns/op | facts/op`
  plus a derived `ns/fact` so the per-fact cost is explicit.
- **project** — reducer dequeue plus graph/content materialization. Shared
  across collectors; bounded by the reducer and Cypher performance contracts.

## Per-Collector Table

Rows are in `scope.AllCollectorKinds()` order
(`go/internal/scope/scope.go:130`). The emit column is the measured
microbenchmark cost (Apple M4 Pro, `benchtime=1x`) from `BenchmarkEmit` (#3797),
reported as `ns/op | facts/op`. `ns/op` is the cost of **one emit operation over
a scope's whole fact batch**, not the cost of a single fact: each `BenchmarkEmit`
iteration drains the entire `facts/op` batch
(`go/internal/collector/emit_bench_test.go:88`). The derived `ns/fact` column is
`round(ns_op / facts_op)`, the per-fact cost. Both are the Tier-0 synthetic emit
cost, **not** a full-corpus wall-clock budget. Claim, ingest, and
project budgets are stage classes, not microbenchmarks: claim is bounded by the
[reducer claim-latency gate](reducer-claim-latency-gate.md); project is bounded
by the [Cypher performance](cypher-performance.md) and reducer contracts.

| Collector kind | Corpus size reference | Claim budget | Ingest budget | Emit (ns/op \| facts/op) | Emit ns/fact (derived) | Project budget | Wall-clock target |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `git` | 896 repos / ~3.5M facts (Tier 4) | claim-latency gate | sync+discover+parse per repo | 60625 \| 3 | ~20208 | reducer + Cypher contract | full corpus measured (gold point; see Gold Points) |
| `aws` | 1 account / bounded resource set | claim-latency gate | account snapshot per scope | 16708 \| 2 | ~8354 | reducer + Cypher contract | per-account snapshot, bounded |
| `azure` | 1 subscription / bounded resource set | claim-latency gate | subscription snapshot per scope | 13125 \| 2 | ~6563 | reducer + Cypher contract | per-subscription snapshot, bounded |
| `gcp` | 1 project / bounded resource set | claim-latency gate | project snapshot per scope | 14208 \| 2 | ~7104 | reducer + Cypher contract | per-project snapshot, bounded |
| `terraform_state` | 1 state file / bounded resources | claim-latency gate | parse state per scope | 10416 \| 2 | ~5208 | reducer + Cypher contract | per-state-file, bounded |
| `webhook` | refresh-trigger only (no corpus) | claim-latency gate | trigger evaluation only | N/A | N/A | N/A | trigger-latency, no fact corpus |
| `documentation` | repo doc tree (Tier 1-2) | claim-latency gate | discover+parse doc sections | 19416 \| 3 | ~6472 | reducer + Cypher contract | per-repo doc set, bounded |
| `oci_registry` | 1 registry / bounded image set | claim-latency gate | registry listing per scope | 11667 \| 2 | ~5834 | reducer + Cypher contract | per-registry, bounded |
| `package_registry` | 1 registry / bounded package set | claim-latency gate | registry/manifest per scope | 25583 \| 5 | ~5117 | reducer + Cypher contract | per-registry, bounded |
| `vulnerability_intelligence` | advisory feed per scope | claim-latency gate | feed fetch+normalize | 18084 \| 3 | ~6028 | reducer + Cypher contract | per-feed refresh, bounded |
| `sbom_attestation` | 1 SBOM/attestation per scope | claim-latency gate | parse SBOM/attestation | 15083 \| 3 | ~5028 | reducer + Cypher contract | per-document, bounded |
| `security_alert` | alert stream per scope | claim-latency gate | alert fetch per scope | 11042 \| 1 | ~11042 | reducer + Cypher contract | per-stream refresh, bounded |
| `ci_cd_run` | run history per scope | claim-latency gate | run fetch per scope | 16875 \| 3 | ~5625 | reducer + Cypher contract | per-run-window, bounded |
| `pagerduty` | incident stream per scope | claim-latency gate | incident fetch per scope | 8792 \| 2 | ~4396 | reducer + Cypher contract | per-stream refresh, bounded |
| `jira` | issue stream per scope | claim-latency gate | issue fetch per scope | 10958 \| 2 | ~5479 | reducer + Cypher contract | per-stream refresh, bounded |
| `scanner_worker` | scan job per scope | claim-latency gate | run scanner per scope | 6833 \| 1 | ~6833 | reducer + Cypher contract | per-scan-job, bounded |
| `semantic_extraction` | repo entity set (Tier 1-2) | claim-latency gate | extract per scope | 9417 \| 2 | ~4709 | reducer + Cypher contract | per-repo extraction, bounded |
| `kubernetes_live` | 1 cluster / bounded workloads | claim-latency gate | cluster snapshot per scope | 15250 \| 3 | ~5083 | reducer + Cypher contract | per-cluster snapshot, bounded |
| `vault_live` | 1 Vault / bounded mounts | claim-latency gate | mount listing per scope | 12334 \| 2 | ~6167 | reducer + Cypher contract | per-Vault snapshot, bounded |
| `prometheus_mimir` | metrics endpoint per scope | claim-latency gate | query endpoint per scope | 10708 \| 2 | ~5354 | reducer + Cypher contract | per-endpoint refresh, bounded |
| `tempo` | trace endpoint per scope | claim-latency gate | query endpoint per scope | 9916 \| 2 | ~4958 | reducer + Cypher contract | per-endpoint refresh, bounded |
| `grafana` | 1 Grafana / bounded dashboards | claim-latency gate | dashboard listing per scope | 10125 \| 2 | ~5063 | reducer + Cypher contract | per-Grafana snapshot, bounded |
| `loki` | log endpoint per scope | claim-latency gate | query endpoint per scope | 8667 \| 2 | ~4334 | reducer + Cypher contract | per-endpoint refresh, bounded |

`webhook` is a refresh-trigger collector: it evaluates stored triggers and
enqueues a refresh for another collector kind, and emits no `fact_records`, so
its emit and project cells are `N/A` (mirroring the `BenchmarkEmit` exemption in
#3797).

## Gold Points

A **gold point** is a concrete corpus size paired with a *measured* wall-clock
target, taken on real hardware on a credential-free or operator-reproducible
corpus. The git 896-repository full-corpus run — with its per-phase numbers
(projection complete 1,245 s, deferred relationship backfill 882 s) — is the
canonical example. A gold point requires a real measurement: a budget reused from
a different envelope, or an estimate, is not a gold point and belongs in [Open
Evidence](#open-evidence).

### What "src ≥ 4" resolved to

The issue (#3801) gates "each collector with `src ≥ 4` has a documented gold
point" on a `src` (source-count) column in `gap-analysis.md § P2-1`. That
column and that section are **not recoverable in the current tree**: the only
gap-analysis document is
`docs/internal/design/2228-code-relation-taxonomy-gap-analysis.md`,
which has no `P2-1` section and no `src` column, and the capability catalog
(`docs/public/reference/capability-catalog.md`) keys maturity on a capability
`ni`/availability level, not a numeric source count. Rather than fabricate a
`src` column, this page assigns a gold point only where a **real full-corpus
wall-clock measurement exists** today. By that bar, `git` is the only collector
with a gold point: it is the one kind with a measured Tier-4 full-corpus run.
Every other collector's full-corpus wall-clock is unmeasured and is recorded in
[Open Evidence](#open-evidence), not claimed as a gold point. When the `src`
source-count metric is reintroduced, this section should be re-derived from it.

### Documented gold points

The gold point is the git collector's measured full-corpus run, reported as its
two measured wall-clock phases rather than a single end-to-end figure (no
single end-to-end git collection number is recorded in the tree):

| Collector | Gold-point corpus | Measured phase | Phase wall-clock | Evidence |
| --- | --- | --- | --- | --- |
| `git` | 896 repositories, 3,501,443 `fact_records` (Tier 4 full corpus) | bootstrap projection complete | 1,245 s | Full-corpus remote Compose run, PostgreSQL 18 + NornicDB (`go/internal/storage/postgres/README.md:1248`) |
| `git` | same run, 207,003 loaded queried-kind facts | deferred relationship backfill | 882 s (~14.7 min) | Deferred-backfill fact-LOAD fan-out across 896 partitions on 8 workers (`go/internal/storage/postgres/README.md:1223`); down from pre-#3710 ~36 min+ single-scan |
| `git` (collection stage) | same run; single 16,659-file giant repo | parse stage total / worst single repo | ~675 s total / ~238 s giant (post largest-first + byte-balanced); pre-change baseline ~1586 s / ~1012 s | Full-corpus collection measurement (`go/internal/collector/README.md:571`, baseline; `:675`, post-change); residual giant-repo tail tracked by #3711 |

`git` is the only collector with a measured full-corpus wall-clock budget today.
The 882 s figure is the **deferred relationship backfill phase** (the fact-LOAD
fan-out per #3710/#3725), not an end-to-end git collection time; the tree records
no single "~15 min end-to-end" git number, so this page presents the measured
phases (projection 1,245 s, backfill 882 s, parse stage ~675 s) instead of
inventing one.

The remaining provider-backed collectors (cloud, registry, alert, and runtime
kinds) ingest per-scope snapshots whose wall-clock depends on the operator's
account/registry size and provider rate limits. The repo-local collectors
(`semantic_extraction`, `documentation`) have **no measured per-repo collection
wall-clock** either: the `local_authoritative` envelope
(`docs/public/reference/local-performance-envelope.md:19`) defines only
single-file reindex-to-visible-graph-update timing, not a per-repo
semantic-extraction or full doc-tree collection budget, so reusing it would
over-claim. Every collector except `git` has a measured emit cost (table above)
but its full-corpus wall-clock is left as open evidence below.

## Methodology

- **Emit numbers** come from `BenchmarkEmit` for every collector kind (#3797),
  run on Apple M4 Pro at `benchtime=1x`, reported as `ns/op | facts/op`. These
  are Tier-0 synthetic microbenchmarks of the emit path for one scope's fact
  batch; they isolate Eshu-owned fact construction from sync, parse, provider
  I/O, and graph round trips. Each benchmark iteration drains the scope's whole
  fact batch (`go/internal/collector/emit_bench_test.go:88`), so `ns/op` is the
  per-emit-operation (batch) cost, **not** per fact. The per-fact cost is the
  derived `ns/fact` column (`round(ns_op / facts_op)`); for example `git`'s
  `60625 | 3` is ~20208 ns/fact, not 60625 ns/fact. Neither figure is a
  full-corpus wall-clock budget.
- **Git full-corpus numbers** come from the operator remote-validation Compose
  run recorded at `go/internal/storage/postgres/README.md:1223` (896
  repositories, 3,501,443 `fact_records`, 207,003 loaded queried-kind facts,
  PostgreSQL 18 + NornicDB): bootstrap projection complete at 1,245 s and the
  deferred relationship backfill at 882 s
  (`go/internal/storage/postgres/README.md:1248`). The collection-stage parse
  numbers come from the giant-repo scheduling measurement at
  `go/internal/collector/README.md:571` (pre-change baseline parse ~1586 s total,
  giant repo ~1012 s) and `:675` (post largest-first + byte-balanced parse
  ~675 s total, giant repo ~238 s). No single end-to-end git collection
  wall-clock is recorded, so this page does not claim one.
- **Deferred-backfill fact-LOAD (#3710)** cut the relationship backfill long
  pole from ~36 min+ (single-scan) to 882 s (~14.7 min) by fanning the fact-LOAD
  across 896 `(scope_id, generation_id)` partitions on 8 workers
  (`go/internal/storage/postgres/README.md:1223`). This 882 s is the backfill
  phase budget, not end-to-end git collection. The residual giant-repository
  `$2` self-exclusion tail is tracked by #3711.
- **Claim and project budgets** are stage classes governed by the
  [Reducer Claim-Latency Gate](reducer-claim-latency-gate.md), the
  [Cypher Performance](cypher-performance.md) contract, and the reducer scale
  envelope in [Local Performance Envelope](local-performance-envelope.md); this
  page does not re-measure them.
- **Dogfood tiers** (0-4) are defined in
  [Local Performance Envelope](local-performance-envelope.md#dogfood-tiers) and
  reused here for corpus-size references.

## Open Evidence

These per-collector wall-clock gold points remain open until an operator-supplied
corpus is measured (the emit microbenchmark exists for all of them; the
full-corpus wall-clock does not):

- per-repo collection wall-clock for `semantic_extraction` and `documentation`:
  the `local_authoritative` envelope
  (`docs/public/reference/local-performance-envelope.md:19`) measures only
  single-file reindex-to-visible-graph-update, not per-repo extraction or
  full doc-tree collection, so no gold point can be derived from it
- per-account/subscription/project wall-clock for `aws`, `azure`, `gcp`
- per-registry wall-clock for `oci_registry`, `package_registry`
- per-stream/feed wall-clock for `vulnerability_intelligence`, `security_alert`,
  `ci_cd_run`, `pagerduty`, `jira`, `sbom_attestation`, `scanner_worker`
- per-endpoint/cluster wall-clock for `kubernetes_live`, `vault_live`,
  `prometheus_mimir`, `tempo`, `grafana`, `loki`
- reintroduction of the `src` source-count metric so the `src ≥ 4` gold-point
  gate can be derived from it rather than from credential-free-evidence maturity

When a provider-backed collector's full-corpus wall-clock is measured, add its
gold-point row above and update the corresponding open-evidence bullet; do not
hide a miss behind the git template number.

## Performance And Observability Markers

No-Regression Evidence: this is a documentation-only deliverable. It adds no
collector runtime, parser, reducer conflict key, queue SQL, graph write, Cypher,
worker, lease, batch, runtime knob, schema DDL, metric, span, log field, status
field, or API/MCP route. The emit numbers are the `BenchmarkEmit` measurements
already produced by #3797; the git full-corpus numbers are the existing
operator remote-validation evidence cited above. No code path changes, so no
hot-path regression is possible.

No-Observability-Change: this page records existing operator-facing signals
(reducer claim-latency gate, Cypher performance contract, collector and reducer
status, `BenchmarkEmit`) and adds no new telemetry. Operators continue to
diagnose collector throughput through `/admin/status`, reducer queue status,
workflow work-item state, Postgres query spans, and the per-family workflow
metrics described in [Local Performance Envelope](local-performance-envelope.md).
