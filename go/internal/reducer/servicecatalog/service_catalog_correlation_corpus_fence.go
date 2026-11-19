// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package servicecatalog

import (
	"context"
	"fmt"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/relationships"
)

// ServiceCatalogCorrelationResolutionNotReadyFailureClass classifies a
// deferral of a service catalog correlation intent whose service deployment
// and dependency evidence would be read while the relationship corpus fence is
// open: some active scope's current relationship generation is
// retired-or-pending, so the by-repos resolved read omits that scope's rows.
//
// Registered as a non-counting reducer retry class
// (nonCountingReducerRetryFailureClasses in
// go/internal/storage/postgres/reducer_queue_readiness_sql.go): the intent is
// waiting on upstream resolution, not failing on its own merits. Without this
// gate the handler committed a service materialization generation from the
// partial set, superseding the prior generation and reporting spurious removed
// deployment and dependency evidence on changed-since; the only producer of
// these intents never reopens them when the foreign scope completes (#7258).
const ServiceCatalogCorrelationResolutionNotReadyFailureClass = "service_catalog_correlation_resolution_not_ready"

// serviceCatalogCorrelationResolutionNotReadyError defers the whole intent,
// before any correlation or materialization write, until the relationship
// corpus fence closes.
type serviceCatalogCorrelationResolutionNotReadyError struct {
	scopeID      string
	generationID string
	// repoIDs and blockingScopeIDs carry the narrowed wait key (#10877):
	// the normalized read set and the verdict-time blocking set the
	// restricted read ran under. Empty on the global path, which parks on
	// the coarse wait.
	repoIDs          []string
	blockingScopeIDs []string
}

func (e serviceCatalogCorrelationResolutionNotReadyError) Error() string {
	return fmt.Sprintf(
		"relationship corpus fence open (an active scope's relationship generation is retired or pending) for service catalog scope %s generation %s; deferring service catalog correlation rather than materializing against a partial resolved set",
		e.scopeID,
		e.generationID,
	)
}

// Retryable reports that the deferral must be retried once the fence closes.
func (serviceCatalogCorrelationResolutionNotReadyError) Retryable() bool { return true }

// FailureClass returns the non-counting readiness class for the queue.
func (serviceCatalogCorrelationResolutionNotReadyError) FailureClass() string {
	return ServiceCatalogCorrelationResolutionNotReadyFailureClass
}

// RestrictedCorpusFencedResolvedRelationshipLoader is the narrowed read
// (#10877): the verdict, the rows, and the verdict-time blocking scope set
// from one statement snapshot. Declared locally rather than imported from
// the reducer root, whose own restricted loader is structurally identical: a
// family subpackage never imports the reducer root (issue #6061), and Go
// interfaces are satisfied structurally. Exported so the reducer wiring can
// pin the two declarations identical at compile time: the handler asserts
// this interface at runtime with a silent global-fence fallback, so a
// signature drift would otherwise only show up as over-waiting.
type RestrictedCorpusFencedResolvedRelationshipLoader interface {
	GetResolvedRelationshipsForReposWithRestrictedFence(
		ctx context.Context,
		repoIDs []string,
	) (rows []relationships.ResolvedRelationship, blockingScopeIDs []string, complete bool, err error)
}

// fingerprintedRestrictedCorpusFencedResolvedRelationshipLoader is the
// narrowed read plus the input fingerprint of exactly the rows returned,
// computed in the same statement (#10877 parity with #8283). Same local
// declaration rule as RestrictedCorpusFencedResolvedRelationshipLoader.
type fingerprintedRestrictedCorpusFencedResolvedRelationshipLoader interface {
	GetFingerprintedResolvedRelationshipsForReposWithRestrictedFence(
		ctx context.Context,
		repoIDs []string,
	) (rows []relationships.ResolvedRelationship, blockingScopeIDs []string, complete bool, fingerprint serviceCatalogRestrictedRead, err error)
}

// serviceCatalogRestrictedRead is the narrowed read's fingerprint: the
// repository set asked for, the blocking set the verdict ran under, and the
// input fingerprint of exactly the rows returned.
type serviceCatalogRestrictedRead struct {
	RepoIDs          []string
	BlockingScopeIDs []string
	Value            string
}

