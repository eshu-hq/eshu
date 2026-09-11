// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

// This file is the reducer root's compatibility surface for the projection and readiness families (iamcan, iamescalation, secretsiam, crossscope, value, codecall, shell)
// (issue #6061). It merges the per-family *_compat.go files listed below
// with no behavior change: every alias and forwarder is preserved
// byte-identical under its stanza marker. A family move adds a stanza
// to the matching bucket file and NEVER creates a new *_compat.go
// (docs/internal/design/reducer-target-tree.md). Each entry is deleted
// once its last caller has moved; see the importer-migration child issue.
//
// Stanzas merged here:
//   - iam_can_compat.go
//   - iam_escalation_compat.go
//   - secrets_iam_compat.go
//   - cross_scope_readiness_compat.go
//   - value_flow_compat.go

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/parser/interproc"
	codecall "github.com/eshu-hq/eshu/go/internal/reducer/code/call"
	"github.com/eshu-hq/eshu/go/internal/reducer/code/shell"
	"github.com/eshu-hq/eshu/go/internal/reducer/code/value"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/crossscope"
	"github.com/eshu-hq/eshu/go/internal/reducer/iamcan"
	"github.com/eshu-hq/eshu/go/internal/reducer/iamescalation"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
	"github.com/eshu-hq/eshu/go/internal/reducer/schemadecode"
	"github.com/eshu-hq/eshu/go/internal/reducer/secretsiam"
	"github.com/eshu-hq/eshu/go/internal/reducer/sqlrelationship"
)

// Stanza: iam_can_compat.go (merged; do not recreate this file).
// This file is the reducer root's compatibility surface for the IAM
// CAN_ASSUME / CAN_PERFORM edge family, which moved to [iamcan] (issue #6061).
// The four aliases below are the family's exported wiring contract: the
// reducer command constructs the writers and the cypher package implements
// them, so their root spelling stays put. Root-internal call sites (the
// additive-domain registries and the INVOKES_CLOUD_ACTION intent builder) name
// [iamcan] directly instead of going through a forwarder.

// IAMCanAssumeEdgeWriter is the root spelling of
// [iamcan.IAMCanAssumeEdgeWriter].
type IAMCanAssumeEdgeWriter = iamcan.IAMCanAssumeEdgeWriter

// IAMCanAssumeMaterializationHandler is the root spelling of
// [iamcan.IAMCanAssumeMaterializationHandler].
type IAMCanAssumeMaterializationHandler = iamcan.IAMCanAssumeMaterializationHandler

// IAMCanPerformEdgeWriter is the root spelling of
// [iamcan.IAMCanPerformEdgeWriter].
type IAMCanPerformEdgeWriter = iamcan.IAMCanPerformEdgeWriter

// IAMCanPerformMaterializationHandler is the root spelling of
// [iamcan.IAMCanPerformMaterializationHandler].
type IAMCanPerformMaterializationHandler = iamcan.IAMCanPerformMaterializationHandler

// IAMCanAssumeNodesNotReadyFailureClass is the root spelling of
// [iamcan.IAMCanAssumeNodesNotReadyFailureClass]. internal/storage/postgres
// names it when it classifies a queue row's readiness-gate miss.
const IAMCanAssumeNodesNotReadyFailureClass = iamcan.IAMCanAssumeNodesNotReadyFailureClass

// IAMCanPerformNodesNotReadyFailureClass is the root spelling of
// [iamcan.IAMCanPerformNodesNotReadyFailureClass].
const IAMCanPerformNodesNotReadyFailureClass = iamcan.IAMCanPerformNodesNotReadyFailureClass

// Stanza: iam_escalation_compat.go (merged; do not recreate this file).
// This file is the reducer root's compatibility surface for the IAM
// privilege-escalation edge family, which moved to [iamescalation]
// (issue #6061). It carries only the two names that still have a caller
// outside the family: the writer type DefaultHandlers declares and cmd/reducer
// satisfies, and the failure-class literal internal/storage/postgres' readiness
// claim gate matches. Root-internal call sites (the additive-domain registry)
// name [iamescalation] directly instead of going through a forwarder.

// IAMEscalationEdgeWriter is the root spelling of
// [iamescalation.IAMEscalationEdgeWriter].
type IAMEscalationEdgeWriter = iamescalation.IAMEscalationEdgeWriter

