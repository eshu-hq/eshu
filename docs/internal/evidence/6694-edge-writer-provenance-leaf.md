# #6694 edge/writer leaf 1: provenance family move evidence

## Moved (behavior-preserving, `git mv` + package/qualifier edits only)

- `go/internal/storage/cypher/provenance_edge_writer.go` →
  `go/internal/storage/cypher/edge/writer/provenance.go`
- `go/internal/storage/cypher/derived_from_edge_writer.go` →
  `go/internal/storage/cypher/edge/writer/derived_from.go`
- Tests moved with the family into `package writer` (leaf already carries
  `recordingExecutor`, `recordingGroupExecutor`, and the bolt harness, so no
  new fakes were needed):
  `provenance_test.go`, `derived_from_test.go`,
  `provenance_identity_live_test.go` (skips without `ESHU_CYPHER_BOLT_DSN`,
  same as in root).
- Leaf imports the parent as `sourcecypher` (fault/executor precedent, #6758);
  the parent never imports the leaf, so no cycle. No root compat alias is
  possible on this shape; exactly two caller files repoint to the leaf:
  `go/cmd/reducer/canonical_graph_writers.go` (constructs the writer) and
  `go/internal/reducer/provenance_replay_tombstone_live_test.go`.
  All other reducer consumers already talk through the narrow
  `PackageProvenanceEdgeWriter` / `ContainerImageProvenanceEdgeWriter`
  interfaces and are untouched.
- `materialized_edge_property_keyed_inventory_test.go` stays in root but now
  also scans `edge/writer`: the DERIVED_FROM / PUBLISHES x2 / BUILT_FROM
  templates moved with the family and stay in the nine-occurrence allow-list.

## No-Regression Evidence (move-only, statement-identical):

Baseline: the pre-move file contents at the rebase base (git records the
moves at 90–99% rename similarity; a filtered diff shows zero Cypher
literal changes). After: `go test -count=1 ./internal/storage/cypher/
./internal/storage/cypher/edge/... ./internal/queryplan/` green on the
post-move tree (root, edge/writer, edge/materialized, queryplan all ok;
~1–3s per package on this host, scheduling noise only — no benchmark
delta is claimed because nothing executes differently). Backend/version:
unit-level proof only (recording fakes); the two live tests
(`TestProvenanceEdgeWriterLiveLegacyRowSetMigration`,
`TestProvenanceEdgeWriterLiveSamePairAssertionIsolation`) skip without
`ESHU_CYPHER_BOLT_DSN`, identical to root behavior — full live proof
stays on the CI B-7/B-12 replay gates. Input shape: unchanged reducer
projection rows; batching, dispatch (sequential vs grouped), retry
wrapping, and retract predicates are the same compiled code at a new
path. Row counts: the property-keyed MERGE inventory is unchanged at
nine occurrences (DERIVED_FROM 1, PUBLISHES 2, BUILT_FROM 1 plus the six
root-owned), and a filtered diff of the moved files shows zero Cypher
literal changes — only package clauses, the `sourcecypher` import,
qualifier prefixes, and gofumpt import grouping. Why safe: the move is
invisible at runtime (same package-level behavior, same statements, same
interfaces); the only production wiring change is the import path in
`cmd/reducer`, covered by `go build ./cmd/reducer/` plus the reducer
correlation/containerimage suites.

## No-Observability-Change:

No telemetry, metrics, spans, structured logs, status surfaces, or pprof
endpoints are added, removed, renamed, or re-labeled by this move: the
touched files contain no telemetry surfaces, and no operator-facing
signal changes. Telemetry coverage for the writer path is unchanged.

## Proof (this worktree, before push)

- `go build ./internal/storage/cypher/... ./cmd/reducer/` — clean.
- `go vet` on cypher tree, `cmd/reducer`, reducer, correlation,
  containerimage — clean.
- `go test -count=1 ./internal/storage/cypher/ ./internal/storage/cypher/edge/...`
  — ok (root, edge/writer, edge/materialized).
- `go test -list '.*Provenance.*|.*DerivedFrom.*'` on the leaf discovers all
  19 moved tests; live tests skip without DSN.
- `go test ./internal/reducer/packages/correlation/ ./internal/reducer/containerimage/`
  — ok.
- `go test ./internal/queryplan/` — ok; no queryplan symbol or path pins this
  family, so no digest re-derivation.
- `scripts/test-generate-dirgate-grandfather-go.sh` — 9/9; cypher root row
  re-pinned 110 → 108 (`verify-dirgate.sh --digest` agrees).
- `scripts/verify-performance-evidence.sh` — no hot-path files changed
  (move-only, zero Cypher statement edits).
- `gofumpt -l` clean on all touched files; every touched file < 500 lines.
- B-7 / B-12 byte-identical replay stays a CI gate (no live backend locally).

## Still in root: edge/writer remainder and its helper blocks (#6694)

Per-file unexported-dependency census of the remaining `*_edge_writer.go`
root files (verified by direct grep after a tokenizer pass):

- `chunkStrings` (root `code_interproc_evidence_writer.go`, stays in root)
  blocks: `azure_cloud_resource`, `cloud_resource`, `gcp_cloud_resource`,
  `observability_coverage`, and the SG ledger.
- `cloneRowWith` / `validateStaticGraphToken` (defined in the SG writer
  itself) tie `cloud_resource_container_image`, `ec2_uses_profile`,
  `iam_can_assume`, `iam_can_perform`, `iam_escalation`,
  `iam_instance_profile_role`, `s3_logs_to` to the SG family — they can move
  together with it, no export needed.
- The SG ledger additionally needs `cloudResourceEdgeRetractUIDBatchSize` +
  `retractCloudResourceEdgesByUIDsCypher` from `cloud_resource_edge_writer.go`
  (moves with the cloud family or hoisted first).
- `package_registry_edge_writer.go` depends on `package_registry_canonical_writer.go`
  and belongs to the `package/registry` leaf, not this one.
- `crossplane_satisfied_by_edge_writer.go` belongs to `crossplane/satisfaction/`.
- `derived_from` and the SG writer proper have no out-of-family deps and can
  move once their helper siblings land as above.
