# PR-B for #10880: reader inventory, the scoped-token fork, and the layer-2 audit

Status: **Evidence record.** The arbiter ruled option B (per-support table) for
grant-scoped and existence-filtered reads; PR-B0 is executed (see the B0
execution record at the end). The sections before it are the evidence the ruling
rested on and are not rewritten. Refs #10880, part of #10860. Companion to
[the first-result-family design record](10880-first-result-family-move.md) and
[the PR-A review limits](10880-pr-a-review-limits.md). PR-A and #11161 are merged
(origin/main `81a8cffd97` at the time of writing).

PR-B was sequenced as slice 2b(ii): move explain, the scoped-token paths, the
advisory evidence reader, and readiness target resolution onto the winners
read model; retire the gate-off legacy list; flip
`ESHU_SUPPLY_CHAIN_IMPACT_WINNERS_READ` to on; land the layer-2 audit. This
record answers three questions before any of that is built:

1. Does a winners-only read return the same answer as the legacy read when a
   canonical finding has supports in more than one scope? (Section 1: no.)
2. Which readers still read finding facts, and which capability does each
   serve? (Section 2: more than the sequence assumed, and readiness is not one.)
3. How does a check fail a handler that reads a store its capability did not
   declare? (Section 3.)

## 1. The scoped-token fork: measured divergence

### Why a divergence can exist at all

The winners table holds one row per canonical key. The key is
`cve | advisory | package | purl | product_criteria | match_criteria |
observed_version | requested_range | impact_status | repository_id |
subject_digest`. The same key can be written from more than one scope (one
`fact_records` row per scope and generation; `source_count` in the winners
table counts them). Two orderings differ:

- **Legacy read** (`ListFindingsQuery`, the aggregate canonical-facts CTE,
  the explain candidate CTEs): apply every filter **and the scoped-token
  grant** to the candidate supports, then pick one winner per key (priority
  descending, payload finding id present, fact id ascending).
- **Winners read**: the winner was picked across all scopes when the row was
  written. Filters and the grant then apply to that one row.

`repository_id` is part of the key, so a **repository** grant cannot diverge.
A **scope** grant can, and so can any filter on a field outside the key
(`priority_bucket`, severity from `cvss_score`, `detection_profile`,
`image_ref`).

### Corpus and method

Disposable `postgres:18.6` (container `10880b-fork`, private port, removed
after), full production bootstrap, the real `RebuildAllWinners`, the real
legacy and winners SQL constants. Correctness only; no timing is claimed.
The probe was a throwaway live test in the impact package, **not committed**.
It called the shipped store for the legacy answer and the shipped winners
queries (with the same 30-argument and 20-argument shapes the stores use) for
the winners answer, so the scope-grant routing that sends granted calls to the
legacy CTE did not apply.

Two active scopes: `SA` (a git-repository scope) and `SB` (an image scope).
Ten finding facts over six canonical keys:

| Key | Support in SA | Support in SB | Winner (why) |
| --- | --- | --- | --- |
| m1 | p=40, cvss 5.0, bucket low | p=80, cvss 9.5, bucket critical | SB (higher priority) |
| m2 | p=90, cvss 9.5, critical | p=30, cvss 4.0, low | SA (higher priority) |
| m3 | p=60, cvss 6.0, low | p=60, cvss 6.0, low | SA (tie, fact id `fact:m3:a` first) |
| m4 | p=60 (`fact:m4:z`) | p=60 (`fact:m4:b`) | SB (tie, fact id `fact:m4:b` first) |
| s1 | p=70, critical | none | SA (control, single support) |
| s2 | none | p=65, low | SB (control, single support) |

The winners table recorded `source_count=2` for the four multi-support keys.

### Scope and repository grants (list, count, five inventory dimensions)

"Equal" means byte-identical JSON between the legacy and winners answers.
The five inventory dimensions are status, priority bucket, severity,
repository, and ecosystem. Status filter `affected_exact` on every row.

