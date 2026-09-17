# NornicDB v1.3.3 alignment evidence (#6646)

## Artifact identity

Eshu's Compose, Helm, R-5 replay, and Kubernetes governance-proof defaults use
one immutable upstream artifact:

```text
timothyswt/nornicdb-cpu-bge:v1.3.3@sha256:81cedbf48898f4c37d05c325fee76b6d797b43e290e3a8a4e9eea936f0ec827f
```

The upstream `v1.3.3` tag (released 2026-09-15) resolves to commit
`a9956536c2cc902e3463aeb9fbc43c695e3dabb0`. Its OCI index contains:

| Platform | Manifest digest | Validation in this change |
| --- | --- | --- |
| `linux/amd64` | `sha256:4416241599d4abe3e608c73af44e4487e7231cacd6691339f1d941bd2547021e` | Docker runtime and live Eshu proof |
| `linux/arm64` | `sha256:c5a247fa6f2e7ef12b31a3389402501984a8044dc5f7094c9ab0bfab3632023e` | Manifest inventory and rejecting verifier fixture only; no arm64 runtime claim |

The amd64 executable has SHA-256
`70c1b9ad775ed3b45d5355445b5b6732e3d3ba03968b777d1d22f7f16fe863d0`.
The published image has no OCI labels. Unlike v1.3.2 — whose tag retained
`1.3.1` in `pkg/buildinfo/VERSION` and therefore reported `NornicDB v1.3.1` —
v1.3.3 corrects the embedded version and reports `NornicDB v1.3.3`, confirmed
live via `CALL dbms.components()` on database `nornic`. The immutable index
digest, platform child digest, selected platform, upstream tag commit, and
executable hash identify the artifact; the version banner alone does not.

## Storage and rollback boundary

This change makes no storage migration: the on-disk format is unchanged. A
disposable volume was written by the exact v1.3.2 artifact (probe node
`CompatProbe {id: 6646}` plus `CALL dbms.components()` self-report
`NornicDB v1.3.1`), the container was stopped and removed, and the exact
v1.3.3 artifact was started on the same volume. It logged
`storage migration check ... on_disk_version:2, binary_version:2 ...
"storage version current; no migrations to run"`, self-reported
`NornicDB v1.3.3`, and read the probe node back with no errors.

The v1.3.3 defaults therefore reuse the v1.3.2-format storage names: the
`nornicdb_v132_data` Compose volume and the `<release>-nornicdb-v132-data`
Helm claim. The legacy pre-v1.3.2 refusal boundary is unchanged — an existing
legacy claim still fails closed until the operator acknowledges fresh-volume
migration, and the legacy claim stays preserved and unmounted so rollback can
restore it without an older binary opening newer-written storage.

## Local proof on 2026-09-16

Commands ran from the feature worktree with isolated per-worktree Go caches.

| Contract | Command | Result |
| --- | --- | --- |
| Runtime defaults and storage contracts | `go test ./internal/runtime -run 'NornicDB' -count=1` | pass (`ok 0.364s`); Compose and Helm defaults resolve the exact v1.3.3 digest |
| Helm template split | `go test ./internal/runtime -count=1`, `helm lint deploy/helm/eshu`, and default `helm template` before/after SHA-256 | pass: runtime suite `ok 1.592s` (includes the Helm image-match gate); `helm lint` 1 chart ok; default render SHA-256 unchanged at `b09dee50f548b25a24a31de2a050e66045f6b85c04a770d6339b43b280a7e8bd` (the default render carries no NornicDB image by design); the governance-values render carries the exact pin `timothyswt/nornicdb-cpu-bge:v1.3.3@sha256:81cedbf48898f4c37d05c325fee76b6d797b43e290e3a8a4e9eea936f0ec827f` (render SHA-256 `5cefef01f70fc241656a3c0cedf1dae6889ecfb1072429edec53a27e9d852d8f`); legacy-PVC rejection covered by the passing suite; both touched template files below 500 lines (`_validation_core.tpl` 110, `pvc-nornicdb.yaml` 60) |
| Snapshot-conflict classifier regression | `go test ./internal/storage/cypher -run '^(TestClassifyTransientNeo4jErrorPrioritizesNornicDBWriteConflict\|TestClassifyTransientNeo4jErrorRejectsV131ConflictNearMisses\|TestRetryingExecutorV131WriteConflictUsesBoundedMetricReason)$' -count=1` | pass (`ok 0.006s`) |
| Shell verifier mirrors | `bash scripts/test-verify-replay-tier.sh`, `bash scripts/test-verify-k8s-two-team-governance-proof.sh`, and `bash scripts/test-k8s-two-team-governance-provenance.sh` | pass (all three self-tests green; rerun with `TMPDIR` on disk after /tmp pressure produced write errors — see note below) |
| Required live backend conformance | `bash scripts/verify_backend_conformance_live.sh` against the exact v1.3.3 amd64 artifact | pass on the accepted default scope: supported corpus, write-conflict retry, stale-attribute removal, and heterogeneous CloudResource batch contracts (log `conform2.log`). The explicitly opt-in value-flow cloud-sink pair returns 0 rows on v1.3.3 — the v1.3.2 alignment record documents the same zero-row result on fresh v1.3.1 and v1.3.2 stores, so this is the existing unsupported-shape boundary, not a v1.3.3 regression, and this change makes no support claim for that pair. |
| R-5 replay | `bash scripts/verify-replay-tier.sh` | pass; 33 `--- PASS`, 0 fail; offline graph truth and tombstone/idempotent replay completed in 223 seconds; SQL UNION branch proof completed in 24 seconds |
| B-7 golden corpus | `bash scripts/verify-golden-corpus-gate.sh` | pass 4/5 runs on the exact artifact (176s, 195s, 177s, 201s; 561 pass, 0 required-fail each green run; ledger:6646-b7-v133-pass-runs); one intermittent `suppression ignored_hidden` miss on otherwise identical code+image (see note below) |
| B-12 snapshot | B-12 snapshot gate (inside the B-7 run) | pass within each green B-7 run; no snapshot or cassette change (no pipeline projection change) |

