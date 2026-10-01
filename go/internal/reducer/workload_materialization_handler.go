// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/reducer/code/value/affected"
	"github.com/eshu-hq/eshu/go/internal/reducer/workload/retract"
	"github.com/eshu-hq/eshu/go/internal/relationships"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// ResolvedRelationshipLoader loads resolved repo relationships for one scope.
type ResolvedRelationshipLoader interface {
	GetResolvedRelationships(
		ctx context.Context,
		scopeID string,
	) ([]relationships.ResolvedRelationship, error)
}

// GenerationScopedResolvedRelationshipLoader can return resolved
// relationships for one exact scope generation, avoiding mixed active
// snapshots when multiple relationship generations exist for the same scope.
type GenerationScopedResolvedRelationshipLoader interface {
	GetResolvedRelationshipsForGeneration(
		ctx context.Context,
		scopeID string,
		generationID string,
	) ([]relationships.ResolvedRelationship, error)
}

// RepositoryScopedResolvedRelationshipLoader returns active resolved
// relationships touching one or more repositories, regardless of which
// repository generation produced the relationship evidence.
type RepositoryScopedResolvedRelationshipLoader interface {
	GetResolvedRelationshipsForRepos(
		ctx context.Context,
		repoIDs []string,
	) ([]relationships.ResolvedRelationship, error)
}

// RepoScopeIdentity holds the active scope and generation for a repository.
type RepoScopeIdentity struct {
	ScopeID      string
	GenerationID string
}

// DeploymentRepoScopeResolver resolves repository graph IDs to their active
// scope and generation identities, enabling cross-repo fact loading during
// workload materialization.
type DeploymentRepoScopeResolver interface {
	ResolveRepoActiveGenerations(ctx context.Context, repoIDs []string) (map[string]RepoScopeIdentity, error)
}

// WorkloadProjectionInputLoader can provide already-correlated workload
// candidates and environment overlays for workload materialization.
type WorkloadProjectionInputLoader interface {
	LoadWorkloadProjectionInputs(
		ctx context.Context,
		intent Intent,
	) ([]WorkloadCandidate, map[string][]string, error)
}

// WorkloadProjectionInputs is what one workload materialization intent loads.
type WorkloadProjectionInputs struct {
	// Candidates are the admitted candidates selected by the intent's entity
	// keys: what this intent writes.
	Candidates []WorkloadCandidate
	// ScopeCandidates are the scope generation's complete admitted set, taken
	// before the entity-key filter. The #7285 stale-edge retract builds its
	// keep-list from these and never from Candidates.
	ScopeCandidates []WorkloadCandidate
	// DeploymentEnvironments overlays environments by repository id.
	DeploymentEnvironments map[string][]string
	// Selection splits the admitted-vs-selected outcome with its reason, so
	// a zero selection is distinguishable from a mismatch (#7384). The
	// correlated loader reports SelectionNoKeys when the intent carries no
	// entity keys; loaders that do not filter leave the zero value.
	Selection CandidateSelectionReport
}

// ScopeWorkloadProjectionInputLoader is a WorkloadProjectionInputLoader that
// can also supply the scope generation's complete admitted set. The handler
// retracts stale repository edges only through a loader with this capability;
// with any other loader it skips the retract and warns.
type ScopeWorkloadProjectionInputLoader interface {
	LoadWorkloadProjectionScopeInputs(ctx context.Context, intent Intent) (WorkloadProjectionInputs, error)
}

// InfrastructurePlatformLookup loads platforms provisioned by infrastructure
// repositories that have already materialized PROVISIONS_PLATFORM graph edges.
type InfrastructurePlatformLookup interface {
	ListProvisionedPlatforms(
		ctx context.Context,
		repoIDs []string,
	) (map[string][]InfrastructurePlatformRow, error)
}