| Grant | List: legacy returns | List: winners returns | Count total legacy / winners | List, count, all 5 dimensions |
| --- | --- | --- | --- | --- |
| scope SA only | m1 (SA copy, p=40), m2, m3, m4, s1 | m2, m3, s1 | 5 / 3 | **all differ** |
| scope SB only | m1 (SB copy, p=80), m2 (SB copy, p=30), m3, m4, s2 | m1, m4, s2 | 5 / 3 | **all differ** |
| scope SA + SB | all six | all six | 6 / 6 | equal |
| repositories m1, m2 | m1, m2 | m1, m2 | 2 / 2 | equal |
| scope SA + repository s2 | m1 (SA copy), m2, m3, m4, s1, s2 | m2, m3, s1, s2 | 6 / 4 | **all differ** |

Per-dimension examples for scope SA only: status `affected_exact` 5 vs 3;
priority bucket `low` 3 and `critical` 2 vs `critical` 2 and `low` 1;
severity `medium` 3, `critical` 1, `high` 1 vs one each; repository buckets 5
vs 3. For scope SB only the priority buckets read `low` 4, `critical` 1 vs
`low` 2, `critical` 1.

What the divergence is, exactly:

- **Winners never returns more than legacy.** A winner whose scope is granted
  is also the best of the granted supports, so the legacy pick is the same
  row. The only difference is rows the winners read hides. There is no
  cross-grant leak in either direction.
- **Hidden rows are findings the grant holder is entitled to see.** A holder
  of only SA loses m1 and m4: each exists in SA, and the winner sits in SB.
  For a security product that is an under-report, not a cosmetic difference.
- **Legacy shows the granted copy.** For m1 the SA-only holder sees SA's
  priority-40 copy, not SB's priority-80 payload. A fix that returned the
  winner's payload to every holder of any scope would leak SB's payload to an
  SA-only grant (see rejected option E).

### Unscoped reads diverge too (this is new, and it affects the default flip)

The same corpus, no grants, shipped `PostgresFindingStore` and
`PostgresAggregateStore` with the winners gate on versus off:

| Unscoped filter | Legacy list / count | Winners list / count | Result |
| --- | --- | --- | --- |
| status only | m1, m2, m3, s1 / 4 | same | equal |
| priority bucket `critical` | m1, m2, s1 / 3 | same | equal |
| severity `critical` | m1, m2 / 2 | same | equal |
| min priority 65 | m1, m2, s1 / 3 | same | equal |
| priority bucket `low` | m1 (SA copy), m2 (SB copy), m3 / 3 | m3 / 1 | **differs** |
| severity `medium` | m1 (SA copy), m2 (SB copy), m3 / 3 | m3 / 1 | **differs** |

The legacy read filters supports first, so a key whose winner is `critical`
still answers "show me `low` findings" through its lower-ranked copy. The
winners read filters the canonical row, so it does not. Filters that are
monotone in the pick order (min priority) cannot diverge; filters on the
priority bucket or the CVSS-derived severity can. The PR-A parity proofs
(`TestFindingsWinnersPayloadParityLive`, `TestAggregatesWinnersParityLive`)
all pass because their seed has one support per key, so the PR-A line "the
named multi-support risk did not materialize on this corpus" holds only for
single-support corpora.

This is a **semantics** question as well as a parity one. "Filter then pick"
(legacy) can show a finding under a bucket its canonical record does not
carry. "Pick then filter" (winners) matches the canonical record. The arbiter
should rule which one the product promises.

### Precedent already shipped

`PostgresGroupedInventoryStore` already serves **scoped** calls from the
winners table and documents this exact divergence in its type comment
("Known divergence", measured there at 0.2 to 0.5 percent of the
unattributed bucket on a shim seed). The list and aggregate stores route
scoped calls to the legacy CTE specifically to avoid it. So the repository
already holds both answers; the ruling should make them uniform.

### Options

Costs below are design estimates read from the code, not measurements. Only
the divergence figures above were measured.