One of five v1.3.3 B-7 runs missed `suppression ignored_hidden query assertion
failed` (default view returned 1 row with `suppression.state == "active"` and
no suppression id, instead of 0 rows) at identical phase timings to the passing
runs. The signature matches closed issue #5887 (suppression scoped to
`repository:r_217415d9` misses when the finding anchor flips), whose structural
fix (`preferSupplyChainImageIdentityConsensus`, no per-run `generation_id` in
the repository decision) is in this tree; passing-run keys were verified byte-
identical across runs and the override join verified live. No product-code
cause was found and none was changed. The gate passed on the immediate rerun,
twice more pre-rebase, and once more post-rebase (201s) on the exact artifact;
CI re-runs arbitrate.
| Live NornicDB query tests | `go test -p 1 ./internal/query/... -count=1` untagged, then with the `live_nornicdb_*` plus self-skipping SLO/profile tags and `ESHU_NEO4J_URI` pointed at a fresh exact-v1.3.3 container | pass: 51 packages `ok`, zero FAIL in both runs. The tagged run covers the pitfall shapes through the production handlers (directory S2 aggregation plus name read, grant scoping, chained OPTIONAL MATCH, call-chain/dead-code grant clauses). Two harness notes, both non-product: (1) an initial parallel-package run showed three `Neo.TransientError.Transaction.Outdated` seed conflicts and five `ESHU_PROOF_*`-gated failures; per `eshu-diagnostic-rigor` gate-contention the conflicts are package-parallel (`-p`) write contention on one shared backend, and the gated files need proof-harness IDs (`live_import_cycle_proof`, `live_story_property_proof` dropped from the subset). The `-p 1` rerun is clean. (2) Local toolchain note: system GCC 16 defaults to C23, which activates glibc 2.44's `_Generic` `bsearch` macro and breaks the vendored BSD `bsearch` in `go-sitter-forest/perl v1.9.9`; builds here use env-only `CGO_CFLAGS="-O2 -g -std=gnu17"`. Reproduced identically on the clean main checkout, so pre-existing and unrelated to this pin; no repo change. |
| Pitfall probes (#6564 grant join, #6541 S2 shapes) | Seeded-graph probes via HTTP `/db/nornic/tx/commit` against the exact v1.3.3 amd64 artifact (fresh labels per probe) | pass with one qualified shape: #6564 two-MATCH grant join returns `(x1,d1)` and `(x2,d2)` with the dangling `x3`/`d9` correctly unmatched; the S2 aggregate-then-MATCH shape without a `WHERE` projects actual repository names with correct counts (`repo-one`/`/a`/2, `repo-two`/`/b`/1). The S2 route form with a MATCH-level `WHERE` (`UNWIND` plus `WHERE f.language IN [...]` plus trailing aggregate `MATCH` projecting `r.name`) still returns the literal text `r.name` with otherwise correct rows — but the byte-identical statements on the exact v1.3.2 artifact return the same literals, so this is pre-existing backend behavior, not a v1.3.3 regression. Production is unaffected by construction: `buildDirectoryCypher` (`go/internal/query/language/cypher.go`) returns only `d.*` projections plus the count and never projects `r.name` in-statement; the handler attaches names via a separate read (`directory.go` `row["repo_name"] = name`), and the live directory tests prove end-to-end names on v1.3.3. Seed-shape note: an initial `UNWIND` of map literals into an anonymous `MERGE` stored null properties; identical on v1.3.2, pre-existing; Eshu writers use the bound-variable plus `SET` shape (`go/internal/graph/batch.go`), which verifies clean. The multi-clause row-loss shapes stay ruled out by the pitfalls pages and are covered through the production handlers by the live query tests below. |
| Same-shape performance | #6296-style interleaved write/read rounds, fresh container plus fresh volume per run, alternating order, paired median | pass, no regression: 8 rounds × 2 arms (old `v1.3.2@sha256:a47ae7ea…`, new `v1.3.3@sha256:81cedbf4…`), workload mirrors `go/internal/graph/batch.go` production shapes (`UNWIND $rows MERGE` bound-variable plus `SET`, 40×50 node batches then 40×50 edge batches, 10 point-plus-traversal lookups plus one aggregation). Every run asserted 2000 nodes / 2000 edges, so both arms did equal work. Write median old 72964 ms vs new 73434 ms (+0.6%); read median old 136.5 ms vs new 136.0 ms (−0.4%). Paired per-round write diffs (new−old, ms): +466, +971, +1383, −667, −456, +313, +489, +175 — mixed signs, all within ±1.9% of the ~73 s base, far under the 10% / 60 s stop threshold. Read diffs ±5 ms noise. Probe script is a throwaway under `/tmp` (`perf-6646-interleaved.sh`); only these numbers are recorded here. |

The v1.3.2 baselines recorded in
[`6162-nornicdb-v132-alignment.md`](6162-nornicdb-v132-alignment.md) are
upgrade evidence for this change only. They are not #6184 acceptance.

## Retry and concurrency contract

Proven on v1.3.3. The classifier unit regression passes (`ok 0.006s`): it still accepts the exact typed v1.3.1-and-v1.3.2 conflict spelling while rejecting untyped, wrong-code, incomplete, and reordered lookalikes. The live relationship-snapshot test forced the conflict on the exact v1.3.3 artifact — which emits the same `conflict detected: ... changed after transaction start` spelling — retried once within the existing budget, and converged (`TestLiveNornicDBRelationshipSnapshotConflictRetryContract` PASS; the companion retry-classification and stale-attribute contracts also PASS). No worker-count or serialization workaround is introduced.

## Golden-corpus and restart truth

Proven on v1.3.3. B-7 replayed the corpus through the real pipeline with zero residual and dead-letter work (see the B-7 row; the single intermittent `ignored_hidden` miss is disclosed there and arbitrated by CI re-runs). The Ifá `4/21` slice selected `cell_baseline` and `cell_restartbackend` on fresh per-cell Compose stacks: baseline 14 s wall, restart 16 s wall, both reaching zero dead letters, and both producing the canonical digest `280a882458096e6813cb4f3d7c6552b92860c5b4c2a6e597ee5cc69c462f8052` — byte-identical to the digest recorded for v1.3.2, so the version bump changes no projected graph truth.

## Live Kubernetes proof

Proven on v1.3.3. `bash scripts/run-k8s-two-team-governance-proof.sh --artifacts <temporary-dir>` ran green (exit 0) on the shared single-node `linux/amd64` Minikube v1.39.0 / Kubernetes v1.37.0 cluster, in a random-suffix namespace that the driver uninstalled on exit (verified no `gov-proof` namespace remains). Provenance records implementation commit `09e9924de`, the immutable index `timothyswt/nornicdb-cpu-bge:v1.3.3@sha256:81cedbf48898f4c37d05c325fee76b6d797b43e290e3a8a4e9eea936f0ec827f` (runtime id `docker-pullable://...@sha256:44162415…`), and the honest in-cluster banner `NornicDB v1.3.3`. Note: provenance pins the code HEAD at proof time; the only later commit on this branch adds evidence text plus comment-only test/doc updates, with no chart, template, value, or query change — the verified attestation before push covers the final head.

## Live storage migration and rollback

Not re-run for v1.3.3: there is no migration. The storage-compat probe above
proves v1.3.3 opens v1.3.2-written storage with no migration, and the v1.3.2
alignment record retains the full legacy-migration and rollback proof for the
pre-v1.3.2 boundary, which this change does not move.

No-Regression Evidence: the supported contracts stay within their existing gate boundaries; nothing here is presented as a speedup over v1.3.2. Same-shape write/read medians are within ±1% (write +0.6%, read −0.4%, mixed-sign paired diffs); the Ifá restart digest is byte-identical to v1.3.2 (`280a8824…f8052`); R-5, B-7/B-12, the live query subset, conformance, retry, and shell-mirror gates are green on the exact artifact with the disclosed intermittent and opt-in exceptions above. The two backend behaviors probed as different (anonymous multi-row `UNWIND`+`MERGE` null properties; `UNWIND`+`WHERE` trailing-`MATCH` literal projection) reproduce byte-identically on v1.3.2 and are avoided by Eshu's writers and handlers by construction.

Machine-conditions note: the shared host's `/tmp` (32G tmpfs) sat at ~80% from other lanes' artifacts during this drive, which produced spurious write errors in the shell mirrors and a transient cgo response-file failure; the mirrors were rerun green with `TMPDIR` on disk, and no peer artifact was touched. Local Go builds on this host additionally need env-only `CGO_CFLAGS="-O2 -g -std=gnu17"` (system GCC 16 defaults to C23, whose glibc `_Generic` `bsearch` collides with the vendored BSD `bsearch` in `go-sitter-forest/perl v1.9.9`); reproduced on the clean main checkout, unrelated to this change.

Observability Evidence: No metric instrument, label key, span, log field,
status schema, alert, dashboard, worker, queue stage, or API/MCP field changes.
