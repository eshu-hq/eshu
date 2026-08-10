# #5385 — name-only Workload identity: options for the owner

**Options only. No implementation, and none should start before the owner picks
one** — the choice changes projected graph truth, so getting it wrong writes bad
data that query-time authorization cannot repair.

## The mechanics, verified on current main

| Claim | Verified |
| --- | --- |
| `workloadID = "workload:<name>"` | `internal/reducer/projection.go:279` |
| `instanceID = "workload-instance:<name>:<env>"` | `internal/reducer/projection.go:327` |
| Identity is the id alone; `repo_id` is a `SET` property | `MERGE (w:Workload {id: row.workload_id})` at `workload_materializer.go:351`, `MERGE (i:WorkloadInstance {id: row.instance_id})` at `:370` |
| A **second** MERGE site exists | `internal/storage/cypher/canonical.go:24,36` — same keys |
| Retract keys off the property, not identity | `internal/graph/mutations.go:121` — `OPTIONAL MATCH (owned_workload:Workload {repo_id: r.id})` |
| Instance ids are in the B-12 golden snapshot | `testdata/golden/e2e-20repo-snapshot.json` |

Two consequences worth separating, because they have different fixes:

1. **Collision is by construction.** Two repositories defining `api` MERGE into
   one node. Both get `DEFINES` edges; cloud USES evidence from either
   cross-attaches.
2. **Retract is already wrong under collision, independently of auth.**
   `repo_id` is last-writer-wins, so the retract predicate
   `{repo_id: r.id}` matches the collided node for exactly one of the
   contributing repos. The other repo can neither retract it nor see it as
   its own. This is a data-lifecycle defect, not a permissions one, and no
   query-time predicate fixes it.

The second point is the part I would weight most heavily in the decision: it
means the current state is not merely "over-shared", it is **unretractable for
all but one contributor**.

## What the collapse is deliberately buying

This is not an accident to be undone without cost. Cross-repo workload
addressing is a designed feature: `reducer/dependency.go:76` builds
`workload:<name>` from a name→repo map on an org-globally-unique-name
assumption, the impact API accepts `workload:checkout` handles, and
`DomainWorkloadIdentity` exists to "resolve canonical workload identity across
sources". Any option that repo-qualifies identity must say what happens to that
addressing, or it silently removes a feature while fixing a defect.

## The options

### A. Repo-qualified instance identity, name-keyed Workload hub

`workload-instance:<repo>:<name>:<env>`; `Workload` stays `workload:<name>` as a
logical hub with `DEFINES` edges from each repo.

- **Fixes:** instance-level contamination and cross-attached USES evidence.
  Each tenant's instance is its own node with its own `repo_id`, so retract
  works again.
- **Leaves open:** the `Workload` hub is still shared, so anything authorizing
  or retracting at the hub level keeps today's behaviour.
- **Cost:** churns every instance-id consumer, cassettes, and the B-12 snapshot.
  This is a node-identity migration — per the repo's own precedent
  (`line_number` in canonical uids), old nodes are not rewritten but reaped by
  generation retraction, so there is a window where both shapes exist.
- **Honest risk:** the migration is the expensive part, not the code change.

### B. Per-repo workload nodes plus explicit correlation edges

Each repo gets its own `Workload`; a `SAME_NAME` (or similar) edge expresses the
cross-repo relationship that the collapse currently expresses physically.

- **Fixes:** both contamination and retract, at both levels. Identity becomes
  honest — one node per real thing.
- **Cost:** the largest. Every cross-repo traversal that today gets correlation
  for free by landing on one node must now traverse an edge, including the
  impact API's `workload:<name>` handles. Correlation becomes explicit and
  therefore *queryable and refusable*, which is an improvement in truth and a
  regression in convenience.
- **Worth noting:** this is the only option where "are these the same workload?"
  becomes evidence-backed rather than assumed by name equality.

### C. Keep the collapse; make it first-class with per-tenant provenance

Document the shared node as intended cross-repo correlation, and preserve
per-tenant provenance on the **edges** rather than on the node.

- **Fixes:** the evidence-attribution half — you can tell which tenant
  contributed what.
- **Does NOT fix:** retract. The node still carries one `repo_id`, so
  `mutations.go:121` still matches for one contributor only. Unless retract is
  re-keyed off the edges, option C leaves the lifecycle defect in place.
- **Cost:** lowest. Mostly documentation plus edge-property work.
- **Caveat I would not skip:** choosing C because it is cheapest, without also
  re-keying retract, converts a known defect into a documented one. That is
  worse than leaving it filed, because documentation reads as a decision.

## What I would want decided, in order

1. **Is retract-under-collision in scope here, or its own issue?** It is the
   concrete data-lifecycle bug, it is independent of the auth work, and options
   A and B fix it incidentally while C does not. If it is out of scope, C is
   viable; if it is in scope, C needs a fourth piece.
2. **Is org-global workload name uniqueness a supported assumption or a bug?**
   `dependency.go:76` depends on it. A and B both deny it; C affirms it. This is
   the actual product question under the technical one.
3. **If A or B: what happens to `workload:<name>` handles in the impact API?**
   They are a public surface. Preserved by resolution, deprecated, or broken.
4. **Migration appetite.** Both A and B are node-identity migrations touching
   the B-12 snapshot and cassettes, reaped by generation retraction rather than
   rewritten.

## What I did not do

No code, no cassette or snapshot changes, no recommendation between A/B/C. The
issue asks for options and owner sign-off, and the choice turns on question 2 —
whether org-global workload names are a product guarantee — which is not mine to
answer.

What I would flag regardless of the outcome: the retract defect at
`mutations.go:121` is real today, is not an authorization problem, and is not
fixed by #5384 or #5167/W6.

Related: #5161 (multi-grant isolation), #5384 and #5167/W6 (query-time fail-closed
predicates, which stand on their own regardless of this outcome).