| | A: scoped stays on legacy | B: supports table (P2 shape) | C: per-support rows in winners | D: accept canonical semantics |
| --- | --- | --- | --- | --- |
| What it is | Granted and non-key-filter calls keep the fact CTE | New typed table, one row per (canonical key, scope): filter columns, payload, finding id, priority. Winners stays the unscoped pick | Winners key becomes (canonical key, scope); an `is_winner` flag marks the pick | Scoped and filtered calls read winners as-is, as grouped inventory does |
| Parity with legacy | Exact | Exact by construction (same pick over the same candidates, on a typed table) | Exact (same) | **No**: hides entitled findings, changes filter results |
| Keeps the write stop (slice 3) possible | **No** for this fact kind: the legacy CTE needs finding facts and the ten indexes | **Yes**, and it is the source slice 3 needs anyway (section 2) | Yes | Yes |
| New schema | None | One table, migration, backfill, dirty-log reuse | PK change on a shipped table, 7 indexes become partial on the flag | None |
| Code touched | None | Writer-adjacent maintainer plus one scoped read set per capability | Every shipped maintainer path (full and keyed resweep, staged audit, writer audit, 331/332 backfill, delete-of-non-winners) | Remove the routing |
| Risk | Cheap now; the epic stalls here | New table to maintain; reads stay off the winners hot path | Rewrites reviewed PR-A machinery; unscoped O(page) rests on a new partial-index shape | Visibility regression for partial-scope grants |
| Storage | No change | One payload per support (moves, after slice 4, the bytes the fact rows hold today); winners keeps its payload copy | One payload per support in one table | No change |

Option E was considered and rejected: add an `all_support_scope_ids` array
to winners and admit a key when any support scope is granted. It restores
visibility but returns the winner's payload, which can come from an ungranted
scope, so it leaks across grants.

**Reading of the evidence.** The scoped fork and the write stop are the same
design problem. Slice 3 stops writing finding facts, so something per
`(scope, generation)` must replace them for the readers in section 2 that ask
"which scopes hold a finding for this CVE, advisory, or repository" and for
the writer's own stored read. A winners-only table cannot answer those. Option
B gives the scoped read, the holder and stored reads, and the P2 typed source
of truth in one structure. My recommendation is B, landed before the default
flip, with PR-B's unscoped work (explain, aggregates, advisory) proceeding on
winners meanwhile. Option A is acceptable only as a temporary safeguard for
the scoped and non-key-filter paths while B lands in the same goal; it cannot
be the end state. That means "retire the gate-off legacy list" cannot ship in
PR-B, because the scoped path runs through the same legacy query.

Two further decisions the ruling needs: (a) filter-then-pick or
pick-then-filter for unscoped non-key filters, and (b) whether `explain` and
the advisory evidence reader (which reads every support, and matches
`service_ids` and `workload_ids` per support) follow the legacy per-support
semantics or the canonical one.

## 2. Reader inventory at current main

Searched with `rg` for `reducer_supply_chain_impact_finding`,
`ReducerSupplyChainImpactFindingFactKind`, `FindingFactKind`, and
`supplyChainImpactFactKind`, excluding test files, then read each hit.
Symbols, not line numbers.

### Query readers (PR-B scope)

