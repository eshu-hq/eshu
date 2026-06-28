# Collector Performance Envelope

This page defines the per-collector performance envelope: one row per collector
kind, with corpus-size reference, per-phase budgets (claim, ingest, emit,
project), and a wall-clock target. It is the answer to "we slow down" — a fixed,
evidence-backed budget an operator can hold each collector to.

The git collector's full-corpus number — about **15 minutes for 896
repositories** — is the **template**, not the only number. Every other collector
has its own corpus shape (cloud accounts, registries, alert streams, runtime
endpoints), so each row anchors its budget on the corpus that collector actually
ingests, expressed in the same claim / ingest / emit / project / wall-clock
shape.

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
- **emit** — turn parsed inputs into `fact_records`. Measured per fact by
  `BenchmarkEmit` (Tier-0 synthetic, see Methodology); the table lists ns/op and
  facts/op so the per-fact cost is explicit.
- **project** — reducer dequeue plus graph/content materialization. Shared
  across collectors; bounded by the reducer and Cypher performance contracts.

## Per-Collector Table

Rows are in `scope.AllCollectorKinds()` order
(`go/internal/scope/scope.go:130`). The emit column is the measured per-fact
microbenchmark cost (Apple M4 Pro, `benchtime=1x`) from `BenchmarkEmit` (#3797),
reported as `ns/op | facts/op`; it is the Tier-0 synthetic emit cost for one
scope's fact batch, **not** a full-corpus wall-clock budget. Claim, ingest, and
project budgets are stage classes, not microbenchmarks: claim is bounded by the
[reducer claim-latency gate](reducer-claim-latency-gate.md); project is bounded
by the [Cypher performance](cypher-performance.md) and reducer contracts.

| Collector kind | Corpus size reference | Claim budget | Ingest budget | Emit (ns/op \| facts) | Project budget | Wall-clock target |
| --- | --- | --- | --- | --- | --- | --- |
| `git` | 896 repos / ~3.5M facts (Tier 4) | claim-latency gate | sync+discover+parse per repo | 60625 \| 3 | reducer + Cypher contract | ~15 min full corpus (gold point) |
| `aws` | 1 account / bounded resource set | claim-latency gate | account snapshot per scope | 16708 \| 2 | reducer + Cypher contract | per-account snapshot, bounded |
| `azure` | 1 subscription / bounded resource set | claim-latency gate | subscription snapshot per scope | 13125 \| 2 | reducer + Cypher contract | per-subscription snapshot, bounded |
| `gcp` | 1 project / bounded resource set | claim-latency gate | project snapshot per scope | 14208 \| 2 | reducer + Cypher contract | per-project snapshot, bounded |
| `terraform_state` | 1 state file / bounded resources | claim-latency gate | parse state per scope | 10416 \| 2 | reducer + Cypher contract | per-state-file, bounded |
| `webhook` | refresh-trigger only (no corpus) | claim-latency gate | trigger evaluation only | N/A | N/A | trigger-latency, no fact corpus |
| `documentation` | repo doc tree (Tier 1-2) | claim-latency gate | discover+parse doc sections | 19416 \| 3 | reducer + Cypher contract | per-repo doc set, bounded |
| `oci_registry` | 1 registry / bounded image set | claim-latency gate | registry listing per scope | 11667 \| 2 | reducer + Cypher contract | per-registry, bounded |
| `package_registry` | 1 registry / bounded package set | claim-latency gate | registry/manifest per scope | 25583 \| 5 | reducer + Cypher contract | per-registry, bounded |
| `vulnerability_intelligence` | advisory feed per scope | claim-latency gate | feed fetch+normalize | 18084 \| 3 | reducer + Cypher contract | per-feed refresh, bounded |
| `sbom_attestation` | 1 SBOM/attestation per scope | claim-latency gate | parse SBOM/attestation | 15083 \| 3 | reducer + Cypher contract | per-document, bounded |
| `security_alert` | alert stream per scope | claim-latency gate | alert fetch per scope | 11042 \| 1 | reducer + Cypher contract | per-stream refresh, bounded |
| `ci_cd_run` | run history per scope | claim-latency gate | run fetch per scope | 16875 \| 3 | reducer + Cypher contract | per-run-window, bounded |
| `pagerduty` | incident stream per scope | claim-latency gate | incident fetch per scope | 8792 \| 2 | reducer + Cypher contract | per-stream refresh, bounded |
| `jira` | issue stream per scope | claim-latency gate | issue fetch per scope | 10958 \| 2 | reducer + Cypher contract | per-stream refresh, bounded |
| `scanner_worker` | scan job per scope | claim-latency gate | run scanner per scope | 6833 \| 1 | reducer + Cypher contract | per-scan-job, bounded |
| `semantic_extraction` | repo entity set (Tier 1-2) | claim-latency gate | extract per scope | 9417 \| 2 | reducer + Cypher contract | per-repo extraction, bounded |
| `kubernetes_live` | 1 cluster / bounded workloads | claim-latency gate | cluster snapshot per scope | 15250 \| 3 | reducer + Cypher contract | per-cluster snapshot, bounded |
| `vault_live` | 1 Vault / bounded mounts | claim-latency gate | mount listing per scope | 12334 \| 2 | reducer + Cypher contract | per-Vault snapshot, bounded |
| `prometheus_mimir` | metrics endpoint per scope | claim-latency gate | query endpoint per scope | 10708 \| 2 | reducer + Cypher contract | per-endpoint refresh, bounded |
| `tempo` | trace endpoint per scope | claim-latency gate | query endpoint per scope | 9916 \| 2 | reducer + Cypher contract | per-endpoint refresh, bounded |
| `grafana` | 1 Grafana / bounded dashboards | claim-latency gate | dashboard listing per scope | 10125 \| 2 | reducer + Cypher contract | per-Grafana snapshot, bounded |
| `loki` | log endpoint per scope | claim-latency gate | query endpoint per scope | 8667 \| 2 | reducer + Cypher contract | per-endpoint refresh, bounded |

`webhook` is a refresh-trigger collector: it evaluates stored triggers and
enqueues a refresh for another collector kind, and emits no `fact_records`, so
its emit and project cells are `N/A` (mirroring the `BenchmarkEmit` exemption in
#3797).

## Gold Points

A **gold point** is a concrete corpus size paired with a wall-clock target,
measured on real hardware on a credential-free or operator-reproducible corpus —
the git 15 min / 896 repo number is the canonical example.

### What "src ≥ 4" resolved to

The issue (#3801) gates "each collector with `src ≥ 4` has a documented gold
point" on a `src` (source-count) column in `gap-analysis.md § P2-1`. That
column and that section are **not recoverable in the current tree**: the only
gap-analysis document is
`docs/internal/design/2228-code-relation-taxonomy-gap-analysis.md`,
which has no `P2-1` section and no `src` column, and the capability catalog
(`docs/public/reference/capability-catalog.md`) keys maturity on a capability
`ni`/availability level, not a numeric source count. Rather than fabricate a
`src` column, this page assigns gold points to the collectors with the most
mature, **credential-free or operator-reproducible measured evidence** today.
When the `src` source-count metric is reintroduced, this section should be
re-derived from it.

### Documented gold points

| Collector | Gold-point corpus | Wall-clock target | Evidence |
| --- | --- | --- | --- |
| `git` | 896 repositories, 3,501,443 `fact_records` (Tier 4 full corpus) | ~15 min end-to-end; deferred relationship backfill 882 s (~14.7 min) | Full-corpus remote Compose run, PostgreSQL 18 + NornicDB (`go/internal/storage/postgres/README.md:1223`); deferred-backfill fact-LOAD fan-out (#3710) |
| `git` (giant-repo tail) | 896 repos; single 16,659-file repo | parse ~1586 s total; giant repo ~1012 s (~0.49 s/file) | Full-corpus collection measurement (`go/internal/collector/README.md:549`); residual tail tracked by #3711 |
| `semantic_extraction` | Tier 1 active repo (~5K files / ~50K entities) | per-repo extraction within the `local_authoritative` reindex envelope (single-file reindex to visible graph update under 5 s) | [Local Performance Envelope](local-performance-envelope.md) `local_authoritative` row; credential-free, repo-only input |
| `documentation` | Tier 1-2 repo doc tree | per-repo doc set within the same reindex envelope | Credential-free, repo-only input; reuses the local reindex envelope |

The git row is the only collector with a true Tier-4 full-corpus wall-clock
number today. `semantic_extraction` and `documentation` qualify as gold points
because they run on credential-free repo-local input and are bounded by the
already-measured `local_authoritative` reindex envelope. The remaining
provider-backed collectors (cloud, registry, alert, and runtime kinds) ingest
per-scope snapshots whose wall-clock depends on the operator's account/registry
size and provider rate limits; their emit cost is measured (table above) but a
full-corpus wall-clock gold point requires an operator-supplied corpus and is
left as open evidence below.

## Methodology

- **Emit (per-fact) numbers** come from `BenchmarkEmit` for every collector kind
  (#3797), run on Apple M4 Pro at `benchtime=1x`, reported as `ns/op | facts/op`.
  These are Tier-0 synthetic microbenchmarks of the emit path for one scope's
  fact batch; they isolate Eshu-owned fact construction from sync, parse,
  provider I/O, and graph round trips. A nanosecond-scale `ns/op` is the per-fact
  emit cost and must not be read as a full-corpus wall-clock budget.
- **Git full-corpus numbers** come from the operator remote-validation Compose
  run recorded at `go/internal/storage/postgres/README.md:1223` (896
  repositories, 3,501,443 `fact_records`, PostgreSQL 18 + NornicDB) and the
  collection-stage measurement at `go/internal/collector/README.md:549`.
- **Deferred-backfill fact-LOAD (#3710)** cut the relationship backfill long
  pole from ~36 min+ (single-scan) to 882 s (~14.7 min) by fanning the fact-LOAD
  across 896 `(scope_id, generation_id)` partitions on 8 workers. The residual
  giant-repository `$2` self-exclusion tail is tracked by #3711.
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
