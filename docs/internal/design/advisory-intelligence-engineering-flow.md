# Advisory Intelligence: Engineering Flow

This page is for the engineers and agents who will build epic #9209. It maps
the advisory pipeline onto real packages and symbols, shows where each work
item in [Advisory Intelligence Phases](advisory-intelligence-phases.md) lands,
and groups the items into waves so that parallel work never edits the same
files.

For the operator view of the same pipeline, read
[How Correlation Works](advisory-intelligence-correlation-flow.md).

Paths are relative to `go/internal/` unless they start with `go/cmd/`, `sdk/`,
`specs/`, or `apps/`.

## 1. Component map

Labels in brackets are phase item ids.

```mermaid
flowchart TB
  subgraph COL["Collection: in-tree now, extracted repo after #8870"]
    CFG["go/cmd/collector-vulnerability-intelligence<br/>mapTarget, selectVulnerabilityInstance"]
    SRC["vulnruntime/source.go<br/>ClaimedSource"]
    PRV["vulnruntime/provider.go<br/>HTTPProvider.collect* [1.3] [1.4] [2.2]"]
    ENV["vulnerabilityintelligence/envelope.go<br/>newOSVCVEEnvelope, sanitizeURL [1.0] [1.1] [3.1]"]
    CACHE["vulnruntime/source_cache.go<br/>CachedSourceProvider"]
  end
  subgraph CO["Scheduling"]
    WCFG["workflow/vulnerability_intelligence_config.go<br/>target and derivation validation [2.1] [2.4]"]
    DER["coordinator/vulnerability/intelligence_derivation.go<br/>deriveVulnerabilityTargets [0.1] [0.2] [2.1]"]
  end
  subgraph ST["Commit seam"]
    SEAM["storage/postgres/ingestion_backfill_seam.go<br/>skipUnchangedGeneration [1.4]"]
    GUARD["storage/postgres/backfill/successor_guard.go<br/>DecideSuccessor [1.4]"]
  end
  subgraph CT["Contracts"]
    CVE["sdk/go/factschema/vulnerability/v1/cve.go [1.5] [1.7] [3.1]"]
    GOLD["sdk/go/factschema/fixturepack/payloads [1.1] [3.1]"]
    REG["specs/fact-kind-registry.v1.yaml"]
  end
  subgraph PJ["Projection"]
    INT["projector/supply/chain/impact/intents.go<br/>candidateFactKinds"]
  end
  subgraph RD["Reducer: reducer/supplychain/core"]
    H["impact.go<br/>SupplyChainImpactHandler.Handle"]
    LOAD["evidence_load.go + active_filter.go<br/>cross-scope expansion"]
    IDX["index_build.go<br/>addSupplyChainImpactIndexEntry [1.6]"]
    DEC["typed_decode.go [1.2] [1.5] [1.7] [3.2]"]
    PROV["provenance_selection.go<br/>source rank [1.2] [4.x]"]
    MATCH["version_match.go + product.go"]
    ALERT["security_alert.go [1.12]"]
    PRI["priority.go [4.5]"]
    SUP["evaluation.go<br/>EvaluateSupplyChainSuppression [3.4]"]
    GOR["go_reachability_*.go [1.10]"]
    WR["writer.go"]
    WM["winners_maintainer.go<br/>30 s default resweep"]
    XS["reducer/crossscope/dependencies.go<br/>dependencyCatalog [3.3]"]
  end
  subgraph SQL["SQL that must stay byte-identical"]
    WIN["storage/postgres/supply_chain_impact_canonical_winners_store.go<br/>supplyChainImpactWinnerSelectSQL [1.2] [3.2]"]
    FQ["query/supply/chain/impact/findings_queries.go [3.2]"]
    AGG["query/supply/chain/impact/aggregates_queries.go [1.2]"]
  end
  subgraph RE["Read surfaces"]
    RDY["impact/readiness_postgres_query.go [1.8]"]
    EVQ["advisory/evidence_sql.go<br/>ListEvidenceQuery [3.5]"]
    RTP["query/supply/chain/kubernetes_runtime_probe.go"]
    MCP["mcp/tools_supply_chain.go"]
    UI["apps/console/src/api/eshuConsoleVulnerabilities.ts<br/>loadVulnerabilities [1.9]"]
    STAT["status/collector/runtime.go [1.11]"]
  end
  WCFG --> DER --> SRC --> PRV --> ENV --> SEAM --> GUARD
  CACHE --- PRV
  CVE --- ENV
  GOLD --- ENV
  SEAM --> INT --> H
  H --> LOAD --> IDX --> DEC
  DEC --> PROV --> MATCH --> PRI --> SUP --> WR
  ALERT --> PRI
  GOR --> PRI
  XS --> H
  WR --> WM --> WIN
  WIN --- FQ
  WIN --- AGG
  FQ --> MCP
  FQ --> UI
  RDY --> MCP
  EVQ --> MCP
  RTP --> MCP
```