| Reader (file, symbol) | Capability served | State |
| --- | --- | --- |
| `go/internal/query/supply/chain/impact/findings_split_queries.go`, `ListFindingsQuery` via `listFindingsQueryTemplate` | `supply_chain.impact_findings.list` | Serves the gate-off path and every granted call. `ListFindingsProtectedQuery` is defined from the same template with protected mode on and has no production caller. |
| `findings_queries.go`, `ListFindingsFromWinnersQuery` | same | Winners path, gate on and ungranted only. |
| `aggregates_queries.go`, `AggregateCanonicalFactsCTE`, `AggregateCombinedCountQuery`, `InventoryQueryTemplate` | `supply_chain.impact_findings.aggregate` | Legacy path for gate off and granted calls. `AggregateCountQuery`, `AggregatePriorityCountQuery`, and `AggregateSeverityCountQuery` have no production caller after #10970; they are the pins the combined query replaced. |
| `aggregates_winners_queries.go`, `AggregateWinnersCombinedCountQuery`, `AggregateWinnersInventoryQuery` | same | Winners path. |
| `explain_postgres.go`, `ExplainFindingQuery`, `ExplainFindingByPublicIDQuery` (built by `buildExplainSupplyChainImpactFindingQuery`) | `supply_chain.impact_explanation.read` | Reads finding facts only. Its evidence hydration (`explainSupplyChainImpactAuthorizedEvidenceFactsQuery`) reads collector facts by id and does not need an impact index. |
| `suppression_split_sql.go`, `supplyChainImpactBoundedOperatorCandidatesCTE` | list, explain | Reads the operator-suppression scope's finding rows for the legacy overlay. Goes away with the legacy reads. |
| `query/supply/chain/advisory/evidence_sql.go`, CTEs `repo_probe` and `impact_candidates` inside `ListEvidenceQuery` | `supply_chain.advisory_evidence.list` | Reads finding facts across **every** support (grant tested on `fact.scope_id` and repository id) and matches `service_ids` and `workload_ids` from each support's payload. **Not named in the sequence.** The capability declares only `fact_records_collector`, so today's declaration is dishonest (section 3). |
| `grouped_inventory_sql.go` and `PostgresGroupedInventoryStore` | `supply_chain.impact_findings.aggregate` surface | Winners only for findings; its runtime legs read other reducer kinds. No finding-fact read remains. |
| `runtime_filter_sql.go` and the readiness queries | list, aggregate, explain, readiness | Read `reducer_workload_identity`, service-catalog, and CI/CD correlation facts. These are **other** reducer families. They keep `fact_records_reducer` declared for list and aggregate permanently. |

### Readiness target resolution is not a reader

The sequence listed it. At this main it reads `vulnerability.affected_package`
(`readinessAffectedPackageTargetQuery`), `sbom.document` and `sbom.component`
(`readinessDigestComponentTargetQuery`), `package_registry.package`
(`readinessPackageRegistryTargetQuery`), and
`container_image_identity_current_supports`
(`readinessImageRefDigestTargetQuery`). A search of the readiness files for
the finding kind and its constants found no hit. Nothing to move.

### Reducer and storage readers (they gate the write stop, slice 3)

These are not query handlers and no capability reads them directly, but each
needs finding rows per `(scope, generation)` and stops working when the fact
write stops. They are the reason slice 3 needs a per-support store.

| Reader (file, symbol) | What it does |
| --- | --- |
| `go/internal/reducer/securityalert/fact_bindings.go`, `reconciliation.go`; `storage/postgres/facts_active_security_alert_reconciliation.go`; `alert_reconciliation_keys.go` | Security-alert reconciliation loads active finding facts as input to compare provider alerts. Serves `supply_chain.security_alert_reconciliations.list` indirectly. |
| `storage/postgres/service_vulnerability_advisory_loader.go`; `reducer/servicecatalog/service_materialization_vulnerabilities.go` | Service vulnerability materialization reads findings per repository. |
| `storage/postgres/supply_chain_impact_fanout.go`, `supply_chain_impact_fanout_key_range.go`, `supply_chain_impact_fanout_mass.go`; `reducer/supplychain/core/fanout_key_range.go` | Holder reads: which scopes hold a finding for a CVE or advisory. |
| `supply_chain_impact_repository_holders.go`, `supply_chain_impact_scoped_packages.go`, `supply_chain_impact_out_of_stamp.go` | Same family: repository holders, scoped package seeds, out-of-stamp probe. |
| `storage/postgres/gomodules/fence.go` | The commit-skew fence reads which scopes hold findings for a repository. |
| `reducer/supplychain/core/writer.go` stored read, `supply_chain_impact_canonical_winners_store.go` | The writer's own diff and retraction input; the winners maintainers' input (`supply_chain_impact_winners_keyed_sql.go`, `supply_chain_impact_winners_staged_scan.go`, the resweep). |
| `storage/postgres/relationship_reference_keys_backfill.go` | Sidecar key backfill over the kind. |
| `supply_chain_impact_canonical_winners_store.go`, `AuthorizeProtectedScanImpactResults` and `PublishProtectedScanImpactResults` on `supplyChainImpactTx` | The protected publication pair runs inside the finding-fact write transaction. Slice 3 must re-home it (PR-C per the sequence). |
| `cmd/ifa/enqueue_advisory_groups.go`, `internal/ifa/familyodu`, `internal/ifa/materializededges/advisory_group.go` | Fixture and cassette plumbing for the advisory-group family. Re-point when the write path moves. |
| `internal/mcp/route_serves_data_registry*.go` | Route-to-kind markers; they follow the readers. |

