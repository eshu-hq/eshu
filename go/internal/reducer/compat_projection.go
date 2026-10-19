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
// [secretsiam] directly (#6626 tail bucket 1).
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

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/parser/interproc"
	codecall "github.com/eshu-hq/eshu/go/internal/reducer/code/call"
	"github.com/eshu-hq/eshu/go/internal/reducer/code/call/materialization"
	"github.com/eshu-hq/eshu/go/internal/reducer/code/call/projection"
	"github.com/eshu-hq/eshu/go/internal/reducer/code/value"
	"github.com/eshu-hq/eshu/go/internal/reducer/code/value/cleanup"
	"github.com/eshu-hq/eshu/go/internal/reducer/gpphase"
	worker "github.com/eshu-hq/eshu/go/internal/reducer/intents/shared/worker"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
	"github.com/eshu-hq/eshu/go/internal/reducer/schemadecode"
	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
)

// Stanza: value_flow_compat.go (merged; do not recreate this file).
// This file is the transitional compatibility surface for the value-flow
// fixpoint family that moved to [value] (issue #6061; relocated from
// [valueflow] to code/value with the taint/value package-clause fix, same
// issue). Reducer-root call sites keep their current spelling; each entry is
// deleted once its last caller has moved into a family subpackage.
//
// code_value_flow_stale_cleanup_runner.go moved to [cleanup]
// (go/internal/reducer/code/value/cleanup, issue #6061): its only production
// blocker was the root PartitionLeaseManager, hoisted to
// sharedintent.PartitionLeaseManager (H1). Its Service.startSideRunners
// wiring case stays in root as code_value_flow_stale_cleanup_wiring_test.go,
// with its own minimal fakes, because only the reducer root can construct
// Service.
// code_value_flow_backfill_state_marker.go moved to [value] as
// [value.BackfillStateMarker] with the code/ tree move (#6609); its only
// root caller, projected_source_edge_backfill.go, keeps the alias below.

// CodeValueFlowBackfillStateMarker is the root spelling of
// [value.BackfillStateMarker].
type CodeValueFlowBackfillStateMarker = value.BackfillStateMarker

// CodeValueFlowStaleCleanupRunner is the root spelling of [cleanup.Runner].
type CodeValueFlowStaleCleanupRunner = cleanup.Runner

// CodeValueFlowStaleCleanupRunnerConfig is the root spelling of
// [cleanup.RunnerConfig].
type CodeValueFlowStaleCleanupRunnerConfig = cleanup.RunnerConfig

// CodeValueFlowCurrentGeneration is the root spelling of
// [cleanup.CurrentGeneration]. internal/storage/postgres' generation reader
// returns it.
type CodeValueFlowCurrentGeneration = cleanup.CurrentGeneration

// CodeTaintStaleEvidenceRetractor is the root spelling of
// [cleanup.TaintStaleEvidenceRetractor].
type CodeTaintStaleEvidenceRetractor = cleanup.TaintStaleEvidenceRetractor

// CodeInterprocStaleEvidenceRetractor is the root spelling of
// [cleanup.InterprocStaleEvidenceRetractor].
type CodeInterprocStaleEvidenceRetractor = cleanup.InterprocStaleEvidenceRetractor

// GraphValueFlowCloudSinkTargetLoader loads graph-backed cloud sink edges for
// the value-flow fixpoint. See [value.GraphCloudSinkTargetLoader].
type GraphValueFlowCloudSinkTargetLoader = value.GraphCloudSinkTargetLoader

// ValueFlowCloudSinkWorkloadRowsCypher is the first of the two bounded Cypher
// statements cloud sink target loading runs. See
// [value.CloudSinkWorkloadRowsCypher].
const ValueFlowCloudSinkWorkloadRowsCypher = value.CloudSinkWorkloadRowsCypher

// ValueFlowCloudSinkTargetsByPairCypher is the second of the two bounded Cypher
// statements cloud sink target loading runs. See
// [value.CloudSinkTargetsByPairCypher].
const ValueFlowCloudSinkTargetsByPairCypher = value.CloudSinkTargetsByPairCypher

// ValueFlowFixpointComponentStore is the durable weak-component cache store
// port. See [value.FixpointComponentStore].
type ValueFlowFixpointComponentStore = value.FixpointComponentStore

