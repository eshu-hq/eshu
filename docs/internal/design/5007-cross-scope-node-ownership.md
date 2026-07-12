# Cross-scope same-uid node ownership — deterministic merge for scope-derived properties

Status: Accepted and implemented. Stage 1 enforcement = the Postgres
**owner-ledger with a per-uid advisory lock** (design (b) below), shipped as the
`graph_node_owner` table (migration 056), the `postgres.GraphNodeOwnerStore`,
and the `internal/graphowner` gate wrapping the five families' node writers in
`cmd/reducer`. The original graph-side guard was disproven on NornicDB by the
mandatory prove-theory shim
(see [Prove-theory result](#prove-theory-result-stage-1-graph-side-guard-is-not-viable-on-nornicdb)),
and the lock-free ledger variant (design (a)) was disproven by a forced
worst-case interleaving; the advisory-lock variant (design (b)) is proven
lost-update-free and convergent in BOTH the ledger and the graph
(see [Owner-ledger prove-theory result](#owner-ledger-prove-theory-result-design-b-is-safe)).
The underlying NornicDB concurrent-property-write limitation is tracked
separately as **#5062**.
Audience: Eshu maintainers
Companion issue: #5007 (filed during Ifá P3, #4396; prerequisite for P6
fault injection over overlapping-identity inputs, #4580)
Related issue: #5062 (NornicDB does not reliably detect concurrent
property-write conflicts on a shared existing node — the root limitation that
forces the owner-ledger design)
Parent design: [Ifá conformance platform](4389-ifa-conformance-platform.md)

Every structural claim below cites `file:line` against `origin/main`
(`b69361d6e`). Claims without a citation are design proposals, not
current-state descriptions.

## Maintainer decisions (accepted)

1. **Order key = latest observation.** The deterministic order key is
   `max (observed_at, source_fact_id)` — a fixed-width UTC nanosecond timestamp
   concatenated with the stable `source_fact_id`, so string comparison agrees
   with chronological order and ties break on the stable fact id. (Open
   Question 1 → recommended option.)
2. **Scope = all resource families in Stage 1.** CloudResource (AWS/GCP/Azure),
   the EC2-instance node writer (`ec2_instance_node_rows.go:167`), and the
   Kubernetes-workload node writer (`kubernetes_workload_materialization.go:312`)
   all adopt the same rule. (Open Question 3 → all families.)
3. **Stage 2 (per-scope provenance satellites) trails this change.** It is a
   separate follow-up and does not block P6. (Open Question 2 → trail.)
4. **Contention Odù covers both** identical-payload same-uid (pure envelope
   contention) and divergent-payload same-uid (divergent observed state).
   (Open Question 4 → both.)

These decisions stand. What does NOT stand is the *enforcement mechanic* Stage 1
proposed (a graph-side guarded `SET`): the mandatory prove-theory shim disproved
it on NornicDB. See the new section below.

---

## Problem

When two ingestion scopes carry the same resource payload identity, both
scopes' reducer intents project the same canonical node uid, the canonical
writer `MERGE`s one node, and then flat-`SET`s every property — so all
scope-derived single-value properties are last-writer-wins by commit order:

- The canonical writer MERGEs on uid only and SETs everything else
  unconditionally: `MERGE (r:CloudResource {uid: row.uid})` followed by a
  flat `SET` of 22 properties, including `source_fact_id`,
  `stable_fact_key`, `source_system`, `source_record_id`,
  `source_confidence`, `collector_kind`, and `evidence_source`
  (`go/internal/storage/cypher/cloud_resource_node_writer.go:21-46`; the
  executed statement is the same constant plus the teeth no-op,
  `cloud_resource_node_writer.go:56`).
- The GCP row builder stamps the writing fact's envelope metadata into those
  properties (`go/internal/reducer/gcp_resource_materialization.go:305-327`,
  `source_fact_id` at `:321`); uid derives from payload identity only —
  `cloudResourceUID(projectID, location, assetType, fullResourceName)`
  (`gcp_resource_materialization.go:304`).
- The AWS row builder is the same shape
  (`go/internal/reducer/aws_resource_materialization.go:282-304`,
  `source_fact_id` at `:298`), with service-anchor fields merged in
  (`aws_resource_materialization.go:305-307`,
  `go/internal/reducer/aws_resource_service_anchor.go:33-54`); uid derives
  from `(account_id, region, resource_type, resource_id)`
  (`aws_resource_materialization.go:333-340`).
- Azure, EC2-instance, and Kubernetes-workload materializers repeat the
  pattern (`go/internal/reducer/azure_resource_materialization.go:203-224`,
  `source_fact_id` at `:218`;
  `go/internal/reducer/ec2_instance_node_rows.go:167`;
  `go/internal/reducer/kubernetes_workload_materialization.go:312`).

Cross-scope concurrency is by design, not an accident. The reducer's claim
fence is scoped per scope: the AWS resource domain is classified `safe` with
a conflict key hashed from the per-scope entity key
`aws_resource_materialization:<scope>`
(`go/internal/storage/postgres/reducer_queue_conflict.go:62-67`, `:256-265`);
GCP/Azure fall back to the per-scope `resource_scope` key (`:69-77`,
`:171`, `:267-269`). Intents from two different scopes never share a
conflict key (the entity key embeds the scope id,
`go/internal/projector/gcp_resource_materialization_intents.go:37`), so two
scopes' writes to the same uid run on concurrent reducer workers, in
separate transactions, with no ordering between them. The domain definitions
declare `CrossScope: true, CanonicalWrite: true` — shared cross-scope nodes
are the intended model
(`aws_resource_materialization.go:30-34`,
`gcp_resource_materialization.go:31-35`).

The consequence for Ifá: the graph digest compares every node property —
`source_fact_id` is explicitly a pass-through content key
(`go/internal/ifa/graphdump/normalize.go:32-36`) — so a determinism matrix
over deliberately overlapping-identity inputs (a "contention Odù") goes red
with no actual bug. This was identified as the P5 amplifier landmine and
deferred to this decision
(`docs/internal/design/4389-ifa-conformance-platform.md`, Layer 3 landmine
and Open Questions). Until ownership semantics are decided and enforced,
overlapping-identity inputs are unusable in the P6 fault-injection matrix.

There is also a smaller within-scope instance of the same order dependence:
duplicate facts for one uid inside a single generation resolve by slice
order ("last fact for a uid wins",
`aws_resource_materialization.go:238-240`,
`gcp_resource_materialization.go:248-250`), which leans on `LoadFacts`'
`observed_at` ordering with unspecified tie behavior. Any rule adopted here
should be applied there too.

## Property classification (current writer)

All properties the canonical CloudResource writer SETs
(`cloud_resource_node_writer.go:23-46`), classified by cross-scope
stability:

| Class | Properties | Cross-scope behavior today |
|---|---|---|
| IDENTITY (uid inputs; identical for same-uid writers by construction) | `uid`, `id` (= uid), `resource_type`, `account_id`, `region`, `resource_id` | LWW, but harmless: every writer writes the same values (`aws_resource_materialization.go:333-340`, `gcp_resource_materialization.go:304`) |
| OBSERVED STATE (payload-derived; identical when payloads are identical, may diverge across scans/times) | `arn` (not a uid input; AWS falls back `resource_id`→arn at `aws_resource_materialization.go:278-281`, so one scope can carry an empty `arn` for the same uid), `name`, `state`, `service_kind`, `correlation_anchors`, `workload_id`, `service_name`, `service_anchor_status`, `service_anchor_source`, `service_anchor_reason`, `service_anchor_names`, `service_anchor_name_tokens` | LWW, order-dependent whenever payloads diverge |
| SCOPE-DERIVED PROVENANCE (envelope-derived; always differ per contributing fact/scope) | `source_fact_id`, `stable_fact_key`, `source_record_id`, `source_system`, `source_confidence`, `collector_kind` | LWW, order-dependent even for byte-identical payloads — this is the #5007 red |
| WRITER-FAMILY TAG | `evidence_source` (annotated per batch, `cloud_resource_node_writer.go:93-101`; constants at `aws_resource_materialization.go:47`, `gcp_resource_materialization.go:50`) | Constant per provider family; families cannot share a uid because `resource_type`/identity feed `facts.StableID` (`go/internal/facts/stableid.go:15-26`), so effectively stable |

Multi-valued vs single-valued today: only `correlation_anchors`,
`service_anchor_names` are lists, and they are normalized whole-values from
the one winning fact (`uniqueSortedStrings`,
`aws_resource_materialization.go:297`,
`gcp_resource_materialization.go:320`) — they are not cross-scope unions.
Nothing on this node is a cross-scope union today.

How the rest of Eshu models multi-source evidence: reducer-side sorted-set
unions written as whole values —
`evidence_fact_ids: uniqueSortedStrings(...)`
(`go/internal/reducer/service_catalog_correlation_writer.go:134`,
`go/internal/reducer/secrets_iam_trust_chain_writer.go:168-220`,
`go/internal/reducer/incident_repository_correlation_writer.go:172`,
`go/internal/reducer/ci_cd_run_correlation_writer.go:123`) and
`related_scope_ids`/`entity_keys` unions
(`go/internal/reducer/workload_identity_writer.go:164-165`, `:200-201`).
The critical structural difference: every one of those unions is computed
inside one handler invocation over inputs that handler loaded, then written
as a whole value. Cross-scope CloudResource contention is the opposite
shape — the two writers are different intents on different workers that
never see each other's facts, so a reducer-side union is not available;
any union must be either graph-side read-modify-write (a lost-update
hazard, see option c) or structurally per-scope-keyed.

Query-truth blast radius of the node's provenance properties is small:
the query surfaces that return `source_fact_id`/`stable_fact_key` to users
read them from relationship properties, not from the CloudResource node
(`go/internal/query/compare.go:207-216` reads `r.*` off the `USES` edge;
`go/internal/query/impact_trace_deployment_resources.go:122-123` and
`go/internal/query/cloud_resource_dependencies.go:43-44` read `rel.*`).
The node's scope-derived properties are canonical graph truth (operator
Cypher, Ifá digest), not a wired API field — so changing how the node's
value is *chosen* does not change any HTTP/MCP response shape, and even
changing its *shape* would not ripple into the OpenAPI surface. The B-12
snapshot asserts no `source_fact_id` anywhere
(`rg -c source_fact_id testdata/golden/e2e-20repo-snapshot.json` = 0
matches, verified on `b69361d6e`).

There is no scope-wide node retract to interact with: the `safe`
classification for the AWS domain rests on "idempotent by CloudResource uid
and has no scope-wide retract"
(`reducer_queue_conflict.go:66`); prior-generation retraction is an
edge-level mechanism filtered by `scope_id` + `evidence_source`
(`go/internal/storage/cypher/cloud_resource_edge_writer.go:44`, `:68`) over
the projected source ledger
(`go/internal/reducer/projected_source_ledger.go:11-57`). A shared node
survives either scope's re-ingestion; only its property values are
contested.

## Determinism contract being decided

A shared node's canonical truth MUST be a deterministic function of the
*set* of contributing facts, never of their arrival/commit order or of the
reducer worker count. This is the graph-level form of the Ifá Layer-2
contract (byte-identical canonical graph across N workers,
`4389-ifa-conformance-platform.md`, Layer 2) applied to cross-scope inputs.

## Options

### (a) Last-writer-wins made deterministic (stable-ordered "last")

Keep single values; define "last" by a stable total order over contributing
facts instead of commit order — e.g. max `source_fact_id`, or better
max `(observed_at, source_fact_id)` — and make the writer's SET conditional:
overwrite the scope-derived group only when the incoming row's order key is
greater than the stored one.

- Provenance: still drops the losing scopes' provenance (same loss as
  today, now deterministic).
- Determinism under N workers: yes *if and only if* the guard itself is
  race-free. A guarded SET is a graph-side read-modify-write; under
  concurrent transactions the classic lost-update interleaving (both
  writers read the pre-image, both pass the guard, lower key commits last)
  must be ruled out against the actual backend's lock/evaluation semantics.
  Neither NornicDB's nor Neo4j's property-write locking may be assumed —
  the repo's Cypher discipline requires research against pinned backend
  source plus a measured contention proof before this ships
  (`docs/public/reference/cypher-performance.md` mandate;
  `docs/public/reference/nornicdb-pitfalls.md` documents adjacent MERGE
  re-projection asymmetries). This proof burden is the option's main cost,
  and it is shared with (c1) below.
- Multi-source modeling fit: none — still single-source truth on a
  multi-source node.
- Query truth: a user can trace exactly one contributing scope; the others
  are invisible. With max-`source_fact_id` ordering the surviving
  provenance is semantically arbitrary (fact ids are hashes, not
  recency); the "winner" can be the stale scan.

### (b) First-writer-wins (deterministically ordered)

Two sub-variants, both rejected:

- Commit-order first writer (`ON CREATE SET` for the scope-derived group,
  never touched `ON MATCH`) is race-safe and cheap — property write is
  atomic with node creation — but the winner is whichever scope's
  transaction creates the node first, i.e. still commit-order-dependent.
  It fails the determinism contract outright.
- Deterministic first writer (min order key) needs exactly the same guard
  machinery as (a), and then *prefers the stalest observation forever*: a
  node observed again with newer state keeps the oldest scope's
  `state`/`name`/anchors. That is accuracy-inverted for OBSERVED STATE and
  merely arbitrary for provenance. Rejected on the accuracy-first motto.

### (c) Deterministic merge (recommended)

Split by the classification table, because the three classes have three
different correct semantics:

- **(c1) Deterministically-resolved single values** for OBSERVED STATE and
  the single provenance slot: the whole scope-derived group (both classes,
  one shared guard so a node can never hold a torn mix of scope A's state
  and scope B's provenance) resolves to the contributor with the maximum
  order key `(observed_at, source_fact_id)`. "Latest observation wins" is
  the semantically right rule for OBSERVED STATE (a newer scan of the same
  resource is better truth), and the `source_fact_id` tiebreak makes
  identical-timestamp synthetic inputs (the contention Odù) total-ordered.
  Mechanically this is option (a)'s guard with a meaningful key; the
  IDENTITY class needs no guard (all writers agree by construction).
  Requires stamping the order key onto the node (one new property, e.g.
  `source_order_key`; `observed_at` is not currently written to the node,
  `cloud_resource_node_writer.go:23-46`).
- **(c2) Provenance preserved from all contributing scopes**, keyed
  per scope rather than unioned in place. In-place list union
  (`SET r.source_fact_ids = r.source_fact_ids + ...`) is rejected as the
  mechanism: it is the same read-modify-write lost-update hazard as the
  guard but on every write, it grows unboundedly across generations with
  no retract hook (node-level retraction does not exist, above), and
  portable Cypher has no sorted-set primitive, so the stored value would be
  order-sensitive anyway. The structurally idempotent alternative is a
  per-scope provenance record MERGEd on its own composite key
  `(uid, scope_id)` — each scope writes only its own record, retries
  converge on it, and no two scopes ever contest one value. Two candidate
  homes, deliberately left open for the maintainer:
  - a graph satellite (e.g. `(:CloudResource)<-[:OBSERVED_BY]-` per-scope
    provenance node/edge carrying `source_fact_id`, `stable_fact_key`,
    `source_system`, `source_record_id`, `source_confidence`,
    `collector_kind`, `observed_at`) — queryable next to the node, but new
    graph shape (golden-corpus impact, orphan lifecycle);
  - a Postgres ledger following the proven `ProjectedSourceLedger` pattern
    (`projected_source_ledger.go:25-57`) — transactionally boring,
    invisible to the graph digest, but provenance is then not answerable
    from Cypher alone.
- Determinism under N workers: (c2) is order-independent by construction;
  (c1) carries the same guard-concurrency proof burden as (a).
- Multi-source modeling fit: exact — per-scope records are the cross-scope
  analogue of the `evidence_fact_ids` sorted-set discipline, adapted to
  writers that cannot see each other's inputs.
- Query truth: full — a user can trace which scope contributed what, and
  the node's single-value slots have a stated, documented semantic
  ("latest observation").

### (d) Treat cross-scope same-uid as a conflict (quarantine / separate nodes)

Rejected. A shared node for the same real-world resource is the product
premise, not a defect: the domains declare `CrossScope: true`
(`aws_resource_materialization.go:30-34`), and the uid is deliberately
recomputable from relationship facts so edge projection joins onto the one
node (`aws_resource_materialization.go:329-340`). Per-scope nodes would
split every edge join target and make *edge* placement commit-order- or
scope-dependent instead; quarantining would turn legitimate multi-account /
multi-scan overlap — routine at fleet scale — into dead letters. Both
variants buy determinism by hiding or refusing correct correlation, which
the life motto forbids (reliability via hidden truth). Quarantine remains
correct only for the existing malformed-input path
(`gcp_resource_materialization.go:122-127`), which is orthogonal.

## Decision (recommended)

**Adopt (c), staged; stage 1 alone closes #5007 and unblocks P6.**

- **Stage 1 — deterministic group resolution (the enforcement #5007
  requires).** Stamp `(observed_at, source_fact_id)` as a single
  lexicographically comparable order key on every CloudResource row; guard
  the entire scope-derived group (OBSERVED STATE + provenance slot +
  order key itself) behind one shared `>=`-on-order-key condition in
  `canonicalCloudResourceUpsertCypher`. Final node state becomes the
  max-key contributor regardless of commit order or worker count. No wire
  contract changes: same properties, same shapes, one added node property.
  Apply the same max-order rule to within-scope duplicate-uid resolution in
  the extractors (`aws_resource_materialization.go:238-240`,
  `gcp_resource_materialization.go:248-250`) so the rule is one rule, not
  two.
- **Stage 2 — per-scope provenance preservation (product truth,
  separate PR).** Add the per-scope provenance record keyed on
  `(uid, scope_id)`; storage home (graph satellite vs Postgres ledger) is
  Open Question 2. Stage 2 is not required for the contention Odù to be a
  valid determinism assertion — Stage 1's resolved node state is already
  order-independent — so P6 does not block on it.

Rationale: (c) is the only option that satisfies all three mottos at once —
accuracy (latest observation is the stated semantic; no scope's evidence is
silently discarded once Stage 2 lands), performance (guard cost is a
per-row CASE in an already-batched UNWIND, measurable before merge), and
concurrency (no serialization anywhere: the per-scope conflict fence,
including the AWS `safe`/`cloud_resource_node` promotion, is untouched;
determinism comes from making the write order-*independent*, not
order-*controlled*). (a) is strictly a subset of (c) with worse semantics;
(b) and (d) fail accuracy outright.

### Mandatory proofs before Stage 1 lands (prove-theory-first)

1. **Guard concurrency shim** (this is the theory to prove *before*
   building): on a live NornicDB (the pinned fork the compose stack runs)
   and Neo4j-compat
   backend, two concurrent transactions MERGE the same uid and SET guarded
   groups with opposite key orders, both interleavings, N repetitions;
   assert the final node always equals the max-key contributor and no
   lost update occurs. If the naive guarded `SET ... CASE` is not
   lock-safe on either backend, the fallback design (still option (c),
   different mechanics) is a retry-on-conflict loop keyed off a re-read, or
   routing the resolution through the row builders' input (both scopes'
   facts) via a cross-scope resolution pass — measured before chosen.
2. **Perf differential** on the canonical writer benchmark: OLD flat-SET vs
   NEW guarded-SET on the same batch data, plus row-set equivalence on a
   single-writer corpus (single-scope inputs must produce byte-identical
   graphs before/after — the guard must be invisible when there is no
   contention).
3. **Contention Odù regression (the new test this ADR specifies):** a
   synthetic cassette pair with deliberately overlapping payload identity
   across K scopes (the inverse of `go/internal/synth/gcp`'s
   `GenerateMultiScope` disjointness-by-construction), driven through
   `scripts/verify-ifa-determinism.sh`'s N ∈ {1,2,4} matrix; assert
   byte-identical `graphdump` digests across all N. Written failing-first:
   it must go red on today's writer (reproducing #5007) and green only with
   the guard.

### Prove-theory result: Stage 1 graph-side guard is NOT viable on NornicDB

The mandatory guard concurrency shim (proof 1 above) was run against a pinned
standalone NornicDB (`timothyswt/nornicdb-cpu-bge:v1.1.9`) and Neo4j
(`neo4j:2026-community`), both on isolated host ports. It DISPROVED the
graph-side guard on the default backend. Two candidate mechanics were tested;
both fail on NornicDB:

**(1) Naive per-property `CASE`-guarded `SET` — does not evaluate on NornicDB.**
The shape
`SET r.arn = CASE WHEN r.source_order_key IS NULL OR row.source_order_key > r.source_order_key THEN row.arn ELSE r.arn END`
persists the LITERAL STRING `"r.arn"` on NornicDB, not the value. Isolated with
Bolt-HTTP `tx/commit` probes:

- A `CASE` that references an `UNWIND` `row.field` AND a node-property
  reference in `ELSE` (`ELSE r.arn`) is not evaluated — NornicDB stringifies
  the whole expression (the same class as the `#4995` "stringified row
  expression" behavior). Confirmed for `row.field` in the `WHEN` and in the
  `THEN`; a `CASE` with only scalar `$params` (no `row.field`) or with a
  literal `ELSE` evaluates correctly.
- NornicDB also has NO `FOREACH` (the standard conditional-write idiom; it
  swallows the clause into the preceding `SET` string), does NOT persist a
  `SET` that follows a `WITH` in an `UNWIND`+`MERGE` pipeline, and does NOT
  create a node from a `MERGE` that follows a `WITH`-projection. So the guard
  cannot be expressed as a single cheap statement.

**(2) `MATCH … WHERE … SET` conditional-update (the ADR's stated fallback) —
not lost-update-safe on NornicDB.** Split into a two-statement group
(pass 1: `MERGE` + unconditional identity `SET`; pass 2:
`MATCH (r {uid}) WHERE r.source_order_key IS NULL OR row.source_order_key > r.source_order_key SET r.arn = row.arn, …`,
plain `SET` filtered by `WHERE`, no `CASE`). This evaluates correctly
single-threaded (fresh/higher-key/lower-key all resolve to the max-key winner)
and pass 2's `MATCH` sees pass 1's `MERGE` in the same transaction. BUT under
two concurrent writers racing the same uid with different order keys, WITH a
retry-on-`Outdated` loop:

- **NornicDB: 26/100 concurrent trials lost the update** — both writers report
  success yet the lower-order-key writer's value is the final state. NornicDB's
  MVCC "changed after transaction start" conflict detection misses ~26% of
  concurrent property-write conflicts on a shared *existing* node. (Without
  retry, 26/50 lost, with the conflict surfaced on the loser; with retry, the
  same 26% lose silently because the conflict is never raised.) The commit-time
  UNIQUE check only fires on concurrent node *creation* (cold), not on warm
  contended property writes.
- **Neo4j: 0/50 concurrent trials lost** — the identical mechanic is fully
  lost-update-free on a conformant backend, isolating the defect to NornicDB.

Commands run (representative; full probes were Bolt-HTTP `POST /db/nornic/tx/commit`
and a Go two-writer harness through the real `RetryingExecutor`):

```
# standalone pinned backends on isolated ports
docker run -d --name eshu5007-nornicdb -p 17687:7687 -p 17474:7474 \
  -e NORNICDB_NO_AUTH=true -e NORNICDB_ASYNC_WRITES_ENABLED=false ... \
  timothyswt/nornicdb-cpu-bge:v1.1.9
docker run -d --name eshu5007-neo4j -p 17688:7687 -e NEO4J_AUTH=neo4j/... neo4j:2026-community
# concurrent two-writer contention over one uid, seed -> low/high race, with retry-on-Outdated
#   NornicDB: 26/100 lost   Neo4j: 0/50 lost
```

**Conclusion.** The decided *semantics* (max `(observed_at, source_fact_id)`
wins) stand, but they CANNOT be enforced by any graph-side write on NornicDB:
the backend neither evaluates the conditional guard nor reliably detects the
concurrent conflict a retry loop would need. Per the repo's
Serialization-Is-Not-A-Fix doctrine, `WORKERS=1` is not an option.

The remaining fallback the ADR names — "routing the resolution through the row
builders' input via a cross-scope resolution pass" — must become an
**application-level coordination** primitive that does NOT rely on NornicDB
conflict detection. The recommended shape, following the proven `Package.uid`
precedent ([NornicDB Pitfalls](../../public/reference/nornicdb-pitfalls.md),
"concurrent MERGE" → Postgres transaction-scoped advisory lock) and the
`ProjectedSourceLedger` pattern (`projected_source_ledger.go:25-57`):

- a Postgres-side per-uid resolution (advisory lock or `ON CONFLICT … WHERE
  excluded.source_order_key > cloud_resource_owner.source_order_key` upsert)
  that records the current max-order-key owner reliably (Postgres row locking
  is trustworthy), and gates the graph write so only the winning contributor's
  scope-derived `SET` reaches NornicDB. The within-scope extractor tie-break
  (max order key over duplicate uids in one generation) is orthogonal and safe
  to land independently.

This is a Stage 1 mechanic change the maintainer must approve before
implementation resumes. The within-scope order-key computation, the contention
synth cassette (overlapping-identity, the inverse of `GenerateMultiScope`), and
the determinism regression tests are all reusable once the coordination
primitive is chosen; only the graph write path changes.

### Re-confirmation on NornicDB v1.1.11 (issue #5062)

The graph-side disproof was re-run on **NornicDB v1.1.11** ("Black Hole Sun",
2026-07-09 — the release that carries the #259 Cypher/Bolt fixes), against
**v1.1.9** as a control, using the same pure graph-side mechanic
(`go/internal/storage/cypher/graph_guard_prove_theory_live_test.go`,
`TestLiveGraphGuardProveTheory`, gated by `ESHU_GRAPH_GUARD_PROVE_LIVE=1`). Each
writer holds one explicit transaction across a 5ms read-modify-write gap so the
two writers' windows overlap; two writers race one pre-seeded uid, retry on
`Outdated`, 100 trials:

- **v1.1.9 (control): 5/100 silently lost** the max winner.
- **v1.1.11: 6/100 silently lost** — statistically identical.

The rate is lower than the ADR's original 26/100 because this shim is gentler
than the original RetryingExecutor two-statement-group harness (2 writers, one
tx, 5ms gap); the point is the qualitative verdict, which is firm and matched by
a source read (the node-property conflict-detection path in NornicDB
`pkg/storage/badger_transaction.go` is byte-identical across v1.1.9..v1.1.11 —
the #259 fix is parser/Bolt only). **v1.1.11 does NOT fix the concurrent
shared-existing-node property-write loss.** The Postgres coordination primitive
below remains required on the default backend. (First measured with a
too-narrow single-statement shim that reported 0/100 on BOTH versions — a false
negative; only widening the transaction window reproduced the loss, and only
then did the control confirm the shim was valid.)

### Owner-ledger prove-theory result: design (b) is safe

The maintainer approved the Postgres owner-ledger redesign. The mandatory
prove-theory gate was re-run for the END-TO-END owner-ledger path (Postgres
ledger + NornicDB graph) against the pinned standalone Postgres 18 + NornicDB
v1.1.9. Two designs were measured; the numbers pick design (b):

**Design (a) — lock-free ledger-derived graph write.** Each writer upserts the
owner ledger (`INSERT … ON CONFLICT (uid) DO UPDATE … WHERE
excluded.source_order_key > owner.source_order_key`, which Postgres row-locking
makes an atomic max resolution), reads back the current ledger winner, then
writes the winner's values to the graph. Theory: concurrent graph writes are
idempotent because they all write the ledger-winning values.

- Random N=4 concurrent trials: 0/100 ledger lost, 0/100 graph lost — BUT this
  passes only because the race window is narrow.
- **Forced worst-case interleaving: the graph lost the update deterministically
  (100%).** With `low` reading the ledger (=low), then `high` completing its
  full upsert+read+graph-write, then `low` writing its stale read to the graph,
  the graph is left at the LOSING value. The stale-read window between the
  ledger read and the graph write is a REAL race, not merely rare. Design (a)
  is rejected.

**Design (b) — per-uid advisory-lock serialized (adopted).** The same steps
wrapped in a `pg_advisory_xact_lock(hash(uid))` held across the ledger-decide +
graph write, so writers to the same uid serialize and the last lock holder
reads the converged ledger and writes it. This also eliminates the concurrent
graph-MERGE-create race as a bonus (only one writer touches a uid at a time).

- Random N=4 concurrent trials: **0/100 ledger lost, 0/100 graph lost, 0 writer
  errors.**
- **Forced worst-case interleaving: converges to the max** — the lock makes the
  design-(a) interleaving impossible.
- This is `partition by conflict key`, not a global worker reduction, so it is
  permitted under Serialization-Is-Not-A-Fix.

**Perf.** Naively acquiring one advisory lock per uid cost 14.3x (500
round-trips). Acquiring ALL per-uid locks in ONE sorted statement
(`SELECT pg_advisory_xact_lock(k) FROM (SELECT DISTINCT k FROM unnest($1) ORDER
BY k) s` — sorted so overlapping batches cannot deadlock) drops it to **2.28x**
(flat graph-only 6.0ms vs ledger+lock+graph 13.7ms for a 500-uid batch,
15µs/row). The `RETURNING`-on-the-upsert idea (skip the winner read-back on the
non-contended case) was subsequently measured and rejected as a material win —
see [Perf optimization (RETURNING)](#perf-optimization-returning-measured-rejected-as-a-win),
which also re-frames this 2.28x against a realistic value-changing graph floor
(~1.3x). CloudResource is low-cardinality relative to File/Function, so the
absolute per-scope impact is small; the perf differential on the real writer is
measured before merge.

Shim commands (recorded evidence,
`go/internal/storage/cypher/owner_ledger_prove_theory_live_test.go`):

```
docker run -d --name eshu5007-pg -p 15636:5432 -e POSTGRES_DB=eshu \
  -e POSTGRES_USER=eshu -e POSTGRES_PASSWORD=change-me postgres:18-alpine
docker run -d --name eshu5007-nornicdb -p 17687:7687 ... nornicdb-cpu-bge:v1.1.9
ESHU_OWNER_LEDGER_PROVE_LIVE=1 ESHU_OWNER_LEDGER_PG_DSN=postgresql://... \
ESHU_GRAPH_BACKEND=nornicdb ESHU_NEO4J_URI=bolt://localhost:17687 ... \
go test ./internal/storage/cypher -run 'TestLiveOwnerLedger' -v
#   a_lockfree/a_widened_window: 0/100 random; FORCED worst-case = 100% graph loss
#   b_advisory_lock:            0/100 random; FORCED worst-case = converges
#   batch perf: flat 6.0ms vs design(b) 13.7ms (2.28x) for 500 uids
```

### Perf optimization (RETURNING): measured, rejected as a win

The maintainer asked for the write path to be cheaper before the 5-family
build, starting with the `RETURNING` idea from the section above. The
prove-theory gate was re-run for the RETURNING variants against the same pinned
standalone Postgres 18 + NornicDB v1.1.9 (fresh isolated containers,
`eshu5007ret-pg` on 15637, `eshu5007ret-nornicdb` on 17689). Result:
**rejected hypothesis as a perf win, but the measurement corrected the cost
model** — the honest write-path number for the full build is below.

**Exact Postgres RETURNING semantics (proven, not assumed).** Two upsert
mechanics were probed
(`owner_ledger_returning_semantics_live_test.go`):

```sql
-- (i) WHERE-guarded + RETURNING: identical semantics to the proven upsert
INSERT INTO cloud_resource_owner (uid, source_order_key, value, updated_at)
VALUES ($1, $2, $3, now())
ON CONFLICT (uid) DO UPDATE
    SET source_order_key = EXCLUDED.source_order_key, value = EXCLUDED.value, updated_at = EXCLUDED.updated_at
    WHERE EXCLUDED.source_order_key > cloud_resource_owner.source_order_key
RETURNING uid, source_order_key, value
```

Proven on Postgres 18: when the `DO UPDATE ... WHERE` is false (losing or
equal-key writer), the row is **not** updated and `RETURNING` yields **no row**
— the losing writer learns nothing about the current winner from the statement
alone and still needs a fallback read for the omitted uids.

```sql
-- (ii) CASE-always-update + RETURNING: the conflict arm always fires
INSERT INTO cloud_resource_owner (uid, source_order_key, value, updated_at)
VALUES ($1, $2, $3, now())
ON CONFLICT (uid) DO UPDATE
    SET source_order_key = CASE WHEN EXCLUDED.source_order_key > cloud_resource_owner.source_order_key
            THEN EXCLUDED.source_order_key ELSE cloud_resource_owner.source_order_key END,
        value = CASE WHEN EXCLUDED.source_order_key > cloud_resource_owner.source_order_key
            THEN EXCLUDED.value ELSE cloud_resource_owner.value END,
        updated_at = now()
RETURNING uid, source_order_key, value
```

Proven: `RETURNING` **always** yields the post-update row — the current winner
— even for a losing writer, eliminating every read-back; the cost is a
self-overwrite heap-tuple version on losing/equal-key rows.

**Correctness re-proof (the optimization must not buy speed with a race).**
`owner_ledger_returning_correctness_live_test.go`:

- N=4 concurrent writers × 100 trials, advisory lock + CASE-RETURNING upsert +
  graph write: **0/100 ledger lost, 0/100 graph lost, 0 writer errors** — same
  as proven design (b).
- Forced worst-case interleaving WITH the lock: converges to the max.
- Forced worst-case interleaving WITHOUT the lock (the tempting "the upsert
  already told me the winner, drop the lock" shortcut): **deterministically
  loses** — low captures its RETURNING winner, high completes upsert+graph
  write, low then writes its captured (stale) winner to the graph. RETURNING
  does not shrink the ledger-decide → graph-write window; **the advisory lock
  stays**. Skipping the lock on "no-overlap" batches is likewise unsound:
  overlap potential is a property of *concurrent* batches from other scopes and
  is unknowable to the local batch.

**Perf differential** (`owner_ledger_returning_perf_live_test.go`, 500 uids ×
20 warm iters, per-stage split; ranges over two independent runs on a fresh
store — the shim's order keys derive from a per-invocation nanosecond nonce so
repeat runs stay in the intended winning/losing regime):

| Case | flat floor | baseline (b) | (b)+RET-WHERE | (b)+RET-CASE |
|---|---|---|---|---|
| non-contended, value-changing (newer key per iter — the common real case) | 35.8–39.8ms | 48.0–50.8ms (1.27–1.34x) | 49.3–49.7ms (1.25–1.37x) | 50.2–51.8ms (1.26–1.45x) |
| all-losers (warm duplicate/lower-key replay) | 9.4–10.7ms | 16.3–17.8ms (1.66–1.72x) | 15.8–17.6ms (1.64–1.67x) | 17.7–17.9ms (1.67–1.88x) |

Stage split for the non-contended baseline: lock 0.7–1.2ms, upsert 4.2–5.5ms,
winner read-back **0.9–1.6ms**, graph 36.2–41.2ms, commit 2.8–3.2ms. The
read-back that RETURNING eliminates is ~2–3% of the batch; both RETURNING
mechanics land within run-to-run noise of the baseline (their ranges overlap
the baseline's in both cases). **RETURNING is a rejected hypothesis as a perf
win** — the overhead lives in the ledger upsert + commit + lock, which *are*
the mechanism.

The measurement did correct the cost model. The earlier 2.28x/2.46x was taken
against an idempotent same-value graph floor (6.0ms — NornicDB re-`SET`ting
identical values). A real newer-observation batch changes property values, and
that flat floor is ~36–40ms; the ledger overhead is roughly constant in
absolute terms (~7–12ms per 500-uid batch, ~14–25µs/row), so the realistic
multiple for the common case is **~1.3x**, and the 2.46x headline was an
artifact of the cheap floor, not the ledger. Two fully-overlapping concurrent
batches serialize on the per-uid locks by design (measured 78ms wall for two
~49ms batches, converging to the max in both stores every iteration);
disjoint-uid batches do not contend.

Performance Evidence: `TestLiveOwnerLedgerReturningBatchPerf` on pinned
Postgres 18 (`postgres:18-alpine`) + NornicDB v1.1.9
(`timothyswt/nornicdb-cpu-bge:v1.1.9`), 500-uid batches × 20 warm iterations,
two independent runs — flat 35.8–39.8ms vs design (b) 48.0–50.8ms (1.27–1.34x,
~22–25µs/row) non-contended value-changing; flat 9.4–10.7ms vs 16.3–17.8ms
(1.66–1.72x) all-losers; RETURNING variants within noise of the baseline in
every run; read-back stage 0.9–1.6ms of ~48–51ms. Correctness: 0/100 ledger
lost, 0/100 graph lost, 0 writer errors at N=4; forced no-lock interleaving
loses deterministically.
No-Observability-Change: measurement shims (env-gated live tests) and ADR text
only; no production code path, telemetry, or wire contract changes.

**Recommendation: GO on plain design (b), skip the RETURNING variant.** The
proven upsert + winner read-back shape stays; RETURNING adds semantics
complexity (WHERE-false row suppression / self-overwrite churn) for a ~1ms
saving that noise absorbs. The write-path budget the 5-family build should be
held to is the absolute one: **~7–12ms of Postgres work per 500-uid batch
(~14–25µs/row), fully overlapped-safe, on top of whatever the graph write
already costs.** CloudResource-family batches are low-cardinality relative to
File/Function, so the per-scope impact is small; the final gate remains the
perf differential on the real writer before merge, as already required above.

### Stage 1 owner-ledger mechanic (decided and implemented)

- A Postgres `graph_node_owner` table keyed on node `uid`, holding the current
  max `source_order_key`, the winning node row as a `winning_row` JSONB column
  (so all ~19 scope-derived properties are carried without a wide schema), plus
  `updated_at`. Migration `056_graph_node_owner.sql` adds the table with a PK on
  `uid`. (Named `graph_node_owner`, not `cloud_resource_owner`: canonical uids
  are globally unique across labels, so one table serves the CloudResource
  AWS/GCP/Azure + EC2-instance family and the KubernetesWorkload family.) The
  store is `postgres.GraphNodeOwnerStore`; the gate is `internal/graphowner`.
- The reducer node-write path, for each batch: sort the batch uids, acquire all
  per-uid advisory locks in one sorted statement, batch-upsert the ledger
  (atomic max), read back the winning order key per uid, and write to the graph
  ONLY the uids where THIS writer is the current max — writing THIS writer's
  OWN (Go-typed) row via the existing plain `MERGE + SET` (no CASE), skipping
  the uids it lost. Commit (releasing the locks).
  - **Refinement (byte-identity):** the graph write uses the current-max
    writer's OWN row, NOT a value round-tripped out of the ledger. Round-tripping
    the winning scope-derived values through JSONB would mangle Go types
    (`[]string`→`[]any`, the EC2 `imds_http_put_hop_limit` `int64`→`float64`),
    which would change the graph node's property representation and BREAK the
    byte-identity requirement for non-contended nodes. Writing the current-max
    writer's own row keeps non-contended writes byte-identical to origin/main
    and keeps contended nodes deterministic (only the max writer's own row ever
    lands, regardless of interleaving). Proven by the `b_gated_own_row` design
    in the shim (0 lost / 100 trials N=4). The ledger still stores the winning
    row as JSONB for the Stage 2 provenance foundation, but Stage 1's graph
    write does not read those values back.
  - `RETURNING` on the upsert is **rejected** per the perf finding (~2-3% win,
    within noise); the plain read-back / gated-own-row path ships.
- All five families (CloudResource AWS/GCP/Azure, EC2-instance, K8s-workload)
  route their node writes through this owner-ledger gate. The within-scope
  extractor tie-break (max order key over duplicate uids in one generation) is
  applied in the `Extract*NodeRows` functions.
- Stage 2 (per-scope provenance satellites) still trails; the JSONB owner row is
  a natural foundation for it.

### P2-1: chunked critical section (per-tx advisory-lock bound)

The mechanic above was originally shipped as **one Postgres transaction per
reducer node-write batch**, and that batch is the entire materialization
intent's rows: `loadFactsForKinds` → `ListFactsByKind` carries no `LIMIT`, so
the batch size is however many `aws_resource` (or GCP/Azure/EC2/K8s) facts
exist for one `(scope_id, generation_id)`. A large cloud-account scope is
thousands to tens of thousands of distinct resource uids, and
`ResolveOwnedUIDs` acquires one `pg_advisory_xact_lock` per distinct uid in
that single transaction — so the per-tx advisory-lock count was unbounded.

**Proven failure (prove-theory-first, cheapest shim):** against a throwaway
Postgres 18 instance on stock defaults (`max_locks_per_transaction=64`,
`max_connections=100`, `max_prepared_transactions=0` — an approximate
6400-slot shared advisory-lock table cluster-wide), one transaction
acquiring N `pg_advisory_xact_lock` calls:

```
N=1000..8000   -> ok
N=20000        -> ERROR: out of shared memory
                  HINT: You might need to increase "max_locks_per_transaction"
```

This was re-proven directly against the live gated writer (`Gate.write`, real
`*sql.DB`, no NornicDB — a no-op graph writer) on the same class of instance:
20000 distinct-uid rows through the pre-fix single-transaction code failed
with the identical `out of shared memory (SQLSTATE 53200)` error. Under
concurrent reducer workers sharing the cluster-wide slot budget, exhaustion
occurs far below 20000 (e.g. a 5000-uid scope × 2 concurrent workers already
approaches 10000).

**Fix:** `Gate.write` now loops over `rows` in chunks of at most
`lockChunkSize` (`internal/graphowner`, set to `cypher.DefaultBatchSize` = 500)
distinct uids. Each chunk gets its own Begin → `ResolveOwnedUIDs` (≤500 lock
acquisitions) → graph write of that chunk's owned rows → Commit, exactly the
same per-uid critical section as before, just bounded per transaction instead
of unbounded per intent. `write` accumulates the owned/contended totals across
chunks and emits one aggregated contention log line (and the
`eshu_dp_cross_scope_ownership_contended_rows_total` counter) for the whole
intent, so the operator-facing signal did not change shape.

**Correctness argument (why chunking is safe, not a serialization
workaround):**

- **Per-uid independence.** Rows arrive already deduped to one row per uid by
  the upstream `Extract*NodeRows` `byUID` map, so slicing the row slice into
  chunks never splits a single uid's lock+upsert+winner resolution across two
  transactions — each uid's critical section still runs whole, inside exactly
  one chunk's transaction. Every uid's ownership decision is independent of
  every other uid's (the ledger upsert is keyed and locked per-uid), so
  resolving different uids under different transactions changes nothing about
  which contributor wins each uid: converge-to-max holds identically whether
  the whole intent runs in one transaction or many.
- **Idempotent retry convergence.** A failure partway through the chunk loop
  leaves earlier chunks committed and returns the error to the reducer, which
  retries the whole intent. The ledger's max-upsert is monotonic (a lower
  order key can never overwrite a higher one, by the
  `WHERE EXCLUDED.source_order_key > graph_node_owner.source_order_key`
  guard) and the graph MERGE is idempotent, so replaying already-owned uids on
  retry reconverges to the same result — partial progress from a mid-intent
  failure is safe, not a correctness hazard.
- **Concurrency improves, not reduces.** Each uid's advisory lock is now held
  only for its own chunk's transaction (released at that chunk's commit)
  instead of for the whole intent's transaction. Total lock hold time per uid
  drops, and cross-scope concurrency is preserved and improved — this is
  explicitly not a `Serialization Is Not A Fix` violation: the chunk bound is
  a partition-by-batch-size on an already per-uid-partitioned critical
  section, not a worker-count reduction or a single-threaded drain.

Proof (unit + live, before/after): `go/internal/graphowner/gated_writer_chunk_test.go`
(fake store + fake `Beginner`, 1201 rows, asserts ≤`lockChunkSize` entries per
`ResolveOwnedUIDs` call and `ceil(1201/lockChunkSize)` transactions, RED on the
pre-chunking code) and `gated_writer_chunk_live_test.go` (20000 rows against a
live Postgres, no NornicDB — fails unchunked with `out of shared memory`,
succeeds chunked in well under a second).

### #5062 P1: LockOnlyGate for the posture/exposure property writers

A deep-research audit flagged a gap in the Stage 1 gate: the RDS/EC2/S3
posture and internet-exposure property writers
(`RDSPostureNodeWriter`/`EC2InternetExposureNodeWriter`/
`EC2BlockDeviceKMSPostureNodeWriter`/`S3InternetExposureNodeWriter` in
`go/internal/storage/cypher`) `SET`/`REMOVE` reducer-owned properties on the
SAME `CloudResource` nodes `Gate` resolves ownership for, but ran completely
unfenced against a concurrent Gate-resolved base-property write to the same
uid. They cannot be given an owner-ledger row (every scope observes the same
posture fact for the same resource — there is no order-key "winner" to
resolve), so the fix is a lock-only critical section
(`go/internal/graphowner.LockOnlyGate`): acquire the SAME per-uid
`pg_advisory_xact_lock` key `Gate`/`ResolveOwnedUIDs` uses
(`postgres.GraphNodeOwnerStore.LockUIDs`, added by refactoring
`acquireLocks` to delegate to it so the two key derivations cannot drift)
across the posture writer's graph write, with no ledger upsert. `Retract*` is
NOT lock-gated: it targets a scope (`WHERE r.<x>_scope_id IN $scope_ids`), not
an explicit uid list, so there is no row-level uid set to lock ahead of the
write.

**Prove-theory-first result — partially disproven, and that is the useful
outcome.** The literal theory ("an ungated posture write racing a gated
base-property write can silently revert the ledger-decided base properties")
was tested with the SAME widened-transaction-window shim that proved the
graph-side guard's 26%/5-6% loss rate above
(`lock_only_gate_prove_theory_live_test.go`: two writers hold an explicit
NornicDB transaction across a 5ms read-modify-write gap, base writer through
the real `Gate` + a real Postgres ledger, posture writer either ungated or
`LockOnlyGate`-wrapped, 100 trials, seeded/warm existing node). Two
independent runs measured **0/100 silent property loss in BOTH the ungated and
the locked scenario.** This is a materially different result from the
graph-side guard's proven loss, and the reason is now understood: this writer
pair's Cypher is an UNCONDITIONAL `MATCH`/`MERGE ... SET` (no `WHERE`-based
compare-and-swap), whereas the guard's proven-lossy mechanic is specifically a
`WHERE`-conditional compare-and-swap SET racing itself. NornicDB's OCC does
correctly abort a concurrent unconditional-SET conflict with
`Neo.TransientError.Transaction.Outdated`, and production's
`cypher.RetryingExecutor` already retries that transient error to safe
convergence — the silent-loss defect measured elsewhere in this ADR is real,
but it did not reproduce for this specific writer pairing's Cypher shape.

**What DID reproduce, and is this change's actual justification:** the ungated
scenario's concurrent transactions repeatedly triggered that same
`Outdated` abort-and-retry cycle, at a **3.6x-30x per-trial latency cost**
(two runs: 2310ms/trial vs 647ms/trial, and separately 1995ms/trial vs
67ms/trial) versus the locked scenario, because the two writers retry-thrashed
against each other instead of serializing. `LockOnlyGate` eliminates that
retry storm by removing the conflict opportunity entirely — a genuine
concurrency-deadlock-rigor contention proof (the fix removes unsafe overlap)
even though it is not an accuracy proof for this writer pair. It remains
defense-in-depth against the conditional-SET-shaped defect class this ADR
already proved is real on this NornicDB version line, for any future writer
that adds a conditional SET to a shared node.

On a non-contended batch, `LockOnlyGate` is provably invisible: 500/500 rows
identical between a flat (ungated) write and a `LockOnlyGate`-wrapped write of
the same rows (`lock_only_gate_perf_live_test.go`,
`non_contended_equivalence`).

Performance Evidence: `lock_only_gate_perf_live_test.go` `batch_perf`, pinned
Postgres 16 (throwaway container) + NornicDB v1.1.11
(`timothyswt/nornicdb-cpu-bge:v1.1.11`), 500-uid batches × 20 warm iterations:
flat graph-only avg 7.17-7.71ms vs lock-only-gated avg 10.44-11.34ms
(1.46-1.47x, ~6.5-7.2µs/row) — cheaper than design (b)'s 2.28x/~15-25µs/row
because there is no ledger upsert or winner read-back, only the lock. Under
forced contention (`lock_only_gate_prove_theory_live_test.go`), the locked
path is 3.6x-30x FASTER than the ungated path across two independent runs
(647ms/trial and 67ms/trial locked vs 2310ms/trial and 1995ms/trial ungated,
100 trials each), because it eliminates NornicDB `Outdated`-abort retry
thrash. Correctness: 0/100 silent property loss in both scenarios across two
runs (see the prove-theory discussion above for why this disproves the
literal silent-loss framing for this writer pair while confirming the
contention/latency hazard).
Observability Evidence: `LockOnlyGate.writeChunk` emits a "graph node owner
lock-only advisory locks acquired slowly" structured log
(`family`, `uid_count`, `wait_seconds`) when lock acquisition takes
≥100ms, mirroring `packageRegistryIdentitySlowLockWait`'s convention for the
same advisory-lock primitive — the operator-facing signal that a lock-only
chunk is contending with a concurrent Gate-resolved write on an overlapping
uid set.

### Golden-corpus / B-12 impact

Expected: none to cassettes or the B-12 snapshot content. The snapshot
asserts no `source_fact_id`; existing corpora contain no overlapping
identity cross-scope inputs (disjoint by construction,
`4389-ifa-conformance-platform.md` Layer 3 landmine note); and for
single-writer nodes the guard always passes, reproducing today's values.
The one visible delta is the new order-key node property, which no query
shape returns. B-7/B-12 gates still run as proof (expected no-diff), and
`eshu-golden-corpus-rigor` applies if Stage 2 later adds graph shape
(OBSERVED_BY satellites would need snapshot + cassette lockstep).

### Follow-on work if adopted

1. Write-path enforcement (Stage 1):
   `go/internal/storage/cypher/cloud_resource_node_writer.go` (+ the teeth
   pair, whose constant-concat structure must survive,
   `cloud_resource_node_writer.go:48-56`), order-key stamping in
   `go/internal/reducer/{aws,gcp,azure}_resource_materialization.go`;
   decide (Open Question 3) whether `ec2_instance_node_rows.go` and
   `kubernetes_workload_materialization.go` join the same change.
   Extractor tie-break fix in the two `Extract*NodeRows` functions. Docs:
   `go/internal/storage/cypher/README.md`, `go/internal/reducer/README.md`
   domain table, and the folder-doc pairs per `eshu-folder-doc-keeper`.
2. Regression tests: guard unit tests in the cypher package; the
   concurrency shim recorded as evidence; the contention Odù (above) added
   to the Ifá matrix and its drop-an-Odù docs
   (`go/internal/ifa/AGENTS.md`).
3. P6 unblock: with the contention Odù green, overlapping-identity inputs
   become legal fixtures for #4580 fault scripts
   (`expire-lease-mid-handler`, `fail-graph-write-once-then-succeed`
   against contested uids — the exact case the fault matrix most needs).
4. Stage 2 provenance record, after Open Question 2 is answered.

## Consequences

- Positive: cross-scope truth becomes order-independent with a stated
  semantic; the determinism matrix gains its missing input class; the
  per-scope concurrency model is preserved untouched; the AWS `safe`
  promotion rationale (`reducer_queue_conflict.go:62-67`) is strengthened,
  not weakened.
- Negative / cost: one more property on every CloudResource node; a CASE
  per guarded property in the hottest canonical node writer (must be
  measured); a real concurrency proof obligation against two backends
  before any code lands; Stage 2 adds either graph shape or a new Postgres
  table.
- Risk: if the guard proves un-lock-safe on NornicDB, Stage 1's mechanics
  change (retry-on-conflict or resolution-pass fallback) while the decided
  semantics (max `(observed_at, source_fact_id)`) stand — the decision is
  the semantic, the Cypher shape is implementation.

## Open questions for the maintainer

1. **Order-key semantics:** max `(observed_at, source_fact_id)` (recommended
   — latest observation wins, hash tiebreak) vs. pure `source_fact_id`
   (the issue's minimal example — fully deterministic but recency-blind).
   This choice is the product-truth heart of the ADR.
2. **Stage 2 provenance home:** graph satellite (Cypher-answerable, golden
   impact, needs a lifecycle/retract story) vs. Postgres ledger
   (transactionally simple, not graph-queryable). Also: must Stage 2 land
   before P6, or may it trail? (Recommendation: trail.)
3. **Enforcement scope:** CloudResource family only (AWS/GCP/Azure share
   `WriteCloudResourceNodes`), or also the pattern siblings
   (`ec2_instance_node_rows.go:167`,
   `kubernetes_workload_materialization.go:312`) in the same change.
   (Recommendation: CloudResource first; siblings as a follow-up issue with
   the same recipe.)
4. **Contention Odù payload shape:** identical payloads across scopes only
   (pure envelope contention — the minimal #5007 repro), or also divergent
   payloads (different `state`/`observed_at` for the same uid), which
   exercises the OBSERVED STATE resolution semantic. (Recommendation:
   both; the second is the one that proves "latest observation wins.")