// WorkloadMaterializationHandler reduces one workload materialization intent
// into canonical graph writes (workloads, instances, deployment sources,
// runtime platforms). It loads facts from the content store, extracts workload
// candidates, builds projection rows, and writes them to Neo4j.
type WorkloadMaterializationHandler struct {
	FactLoader                   FactLoader
	ResolvedLoader               ResolvedRelationshipLoader
	InputLoader                  WorkloadProjectionInputLoader
	InfrastructurePlatformLookup InfrastructurePlatformLookup
	Materializer                 *WorkloadMaterializer
	DependencyLookup             WorkloadDependencyGraphLookup
	WorkloadDependencyEdgeWriter SharedProjectionEdgeWriter
	// InstanceRetractionLookup resolves previously materialized WorkloadInstance
	// ids that this generation's projection superseded (e.g. a pre-canonical
	// environment alias key retired by the #5473 environment-alias contract), so
	// Handle can retract the orphaned node and its INSTANCE_OF/DEPLOYMENT_SOURCE/
	// RUNS_ON edges after the replacement MERGE write commits. A nil lookup (the
	// default) makes retraction a no-op, keeping the hot workload materialization
	// path byte-identical.
	InstanceRetractionLookup WorkloadInstanceRetractionLookup
	// RepositoryEdgeReader reads current DEFINES / repository-side
	// EXPOSES_ENDPOINT targets so the #7285 stale-edge retract deletes only
	// stale ids and issues no DELETE in steady state. Nil runs the keep-list
	// deletes unconditionally (correct, but a zero-row DELETE per run).
	RepositoryEdgeReader retract.Reader
	PhasePublisher       GraphProjectionPhasePublisher
	// RepairQueue captures exact workload-materialization phase rows when graph
	// writes have committed but phase publication fails.
	RepairQueue GraphProjectionPhaseRepairQueue
	// EndpointPresenceWriter records property-keyed (repo_id, path) :Endpoint
	// presence after the endpoint nodes commit so the handles_route projection
	// gate can prove a specific endpoint exists (#2809). A nil writer (the
	// default) makes presence publication a no-op, keeping the hot workload
	// materialization path byte-identical.
	EndpointPresenceWriter EndpointPresenceWriter
	// AffectedGraph runs the value-flow refresh emit gate over the repos this
	// projection materialized. Nil skips the gate and reports affected (fail
	// open); production wires the graph query runner.
	AffectedGraph affected.Runner
	// Tracer bounds the refresh emit-gate read in a span. Nil skips the span
	// (test wiring); production wires the reducer tracer.
	Tracer trace.Tracer
	// Instruments records the eshu_dp_value_flow_refresh_gate_evaluations_total
	// counter. Nil skips emission (test wiring); production wires the reducer
	// instruments.
	Instruments *telemetry.Instruments
}

// workloadMaterializationTiming keeps success-path stage timings comparable
// with SQL and semantic reducer logs without changing handler behavior.
type workloadMaterializationTiming struct {
	loadInputsDuration      time.Duration
	buildProjectionDuration time.Duration
	graphWriteDuration      time.Duration
	instanceRetract         time.Duration
	dependencyReconcile     time.Duration
	dependencyRetract       time.Duration
	dependencyWrite         time.Duration
	phasePublishDuration    time.Duration
	totalDuration           time.Duration
}