PR-B moves only the query rows of the first table. The second table is
sequencing input for the fork ruling and for PR-C.

## 3. Layer-2 audit: a check that fails when a read uses an undeclared store

Acceptance criterion 1 of #10880 requires a check that fails when a read uses
a store its capability did not declare. Today
`TestCapabilityAnsweringStoresDeclaredForAllCapabilities` proves every
capability **declares** (layer 1), and `answeringStoreProbes` in
`go/internal/query/answering_store_lockstep_test.go` pins a sampled set of
declarations by hand-written evidence strings. Nothing derives the used set,
so a declaration can be wrong without any check failing. The advisory
evidence reader in section 2 is the first proof: its declaration omits
`fact_records_reducer` and nothing catches it.

### Why a pure static check does not work here

Query text is built at package init from package-level variables and
functions (`supplyChainImpactReadModeSQL`, `AggregateCanonicalFactsCTE`,
string concatenation with helper calls). A `go/ast` or constant-evaluation
pass cannot resolve those strings, and the kind is often a bind argument
(`fact.fact_kind = $1` with `FindingFactKind`). A pass that resolves only
literals would pass a read it could not see. A pass that is allowed to skip
what it cannot resolve is not a check.

### Design: record the real SQL, classify it, compare to the declaration

Two cooperating checks. The first is the primary.

**Check 1: recorded handler probes (per capability, precise).**

- A recording wrapper around `db.Queryer`, `db.RowQueryer`, and the graph
  reader port. It captures the exact SQL or Cypher text **and bind arguments**
  of every call and returns canned rows. The seam exists: the query stores
  depend on `db.Queryer`, `db.RowQueryer`, and `db.ReadStore`, and the
  handlers on `GraphQuery` and `ContentStore`.
- A per-capability probe table (extending `answeringStoreProbes`) drives the
  production handler or store through each branch that can pick a different
  store: gate on and off, granted and ungranted, each route. The mixed
  declarations (13 capabilities) need every variant, so a probe lists the
  variants it covers.
- A classifier turns each recorded statement into store tokens from the
  closed vocabulary:
  - SQL: take `FROM` and `JOIN` relations (CTE names excluded). `fact_records`
    resolves by its kind: a literal `fact_kind = '...'`, `= ANY($n)` or `= $n`
    resolved from the bound arguments, then mapped to
    `fact_records_reducer` when the kind starts with `reducer_`, otherwise to
    `fact_records_collector`. `facts.ReducerDerivedFactKinds()` alone is not
    enough: it lists eight governed kinds and does not name, for example,
    `reducer_workload_identity`. The classifier should cross-check the prefix
    rule against the generated fact-kind registry. An unresolved kind is its
    own failing token, so the classifier cannot silently under-report.
  - Any other relation becomes `read_table:<relation>` through an alias table
    for views and functions (for example `container_image_identity_current_supports`
    maps to `read_table:container_image_identity_supports`).
  - A closed structural allow-list (`ingestion_scopes`, `scope_generations`,
    and the other lifecycle joins) is ignored; each entry needs a stated reason.
  - Cypher `MATCH` becomes `graph`; content-store method calls become
    `content_search`; fixed status reads become `status_fixed`.
- The assertion is `used ⊆ declared`, from the loaded matrix
  (`capabilitycatalog.LoadMatrix`), failing with the capability, the relation
  and kind that was used, and the SQL's first line.

**Check 2: package census (static, complete, cheap).** A `go/ast` pass over
non-test files in each capability-owning package under `go/internal/query`
collects every string literal that contains a relation reference or a
`fact_kind` literal, and requires that the union of tokens found is covered
by the union of the declarations of the capabilities the package serves. It
over-approximates (it cannot tell which capability a literal belongs to), but
it needs no probe and cannot be bypassed by omitting one. A package that
holds a store no capability declares fails.