// NewValueFlowFixpointCache forwards to [value.NewFixpointCache].
func NewValueFlowFixpointCache() *value.FixpointCache {
	return value.NewFixpointCache()
}

// ValueFlowProgramInput is the bounded in-memory snapshot used to assemble a
// value-flow Program. See [value.ProgramInput].
type ValueFlowProgramInput = value.ProgramInput

// ValueFlowCallEdge is one active code-call edge used by Program assembly.
// See [value.CallEdge].
type ValueFlowCallEdge = value.CallEdge

// BuildValueFlowProgram forwards to [value.BuildProgram].
func BuildValueFlowProgram(input ValueFlowProgramInput) (interproc.Program, value.ProgramAssemblyStats) {
	return value.BuildProgram(input)
}

// FunctionSummarySnapshotLoader reloads durable value-flow summaries for the
// cross-repo fixpoint. See [value.FunctionSummarySnapshotLoader].
type FunctionSummarySnapshotLoader = value.FunctionSummarySnapshotLoader

// FunctionSourceSnapshotLoader reloads durable value-flow source ports for
// the cross-repo fixpoint. See [value.FunctionSourceSnapshotLoader].
type FunctionSourceSnapshotLoader = value.FunctionSourceSnapshotLoader

// FunctionGraphIDSnapshotLoader reloads durable FunctionID->Function.uid
// mappings. See [value.FunctionGraphIDSnapshotLoader].
type FunctionGraphIDSnapshotLoader = value.FunctionGraphIDSnapshotLoader

// ValueFlowFixpointEvidenceLoader composes durable function summaries,
// source ports, graph ids, and graph-backed cloud sink targets into the
// existing code_interproc_evidence reducer input. See
// [value.FixpointEvidenceLoader].
type ValueFlowFixpointEvidenceLoader = value.FixpointEvidenceLoader

// ValueFlowFixpointEvidenceProjector writes summary-fixpoint findings as
// TAINT_FLOWS_TO evidence. See [value.FixpointEvidenceProjector].
type ValueFlowFixpointEvidenceProjector = value.FixpointEvidenceProjector

// Stanza: code-call family move (#6609; no prior compat file).
// The code-call extraction, entity-index, resolver, and intent-building
// family moved to [codecall] (go/internal/reducer/code/call); the handler,
// handles_route, runs_in, invokes_cloud_action, and symbol-runtime refresh
// files moved to [materialization]
// (go/internal/reducer/code/call/materialization, issue #6061); the seven
// code_call_projection_* runner files moved to [projection]
// (go/internal/reducer/code/call/projection, issue #6061). The exported
// forwarders keep the reducer.X spelling for callers outside these
// packages. Each entry is deleted once its last caller names
// [codecall]/[materialization]/[projection] directly.

// CodeCallProjectionRunner is the root spelling of [projection.Runner].
type CodeCallProjectionRunner = projection.Runner

// CodeCallProjectionRunnerConfig is the root spelling of
// [projection.RunnerConfig].
type CodeCallProjectionRunnerConfig = projection.RunnerConfig

// DefaultCodeCallProjectionLeaseOwnerPrefix is the root spelling of
// [projection.DefaultLeaseOwnerPrefix].
const DefaultCodeCallProjectionLeaseOwnerPrefix = projection.DefaultLeaseOwnerPrefix

// DefaultCodeCallAcceptanceScanLimit is the root spelling of
// [projection.DefaultAcceptanceScanLimit].
const DefaultCodeCallAcceptanceScanLimit = projection.DefaultAcceptanceScanLimit

// CodeCallProjectionFilePartitionKeyPrefix forwards to
// [projection.FilePartitionKeyPrefix]. internal/storage/postgres calls this
// as reducer.CodeCallProjectionFilePartitionKeyPrefix.
func CodeCallProjectionFilePartitionKeyPrefix() string {
	return projection.FilePartitionKeyPrefix()
}

// CodeCallProjectionPartitionCandidateReader is the root spelling of
// [projection.PartitionCandidateReader].
type CodeCallProjectionPartitionCandidateReader = projection.PartitionCandidateReader

// CodeCallProjectionUnhashedCandidateReader is the root spelling of
// [projection.UnhashedCandidateReader].
type CodeCallProjectionUnhashedCandidateReader = projection.UnhashedCandidateReader

