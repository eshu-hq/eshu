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
| Runtime defaults and storage contracts | `go test ./internal/runtime -run 'NornicDB' -count=1` | PENDING live suite |
| Helm template split | `go test ./internal/runtime -count=1`, `helm lint deploy/helm/eshu`, and default `helm template` before/after SHA-256 | PENDING live suite |
| Snapshot-conflict classifier regression | `go test ./internal/storage/cypher -run '^(TestClassifyTransientNeo4jErrorPrioritizesNornicDBWriteConflict\|TestClassifyTransientNeo4jErrorRejectsV131ConflictNearMisses\|TestRetryingExecutorV131WriteConflictUsesBoundedMetricReason)$' -count=1` | PENDING live suite |
| Shell verifier mirrors | `bash scripts/test-verify-replay-tier.sh`, `bash scripts/test-verify-k8s-two-team-governance-proof.sh`, and `bash scripts/test-k8s-two-team-governance-provenance.sh` | PENDING live suite |
| Required live backend conformance | `bash scripts/verify_backend_conformance_live.sh` against the exact v1.3.3 amd64 artifact | PENDING live suite |
| R-5 replay | `bash scripts/verify-replay-tier.sh` | PENDING live suite |
| B-7 golden corpus | `bash scripts/verify-golden-corpus-gate.sh` | PENDING live suite |
| B-12 snapshot | B-12 snapshot gate | PENDING live suite |
| Live NornicDB query tests | `go test ./internal/query -count=1` (NornicDB live subset) | PENDING live suite |
| Pitfall probes (#6564 grant join, #6541 S2 shapes) | Seeded-graph probes against the exact v1.3.3 artifact | PENDING live suite |
| Same-shape performance | #6296-style interleaved write/read rounds, fresh container per run, paired median | PENDING live suite |

The v1.3.2 baselines recorded in
[`6162-nornicdb-v132-alignment.md`](6162-nornicdb-v132-alignment.md) are
upgrade evidence for this change only. They are not #6184 acceptance.

## Retry and concurrency contract

PENDING live suite. The classifier still accepts the exact typed
v1.3.1-and-v1.3.2 conflict spelling while rejecting untyped, wrong-code,
incomplete, and reordered lookalikes; the live relationship-snapshot test must
force the conflict, retry once within the existing budget, and converge on the
v1.3.3 artifact. No worker-count or serialization workaround is introduced.

## Golden-corpus and restart truth

PENDING live suite. B-7 must replay the corpus through the real pipeline on
v1.3.3 with zero residual and dead-letter work; the Ifá baseline/restart cells
must produce identical graph digests.

## Live Kubernetes proof

PENDING live suite. `bash scripts/run-k8s-two-team-governance-proof.sh
--artifacts <temporary-dir>` on a disposable single-node `linux/amd64`
cluster; provenance must record the implementation commit, the immutable
v1.3.3 index, and its honest `NornicDB v1.3.3` banner.

## Live storage migration and rollback

Not re-run for v1.3.3: there is no migration. The storage-compat probe above
proves v1.3.3 opens v1.3.2-written storage with no migration, and the v1.3.2
alignment record retains the full legacy-migration and rollback proof for the
pre-v1.3.2 boundary, which this change does not move.

No-Regression Evidence: PENDING live suite. Results will prove the supported
contracts stay within their existing gate boundaries; they are not presented
as a speedup over v1.3.2.

Observability Evidence: No metric instrument, label key, span, log field,
status schema, alert, dashboard, worker, queue stage, or API/MCP field changes.
