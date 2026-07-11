# Cross-scope same-uid node ownership — deterministic merge for scope-derived properties

Status: Accepted (semantics) / BLOCKED on Stage 1 enforcement mechanic — the
graph-side guard is disproven on the default NornicDB backend by the mandatory
prove-theory shim (see [Prove-theory result](#prove-theory-result-stage-1-graph-side-guard-is-not-viable-on-nornicdb)).
No write-path change has landed. Stage 1 needs the maintainer to choose the
application-level coordination fallback before implementation resumes.
Audience: Eshu maintainers
Companion issue: #5007 (filed during Ifá P3, #4396; prerequisite for P6
fault injection over overlapping-identity inputs, #4580)
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