**Coverage rule.** Every matrix capability with an API or MCP surface must
have a probe entry or a waiver naming a reason; the waiver list can only
shrink (a test pins its length, the same ratchet the dirgate gate uses).

### Seeded RED/GREEN pair

Per the repository gate rule, the audit ships with a planted violation:

- RED: a fixture handler that reads `fact_records` with a reducer kind under
  a `graph`-only declaration. Check 1 and check 2 both fail on it.
- RED: the same handler under a declaration missing `fact_records_collector`
  after it reads a collector kind (kind-resolution case), and a handler whose
  kind arrives only as a bind argument (the argument-resolution case).
- GREEN: the clean tree, with the three impact capabilities declared as in
  the next paragraph.

### What the audit changes at PR-B

After the PR-B reader moves the declared sets should read, and the audit
verifies them:

| Capability | Declared stores after PR-B |
| --- | --- |
| `supply_chain.impact_findings.list` | `read_table:supply_chain_impact_canonical_winners`, `fact_records_reducer` (the runtime filter legs, permanent), plus the scoped table from the fork ruling |
| `supply_chain.impact_findings.aggregate` | same |
| `supply_chain.impact_explanation.read` | the same winners and scoped-table declaration, `fact_records_reducer` (runtime filter), `fact_records_collector` (evidence hydration) |
| `supply_chain.advisory_evidence.list` | add `fact_records_reducer` now; replace it with the scoped table when the reader moves |

The advisory evidence declaration is wrong today and is the audit's first
required fix.

Size and placement: the recorder, classifier, and census live in a new leaf
package under `go/internal/query` (for example `go/internal/query/store/audit`)
with its three package files; the probe table stays beside
`answeringStoreProbes`. Both new files and the existing lockstep test stay
under 500 lines by splitting the probe table per family.

## 4. What this record does not decide

Arbiter questions, in order:

1. Option A, B, C, or D for scoped and non-key-filter reads (recommendation B).
2. Filter-then-pick or pick-then-filter for unscoped non-key filters.
3. Per-support or canonical semantics for explain and for the advisory
   evidence reader.
4. Whether PR-B flips the default before B lands (recommendation: no; the
   flip would ship the unscoped divergence in section 1).

## NOT_CHECKED

- How often one canonical key has supports in more than one scope in a real
  deployment. The corpus is synthetic. The grouped-inventory comment reports
  0.2 to 0.5 percent of its unattributed bucket on a shim seed; there is no
  QA or production figure. This sizes the user impact, not the existence of
  the divergence.
- Latency, plan shape, and storage cost of options B and C. Design estimates
  only, per Section 1.
- Golden-corpus API and MCP equality (acceptance criterion 3). Not run in this
  phase.
- The grouped-inventory store's scoped divergence on this corpus. Its own code
  comment documents it; this probe did not exercise it.
- Whether the `ESHU_SUPPLY_CHAIN_IMPACT_WINNERS_READ` default is already on in
  any deployed profile.
- Static gates and `make pre-push`, per the owner order for this lane. Lint
  and vet were run on the doc-only change as described in the handoff report.

## B0 execution record (2026-11-22)

Arbiter ruling: option B for every grant-scoped and existence-filtered read,
canonical pick-then-filter for rank filters, any-support filter-then-pick for
existence filters; PR-B splits into B0..B3 and the legacy read retires in PR-C.
B0 lands the pieces that need no supports table. Commits on
`drive/10880-pr-b`, each test-first:

1. **Advisory evidence declaration.** `supply_chain.advisory_evidence.list`
   now declares `fact_records_collector` and `fact_records_reducer`. The
   lockstep probe failed on the old row first. The catalog regenerated and
   verified through the stub satellite harness, and the "source-only" note
   says impact findings are read only to select evidence.