// IAMEscalationNodesNotReadyFailureClass is the root spelling of
// [iamescalation.IAMEscalationNodesNotReadyFailureClass]. internal/storage/
// postgres matches this exact string when it decides to re-enqueue rather than
// dead-letter, so the literal is a storage contract, not just a Go identifier.
const IAMEscalationNodesNotReadyFailureClass = iamescalation.IAMEscalationNodesNotReadyFailureClass

// Stanza: secrets_iam_compat.go (merged; do not recreate this file).
// This file is the transitional compatibility surface for the secrets/IAM
// trust-chain and graph-projection family that moved to [secretsiam]
// (issue #6061). It carries only the names that still have a caller: the
// reducer root's own registration and handler wiring, plus cmd/reducer's
// writer construction, internal/storage/postgres' readiness claim gate and
// evidence loader, and internal/replay/costcounting's cost test. Everything
// else the family exports is reached as secretsiam.X, and each entry here is
// deleted once its last caller has moved.

// SecretsIAMTrustChainLoadStats summarizes the bounded evidence packet one
// intent loaded. internal/storage/postgres' evidence loader returns it. See
// [secretsiam.SecretsIAMTrustChainLoadStats].
type SecretsIAMTrustChainLoadStats = secretsiam.SecretsIAMTrustChainLoadStats

// SecretsIAMTrustChainEvidenceLoader loads the bounded AWS IAM, Kubernetes,
// GCP and Vault source-fact packet. See
// [secretsiam.SecretsIAMTrustChainEvidenceLoader].
type SecretsIAMTrustChainEvidenceLoader = secretsiam.SecretsIAMTrustChainEvidenceLoader

// SecretsIAMTrustChainWriter persists the four reducer-owned secrets/IAM fact
// kinds. See [secretsiam.SecretsIAMTrustChainWriter].
type SecretsIAMTrustChainWriter = secretsiam.SecretsIAMTrustChainWriter

// PostgresSecretsIAMTrustChainWriter is the Postgres-backed trust-chain
// writer cmd/reducer constructs. See
// [secretsiam.PostgresSecretsIAMTrustChainWriter].
type PostgresSecretsIAMTrustChainWriter = secretsiam.PostgresSecretsIAMTrustChainWriter

// SecretsIAMTrustChainHandler is the reducer handler for the trust-chain
// domain. See [secretsiam.SecretsIAMTrustChainHandler].
type SecretsIAMTrustChainHandler = secretsiam.SecretsIAMTrustChainHandler

// SecretsIAMGraphWriter projects exact secrets/IAM read-model rows into the
// canonical graph. A nil one keeps the projection domain unregistered, which
// is how live graph writes stay off until a deployment opts in. See
// [secretsiam.SecretsIAMGraphWriter].
type SecretsIAMGraphWriter = secretsiam.SecretsIAMGraphWriter

// SecretsIAMGraphProjectionHandler is the reducer handler for the secrets/IAM
// graph projection domain. See
// [secretsiam.SecretsIAMGraphProjectionHandler].
type SecretsIAMGraphProjectionHandler = secretsiam.SecretsIAMGraphProjectionHandler

// SecretsIAMEndpointNotReadyFailureClass is the retryable failure class the
// cross-scope readiness gate returns. internal/storage/postgres' reducer queue
// matches this exact string when it decides to re-enqueue rather than
// dead-letter, so the literal is a storage contract, not just a Go identifier.
// See [secretsiam.SecretsIAMEndpointNotReadyFailureClass].
const SecretsIAMEndpointNotReadyFailureClass = secretsiam.SecretsIAMEndpointNotReadyFailureClass

// secretsIAMTrustChainDomainDefinition forwards to
// [secretsiam.TrustChainDomainDefinition].
func secretsIAMTrustChainDomainDefinition() DomainDefinition {
	return secretsiam.TrustChainDomainDefinition()
}

// secretsIAMGraphProjectionDomainDefinition forwards to
// [secretsiam.GraphProjectionDomainDefinition].
func secretsIAMGraphProjectionDomainDefinition() DomainDefinition {
	return secretsiam.GraphProjectionDomainDefinition()
}

// Stanza: cross_scope_readiness_compat.go (merged; do not recreate this file).
// This file is the transitional compatibility surface for the cross-scope
// producer-readiness floor and dependency catalog that moved to [crossscope]
// (issue #6061). Reducer-root call sites keep their current spelling; each
// entry is deleted once its last caller has moved into a family subpackage.