## 2. Write path, step by step

| Step | Symbol | Contract it must keep |
| --- | --- | --- |
| 1. Plan targets | `deriveVulnerabilityTargets` | Bounded targets: at most 100 queries or CVE ids per target, `DerivedTargetLimit` per pass (default 100, ceiling 5,000). Ecosystems default to npm. |
| 2. Claim | `ClaimedSource` | The scope id must be configured or derivable, and the fencing token is copied onto every fact. |
| 3. Fetch | `HTTPProvider` | The client never auto-paginates NVD. Paging belongs in the provider loop, which reads one page today and pages fully after item 1.3. |
| 4. Normalize | `newOSVCVEEnvelope` and siblings | Report only what the source says (`source_confidence=reported`). Never infer impact. URLs pass `sanitizeURL`. |
| 5. Commit | `skipUnchangedGeneration`, `DecideSuccessor` | One generation per scope. Absent records retract. The guard is idle without a `FreshnessHint`. |
| 6. Trigger | `candidateFactKinds` | One `supply_chain_impact` intent per scope generation that holds a trigger kind. Git file facts are not triggers. |
| 7. Load | `SupplyChainImpactHandler.Handle` | At most 8 expansion rounds and 100,000 envelopes. Hitting a cap marks the evidence partial: upsert only, no retraction. |
| 8. Decide | `buildSupplyChainImpactFindingsWithQuarantine` | Group by CVE, then package. The product path runs only when no package rows exist. An owned anchor is required. |
| 9. Write | `writer.go` | Replace-set per (scope, generation) under an advisory lock plus a fencing check. |
| 10. Dedup | `SupplyChainImpactWinnersMaintainer.RunOnce` | Full resweep every 30 s by default (`defaultSupplyChainImpactWinnersInterval`). One winner per `canonical_key`, chosen by `priority_score DESC`. |

## 3. Read path

| Surface | Symbol | Notes |
| --- | --- | --- |
| Findings list | `findings_queries.go` | The canonical key and finding id must stay byte-identical to the winners store. |
| Aggregates | `aggregates_queries.go` | `severity_bucket` comes from `cvss_score` in SQL. It must match the winners `CASE`. |
| Readiness | `readiness_postgres_query.go` | Reads fact counts and `vulnerability_source_states`. NVD, KEV, and EPSS are matched by literal scope ids. |
| Runtime context | `kubernetes_runtime_probe.go` | Follows `RUNS_IMAGE` from an image digest to Kubernetes workloads at read time, at most 200 results (`KubernetesRuntimeProbeMaxResults`). |
| Advisory evidence | `ListEvidenceQuery` | Source-only view. It expands aliases one hop, for the CVE, GHSA, and OSV prefixes only. |
| Console | `loadVulnerabilities` | 100 rows per status, no paging. |

## 4. Shared-file zones

Two items in the same zone must not be in flight at the same time. Land them
in the listed order, or have one engineer own the whole zone.

| Zone | Files | Items, in landing order | Why they conflict |
| --- | --- | --- | --- |
| A. OSV envelope | `vulnerabilityintelligence/envelope.go`, plus its extracted-repo twin | 1.0, 1.1, 3.1 | Same payload map and the same shared golden. |
| B. Provider loop | `vulnruntime/provider.go`, plus its twin | 1.3, 1.4 (attestation), 2.2 | Paging, the completeness attestation, and the cursor all live in the same fetch loop. |
| C. Commit seam | `ingestion_backfill_seam.go`, `backfill/successor_guard.go` | 1.4 | Shared by every collector. Needs a concurrency proof. |
| D. Parity SQL | winners store, `findings_queries.go`, `aggregates_queries.go` | 1.2, then 2.3 (only if it chooses read-time enrichment), then 3.2 | The byte-parity rule. One change at a time, each with a parity test. If 2.3 keeps per-scope copies, it does not enter this zone. |
| E. Reducer decode | `typed_decode.go`, `provenance_selection.go`, `index.go` | 1.13, 1.2, 1.5, 1.7, 3.2 | Same decode functions, the same selection loop, and the same classification path. |
| F. Contract | `sdk/go/factschema/vulnerability/v1/cve.go` (1.5, 3.1, the 1.2 marker), `affected_product.go` (1.7), the registry | 1.5, 1.7, 3.1 | Each one is a minor version bump. Combine them or serialize them. |
| G. Derivation and planning | `intelligence_derivation.go`, `vulnerability_intelligence_config.go`, `workflow/owned_package_manager_aliases.go`, `storage/postgres/owned_package_targets.go`, the coordinator scheduler | 2.1, 2.2, 2.4 | Same planner and validators. Item 2.2 is also in zone B, after 1.4. The `package_manager` alias table (gradle to maven) feeds both the reader SQL and the derivation mapping; edit it in one place. |
| H. Alert path | `core/security_alert.go` | 1.12, then the alert half of 1.2 | Both edit the alert-to-finding mapping. |

