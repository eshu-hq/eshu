// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sqlrelationship

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/facts"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
	"github.com/eshu-hq/eshu/go/internal/reducer/schemadecode"
	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
	codegraphv1 "github.com/eshu-hq/eshu/sdk/go/factschema/codegraph/v1"
)

// FactLoader loads fact envelopes for one scope generation. Alias for
// [factload.FactLoader], mirroring the reducer root's own alias
// (scoped_fact_loader_compat.go) so this family's handler keeps its existing
// field spelling without importing the reducer root (issue #6061).
type FactLoader = factload.FactLoader

// Intent is one queued reducer unit of work. Alias for [reducercontract.Intent].
type Intent = reducercontract.Intent

// Result is the outcome of one intent handler execution. Alias for
// [reducercontract.Result].
type Result = reducercontract.Result

// Domain names a reducer intent's target domain. Alias for
// [reducercontract.Domain].
type Domain = reducercontract.Domain

// ResultStatusSucceeded means the execution completed successfully.
const ResultStatusSucceeded = reducercontract.ResultStatusSucceeded

// IntentStatus is the lifecycle status of one queued reducer intent. Alias
// for [reducercontract.IntentStatus].
type IntentStatus = reducercontract.IntentStatus

// IntentStatusPending means the intent is ready to be claimed.
const IntentStatusPending = reducercontract.IntentStatusPending

// DomainSQLRelationshipMaterialization owns SQL relationship materialization.
const DomainSQLRelationshipMaterialization = reducercontract.DomainSQLRelationshipMaterialization

// DomainCodeCallMaterialization owns code-call materialization. Referenced
// only by this family's own tests, which assert the handler rejects a
// mismatched domain.
const DomainCodeCallMaterialization = reducercontract.DomainCodeCallMaterialization

// DomainSQLRelationships is the shared-projection domain the SQL relationship
// family's durable intents are filed under.
const DomainSQLRelationships = reducercontract.DomainSQLRelationships

// SharedProjectionIntentRow is one durable shared-domain projection intent.
// Alias for [sharedintent.Row].
type SharedProjectionIntentRow = sharedintent.Row

// SharedProjectionIntentInput holds the parameters for building one
// deterministic shared projection intent row. Alias for [sharedintent.Input].
type SharedProjectionIntentInput = sharedintent.Input

// ProjectionContext carries the acceptance-unit identity a shared-projection
// intent is emitted under. Alias for [sharedintent.ProjectionContext].
type ProjectionContext = sharedintent.ProjectionContext

// BuildSharedProjectionIntent forwards to [sharedintent.Build].
func BuildSharedProjectionIntent(input SharedProjectionIntentInput) SharedProjectionIntentRow {
	return sharedintent.Build(input)
}

// isRepoRefreshRow reports whether a row is a per-repo refresh intent.
func isRepoRefreshRow(row SharedProjectionIntentRow) bool {
	return payloadcore.AnyToString(row.Payload["intent_type"]) == sharedintent.RepoRefreshIntentType
}

// RepoRefreshIntentType, repoRefreshAction, and retractViaRefreshKey forward
// to their [sharedintent] equivalents, mirroring the reducer root's own
// shared_projection_worker_refresh_fence.go.
const (
	RepoRefreshIntentType = sharedintent.RepoRefreshIntentType
	repoRefreshAction     = sharedintent.RepoRefreshAction
	retractViaRefreshKey  = sharedintent.RetractViaRefreshKey
)

// repoWideRetractRefreshPartitionKey forwards to
// [sharedintent.RepoWideRetractRefreshPartitionKey].
func repoWideRetractRefreshPartitionKey(domain, repoID string) string {
	return sharedintent.RepoWideRetractRefreshPartitionKey(domain, repoID)
}

// deltaScopeRepositorySet forwards to [sharedintent.DeltaScopeRepositorySet].
func deltaScopeRepositorySet(repositoryIDs []string) map[string]struct{} {
	return sharedintent.DeltaScopeRepositorySet(repositoryIDs)
}

// applyRepoRefreshDeltaScope forwards to
// [sharedintent.ApplyRepoRefreshDeltaScope].
func applyRepoRefreshDeltaScope(
	payload map[string]any,
	repoID string,
	deltaRepositoryIDs map[string]struct{},
	filePathsByRepoID map[string][]string,
) {
	sharedintent.ApplyRepoRefreshDeltaScope(payload, repoID, deltaRepositoryIDs, filePathsByRepoID)
}

// semanticPayloadString forwards to [payloadcore.SemanticPayloadString].
func semanticPayloadString(payload map[string]any, key string) string {
	return payloadcore.SemanticPayloadString(payload, key)
}

// payloadMap forwards to [payloadcore.PayloadMap].
func payloadMap(payload map[string]any, key string) map[string]any {
	return payloadcore.PayloadMap(payload, key)
}