// CodeCallProjectionRefreshFenceLookup is the root spelling of
// [projection.RefreshFenceLookup].
type CodeCallProjectionRefreshFenceLookup = projection.RefreshFenceLookup

// CodeCallMaterializationHandler is the root spelling of
// [materialization.Handler].
type CodeCallMaterializationHandler = materialization.Handler

// CodeCallIntentWriter is the root spelling of [materialization.IntentWriter].
type CodeCallIntentWriter = materialization.IntentWriter

// ExtractSymbolRuntimeIntentRows is the root spelling of
// [materialization.ExtractIntentRows]. internal/ifa's symbol-runtime family
// odù fixtures call this as reducer.ExtractSymbolRuntimeIntentRows.
func ExtractSymbolRuntimeIntentRows(
	envelopes []facts.Envelope,
	generationID string,
	createdAt time.Time,
) []sharedintent.Row {
	return materialization.ExtractIntentRows(envelopes, generationID, createdAt)
}

// BuildHandlesRouteIntentRowsForQueryProof is the root spelling of
// [materialization.BuildRouteIntentRowsForQueryProof]. internal/mcp and
// internal/query's route-to-caller proof tests call this as
// reducer.BuildHandlesRouteIntentRowsForQueryProof.
func BuildHandlesRouteIntentRowsForQueryProof(envelopes []facts.Envelope) []sharedintent.Row {
	return materialization.BuildRouteIntentRowsForQueryProof(envelopes)
}

// buildCodeCallProjectionContexts forwards to
// [schemadecode.BuildProjectionContexts]. Kept for the root-only sibling
// cross-domain proofs (rationale_edge_materialization.go and its partition
// test, sibling_edge_intent_delta_gate_test.go) that still call it
// unqualified; [materialization] and [codecall] call schemadecode directly.
func buildCodeCallProjectionContexts(envelopes []facts.Envelope, generationID string) map[string]ProjectionContext {
	return schemadecode.BuildProjectionContexts(envelopes, generationID)
}

// mapSlice forwards to [payloadcore.MapSlice], the owner the moved family
// also calls.
func mapSlice(value any) []map[string]any {
	return payloadcore.MapSlice(value)
}

// pythonMetaclassEvidenceSource is [codecall.PythonMetaclassEvidenceSource],
// kept for the root's python_metaclass_materialization_test.go.
const pythonMetaclassEvidenceSource = codecall.PythonMetaclassEvidenceSource

// Stanza: shared-projection worker/runner plumbing (H5 root-remnant fold,
// issue #6061; folds shared_projection_worker.go, shared_projection_runner.go,
// shared_projection_config.go, shared_projection_partition_candidate.go,
// shared_projection_batch_selection.go, selection_phase_durations.go, and
// acceptance_observability.go). SelectPartitionBatch, PartitionBatchResult,
// and filterRowsByReadiness were dropped, not folded: their only callers were
// root test/production files, repointed to worker.SelectPartitionBatch/
// worker.FilterRowsByReadiness directly (D12). Each remaining entry is
// deleted once its last caller names [worker]/[sharedintent] directly.

// maxSharedSelectionScanLimit mirrors [worker]'s unexported scan cap (const
// aliases cannot cross packages to an unexported name).
const maxSharedSelectionScanLimit = 10_000

// SharedProjectionEdgeWriter is the root spelling of [sharedintent.EdgeWriter].
type SharedProjectionEdgeWriter = sharedintent.EdgeWriter

// PartitionLeaseManager is the root spelling of [sharedintent.PartitionLeaseManager].
type PartitionLeaseManager = sharedintent.PartitionLeaseManager

// AcceptedGenerationLookup is the root spelling of [sharedintent.AcceptedGenerationLookup].
type AcceptedGenerationLookup = sharedintent.AcceptedGenerationLookup

// AcceptedGenerationPrefetch is the root spelling of [sharedintent.AcceptedGenerationPrefetch].
type AcceptedGenerationPrefetch = sharedintent.AcceptedGenerationPrefetch

// PartitionProcessorConfig is the root spelling of [worker.PartitionProcessorConfig].
type PartitionProcessorConfig = worker.PartitionProcessorConfig

// PartitionProcessResult is the root spelling of [worker.PartitionProcessResult].
type PartitionProcessResult = worker.PartitionProcessResult