// CrossScopeDependency declares that a consumer reducer domain reads canonical
// facts a producer domain writes in a DIFFERENT ingestion scope. The consumer's
// cross-scope active-fact load can run before the producer has committed its
// latest output, so producer completion must schedule the canonical consumer
// again.
type CrossScopeDependency = reducercontract.CrossScopeDependency

// CrossScopeConsumerDomains forwards to [crossscope.ConsumerDomains].
func CrossScopeConsumerDomains() []Domain {
	return crossscope.ConsumerDomains()
}

// CrossScopeCompletionEdge is one producer-to-consumer fanout edge derived
// from the cross-scope dependency catalog.
type CrossScopeCompletionEdge = crossscope.CompletionEdge

// CrossScopeCompletionEdges forwards to [crossscope.CompletionEdges].
func CrossScopeCompletionEdges() []CrossScopeCompletionEdge {
	return crossscope.CompletionEdges()
}

// crossScopeDependenciesForRegistration forwards to
// [crossscope.DependenciesForRegistration].
func crossScopeDependenciesForRegistration(domain Domain) []CrossScopeDependency {
	return crossscope.DependenciesForRegistration(domain)
}

// CrossScopeProducerNotReadyFailureClass is the durable failure_class a
// cross-scope consumer domain self-classifies with when a producer it declares
// a CrossScopeDependency on has not yet activated its generation for the
// relevant scope. See [crossscope.ProducerNotReadyFailureClass].
const CrossScopeProducerNotReadyFailureClass = crossscope.ProducerNotReadyFailureClass

// crossScopeProducerNotReadyError marks a cross-scope producer-readiness miss
// as retryable. See [crossscope.ProducerNotReadyError].
type crossScopeProducerNotReadyError = crossscope.ProducerNotReadyError

// CrossScopeProducerReadiness answers whether the producer scopes a consumer
// depends on have finished publishing. See [crossscope.ProducerReadiness].
type CrossScopeProducerReadiness = crossscope.ProducerReadiness

// CrossScopeProducerReadinessByDomain answers readiness for each producer
// domain separately. See [crossscope.ProducerReadinessByDomain].
type CrossScopeProducerReadinessByDomain = crossscope.ProducerReadinessByDomain

// Stanza: value_flow_compat.go (merged; do not recreate this file).
// This file is the transitional compatibility surface for the value-flow
// fixpoint family that moved to [value] (issue #6061; relocated from
// [valueflow] to code/value with the taint/value package-clause fix, same
// issue). Reducer-root call sites keep their current spelling; each entry is
// deleted once its last caller has moved into a family subpackage.
//
// code_value_flow_stale_cleanup_runner.go stays in root: it is a side runner
// that needs the root PartitionLeaseManager and Service.startSideRunners
// wiring, the same reason the code_call_projection_* runners stay.
// code_value_flow_backfill_state_marker.go moved to [value] as
// [value.BackfillStateMarker] with the code/ tree move (#6609); its only
// root caller, projected_source_edge_backfill.go, keeps the alias below.

// CodeValueFlowBackfillStateMarker is the root spelling of
// [value.BackfillStateMarker].
type CodeValueFlowBackfillStateMarker = value.BackfillStateMarker

// GraphValueFlowCloudSinkTargetLoader loads graph-backed cloud sink edges for
// the value-flow fixpoint. See [value.GraphCloudSinkTargetLoader].
type GraphValueFlowCloudSinkTargetLoader = value.GraphCloudSinkTargetLoader

// ValueFlowCloudSinkTargetsCypher is the bounded Cypher query cloud sink
// target loading runs. See [value.CloudSinkTargetsCypher].
const ValueFlowCloudSinkTargetsCypher = value.CloudSinkTargetsCypher

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

// ValueFlowProgramAssemblyStats summarizes one Program assembly cycle. See
// [value.ProgramAssemblyStats].
type ValueFlowProgramAssemblyStats = value.ProgramAssemblyStats

