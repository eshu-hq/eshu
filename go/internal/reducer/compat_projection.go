// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

// This file is the reducer root's compatibility surface for the projection and readiness families (iamcan, iamescalation, secretsiam, crossscope, platformfam)
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
//   - platform_compat.go

import (
	"context"
	"log/slog"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/crossscope"
	"github.com/eshu-hq/eshu/go/internal/reducer/iamcan"
	"github.com/eshu-hq/eshu/go/internal/reducer/iamescalation"
	"github.com/eshu-hq/eshu/go/internal/reducer/platformfam"
	"github.com/eshu-hq/eshu/go/internal/reducer/secretsiam"
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

// newCrossScopeProducerNotReadyError forwards to
// [crossscope.NewProducerNotReadyError].
func newCrossScopeProducerNotReadyError(
	consumerDomain Domain,
	scopeID string,
	generationID string,
	producerDomains []Domain,
) crossScopeProducerNotReadyError {
	return crossscope.NewProducerNotReadyError(consumerDomain, scopeID, generationID, producerDomains)
}

// CrossScopeProducerReadiness answers whether the producer scopes a consumer
// depends on have finished publishing. See [crossscope.ProducerReadiness].
type CrossScopeProducerReadiness = crossscope.ProducerReadiness

// CrossScopeProducerReadinessByDomain answers readiness for each producer
// domain separately. See [crossscope.ProducerReadinessByDomain].
type CrossScopeProducerReadinessByDomain = crossscope.ProducerReadinessByDomain

// crossScopeProducerReadinessSignal is the floor's answer, captured BEFORE the
// consumer's cross-scope load runs. See [crossscope.ProducerReadinessSignal].
type crossScopeProducerReadinessSignal = crossscope.ProducerReadinessSignal

// checkCrossScopeProducerReadinessBeforeLoad forwards to
// [crossscope.CheckProducerReadinessBeforeLoad].
func checkCrossScopeProducerReadinessBeforeLoad(
	ctx context.Context,
	readiness CrossScopeProducerReadiness,
	intent Intent,
	now time.Time,
	crossScopeLookupPlanned bool,
) (crossScopeProducerReadinessSignal, error) {
	return crossscope.CheckProducerReadinessBeforeLoad(ctx, readiness, intent, now, crossScopeLookupPlanned)
}

// crossScopeUnreadyProducers forwards to [crossscope.UnreadyProducers].
func crossScopeUnreadyProducers(
	signal crossScopeProducerReadinessSignal,
	resolvedByProducer map[Domain]int,
) []Domain {
	return crossscope.UnreadyProducers(signal, resolvedByProducer)
}

// logCrossScopeProducerNotReadyDefer forwards to
// [crossscope.LogProducerNotReadyDefer].
func logCrossScopeProducerNotReadyDefer(
	ctx context.Context,
	logger *slog.Logger,
	intent Intent,
	now time.Time,
	producerDomains []Domain,
) {
	crossscope.LogProducerNotReadyDefer(ctx, logger, intent, now, producerDomains)
}

// Stanza: platform_compat.go (merged; do not recreate this file).
// This file is the transitional compatibility surface for the platform family
// that moved to [platformfam] (issue #6061). Reducer-root call sites and the
// external packages that name these types -- cmd/reducer, internal/query and
// internal/storage/postgres -- keep their current spelling; each entry is
// deleted once its last caller has moved out of the reducer root.

// PlatformMaterializationWrite captures the bounded canonical reconciliation
// request for one platform materialization reducer intent. See
// [platformfam.PlatformMaterializationWrite].
type PlatformMaterializationWrite = platformfam.PlatformMaterializationWrite

// PlatformMaterializationWriteResult captures the canonical platform
// materialization write outcome returned by the backend adapter. See
// [platformfam.PlatformMaterializationWriteResult].
type PlatformMaterializationWriteResult = platformfam.PlatformMaterializationWriteResult

// PlatformMaterializationWriter persists one platform materialization request
// into a canonical reducer-owned target. See
// [platformfam.PlatformMaterializationWriter].
type PlatformMaterializationWriter = platformfam.PlatformMaterializationWriter

// PlatformGraphLocker coordinates writes that can touch the same Platform.id.
// See [platformfam.PlatformGraphLocker].
type PlatformGraphLocker = platformfam.PlatformGraphLocker

// WorkloadMaterializationReplayer requeues workload materialization after
// stronger deployment evidence becomes available for the same scope
// generation. See [platformfam.WorkloadMaterializationReplayer].
type WorkloadMaterializationReplayer = platformfam.WorkloadMaterializationReplayer

// CrossRepoRelationshipResolver is the cross-repo resolution seam the platform
// materialization handler depends on. [CrossRepoRelationshipHandler] is the
// production implementation. See [platformfam.CrossRepoRelationshipResolver].
type CrossRepoRelationshipResolver = platformfam.CrossRepoRelationshipResolver

// PlatformMaterializationHandler reduces one platform materialization intent
// into a bounded canonical write request. See
// [platformfam.PlatformMaterializationHandler].
type PlatformMaterializationHandler = platformfam.PlatformMaterializationHandler

// PostgresPlatformMaterializationWriter persists one platform-materialization
// reducer reconciliation into the shared fact store. See
// [platformfam.PostgresPlatformMaterializationWriter].
type PostgresPlatformMaterializationWriter = platformfam.PostgresPlatformMaterializationWriter

// TerraformRuntimeFamily describes one Terraform-managed runtime family. See
// [platformfam.TerraformRuntimeFamily].
type TerraformRuntimeFamily = platformfam.TerraformRuntimeFamily

// RuntimeFamilies forwards to [platformfam.RuntimeFamilies].
func RuntimeFamilies() []TerraformRuntimeFamily {
	return platformfam.RuntimeFamilies()
}

// LookupRuntimeFamily forwards to [platformfam.LookupRuntimeFamily].
func LookupRuntimeFamily(kind string) *TerraformRuntimeFamily {
	return platformfam.LookupRuntimeFamily(kind)
}

// InferTerraformRuntimeFamilyKind forwards to
// [platformfam.InferTerraformRuntimeFamilyKind].
func InferTerraformRuntimeFamilyKind(content string) string {
	return platformfam.InferTerraformRuntimeFamilyKind(content)
}

// InferRuntimeFamilyKindFromIdentifiers forwards to
// [platformfam.InferRuntimeFamilyKindFromIdentifiers].
func InferRuntimeFamilyKindFromIdentifiers(values []string) string {
	return platformfam.InferRuntimeFamilyKindFromIdentifiers(values)
}

// InferInfrastructureRuntimeFamilyKind forwards to
// [platformfam.InferInfrastructureRuntimeFamilyKind].
func InferInfrastructureRuntimeFamilyKind(resourceTypes, moduleSources []string) string {
	return platformfam.InferInfrastructureRuntimeFamilyKind(resourceTypes, moduleSources)
}

// MatchesServiceModuleSource forwards to
// [platformfam.MatchesServiceModuleSource].
func MatchesServiceModuleSource(source, kind string) bool {
	return platformfam.MatchesServiceModuleSource(source, kind)
}

// TerraformPlatformEvidenceKind forwards to
// [platformfam.TerraformPlatformEvidenceKind].
func TerraformPlatformEvidenceKind(kind, scope string) string {
	return platformfam.TerraformPlatformEvidenceKind(kind, scope)
}

// FormatPlatformKindLabel forwards to [platformfam.FormatPlatformKindLabel].
func FormatPlatformKindLabel(kind string) string {
	return platformfam.FormatPlatformKindLabel(kind)
}