// semanticPayloadStringSlice forwards to [payloadcore.SemanticPayloadStringSlice].
func semanticPayloadStringSlice(payload map[string]any, key string) []string {
	return payloadcore.SemanticPayloadStringSlice(payload, key)
}

// anyToString forwards to [payloadcore.AnyToString].
func anyToString(v any) string {
	return payloadcore.AnyToString(v)
}

// mapSlice forwards to [payloadcore.MapSlice].
func mapSlice(value any) []map[string]any {
	return payloadcore.MapSlice(value)
}

// copyPayload forwards to [payloadcore.CopyPayload].
func copyPayload(m map[string]any) map[string]any {
	return payloadcore.CopyPayload(m)
}

// codeCallInt returns the first value convertible to int, or 0 if none is.
// Duplicated from the reducer root's codeCallInt (code_call_materialization_path_helpers.go)
// rather than imported: that helper is owned by the code_call family, which
// has not moved out of root yet, so this package cannot import it without
// violating the "never import the reducer root" rule (issue #6061). It is a
// four-branch type switch with no reducer-specific behavior, so a local copy
// carries no drift risk worth a shared package for.
func codeCallInt(values ...any) int {
	for _, value := range values {
		switch typed := value.(type) {
		case int:
			return typed
		case int32:
			return int(typed)
		case int64:
			return int(typed)
		case float32:
			return int(typed)
		case float64:
			return int(typed)
		}
	}
	return 0
}

// Fact-kind names the scoped loader filters on. Forwarders to [factload],
// mirroring the reducer root's own scoped_fact_loader_compat.go.
const (
	factKindContentEntity = factload.FactKindContentEntity
	factKindFile          = factload.FactKindFile
	factKindRepository    = factload.FactKindRepository
)

// factKindLoader is the optional kind-filtering extension a FactLoader may
// implement. Alias for [factload.FactKindLoader].
type factKindLoader = factload.FactKindLoader

// factPayloadValueLoader is the optional payload-value-filtering extension a
// FactLoader may implement. Alias for [factload.FactPayloadValueLoader].
type factPayloadValueLoader = factload.FactPayloadValueLoader

// loadFactsForKinds forwards to [factload.LoadFactsForKinds].
func loadFactsForKinds(
	ctx context.Context,
	loader FactLoader,
	scopeID string,
	generationID string,
	factKinds []string,
) ([]facts.Envelope, error) {
	return factload.LoadFactsForKinds(ctx, loader, scopeID, generationID, factKinds)
}

// loadFactsForKindAndPayloadValue forwards to
// [factload.LoadFactsForKindAndPayloadValue].
func loadFactsForKindAndPayloadValue(
	ctx context.Context,
	loader FactLoader,
	scopeID string,
	generationID string,
	factKind string,
	payloadKey string,
	payloadValues []string,
) ([]facts.Envelope, error) {
	return factload.LoadFactsForKindAndPayloadValue(
		ctx, loader, scopeID, generationID, factKind, payloadKey, payloadValues)
}

// classifyFactLoadError forwards to [factload.ClassifyFactLoadError].
func classifyFactLoadError(err error) error {
	return factload.ClassifyFactLoadError(err)
}

// decodeCodegraphRepository and decodeCodegraphFile forward to the
// [schemadecode] typed decode seam, mirroring the reducer root's own
// decode_seam_compat.go.
var (
	decodeCodegraphFile       = schemadecode.DecodeCodegraphFile
	decodeCodegraphRepository = schemadecode.DecodeCodegraphRepository
)

// codeCallDeltaRelativePathsFromRepository returns the deduplicated union of a
// decoded codegraphv1.Repository's DeltaRelativePaths and
// DeltaDeletedRelativePaths. Duplicated from the reducer root's
// code_call_materialization_intents.go rather than imported: that helper is
// owned by the code_call family, which has not moved out of root yet, so this
// package cannot import it without violating the "never import the reducer
// root" rule (issue #6061). It operates only on the SDK-owned
// codegraphv1.Repository type, so a local copy carries no drift risk worth a
// shared package for.
func codeCallDeltaRelativePathsFromRepository(repository codegraphv1.Repository) []string {
	seen := make(map[string]struct{})
	var paths []string
	for _, relativePath := range repository.DeltaRelativePaths {
		if _, ok := seen[relativePath]; ok {
			continue
		}
		seen[relativePath] = struct{}{}
		paths = append(paths, relativePath)
	}
	for _, relativePath := range repository.DeltaDeletedRelativePaths {
		if _, ok := seen[relativePath]; ok {
			continue
		}
		seen[relativePath] = struct{}{}
		paths = append(paths, relativePath)
	}
	return paths
}