// attachServiceRelationshipEvidence loads the resolved cross-repo relationships
// for the correlated services' repositories once, through the corpus-fenced
// read, and attaches BOTH the deployment (#1985) and dependencies (#1987)
// evidence families to the matching per-service writes. Both families share
// the same resolved_relationships source and a single bounded load; the build
// helpers partition the loaded set by relationship type so neither family
// admits the other's edges.
//
// Handle calls it before any write. An open corpus fence returns
// serviceCatalogCorrelationResolutionNotReadyError so the intent defers with
// nothing written; a read failure is an ordinary error. It is a no-op (no
// read, no fence) when no loader is wired, no service is being materialized,
// or no decision carries a repository, so both families stay purely additive.
//
// With a restricted loader the narrowed read runs instead of the global one:
// the verdict covers only the producing scopes of the read set, the deferral
// parks on the verdict-time blocking set, and the returned fingerprint lets
// Handle record the read on success for the late-arrival reopen gate.
func (h ServiceCatalogCorrelationHandler) attachServiceRelationshipEvidence(
	ctx context.Context,
	scopeID string,
	generationID string,
	writes []ServiceMaterializationWrite,
	decisions []ServiceCatalogCorrelationDecision,
) (fingerprint reducercontract.ResolvedReadFingerprint, fingerprinted bool, err error) {
	if h.DeploymentRelationshipLoader == nil || len(writes) == 0 {
		return reducercontract.ResolvedReadFingerprint{}, false, nil
	}
	repoByService := serviceRepositoryIndex(decisions)
	repoIDs := distinctServiceRepositoryIDs(writes, repoByService)
	if len(repoIDs) == 0 {
		return reducercontract.ResolvedReadFingerprint{}, false, nil
	}
	if restricted, ok := h.DeploymentRelationshipLoader.(fingerprintedRestrictedCorpusFencedResolvedRelationshipLoader); ok {
		resolved, blocking, complete, read, rerr := restricted.
			GetFingerprintedResolvedRelationshipsForReposWithRestrictedFence(ctx, repoIDs)
		if rerr != nil {
			return reducercontract.ResolvedReadFingerprint{}, false, fmt.Errorf("load service deployment and dependency relationships: %w", rerr)
		}
		if !complete {
			return reducercontract.ResolvedReadFingerprint{}, false, serviceCatalogCorrelationResolutionNotReadyError{
				scopeID: scopeID, generationID: generationID, repoIDs: repoIDs, blockingScopeIDs: blocking,
			}
		}
		if derr := attachServiceRelationshipEvidenceRows(writes, repoByService, resolved); derr != nil {
			return reducercontract.ResolvedReadFingerprint{}, false, derr
		}
		return reducercontract.ResolvedReadFingerprint{RepoIDs: read.RepoIDs, Value: read.Value}, true, nil
	}
	if restricted, ok := h.DeploymentRelationshipLoader.(RestrictedCorpusFencedResolvedRelationshipLoader); ok {
		resolved, blocking, complete, rerr := restricted.
			GetResolvedRelationshipsForReposWithRestrictedFence(ctx, repoIDs)
		if rerr != nil {
			return reducercontract.ResolvedReadFingerprint{}, false, fmt.Errorf("load service deployment and dependency relationships: %w", rerr)
		}
		if !complete {
			return reducercontract.ResolvedReadFingerprint{}, false, serviceCatalogCorrelationResolutionNotReadyError{
				scopeID: scopeID, generationID: generationID, repoIDs: repoIDs, blockingScopeIDs: blocking,
			}
		}
		if derr := attachServiceRelationshipEvidenceRows(writes, repoByService, resolved); derr != nil {
			return reducercontract.ResolvedReadFingerprint{}, false, derr
		}
		return reducercontract.ResolvedReadFingerprint{}, false, nil
	}
	resolved, complete, err := h.DeploymentRelationshipLoader.GetResolvedRelationshipsForReposWithCorpusFence(ctx, repoIDs)
	if err != nil {
		return reducercontract.ResolvedReadFingerprint{}, false, fmt.Errorf("load service deployment and dependency relationships: %w", err)
	}
	if !complete {
		return reducercontract.ResolvedReadFingerprint{}, false, serviceCatalogCorrelationResolutionNotReadyError{scopeID: scopeID, generationID: generationID}
	}
	if err := attachServiceRelationshipEvidenceRows(writes, repoByService, resolved); err != nil {
		return reducercontract.ResolvedReadFingerprint{}, false, err
	}
	return reducercontract.ResolvedReadFingerprint{}, false, nil
}

