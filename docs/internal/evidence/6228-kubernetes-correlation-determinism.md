# #6228 kubernetes_correlation determinism evidence

TDD slice for the kubernetes_correlation family
(WriteKubernetesCorrelationEdges -> RUNS_IMAGE), mirroring the s3_logs_to
slice: offline Odù (10 facts, uniqueness RED then GREEN) + hand-derived
expected-edge fixture + guard (resolution_mode=digest pinned, rows carry
image_ref + source_digest pins) + registry row 20 + cassette projection +
drive/assert fns + post-delta assert + determinism/fault mirrors.

The fixture: 3 OCI digest-addressed sources (one active manifest, one
active index -- proving both digest-addressed source labels the template
MATCHes -- plus one tombstoned manifest) + 2 tag observations sharing one
tag with different digests + 5 pod templates: two digest-form refs
matching the active sources (the edges), one naming the tombstone-only
digest (stale, the conservative skip), one naming the ambiguous tag
(never promoted to exact), and one naming an unobserved digest
(unresolved).

The 2 edge endpoint uids are literal, not hashed (k8s object_ids and
oci-descriptor:// uids transcribed from the fixture definitions), then
confirmed by the guard running the REAL extractor: coverage GREEN with
zero MISSING/EXTRA, plus a seeded-violation RED (mutated target uid ->
MISSING + EXTRA) then GREEN on restore, byte-identical. The OciImageDescriptor
source label is not exercised: no collector emits descriptor-kind facts in
this fixture's corpus.

## Live scope-stamp finding (2026-09-28, two failed drives before green)

Drive 1 (family scope stamped `kubernetes_live`): the handler RAN but
logged `fact_count=6 edge_count=0` and the exact-set assert failed with
both edges missing. Root cause, read off the live `fact_records`: replay
stamps `fact.source_system` from the SCOPE, and the
container-image-identity loader the handler reads
(`ListActiveContainerImageIdentityFacts`,
`go/internal/storage/postgres/facts_active_container_image_identity.go`)
only accepts OCI facts with `source_system='oci_registry'`.

Drive 2 (second OCI sources seed cassette in per-repo `oci_registry`
scopes): the k8s assert went green but N=1 vs N=4 dumps DIVERGED on the
OCI nodes' scope-stamped provenance columns (scope_id, source_system,
generation_id, source_fact_id). Same logical facts in two scopes race
last-writer-wins in node projection -- duplicate substrate across scopes
is inherently nondeterministic here, a fixture-topology artifact, not a
production defect (real collectors write each OCI fact in exactly one
scope).

 landed shape: the family cassette carries both substrates in its ONE
scope, stamped `source_system='oci_registry'`. The kind-scoped loads
(`ListFactsByKind`: scope+generation+kind, no `source_system`
predicate) are unaffected, per-fact `collector_kind` values stay honest,
and scope descriptors are replay-transport, not fixture truth (the same
reason `loadDirectFamilyOdu` declines to project them). A jq fail-fast
pin in `ifa_family_fixtures_require` rejects a lost stamp with the cause
named (seeded RED exit 1, then GREEN, restore byte-identical).

## Live N=1/2/4 determinism drive (2026-09-28)

`NORNICDB_IMAGE=ghcr.io/eshu-hq/nornicdb-amd64-cpu:fix-500-e022384c
bash scripts/verify-ifa-determinism.sh --keep` (tag form; the digest form
fails with "cannot overwrite digest", same workaround as prior slices)
drove all three cells to their graph dumps with 0 failed asserts:

- All three canonical dumps are byte-identical: `graph-n{1,2,4}.dump`
  sha256 `5d23e7270963a2d0940900aeb44d7b88db78588f8e80d51dd38320d5af97449e`
  (1,630,079 bytes each) -- the determinism comparison itself, measured
  directly rather than via the gate's digest line. N=1 wall 204s, N=2
  204s, N=4 205s.
- Terminal counts: 10 committed cassette facts under the new scope (5
  `pod_template` + 2 active OCI sources + 1 tombstoned source + 2 tag
  observations) with `generations_committed=1` at workers=1, workers=2,
  and workers=4; exact two-edge `RUNS_IMAGE` set per run (pre-delta via
  the registry loop, post-delta via the pinned chain -- 6/6
  `expected=2 edges matched exactly`); 0 failed asserts (49 `[PASS]`
  lines, `grep -c FAIL` = 0 on the gate log). The within-run control is
  the sibling set: the same script on the same stack without this wiring
  drives everything but kubernetes_correlation, and the after measurement
  adds the driven family while every sibling assert stays green. Kept
  workdir: `$TMPDIR/ifa-determinism.XXXXXX.zKpwp4k0vM` (dumps, rationale
  deltas, per-cell drive logs).
- Direct graph truth (canonical N=1 dump): 5 `KubernetesWorkload` nodes
  and 3 OCI source nodes carry the exact fixture uids; exactly 2
  `RUNS_IMAGE` edges exist graph-wide, both `resolution_mode=digest`
  with `evidence_source=reducer/kubernetes-correlation`. The tombstoned
  legacy source still projects a node, yet carries no edge -- the
  classifier's stale restraint holds with the endpoint present, the
  strongest form of the negative case. No query-surface change: the
  `GET /api/v0/kubernetes/correlations` read surface is untouched, so no
  repo/service/deployment surface reconciliation applies.
- Gate log: /tmp/k8s-live-run3.log (retained on the driving machine only).

## No-Regression

No-Regression Evidence: the writer file edit
(go/internal/storage/cypher/kubernetes_correlation_edge_writer.go)
is const-export renames ONLY
(canonicalKubernetesCorrelationEdgeUpsertCypherFormat,
retractKubernetesCorrelationEdgesCypher -> exported spellings, plus
their doc comments); the upsert/retract Cypher text is byte-identical,
no statement shape, predicate, or plan change.
`verify-query-plan-regression` is run singly before push (DEFER-CI
surface: writer file touched). Full offline proof: familyodu +
materializededges + full ifa tree GREEN, cassette-author gate EXIT 0
(registry.example.com is allowlisted; digests are repdigit documentation
forms), determinism + fault-injection mirrors GREEN, gofmt + git diff
--check clean. No baseline exists for this family (first drive); the
sibling-set within-run control above is the regression control.

## No-Observability-Change

No-Observability-Change: no new spans, metrics, logs, or status surface.
The slice wires an existing writer, existing fact kinds, and existing gate
machinery. The determinism gate's existing PASS/FAIL lines cover the new
asserts.
