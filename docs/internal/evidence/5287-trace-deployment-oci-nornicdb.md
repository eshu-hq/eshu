# #5287 — OCI trace-deployment registry-truth NornicDB-safe rewrite: before/after

Fixes the OCI registry-truth reads behind
`POST /api/v0/impact/trace-deployment-chain` (also mounted on the MCP server).
On the pinned NornicDB build both reads used multi-clause shapes the string-
slicing interpreter mis-executes:

- the digest read (`fetchOCIImageDigestRows`) placed a second
  `MATCH (repo:OciRegistryRepository) WHERE repo.uid = image.repository_id`
  after the image anchor and projected with `RETURN DISTINCT
  coalesce(image.id, image.descriptor_id) AS image_id …`;
- the tag read (`fetchOCIImageTagRows`) chained three `MATCH` clauses
  (tag → repo → image) with `RETURN DISTINCT`.

Both are rewritten to single-anchor-clause reads joined application-side
(`go/internal/query/impact_trace_deployment_oci.go`): images by digest per
label, registry repositories by uid, tag observations by ref — each a single
`MATCH … WHERE … RETURN … ORDER BY` — with the image↔repository and
tag↔repository↔image joins in Go. Inner-join semantics are preserved (an image
with no matching repository, or a tag whose repository/resolved image is absent,
is omitted, exactly as the old `MATCH` chain required).

Backend: NornicDB `eshu-nornicdb-pr261:149245885258`
(commit `1492458852588c884c32f70d27ea2ee07086769c`), standalone isolated
container, no-auth Bolt on `bolt://localhost:17687`, database `nornic`.
Reproduced through Eshu's own driver path (`query.Neo4jReader.Run` →
`session.Run`, `AccessModeRead`), i.e. exactly how the handler executes.

Machine profile: local developer workstation (macOS, Apple silicon); localhost
Bolt. Same-machine relative before/after on a minimal representative fixture (1
registry repo, 1 `ContainerImage`, 1 `ContainerImageTagObservation` resolving to
the image digest). Absolute milliseconds are not a scaled-corpus SLO; OLD and NEW
were measured back-to-back on the identical seeded graph so the delta is valid.

## Accuracy Evidence (behavior fix — corrected delta, not identity)

The OLD output is wrong on this backend, so the proof is the intended delta
(old-wrong → new-correct), measured live in
`TestLiveOCITraceDeploymentRegistryTruth`:

| read | OLD output (pinned NornicDB) | NEW output |
|---|---|---|
| digest (2× MATCH + DISTINCT) | 1 row, `image_id = null` — `coalesce(image.id, image.descriptor_id)` corrupted, canonical image identity lost | `image_id = "oci-img://payments@d1"`, `match_strength = canonical_digest`, registry metadata joined |
| tag (3× MATCH + DISTINCT) | **0 rows** — every tag-resolved row dropped | 1 row, `match_strength = tag_resolved_to_digest`, tag→digest→image→registry all correct |

Deterministic across runs: OLD digest always returns `image_id = null`; OLD tag
always returns 0 rows. The rewrite is also correct on Neo4j — `RETURN DISTINCT`
over the joined rows is unnecessary once identity is single-anchored, and the
Go join makes the ambiguous-tag (multiple digests for one mutable tag) path
explicit instead of relying on cross-clause row multiplication.

## Performance Evidence (correctness win; no meaningful regression)

No-Regression Evidence: warm median over 21 iterations on the seeded fixture,
back-to-back on the pinned backend, measured through `Neo4jReader.Run`:

| path | warm median | result |
|---|---:|---|
| OLD multi-clause total (3 digest + 3 tag statements) | 1.157875 ms | corrupt (`image_id=null`) / empty (0 tag rows) |
| NEW single-clause + Go join (4 digest + 5 tag statements) | 1.300208 ms | correct |

The NEW path issues more single-clause round-trips (one extra registry-repository
read per branch) for +0.142 ms warm on this minimal fixture. The OLD path returns
corrupt/empty results, so there is no valid latency baseline to preserve; this is
a Correctness win at negligible sub-millisecond cost. Each single-clause read is
label-anchored on an indexed property (`digest`, `uid`, `image_ref`), so the
shape scales with match count rather than the multi-clause interpreter's whole-
statement re-evaluation. Classification: `Correctness win`.

## Observability Evidence

No-Observability-Change: the fix changes no metric, span, log field, queue stage,
worker knob, or schema phase. Each read still runs through `Neo4jReader.Run`,
whose existing `neo4j.query` span (`db.system`, `db.name`, `db.statement`)
continues to expose every OCI registry-truth statement to an operator; the
handler now emits a few more single-clause spans instead of the old multi-clause
ones.

## Verification

- `go test ./internal/query -run 'TestFetchOCIImageRegistryTruth|TestOCIRegistryTruthQueriesAreNornicDBSafe|TestBuildDeploymentTraceResponseIncludesOCI' -count=1`
  — unit coverage of the digest/tag/ambiguous flows and the static single-clause
  guard. The guard was proven to fail when a second `MATCH` is reintroduced.
- `ESHU_OCI_PROVE_LIVE=1 ESHU_NEO4J_URI=bolt://localhost:17687 go test ./internal/query -run TestLiveOCITraceDeploymentRegistryTruth -count=1 -v`
  — backend-required live before/after against the pinned NornicDB: captures the
  OLD-shape corruption and asserts the shipped `fetchOCIImageRegistryTruth`
  returns the correct canonical-digest and tag-resolved truth.
- `go test ./internal/query -count=1` — full query package green.

## Scope

This PR fixes the two `trace-deployment-chain` OCI registry-truth reads. The
remaining #5287 sweep item — the two change-surface variable-length impact
traversals (`/change-surface`, `/change-surface/investigate`) — is a separate
follow-up PR with its own live proof, because those are untyped variable-length
traversals with per-edge projection rather than property joins.

Refs #5287
