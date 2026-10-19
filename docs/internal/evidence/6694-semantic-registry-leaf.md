# #6694 semantic/registry leaf: cypher/semantic/entity + cypher/package/registry — no-regression evidence

The `semantic_entity` (6 files) and `package_registry` (5 files) families move
from `go/internal/storage/cypher` into `semantic/entity` (`package entity`)
and `package/registry` (`package registry`). File names lose the prefix their
directory now carries (`semantic_entity_rows.go` becomes `semantic/entity/rows.go`).
No statement text, predicate, batching, retry, telemetry, phase order, or
writer contract changes.

## Seam directions

- `semantic/entity` imports the parent (as `sourcecypher`) for the
  `Executor`/`Statement` seam, like `edge/writer`. The parent never imports
  the leaf. External callers (`cmd/reducer` wiring, backend-conformance
  corpus, `edge/materialized`, `ifa/materializededges`, replay/costcounting
  and offline-tier tests) switch to the `entity` import; identifiers keep
  their names.
- `package/registry` never imports the parent, like `canonical/terraform`.
  The leaf owns templates, row builders, phase names, and the identity lock
  set as statement-free `Part` values; the parent keeps `Statement` assembly
  (`packageRegistryBatchedStatements`, byte-identical), the eleven thin
  `CanonicalNodeWriter` build methods (now in `package_registry_adapter.go`),
  phase dispatch, and the deferred second `ExecuteGroup`. Phase-name and
  label string values are unchanged.

## Narrow seam splits (cycle-driven, behavior-preserving)

- `ClassifyRetryableGraphWriteGroupError` is newly exported from
  `retrying_executor.go` (pure function, all-exported signature; the
  unexported name keeps serving the in-package call site). The retry
  classifier tables move with their real-statement fixtures into the entity
  leaf, where they pin the production statements through the exported
  classifier.
- `IdentityLocks.RefCount` is the one new lock accessor; the root
  concurrency tests poll it instead of the lock internals.
- `collectKustomizeOverlayEntities` keeps a local copy of the one metadata
  coercion it used (`kustomizeMetadataStringSlice`, with a sync comment):
  the leaf owns the original and imports this package, and the root
  `PayloadStringSlice` differs semantically (untrimmed `[]string`,
  `fmt.Sprint` coercion), so neither reuse is behavior-safe.
- `TestLiveCanonicalWriteSkipsOversizedIndexKeys` keeps its canonical half in
  the root and drops its semantic half: the semantic oversized proof is the
  always-on, all-five-modes
  `TestSemanticEntityWriterSkipsOversizedIndexKeysInEveryWriteMode`, which
  moved into the entity leaf and drives the production writer over the same
  shapes without a live backend. No copied Cypher fixtures were introduced.
- DSN-gated live tests that need the root bolt harness stay in the root as
  `package cypher_test` importing the leaf (the edge-slice precedent); the
  entity leaf carries its own copy of the bolt harness for its containment
  retract live test (mirroring `edge/writer/bolt_harness_test.go`), and unit
  fakes are duplicated per side.

## No-Regression Evidence (#6694):

- Baseline: `origin/main` at branch creation (`a131e52f5c`); after: branch HEAD.
  Backend/version: NornicDB/Neo4j Cypher text compared statically; live
  backend lanes run in CI (Ifá/Odù cells).
- Cypher proof: every backtick literal extracted from each moved file
  (`HEAD` path vs new path, sorted) compares byte-identical — 24/24 across
  the 6 semantic non-test files, 14/14 moved semantic test files with zero
  drift, and all 5 registry non-test files with zero backtick drift. The
  double-quoted string diff over the registry files shows only relocated
  statement-assembly strings (`"rows"`, `"label=%s rows=%d"`, lock metric
  names, the slow-lock log line), each verified verbatim in the root adapter;
  `packageRegistryBatchedStatements` diffs byte-identical against its HEAD
  original.
- Tests: `go test -count=1 ./internal/storage/cypher/...` green — root,
  both new leaves, and every pre-existing leaf. `go test -list '.*'`
  proves the move carried its tests: 51 tests + 1 benchmark discovered in
  `semantic/entity`, 3 in `package/registry`. The staying root tests
  (`TestCanonicalNodeWriterBuildsPackageRegistryStatements`,
  `TestPackageRegistryIdentityLockKeysCoverPackageSources`, the serialization
  trio, phase-group and dispatch tests) pass unweakened against the adapters.
- Input shape: unchanged — same reducer projection rows in, same `Statement`
  batches out through the same `Executor` seam, same phase order, same
  deferred edge group.
- Why safe: the diff is package clauses, import qualifications
  (`sourcecypher`/`entity`/`registry` aliases), the Part/adapter split with
  identical assembly, exported-identifier renames for phases/rows/locks, and
  doc trios. No predicate, ordering, batch-size, retry, lease, or lock
  logic was touched.
- B-7/B-12: not run locally — both are live lanes needing fixed-port
  backends while the phase CI gates are in flight. Their graph inputs are
  covered by the byte-identical statement proof above; the live verdict
  belongs to CI.
- Query-plan regression gate: not run locally (it provisions a pinned Neo4j
  container on the same fixed ports). Static position: the move adds no
  `Run`/`RunSingle` site and changes no query text, so every disposition,
  fingerprint, and digest still binds the same bytes.

## No-Observability-Change:

- Instrument names, labels, and emission sites are unchanged
  (`eshu_dp_graph_oversized_index_keys_skipped_total`,
  `package_registry_identity_lock_*` span attributes and slow-lock log);
  only the files holding them moved.
- The leaf tests assert the same instruments and pass in both packages.