// BuildValueFlowProgram forwards to [value.BuildProgram].
func BuildValueFlowProgram(input ValueFlowProgramInput) (interproc.Program, ValueFlowProgramAssemblyStats) {
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

// ValueFlowFixpointProjectionResult records the visible outcome of a
// post-summary fixpoint projection. See
// [value.FixpointProjectionResult].
type ValueFlowFixpointProjectionResult = value.FixpointProjectionResult

// Stanza: code-call family move (#6609; no prior compat file).
// The code-call extraction, entity-index, resolver, and intent-building family
// moved to [codecall] (go/internal/reducer/code/call). The handler
// (code_call_materialization.go) and the seven code_call_projection_* runner
// files stay in root: the handler composes code-call rows with the
// handles_route, runs_in, and invokes_cloud_action families, and the runner
// needs the root lease and shared-projection machinery. The exported
// forwarders keep the reducer.X spelling for callers outside this package; the
// unexported spellings keep the runner files' call sites unchanged. Each entry is deleted
// once its last caller names [codecall] directly.

// ExtractCodeCallRows forwards to [codecall.ExtractRows].
func ExtractCodeCallRows(envelopes []facts.Envelope) ([]string, []map[string]any) {
	return codecall.ExtractRows(envelopes)
}

// ExtractAllCodeRelationshipRows forwards to
// [codecall.ExtractAllRelationshipRows].
func ExtractAllCodeRelationshipRows(envelopes []facts.Envelope) (
	codeCallRepoIDs []string,
	codeCallRows []map[string]any,
	metaclassRepoIDs []string,
	metaclassRows []map[string]any,
) {
	return codecall.ExtractAllRelationshipRows(envelopes)
}

// codeEntityIndex is [codecall.EntityIndex] for the root handles_route,
// runs_in, invokes_cloud_action, and symbol-runtime builders.
type codeEntityIndex = codecall.EntityIndex

// buildCodeEntityIndex forwards to [codecall.BuildEntityIndex].
func buildCodeEntityIndex(envelopes []facts.Envelope) codeEntityIndex {
	return codecall.BuildEntityIndex(envelopes)
}

// extractAllCodeRelationshipRowsWithIndex forwards to
// [codecall.ExtractAllRelationshipRowsWithIndex].
func extractAllCodeRelationshipRowsWithIndex(envelopes []facts.Envelope) (
	codeCallRepoIDs []string,
	codeCallRows []map[string]any,
	metaclassRepoIDs []string,
	metaclassRows []map[string]any,
	entityIndex codeEntityIndex,
	quarantined []quarantinedFact,
) {
	return codecall.ExtractAllRelationshipRowsWithIndex(envelopes)
}

// buildCodeCallProjectionContexts forwards to
// [schemadecode.BuildProjectionContexts], the owner the moved family also
// calls.
func buildCodeCallProjectionContexts(envelopes []facts.Envelope, generationID string) map[string]ProjectionContext {
	return schemadecode.BuildProjectionContexts(envelopes, generationID)
}

// buildCodeCallSharedIntentRows forwards to [codecall.BuildSharedIntentRows].
func buildCodeCallSharedIntentRows(
	rows []map[string]any,
	contextByRepoID map[string]ProjectionContext,
	createdAt time.Time,
	evidenceSource string,
	deltaScopesByRepoID map[string]codecall.DeltaFileScope,
) []SharedProjectionIntentRow {
	return codecall.BuildSharedIntentRows(rows, contextByRepoID, createdAt, evidenceSource, deltaScopesByRepoID)
}

// buildCodeCallRefreshIntentsWithDeltaFileScopes forwards to
// [codecall.BuildRefreshIntentsWithDeltaFileScopes].
func buildCodeCallRefreshIntentsWithDeltaFileScopes(
	contextByRepoID map[string]ProjectionContext,
	deltaScopesByRepoID map[string]codecall.DeltaFileScope,
	createdAt time.Time,
) []SharedProjectionIntentRow {
	return codecall.BuildRefreshIntentsWithDeltaFileScopes(contextByRepoID, deltaScopesByRepoID, createdAt)
}

// buildCodeCallFileScopesByRepoID forwards to [codecall.BuildFileScopesByRepoID].
func buildCodeCallFileScopesByRepoID(envelopes []facts.Envelope) codecall.FileScopeBuildResult {
	return codecall.BuildFileScopesByRepoID(envelopes)
}

// codeCallReferencedSymbolKeys forwards to [codecall.ReferencedSymbolKeys].
func codeCallReferencedSymbolKeys(envelopes []facts.Envelope) []string {
	return codecall.ReferencedSymbolKeys(envelopes)
}

// resolveContainingCodeEntityID forwards to [codecall.ResolveContainingEntityID].
func resolveContainingCodeEntityID(index codeEntityIndex, rawPath string, relativePath string, line int) string {
	return codecall.ResolveContainingEntityID(index, rawPath, relativePath, line)
}

// codeCallEndpointEntityType forwards to [codecall.EndpointEntityType].
func codeCallEndpointEntityType(index codeEntityIndex, repositoryID string, entityID string) string {
	return codecall.EndpointEntityType(index, repositoryID, entityID)
}

// codeCallPathKeys forwards to [codecall.PathKeys].
func codeCallPathKeys(rawPath string, relativePath string) []string {
	return codecall.PathKeys(rawPath, relativePath)
}

// codeCallInt forwards to [codecall.PayloadInt].
func codeCallInt(values ...any) int {
	return codecall.PayloadInt(values...)
}

// mapSlice forwards to [payloadcore.MapSlice], the owner the moved family
// also calls.
func mapSlice(value any) []map[string]any {
	return payloadcore.MapSlice(value)
}

// codeCallEvidenceSource is [codecall.EvidenceSource] for the root handler and
// the code_call_projection_work.go runner file.
const codeCallEvidenceSource = codecall.EvidenceSource

// pythonMetaclassEvidenceSource is [codecall.PythonMetaclassEvidenceSource]
// for the root handler and the code_call_projection_work.go runner file.
const pythonMetaclassEvidenceSource = codecall.PythonMetaclassEvidenceSource

// codeCallPartitionKeyVersion is [codecall.PartitionKeyVersion], kept for the
// code_call_projection_partitions.go runner file.
const codeCallPartitionKeyVersion = codecall.PartitionKeyVersion

// codeCallPayloadBool forwards to [codecall.PayloadBool] for the
// code_call_projection_work.go runner file.
func codeCallPayloadBool(payload map[string]any, key string) bool {
	return codecall.PayloadBool(payload, key)
}

// Stanza: shell-exec family move (#6061; no prior compat file).
// The shell-exec fact extraction, materialization, and shared-intent row
// construction family moved to [shell] (go/internal/reducer/code/shell).
// Every entry keeps the reducer.X spelling (exported) or the unexported
// root call sites' spelling (the sibling delta-gate/retract-reachability
// cross-domain proofs and the factload benchmark) unchanged. Each entry is
// deleted once its last caller names [shell] directly.

// ShellExecIntentWriter is the root spelling of [shell.ExecIntentWriter].
type ShellExecIntentWriter = shell.ExecIntentWriter

// shellExecMaterializationFactKinds is the root spelling of
// [shell.MaterializationFactKinds]. The reducer root's
// factload_materialization_bench_test.go corpus-coverage guard reads it.
var shellExecMaterializationFactKinds = shell.MaterializationFactKinds

// ShellExecMaterializationHandler is the root spelling of
// [shell.ExecMaterializationHandler].
type ShellExecMaterializationHandler = shell.ExecMaterializationHandler

// ExtractShellExecRows forwards to [shell.ExtractExecRows].
func ExtractShellExecRows(envelopes []facts.Envelope) ([]string, []map[string]any) {
	return shell.ExtractExecRows(envelopes)
}

// loadShellExecMaterializationFacts forwards to
// [shell.LoadMaterializationFacts]. The reducer root's
// factload_materialization_bench_test.go benches it under this spelling.
func loadShellExecMaterializationFacts(
	ctx context.Context,
	loader FactLoader,
	scopeID string,
	generationID string,
) ([]facts.Envelope, error) {
	return shell.LoadMaterializationFacts(ctx, loader, scopeID, generationID)
}

// buildShellExecRefreshIntents forwards to [shell.BuildRefreshIntents]. The
// reducer root's sibling_edge_intent_delta_gate_test.go drives this
// alongside inheritance.BuildRefreshIntents and sqlrelationship.BuildRefreshIntents
// through one cross-domain table.
func buildShellExecRefreshIntents(
	deltaScope sqlrelationship.DeltaScope,
	repoIDs []string,
	contextByRepoID map[string]ProjectionContext,
	createdAt time.Time,
) []SharedProjectionIntentRow {
	return shell.BuildRefreshIntents(deltaScope, repoIDs, contextByRepoID, createdAt)
}

// buildShellExecSharedIntentRows forwards to [shell.BuildSharedIntentRows].
// The reducer root's sibling_edge_intent_retract_reachability_test.go drives
// this alongside inheritance.BuildSharedIntentRows and
// sqlrelationship.BuildSharedIntentRows through one cross-domain table.
func buildShellExecSharedIntentRows(
	edgeRows []map[string]any,
	deltaScope sqlrelationship.DeltaScope,
	repoIDs []string,
	contextByRepoID map[string]ProjectionContext,
	createdAt time.Time,
) []SharedProjectionIntentRow {
	return shell.BuildSharedIntentRows(edgeRows, deltaScope, repoIDs, contextByRepoID, createdAt)
}