Items with a zone of their own:

- `index_build.go`: 1.6.
- `readiness_postgres_query.go`: 1.8.
- The console: 1.9.
- `go_reachability_*.go`: 1.10.
- `status/collector/runtime.go`: 1.11.
- Deployment configuration: 0.1-0.4.

## 5. Waves for assigning work

```mermaid
flowchart LR
  subgraph W1["Wave 1: start in parallel"]
    I00["0.1-0.4 config"]
    I10["1.0 redaction back-port"]
    I13["1.3 NVD paging"]
    I16["1.6 EPSS by score_date"]
    I18["1.8 readiness"]
    I19["1.9 console"]
    I110["1.10 Go reachability"]
    I111["1.11 docs, status"]
    I112["1.12 alert EPSS"]
    I113["1.13 SBOM path range check: reproduce first"]
  end
  subgraph W2["Wave 2"]
    I11["1.1 OSV severity fields"]
    I12["1.2 severity from vector or label"]
    I14["1.4 successor protection"]
  end
  subgraph W3["Wave 3"]
    I15["1.5 REJECTED and withdrawn"]
    I17["1.7 CPE flags"]
    I21["2.1 EPSS derivation"]
    I22["2.2 NVD window planner"]
    I24["2.4 per-source cadence"]
  end
  subgraph W4["Wave 4"]
    I23["2.3 enrichment refresh"]
    I31["3.1 identity edges"]
    I32["3.2 advisory groups"]
    I33["3.3 re-key"]
    I34["3.4 alias-aware VEX"]
    I35["3.5 id prefixes"]
  end
  ADR["ADR, then Phase 4-5<br/>extracted repo after #8870"]
  I10 --> I11
  I11 --> I12
  I13 --> I14
  I14 --> I22
  I12 --> I15
  I15 --> I17
  I00 --> I21
  I21 --> I22
  I22 --> I24
  I112 --> I12
  I113 --> I15
  I12 --> I23
  I16 --> I23
  I17 --> I31
  I31 --> I32
  I32 --> I33
  I32 --> I34
  I23 --> I32
  I33 --> ADR
```

Notes for whoever assigns the work:

- **Wave 1 is safe to fan out.** Every item there has a zone of its own,
  except 1.0, 1.3, 1.12, and 1.13. Each of those is the first item in its
  zone (A, B, H, and E).
- **Paired items need two PRs.** Items 1.1, 1.3, 1.4, 2.2, and 3.1 each need
  an in-tree PR plus an extracted-repository PR in the same wave.
  - Item 1.0 is in-tree only: the extracted repository already has the
    fragment fix (its PR #2).
  - Per plan §4.7, the extracted-repository PR is opened and cross-linked
    before the in-tree PR merges, and the in-tree PR is not merge-ready until
    the extracted PR is green.
- **Contract bumps (zone F) go to one owner.** Merge 1.5, 1.7, and 3.1 into
  one minor version if they land close together. That saves the downstream
  decode-lock and factschema-diff churn.
- **Item 2.3 starts with a fixture test of today's behavior.** No design work
  until that fixture shows what the per-scope copies actually do.
- **Theory first.** Items 2.3, 3.2, and 5.1 need a measured proof before any
  executor is dispatched (prove-the-theory-first in `AGENTS.md`).
- **Core rule.** The first core PR in zone E rewords the "behavior-preserving"
  rule in `reducer/supplychain/core/AGENTS.md` (plan §4.8).

## 6. Proof every item carries

- A failing regression first, for a defect.
- For a contract change: `eshu-contract-rigor`, a factschema-diff, and the
  payload-usage manifest.
- For a fact-emission or read-shape change: the golden corpus, meaning the
  B-7 cassette and snapshot (`eshu-golden-corpus-rigor`).
- For any SQL change in zone D: an `EXPLAIN ANALYZE` on representative data,
  plus the parity test.
- For a runtime change: the operator signal named in the phases table, and the
  QA check measured after deploy.

No-Regression Evidence: this page maps planned work onto existing code. It
changes no runtime behavior, wire contract, or gate.

No-Observability-Change: this page adds no metric, span, log key, or status
field.