// ProcessPartitionOnce forwards to [worker.ProcessPartitionOnce].
func ProcessPartitionOnce(
	ctx context.Context,
	now time.Time,
	cfg PartitionProcessorConfig,
	leaseManager PartitionLeaseManager,
	reader worker.IntentReader,
	edgeWriter SharedProjectionEdgeWriter,
	acceptedGen AcceptedGenerationLookup,
	prefetch AcceptedGenerationPrefetch,
	readinessLookup gpphase.ReadinessLookup,
	readinessPrefetch gpphase.ReadinessPrefetch,
	endpointPresence gpphase.EndpointPresenceLookup,
	refreshFence worker.RefreshFenceLookup,
	firstProjection FirstProjectionLookup,
	unroutableWriter worker.UnroutableWriter,
) (result PartitionProcessResult, retErr error) {
	return worker.ProcessPartitionOnce(ctx, now, cfg, leaseManager, reader, edgeWriter, acceptedGen, prefetch, readinessLookup, readinessPrefetch, endpointPresence, refreshFence, firstProjection, unroutableWriter)
}

// The default* constants are root spellings of [worker]'s exported defaults.
const (
	defaultBatchLimit         = worker.DefaultBatchLimit
	defaultSharedPollInterval = worker.DefaultPollInterval
	defaultEvidenceSource     = worker.DefaultEvidenceSource
)

// DefaultSharedProjectionLeaseOwnerPrefix is the root spelling of
// [worker.DefaultLeaseOwnerPrefix].
const DefaultSharedProjectionLeaseOwnerPrefix = worker.DefaultLeaseOwnerPrefix

// sharedProjectionDomains copies [worker.Domains]'s result once at init (no
// cross-package var/const alias exists in Go).
var sharedProjectionDomains = worker.Domains()

// SharedProjectionRunnerConfig is the root spelling of [worker.RunnerConfig].
type SharedProjectionRunnerConfig = worker.RunnerConfig

// SharedProjectionRunner is the root spelling of [worker.Runner].
type SharedProjectionRunner = worker.Runner

// LoadSharedProjectionConfig forwards to [worker.LoadConfig].
func LoadSharedProjectionConfig(getenv func(string) string) SharedProjectionRunnerConfig {
	return worker.LoadConfig(getenv)
}

// SharedProjectionPartitionCandidateReader is the root spelling of
// [worker.PartitionCandidateReader].
type SharedProjectionPartitionCandidateReader = worker.PartitionCandidateReader

// SharedProjectionUnhashedCandidateReader is the root spelling of
// [worker.UnhashedCandidateReader].
type SharedProjectionUnhashedCandidateReader = worker.UnhashedCandidateReader

// LatestIntentsByRepoAndPartition forwards to [worker.LatestIntentsByRepoAndPartition].
func LatestIntentsByRepoAndPartition(intents []sharedintent.Row) ([]sharedintent.Row, []string) {
	return worker.LatestIntentsByRepoAndPartition(intents)
}

// FilterAuthoritativeIntents forwards to [worker.FilterAuthoritativeIntents].
func FilterAuthoritativeIntents(
	intents []sharedintent.Row,
	acceptedGen AcceptedGenerationLookup,
) (active []sharedintent.Row, staleIDs []string) {
	return worker.FilterAuthoritativeIntents(intents, acceptedGen)
}

// sharedAcceptanceLookupEvent is the root spelling of [worker.AcceptanceLookupEvent].
type sharedAcceptanceLookupEvent = worker.AcceptanceLookupEvent

// sharedAcceptanceTelemetry is the root spelling of [worker.AcceptanceTelemetry].
type sharedAcceptanceTelemetry = worker.AcceptanceTelemetry

// Stanza: repo-wide-retract refresh fence (H5 root-remnant fold, #6061).
// RepoRefreshIntentType is a storage/cypher wire contract (#5998).
const (
	UnroutableReasonMissingRequiredField = sharedintent.UnroutableReasonMissingRequiredField
	UnroutableReasonNoStatementForType   = sharedintent.UnroutableReasonNoStatementForType
	RepoRefreshIntentType                = sharedintent.RepoRefreshIntentType
	repoRefreshAction                    = sharedintent.RepoRefreshAction
	retractViaRefreshKey                 = sharedintent.RetractViaRefreshKey
)

func RepoWideRetractDomains() []string { return sharedintent.RepoWideRetractDomains() }
