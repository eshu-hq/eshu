// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

// This file is the reducer root's compatibility surface for the projection and readiness families (crossscope, value, codecall, shell)
// (issue #6061). It merges the per-family *_compat.go files listed below
// with no behavior change: every alias and forwarder is preserved
// byte-identical under its stanza marker. A family move adds a stanza
// to the matching bucket file and NEVER creates a new *_compat.go
// (docs/internal/design/reducer-target-tree.md). Each entry is deleted
// once its last caller has moved; see the importer-migration child issue.
// The iam_can, iam_escalation, and secrets_iam stanzas below were fully
// retired once their last callers named [iamcan], [iamescalation], and
// [secretsiam] directly (#6626 tail bucket 1). The value-flow fixpoint and
// code-call family stanzas below were fully retired once their last callers
// named [value]/[cleanup], [codecall]/[materialization]/[projection],
// [schemadecode], and [payloadcore] directly (#6626 value-flow lane).
//
// Stanzas merged here:
//   - value_flow_compat.go
//
// cross_scope_readiness_compat.go relocated byte-identical to
// compat_decode.go (issue #6061 code/ subtree move) to keep this bucket
// under the 500-line cap; see that file's header for its stanza list.
//
// shell-exec family stanza relocated byte-identical to compat_cloud.go
// (issue #6061 H5 root-remnant fold) to make room for the shared-projection
// worker/runner/refresh-fence stanzas below; see
// that file's header for its stanza list.

// Stanza: value_flow_compat.go (merged; do not recreate this file): fully
// retired (#6626 value-flow lane). The value-flow fixpoint family moved to
// [value] (issue #6061; relocated from [valueflow] to code/value with the
// taint/value package-clause fix, same issue); the stale-cleanup runner moved
// to [cleanup] (go/internal/reducer/code/value/cleanup, issue #6061). Every
// caller below now names the leaf directly; the notes below are ledger.
//
// Retired entries and their canonical spellings:
//   - CodeValueFlowBackfillStateMarker -> [value.BackfillStateMarker]
//     (projected_source_edge_backfill.go)
//   - CodeValueFlowStaleCleanupRunner,
//     CodeValueFlowStaleCleanupRunnerConfig,
//     CodeValueFlowCurrentGeneration,
//     CodeTaintStaleEvidenceRetractor,
//     CodeInterprocStaleEvidenceRetractor -> [cleanup].Runner, .RunnerConfig,
//     .CurrentGeneration, .TaintStaleEvidenceRetractor,
//     .InterprocStaleEvidenceRetractor (cmd/reducer wiring + config,
//     internal/storage/postgres current-generation store)
//   - GraphValueFlowCloudSinkTargetLoader,
//     ValueFlowCloudSinkWorkloadRowsCypher,
//     ValueFlowCloudSinkTargetsByPairCypher,
//     ValueFlowFixpointComponentStore, NewValueFlowFixpointCache,
//     ValueFlowProgramInput, ValueFlowCallEdge, BuildValueFlowProgram,
//     FunctionSummarySnapshotLoader, FunctionSourceSnapshotLoader,
//     FunctionGraphIDSnapshotLoader, ValueFlowFixpointEvidenceLoader,
//     ValueFlowFixpointEvidenceProjector -> [value].GraphCloudSinkTargetLoader,
//     .CloudSinkWorkloadRowsCypher, .CloudSinkTargetsByPairCypher,
//     .FixpointComponentStore, NewFixpointCache, .ProgramInput, .CallEdge,
//     BuildProgram, .FunctionSummarySnapshotLoader,
//     .FunctionSourceSnapshotLoader, .FunctionGraphIDSnapshotLoader,
//     .FixpointEvidenceLoader, .FixpointEvidenceProjector (cmd/reducer
//     value-flow wiring, internal/storage/postgres program loader,
//     internal/backendconformance corpus cases)
//
// code_value_flow_stale_cleanup_runner.go's production blocker was the root
// PartitionLeaseManager, hoisted to sharedintent.PartitionLeaseManager (H1).
// Its Service.startSideRunners wiring case stays in root as
// code_value_flow_stale_cleanup_wiring_test.go, with its own minimal fakes,
// because only the reducer root can construct Service.

// Stanza: code-call family move (#6609; no prior compat file): fully retired
// (#6626 value-flow lane). The code-call extraction, entity-index, resolver,
// and intent-building family moved to [codecall]
// (go/internal/reducer/code/call); the handler, handles_route, runs_in,
// invokes_cloud_action, and symbol-runtime refresh files moved to
// [materialization] (go/internal/reducer/code/call/materialization, issue
// #6061); the seven code_call_projection_* runner files moved to [projection]
// (go/internal/reducer/code/call/projection, issue #6061). Every caller below
// now names the leaf directly; the notes below are ledger.
//
// Retired entries and their canonical spellings:
//   - CodeCallProjectionRunner, CodeCallProjectionRunnerConfig,
//     DefaultCodeCallProjectionLeaseOwnerPrefix,
//     DefaultCodeCallAcceptanceScanLimit,
//     CodeCallProjectionFilePartitionKeyPrefix,
//     CodeCallProjectionPartitionCandidateReader,
//     CodeCallProjectionUnhashedCandidateReader,
//     CodeCallProjectionRefreshFenceLookup -> [projection].Runner,
//     .RunnerConfig, .DefaultLeaseOwnerPrefix, .DefaultAcceptanceScanLimit,
//     FilePartitionKeyPrefix, .PartitionCandidateReader,
//     .UnhashedCandidateReader, .RefreshFenceLookup (cmd/reducer projection
//     config + wiring, internal/storage/postgres intent history + lease
//     heartbeat proof)
//   - CodeCallMaterializationHandler, CodeCallIntentWriter,
//     ExtractSymbolRuntimeIntentRows, BuildHandlesRouteIntentRowsForQueryProof
//     -> [materialization].Handler, .IntentWriter, ExtractIntentRows,
//     BuildRouteIntentRowsForQueryProof (reducer root defaults catalog,
//     internal/ifa symbol-runtime guards, internal/query + internal/mcp
//     route-to-caller proofs)
//   - buildCodeCallProjectionContexts -> [schemadecode.BuildProjectionContexts]
//     (rationale_edge_materialization.go and its partition test; the root
//     ProjectionContext alias in intent_emission.go stays for the
//     rationale/domain-owned signatures)
//   - mapSlice -> [payloadcore.MapSlice] (rationale_edge_materialization.go,
//     code_import_repo_edge.go, code_import_repo_edge_retract.go)
//   - pythonMetaclassEvidenceSource ->
//     [codecall.PythonMetaclassEvidenceSource]
//     (python_metaclass_materialization_test.go)

// Stanza shared-projection worker/runner plumbing: fully retired (#6626
// tail bucket 2). SelectPartitionBatch, PartitionBatchResult, and
// filterRowsByReadiness were dropped, not folded: their only callers were
// root test/production files, repointed to worker.SelectPartitionBatch/
// worker.FilterRowsByReadiness directly (D12). Every remaining entry was
// deleted once its last caller named [worker]/[sharedintent] directly.
// The notes below are ledger.

// maxSharedSelectionScanLimit mirrors [worker]'s unexported scan cap (const
// aliases cannot cross packages to an unexported name). It is retired:
// the root's shared_projection_worker_test.go now carries the literal 10_000
// with a pointer at worker/selection.go, so no root declaration remains.

// Stanza repo-wide-retract refresh fence: fully retired (#6626 tail
// bucket 2). RepoRefreshIntentType is a storage/cypher wire contract
// (#5998); every caller now names [sharedintent] directly. The notes below
// are ledger.