// Handle executes the workload materialization reduction path.
func (h WorkloadMaterializationHandler) Handle(
	ctx context.Context,
	intent Intent,
) (Result, error) {
	totalStarted := time.Now()
	var timing workloadMaterializationTiming

	if intent.Domain != DomainWorkloadMaterialization {
		return Result{}, fmt.Errorf(
			"workload materialization handler does not accept domain %q",
			intent.Domain,
		)
	}
	if h.FactLoader == nil {
		return Result{}, fmt.Errorf("workload materialization fact loader is required")
	}
	if h.Materializer == nil {
		return Result{}, fmt.Errorf("workload materialization materializer is required")
	}

	loadStarted := time.Now()
	inputs, keep, err := h.loadProjectionInputs(ctx, intent)
	timing.loadInputsDuration = time.Since(loadStarted)
	if err != nil {
		return Result{}, err
	}
	candidates, deploymentEnvironments := inputs.Candidates, inputs.DeploymentEnvironments
	repositoryFacts, err := loadScopeRepositoryFacts(ctx, h.FactLoader, intent.ScopeID, intent.GenerationID)
	if err != nil {
		return Result{}, err
	}
	if len(candidates) == 0 {
		phaseStarted := time.Now()
		repoIDs := repositoryGraphIDsFromEnvelopes(repositoryFacts)
		// No candidate for this intent's keys; the scope keep-list still holds
		// what sibling intents wrote (#7285).
		if _, err := h.retractStaleRepositoryEdges(ctx, intent, repositoryFacts, keep); err != nil {
			return Result{}, fmt.Errorf("retract stale repository edges: %w", err)
		}
		if err := publishIntentGraphPhaseWithRepair(
			ctx,
			h.PhasePublisher,
			h.RepairQueue,
			intent,
			GraphProjectionKeyspaceServiceUID,
			GraphProjectionPhaseWorkloadMaterialization,
			time.Now().UTC(),
		); err != nil {
			if repairErr := enqueueRepoReadinessPhaseRepairs(
				ctx,
				h.RepairQueue,
				h.EndpointPresenceWriter,
				intent.ScopeID,
				intent.GenerationID,
				repoIDs,
				time.Now().UTC(),
				err,
			); repairErr != nil {
				return Result{}, fmt.Errorf("%w (enqueue repo readiness repairs: %v)", err, repairErr)
			}
			return Result{}, err
		}
		// Also publish the deterministic per-repo readiness row (#2891) for every
		// repo in scope so a route-only repo — one with framework routes but no
		// workload candidate — still resolves its handles_route phase gate (and is
		// then terminalized by the absent-endpoint presence gate), instead of
		// looping forever. Additive to the per-EntityKey publish above.
		if err := publishRepoReadinessPhasesWithRepair(
			ctx,
			h.PhasePublisher,
			h.RepairQueue,
			h.EndpointPresenceWriter,
			intent.ScopeID,
			intent.GenerationID,
			repoIDs,
			time.Now().UTC(),
		); err != nil {
			return Result{}, err
		}
		if err := publishRepoDependencyReadinessFenceWithRepair(
			ctx,
			h.PhasePublisher,
			h.RepairQueue,
			intent,
			time.Now().UTC(),
		); err != nil {
			return Result{}, err
		}
		timing.phasePublishDuration = time.Since(phaseStarted)
		timing.totalDuration = time.Since(totalStarted)
		zeroSelection := inputs.Selection
		zeroSelection.Reason = refineCandidateSelectionReason(zeroSelection, intent.EntityKeys, repoIDs)
		logWorkloadMaterializationCompleted(ctx, intent, candidates, zeroSelection, nil, MaterializeResult{}, timing, 0, 0, 0)
		return Result{
			IntentID:        intent.IntentID,
			Domain:          DomainWorkloadMaterialization,
			Status:          ResultStatusSucceeded,
			EvidenceSummary: "no workload candidates found",
			SubDurations:    workloadMaterializationSubDurations(timing),
		}, nil
	}

	buildStarted := time.Now()
	infrastructurePlatforms, err := h.loadInfrastructurePlatforms(ctx, candidates)
	if err != nil {
		return Result{}, err
	}
	projection := BuildProjectionRowsWithInfrastructurePlatforms(
		candidates,
		deploymentEnvironments,
		infrastructurePlatforms,
	)
	timing.buildProjectionDuration = time.Since(buildStarted)

	graphStarted := time.Now()
	materializeResult, err := h.Materializer.Materialize(ctx, projection)
	timing.graphWriteDuration = time.Since(graphStarted)
	if err != nil {
		return Result{}, fmt.Errorf("materialize workloads: %w", boundDeploymentSourceDeferral(err, intent, time.Now(), h.Materializer))
	}

	// Record property-keyed (repo_id, path) presence for the committed :Endpoint
	// nodes so the handles_route projection gate can prove each endpoint exists
	// before resolving a HANDLES_ROUTE edge against it (#2809). Published only
	// after Materialize succeeds so presence never claims an endpoint that did not
	// commit. Flag-gated: a nil EndpointPresenceWriter (the default) is a no-op.
	if err := publishAPIEndpointRepoPathPresence(
		ctx,
		h.EndpointPresenceWriter,
		intent.ScopeID,
		intent.GenerationID,
		projection.EndpointRows,
		time.Now().UTC(),
	); err != nil {
		return Result{}, fmt.Errorf("record api endpoint repo/path presence: %w", err)
	}

	// Record repo-keyed presence for the committed :Workload nodes so the runs_in
	// projection gate can prove a repo's Workloads exist before resolving a
	// Function-[:RUNS_IN]->Workload edge against them (#2855). Same presence store
	// and writer as the endpoint presence above, a different keyspace. Published
	// only after Materialize succeeds; a nil writer (the default) is a no-op.
	if err := publishRepoWorkloadPresence(
		ctx,
		h.EndpointPresenceWriter,
		intent.ScopeID,
		intent.GenerationID,
		projection.WorkloadRows,
		time.Now().UTC(),
	); err != nil {
		return Result{}, fmt.Errorf("record repo workload presence: %w", err)
	}

	totalWrites := materializeResult.WorkloadsWritten +
		materializeResult.InstancesWritten +
		materializeResult.DeploymentSourcesWritten +
		materializeResult.RuntimePlatformsWritten +
		materializeResult.EndpointsWritten
	repoReadinessRepoIDs := projectionRepoReadinessRepoIDs(projection)

	instanceRetractRows := 0
	if h.InstanceRetractionLookup != nil {
		retractStarted := time.Now()
		instanceRetractRepoIDs, supersededInstanceIDs, err := ReconcileWorkloadInstanceRetraction(
			ctx,
			projection.RepoDescriptors,
			projection.InstanceRows,
			h.InstanceRetractionLookup,
		)
		if err != nil {
			return Result{}, fmt.Errorf("reconcile workload instance retraction: %w", err)
		}
		if len(supersededInstanceIDs) > 0 {
			// instanceRetractRepoIDs is the exact scope ReconcileWorkloadInstanceRetraction
			// used to decide supersession; it MUST be threaded unmodified into the
			// delete-time predicate (see batchWorkloadInstanceRetractCypher) so a
			// concurrent write that re-owns one of these ids under a different repo
			// is never deleted by this stale decision.
			if err := h.Materializer.RetractInstances(
				ctx,
				supersededInstanceIDs,
				instanceRetractRepoIDs,
				EvidenceSourceWorkloads,
			); err != nil {
				return Result{}, fmt.Errorf("retract superseded workload instances: %w", err)
			}
			instanceRetractRows = len(supersededInstanceIDs)
			totalWrites += instanceRetractRows
		}
		timing.instanceRetract = time.Since(retractStarted)
	}

	dependencyRetractRows := 0
	dependencyWriteRows := 0
	if h.DependencyLookup != nil && h.WorkloadDependencyEdgeWriter != nil {
		reconcileStarted := time.Now()
		dependencyRows, retractRows, err := ReconcileWorkloadDependencyEdges(
			ctx,
			projection.RepoDescriptors,
			h.DependencyLookup,
		)
		timing.dependencyReconcile = time.Since(reconcileStarted)
		if err != nil {
			return Result{}, fmt.Errorf("reconcile workload dependencies: %w", err)
		}
		if len(retractRows) > 0 {
			retractStarted := time.Now()
			if err := h.WorkloadDependencyEdgeWriter.RetractEdges(
				ctx,
				DomainWorkloadDependency,
				retractRows,
				EvidenceSourceWorkloads,
			); err != nil {
				return Result{}, fmt.Errorf("retract workload dependencies: %w", err)
			}
			timing.dependencyRetract = time.Since(retractStarted)
			dependencyRetractRows = len(retractRows)
			totalWrites += len(retractRows)
		}
		if writeRows := BuildWorkloadDependencyIntentRowsFromEdges(dependencyRows); len(writeRows) > 0 {
			writeStarted := time.Now()
			if _, err := h.WorkloadDependencyEdgeWriter.WriteEdges(
				ctx,
				DomainWorkloadDependency,
				writeRows,
				EvidenceSourceWorkloads,
			); err != nil {
				return Result{}, fmt.Errorf("write workload dependencies: %w", err)
			}
			timing.dependencyWrite = time.Since(writeStarted)
			dependencyWriteRows = len(writeRows)
			totalWrites += len(writeRows)
		}
	}
	// Stale DEFINES / repository-side EXPOSES_ENDPOINT go only after the current
	// edges above committed, and only for full-generation repositories (#7285).
	repositoryEdgeRetract, err := h.retractStaleRepositoryEdges(ctx, intent, repositoryFacts, keep)
	if err != nil {
		return Result{}, fmt.Errorf("retract stale repository edges: %w", err)
	}
	totalWrites += int(repositoryEdgeRetract.DefinesDeleted + repositoryEdgeRetract.EndpointEdgesDeleted)
	phaseStarted := time.Now()
	if err := publishIntentGraphPhaseWithRepair(
		ctx,
		h.PhasePublisher,
		h.RepairQueue,
		intent,
		GraphProjectionKeyspaceServiceUID,
		GraphProjectionPhaseWorkloadMaterialization,
		time.Now().UTC(),
	); err != nil {
		if repairErr := enqueueRepoReadinessPhaseRepairs(
			ctx,
			h.RepairQueue,
			h.EndpointPresenceWriter,
			intent.ScopeID,
			intent.GenerationID,
			repoReadinessRepoIDs,
			time.Now().UTC(),
			err,
		); repairErr != nil {
			return Result{}, fmt.Errorf("%w (enqueue repo readiness repairs: %v)", err, repairErr)
		}
		return Result{}, err
	}
	// Also publish the deterministic per-repo readiness row (#2891) for every repo
	// whose endpoints/workloads this projection materialized — the same repos that
	// fed the presence publishes above. The handles_route/runs_in consumer
	// reconstructs this exact key from (scope, repo_id, generation), so its
	// code-stage intent finds the phase row across the source-run boundary the old
	// intent-keyed lookup could never cross. Additive to the per-EntityKey publish.
	if err := publishRepoReadinessPhasesWithRepair(
		ctx,
		h.PhasePublisher,
		h.RepairQueue,
		h.EndpointPresenceWriter,
		intent.ScopeID,
		intent.GenerationID,
		repoReadinessRepoIDs,
		time.Now().UTC(),
	); err != nil {
		return Result{}, err
	}
	if err := publishRepoDependencyReadinessFenceWithRepair(
		ctx,
		h.PhasePublisher,
		h.RepairQueue,
		intent,
		time.Now().UTC(),
	); err != nil {
		return Result{}, err
	}
	timing.phasePublishDuration = time.Since(phaseStarted)
	timing.totalDuration = time.Since(totalStarted)
	successSelection := inputs.Selection
	successSelection.Reason = refineCandidateSelectionReason(successSelection, intent.EntityKeys, repositoryGraphIDsFromEnvelopes(repositoryFacts))
	logWorkloadMaterializationCompleted(
		ctx,
		intent,
		candidates,
		successSelection,
		projection,
		materializeResult,
		timing,
		instanceRetractRows,
		dependencyRetractRows,
		dependencyWriteRows,
	)

	return Result{
		IntentID: intent.IntentID,
		Domain:   DomainWorkloadMaterialization,
		Status:   ResultStatusSucceeded,
		EvidenceSummary: fmt.Sprintf(
			"materialized %d workloads, %d instances, %d deployment sources, %d runtime platforms, %d endpoints",
			materializeResult.WorkloadsWritten,
			materializeResult.InstancesWritten,
			materializeResult.DeploymentSourcesWritten,
			materializeResult.RuntimePlatformsWritten,
			materializeResult.EndpointsWritten,
		),
		CanonicalWrites: totalWrites,
		SubSignals:      h.refreshResultSignals(ctx, intent, nil, totalWrites, repoReadinessRepoIDs),
		SubDurations:    workloadMaterializationSubDurations(timing),
	}, nil
}