2. **Multi-support live proof.** The corpus of section 1, plus two derived-class
   facts, is committed as a live test (`TestMultiSupportScopedGrantDivergenceLive`,
   `TestMultiSupportUnscopedRankFilterDivergenceLive`,
   `TestMultiSupportWinnersRecordsEverySupport`), enrolled in the
   live-postgres-readiness plan. It pins today's answers, so a later PR cannot
   change them silently: the legacy and winners answers for each grant shape and
   rank filter, the gate-on stores keeping granted calls on the legacy read, and
   derived-class facts appearing in no answer. One derived fact sits in an
   `image://derived/` scope and one in an ordinary scope, so scope-prefix
   exclusion and classification exclusion are each shown to hold alone; dropping
   the classification turns the proof red. The third grant leg in the legacy
   SQL, `OR finding_class.source_class = 'derived'`, is dead in static mode: the
   derived branch is closed by a literal `AND FALSE`, so no derived-class row
   reaches it. The PR-B3 expectations for rank filters change on purpose.
3. **Layer-2 audit** (`go/internal/query/store/audit`). A recorder, a
   classifier, a source census, and tests that read the real matrix.
   - Seeded RED/GREEN pairs: a reducer-kind read under a graph-only declaration,
     bind-argument kinds, an unresolvable `fact_records` read, and a fixture
     package for the census.
   - First real find: the advisory evidence declaration. Reverting it in the
     matrix fails three audit tests; with it restored they pass.
   - Probes drive impact list (gate off, gate on, scope grant, `image_ref`),
     aggregate and inventory (same variants), grouped inventory (with a static
     watermark row so it reaches its grouped statement), explain (by public id
     and by selector), advisory evidence (two anchors), catalog, and alert
     reconciliations. After the classifier drops the closed derived branch,
     every probe reads a subset of its declaration.
   - The census found what the probes cannot see: the impact package's readiness
     code reads eight tables no capability declares
     (`container_image_identity_supports`, three `package_manifest_consumption_*`
     tables, `package_registry_identity_keys`, two `vulnerability_source_state*`
     tables, `workflow_runs`). They feed the `readiness_state` the list and
     explain responses carry. B0 lists them as supporting tables with the reason
     rather than invent declarations; whether list and explain should declare
     them is a question for the arbiter.
   - Waivers: 156 capabilities have no probe yet (162 minus six probed) and 18
     query packages have no census owner. Both lists can only shrink; the
     ceilings are pinned in `audit_test.go`.
   - Not recorded yet: graph and content-store reads. Those capabilities sit in
     the waiver list until a graph recorder lands.
4. **`image_ref` guard.** The gate-on list and aggregate stores serve an
   `image_ref` call from the legacy read; grouped inventory, which has no legacy
   read, answers `ErrGroupedInventoryReadModelUnavailable` (503). The tests
   failed before the guard.
5. **Profile proof: supports of one key can differ.** The detection profile
   is a function of status, observed version, and match reason. The first two
   are in the canonical key; the match reason is not, and the package path never
   sets the criteria ids. On the OS path a vendor exact-version list and a tagged
   OSV range both give `affected_exact` for one observed version and release
   line, with reasons `dpkg_exact_affected_version` (precise) and
   `dpkg_affected_range` (comprehensive).
   `TestDetectionProfileIsAPerSupportOutputOfOneCanonicalKey` pins it. No writer
   invariant can hold, so B0 adds none. A `profile` filter answered from the
   winners pick can hide a finding whose other support is precise; B2 serves it
   as an existence filter from supports, and the arbiter ruling for the question
   is pending.
6. **Gate text and HTTP reference.** The `ESHU_SUPPLY_CHAIN_IMPACT_WINNERS_READ`
   description no longer says the findings output is byte-identical; the env
   reference regenerated; the inventory HTTP reference gained a section on rank
   and existence filters on multi-scope findings and a corrected "bodies match"
   sentence.

### What B0 does not do

No supports table, no reader switch, no default flip, no change to the legacy
read. The scope-id grant divergence and the rank-filter divergence stay in the
code exactly as section 1 measured them, now pinned by the live proof.

### NOT_CHECKED in B0

- The live proofs ran against a private `postgres:18` container; no QA or
  production census of multi-support keys exists.
- Golden-corpus API and MCP equality (acceptance criterion 3) and the
  read-latency gate are not part of B0.
- The census is a floor: a SQL literal outside the `KnownRelations` set, or a
  query built without string literals, is invisible to it.
