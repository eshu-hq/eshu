package reducer

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/relationships"
)

// FactLoader loads fact envelopes for one scope generation.
type FactLoader interface {
	ListFacts(ctx context.Context, scopeID, generationID string) ([]facts.Envelope, error)
}

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
	PhasePublisher               GraphProjectionPhasePublisher
	// RepairQueue captures exact workload-materialization phase rows when graph
	// writes have committed but phase publication fails.
	RepairQueue GraphProjectionPhaseRepairQueue
	// EndpointPresenceWriter records property-keyed (repo_id, path) :Endpoint
	// presence after the endpoint nodes commit so the handles_route projection
	// gate can prove a specific endpoint exists (#2809). A nil writer (the
	// default) makes presence publication a no-op, keeping the hot workload
	// materialization path byte-identical.
	EndpointPresenceWriter EndpointPresenceWriter
}

// workloadMaterializationTiming keeps success-path stage timings comparable
// with SQL and semantic reducer logs without changing handler behavior.
type workloadMaterializationTiming struct {
	loadInputsDuration      time.Duration
	buildProjectionDuration time.Duration
	graphWriteDuration      time.Duration
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
	candidates, deploymentEnvironments, err := h.loadProjectionInputs(ctx, intent)
	timing.loadInputsDuration = time.Since(loadStarted)
	if err != nil {
		return Result{}, err
	}
	if len(candidates) == 0 {
		phaseStarted := time.Now()
		repoIDs, repoErr := scopeRepositoryGraphIDs(ctx, h.FactLoader, intent.ScopeID, intent.GenerationID)
		if repoErr != nil {
			return Result{}, repoErr
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
		timing.phasePublishDuration = time.Since(phaseStarted)
		timing.totalDuration = time.Since(totalStarted)
		logWorkloadMaterializationCompleted(ctx, intent, candidates, nil, MaterializeResult{}, timing, 0, 0)
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
		return Result{}, fmt.Errorf("materialize workloads: %w", err)
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
			if err := h.WorkloadDependencyEdgeWriter.WriteEdges(
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
	timing.phasePublishDuration = time.Since(phaseStarted)
	timing.totalDuration = time.Since(totalStarted)
	logWorkloadMaterializationCompleted(
		ctx,
		intent,
		candidates,
		projection,
		materializeResult,
		timing,
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
		SubDurations:    workloadMaterializationSubDurations(timing),
	}, nil
}

// workloadMaterializationSubDurations converts the internal per-phase timing
// struct into the Result.SubDurations map so the service layer can emit
// per-phase log attributes alongside handler_duration_seconds. Keys use the
// same names as the workload materialization log attributes without the
// "_duration_seconds" suffix so callers can reconstruct attribute names
// consistently.
func workloadMaterializationSubDurations(t workloadMaterializationTiming) map[string]float64 {
	return map[string]float64{
		"load_inputs":      t.loadInputsDuration.Seconds(),
		"build_projection": t.buildProjectionDuration.Seconds(),
		"graph_write":      t.graphWriteDuration.Seconds(),
		"dep_reconcile":    t.dependencyReconcile.Seconds(),
		"dep_retract":      t.dependencyRetract.Seconds(),
		"dep_write":        t.dependencyWrite.Seconds(),
		"phase_publish":    t.phasePublishDuration.Seconds(),
	}
}

func (h WorkloadMaterializationHandler) loadInfrastructurePlatforms(
	ctx context.Context,
	candidates []WorkloadCandidate,
) (map[string][]InfrastructurePlatformRow, error) {
	if h.InfrastructurePlatformLookup == nil {
		return nil, nil
	}
	repoIDs := uniqueProvisioningRepoIDs(candidates)
	if len(repoIDs) == 0 {
		return nil, nil
	}
	platforms, err := h.InfrastructurePlatformLookup.ListProvisionedPlatforms(ctx, repoIDs)
	if err != nil {
		return nil, fmt.Errorf("load provisioned infrastructure platforms: %w", err)
	}
	return platforms, nil
}

func uniqueProvisioningRepoIDs(candidates []WorkloadCandidate) []string {
	seen := make(map[string]struct{})
	var repoIDs []string
	for _, candidate := range candidates {
		for _, repoID := range candidate.ProvisioningRepoIDs {
			if repoID == "" {
				continue
			}
			if _, ok := seen[repoID]; ok {
				continue
			}
			seen[repoID] = struct{}{}
			repoIDs = append(repoIDs, repoID)
		}
	}
	return repoIDs
}

func logWorkloadMaterializationCompleted(
	ctx context.Context,
	intent Intent,
	candidates []WorkloadCandidate,
	projection *ProjectionResult,
	materializeResult MaterializeResult,
	timing workloadMaterializationTiming,
	dependencyRetractRows int,
	dependencyWriteRows int,
) {
	workloadRows := 0
	instanceRows := 0
	deploymentSourceRows := 0
	runtimePlatformRows := 0
	endpointRows := 0
	if projection != nil {
		workloadRows = len(projection.WorkloadRows)
		instanceRows = len(projection.InstanceRows)
		deploymentSourceRows = len(projection.DeploymentSourceRows)
		runtimePlatformRows = len(projection.RuntimePlatformRows)
		endpointRows = len(projection.EndpointRows)
	}

	slog.InfoContext(ctx, "workload materialization completed",
		slog.String("scope_id", intent.ScopeID),
		slog.String("generation_id", intent.GenerationID),
		slog.String("domain", string(DomainWorkloadMaterialization)),
		slog.Int("candidate_count", len(candidates)),
		slog.Int("workload_row_count", workloadRows),
		slog.Int("instance_row_count", instanceRows),
		slog.Int("deployment_source_row_count", deploymentSourceRows),
		slog.Int("runtime_platform_row_count", runtimePlatformRows),
		slog.Int("endpoint_row_count", endpointRows),
		slog.Int("workloads_written", materializeResult.WorkloadsWritten),
		slog.Int("instances_written", materializeResult.InstancesWritten),
		slog.Int("deployment_sources_written", materializeResult.DeploymentSourcesWritten),
		slog.Int("runtime_platforms_written", materializeResult.RuntimePlatformsWritten),
		slog.Int("endpoints_written", materializeResult.EndpointsWritten),
		slog.Int("dependency_retract_row_count", dependencyRetractRows),
		slog.Int("dependency_write_row_count", dependencyWriteRows),
		slog.Float64("load_inputs_duration_seconds", timing.loadInputsDuration.Seconds()),
		slog.Float64("build_projection_duration_seconds", timing.buildProjectionDuration.Seconds()),
		slog.Float64("graph_write_duration_seconds", timing.graphWriteDuration.Seconds()),
		slog.Float64("workload_graph_write_duration_seconds", materializeResult.WorkloadWriteDuration.Seconds()),
		slog.Float64("instance_graph_write_duration_seconds", materializeResult.InstanceWriteDuration.Seconds()),
		slog.Float64("deployment_source_graph_write_duration_seconds", materializeResult.DeploymentSourceDuration.Seconds()),
		slog.Float64("runtime_platform_graph_write_duration_seconds", materializeResult.RuntimePlatformDuration.Seconds()),
		slog.Float64("endpoint_graph_write_duration_seconds", materializeResult.EndpointWriteDuration.Seconds()),
		slog.Float64("dependency_reconcile_duration_seconds", timing.dependencyReconcile.Seconds()),
		slog.Float64("dependency_retract_duration_seconds", timing.dependencyRetract.Seconds()),
		slog.Float64("dependency_write_duration_seconds", timing.dependencyWrite.Seconds()),
		slog.Float64("phase_publish_duration_seconds", timing.phasePublishDuration.Seconds()),
		slog.Float64("total_duration_seconds", timing.totalDuration.Seconds()),
	)
}

func (h WorkloadMaterializationHandler) loadProjectionInputs(
	ctx context.Context,
	intent Intent,
) ([]WorkloadCandidate, map[string][]string, error) {
	inputLoader := h.InputLoader
	if inputLoader == nil {
		inputLoader = CorrelatedWorkloadProjectionInputLoader{
			FactLoader:     h.FactLoader,
			ResolvedLoader: h.ResolvedLoader,
		}
	}
	candidates, deploymentEnvironments, err := inputLoader.LoadWorkloadProjectionInputs(ctx, intent)
	if err != nil {
		return nil, nil, fmt.Errorf("load workload projection inputs: %w", err)
	}
	return candidates, deploymentEnvironments, nil
}
