# Evidence: retire decode-bucket compat aliases (#6626 Slice A)

## What changed

58 aliases, consts, vars, and thin forwarders in the reducer-root compat
surface are deleted from `compat_decode.go` (53) and `compat_correlation.go`
(5: 4 phase-repair aliases + the `RepairsFromStates` forwarder), and every
caller now spells the owning subpackage:

- `schemadecode` (14: 11 decode vars + 3 compat2 vars): `decodeAWSRelationship`,
  `decodeAWSResource`, `decodeAzureCloudRelationship`,
  `decodeAzureCloudResource`, `decodeCodegraphFile`,
  `decodeDocumentationDocument`, `decodeDocumentationEntityMention`,
  `decodeGCPCloudRelationship`, `decodeGCPCloudResource`,
  `decodeKubernetesLiveNamespace`, `decodeKubernetesLivePodTemplate`,
  `decodeReducerPackageOwnershipCorrelation`,
  `decodeReducerPackagePublicationCorrelation`, `decodeSubmodulePin`.
- `factdecode` (11): `QuarantinedFactRecord`, `QuarantinedFactWriter`,
  `quarantinedFact`, `factDecodeError`, `WithQuarantineWriter`,
  `newFactDecodeError`, `partitionDecodeFailures`,
  `quarantinedAttributeShapeFact`, `attributeShapeAsFactDecodeError`,
  `inputInvalidSubSignals`, `recordQuarantinedFacts`.
- `factwrite` (12): `workloadIdentityExecer`, `reducerFactRow`,
  `reducerFactVersionedRow`, `reducerFactBatchInsertQuery`,
  `reducerFactBatchSize`, `reducerFactBatchInsertVersionedQuery`,
  `canonicalReducerFactInsertQuery`, `reducerBatchInsertFacts`,
  `reducerBatchInsertVersionedFacts`, `dedupeReducerFactRowsByFactID`,
  `reducerWriterNow`, `reducerFactCollectorKind`.
- `factload` (7 minus the type): `factKindContentEntity`, `factKindFile`,
  `factKindParsedFile`, `factKindRepository`, `factKindCodeownersOwnership`,
  `factKindSubmodulePin`, `loadFactsForKinds`. `FactLoader` itself stays for
  Slice B (~150 root users move separately).
- `payloadcore` (5): `payloadMap`, `semanticPayloadString`,
  `semanticPayloadStringSlice`, `semanticQualifyDeltaPath`,
  `semanticDeltaPayloadBool`.
- `owners` (`code/owners`, 3): `CodeownersOwnershipEdgeMaterializationHandler`,
  `codeownersMaterializationFactKinds`,
  `loadCodeownersOwnershipMaterializationFacts`.
- `reducercontract` (1): `CrossScopeDependency`. `Domain*` consts stay for a
  later slice.
- `gpphase` + `intents/phase/repair` (5+1): `GraphProjectionPhaseRepair`,
  `GraphProjectionPhaseRepairQueue`, `GraphProjectionPhaseRepairerConfig`,
  `GraphProjectionPhaseRepairer`, `GraphProjectionPhaseRepairsFromStates`.

Cross-package `reducer.X` callers moved too: `cmd/reducer`
(config + helpers), `projector/runtime`, and `storage/postgres`
(phase-repair queue store + input-invalid facts store and tests).

Guard: `TestDecodeBucketFamilyRetired` (RED before, GREEN after).

## Why no regression

No-Regression Evidence: every retired entry is an identical-type alias
(`type X = pkg.X`), an identical const/var, or a thin forwarder with the
same signature, so every repoint is spelling-only; no signature, envelope,
row, or ordering change.

Proof on this head: `go build ./...` exit 0; `go vet` on
`internal/reducer`, `internal/projector/runtime`,
`internal/storage/postgres`, and `cmd/reducer` exit 0 (after fixing the
breakages the deletion exposed: field-name/field-key qualifier rewrites
the scripts made, since repaired; a `repair`-package shadowing local in
the phase-repair test, renamed; a `Service` struct key, restored);
`TestDecodeBucketFamilyRetired` PASS (RED before the repoint, GREEN after
the deletion); `go test -count=1` green over `internal/reducer` (6.2s),
`internal/projector/runtime`, `cmd/reducer` (12.6s), and the touched
postgres input-invalid/phase-repair suites; `git diff --check` clean;
gofumpt clean on staged files.

Benchmark Evidence (no number claimed): the repointed-caller benchmark
`BenchmarkReducerFactInsertPerRowVsBatched` needs a live Postgres
(`ESHU_POSTGRES_DSN`) and skips locally; the no-regression case rests on
code identity (same leaf function under the new spelling), and the unit
proof above shows every repointed path runs. Baseline: `origin/main`
`d63ee0876f97` (post-#8681); after: this head; backend n/a (pure
compile-time spelling change, no query or driver path touched); input
shape n/a (no fixture or row-shape change).

Local toolchain note: the repo pins go 1.26.x (`GOTOOLCHAIN=go1.26.9`)
while the host mise `go` is 1.27.2. All Go proof above ran under the
pinned toolchain with `CGO_CFLAGS="-O2 -g -std=gnu17"` (host glibc/gcc
C23 workaround).

## Observability

No-Observability-Change: no metric, span, structured-log, status, or
pprof surface is added, removed, or renamed; the touched files carry no
telemetry emitters and the rewrites preserve every call argument.