// attachServiceRelationshipEvidenceRows partitions one resolved set into the
// deployment and dependency families and attaches them to the matching
// per-service writes. Pure: shared by the global and narrowed read paths so
// the two cannot diverge in what they attach.
func attachServiceRelationshipEvidenceRows(
	writes []ServiceMaterializationWrite,
	repoByService map[string]string,
	resolved []relationships.ResolvedRelationship,
) error {
	deploymentByRepo := groupDeploymentRelationshipsByRepo(resolved)
	dependencyByRepo := groupDependencyRelationshipsByRepo(resolved)
	for i := range writes {
		repoID := repoByService[writes[i].ServiceID]
		if repoID == "" {
			continue
		}
		writes[i].Deployment = buildServiceDeploymentEvidence(deploymentByRepo[repoID])
		writes[i].Dependencies = buildServiceDependencyEvidence(dependencyByRepo[repoID])
	}
	return nil
}

// CorpusWaitRepoIDs reports the narrowed repository set of a service catalog
// correlation deferral for the queue's wait key. Empty on the global path.
func (e serviceCatalogCorrelationResolutionNotReadyError) CorpusWaitRepoIDs() []string {
	return e.repoIDs
}

// CorpusWaitBlockingScopeIDs reports the verdict-time blocking scope set of
// a service catalog correlation deferral for the queue's wait key. Empty on
// the global path.
func (e serviceCatalogCorrelationResolutionNotReadyError) CorpusWaitBlockingScopeIDs() []string {
	return e.blockingScopeIDs
}

// prepareServiceMaterializations builds the per-service ownership writes and
// attaches the corpus-fenced deployment and dependency families and the
// readiness-fenced runtime family. It performs reads only, so Handle runs it
// before any write; an open corpus fence or runtime fence returns its
// retryable deferral unwrapped. It returns nil writes when
// MaterializationWriter is nil, preserving the existing correlation contract.
// The fingerprint is the narrowed relationship read's input fingerprint with
// whether it describes exactly the rows read; Handle records it on success
// for the late-arrival reopen gate (#10877).
func (h ServiceCatalogCorrelationHandler) prepareServiceMaterializations(
	ctx context.Context,
	intent reducercontract.Intent,
	decisions []ServiceCatalogCorrelationDecision,
) (writes []ServiceMaterializationWrite, fingerprint reducercontract.ResolvedReadFingerprint, fingerprinted bool, err error) {
	if h.MaterializationWriter == nil {
		return nil, reducercontract.ResolvedReadFingerprint{}, false, nil
	}
	writes = buildServiceOwnershipMaterializations(intent.IntentID, intent.ScopeID, decisions)
	fingerprint, fingerprinted, err = h.attachServiceRelationshipEvidence(ctx, intent.ScopeID, intent.GenerationID, writes, decisions)
	if err != nil {
		return nil, reducercontract.ResolvedReadFingerprint{}, false, err
	}
	// The runtime fence and read run after the corpus fence and before any
	// write (#7816): an in-flight WorkloadInstance producer defers the intent
	// with nothing written, and the read never runs while the fence is open.
	if err := h.checkServiceRuntimeReadiness(ctx, intent, writes, decisions); err != nil {
		return nil, reducercontract.ResolvedReadFingerprint{}, false, err
	}
	if err := h.attachServiceRuntimeEvidence(ctx, writes, decisions); err != nil {
		return nil, reducercontract.ResolvedReadFingerprint{}, false, err
	}
	return writes, fingerprint, fingerprinted, nil
}

// recordServiceCatalogReopenFingerprint stores the input fingerprint of a run
// that is about to succeed, fenced on its exact claim (#10877 parity with
// the deployable-unit path). Nil recorder, an unfingerprinted read, or an
// unclaimed run records nothing.
func (h ServiceCatalogCorrelationHandler) recordServiceCatalogReopenFingerprint(
	ctx context.Context,
	intent reducercontract.Intent,
	fingerprint reducercontract.ResolvedReadFingerprint,
	fingerprinted bool,
) error {
	if h.ReopenFingerprintRecorder == nil || !fingerprinted || intent.ClaimedAt == nil {
		return nil
	}
	if err := h.ReopenFingerprintRecorder.RecordServiceCatalogCorrelationReopenFingerprint(
		ctx, intent.IntentID, intent.ClaimedAt.UTC(), fingerprint,
	); err != nil {
		return fmt.Errorf("record service catalog reopen fingerprint: %w", err)
	}
	return nil
}
