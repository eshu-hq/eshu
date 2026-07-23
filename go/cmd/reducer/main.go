// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/clock"
	"github.com/eshu-hq/eshu/go/internal/graphowner"
	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func buildReducerService(
	ctx context.Context,
	database postgres.ExecQueryer,
	neo4jExec sourcecypher.Executor,
	cypherExec reducer.CypherExecutor,
	intentStore *postgres.SharedIntentStore,
	neo4jReader sourcecypher.CypherReader,
	graphReader query.GraphQuery,
	getenv func(string) string,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
	identityCache *postgres.IdentityEpochCache,
) (reducer.Service, error) {
	graphBackend, err := runtimecfg.LoadGraphBackend(getenv)
	if err != nil {
		return reducer.Service{}, err
	}
	sharedCfg := reducer.LoadSharedProjectionConfig(getenv)
	codeCallCfg := loadCodeCallProjectionConfig(getenv)
	repoDependencyCfg := loadRepoDependencyProjectionConfig(getenv, graphBackend)
	repairCfg := loadGraphProjectionPhaseRepairConfig(getenv)
	generationRetentionCfg := loadGenerationRetentionConfig(getenv)
	if err := validateGenerationRetentionConfig(getenv, generationRetentionCfg); err != nil {
		return reducer.Service{}, err
	}
	generationLivenessCfg := loadGenerationLivenessConfig(getenv)
	poisonLivenessCfg := loadPoisonLivenessConfig(getenv)
	graphOrphanSweepCfg := loadGraphOrphanSweepConfig(getenv)
	codeValueFlowStaleCleanupCfg := loadCodeValueFlowStaleCleanupConfig(getenv)
	searchVectorBuildRunner, err := searchVectorBuildRunnerFor(database, getenv, logger, instruments)
	if err != nil {
		return reducer.Service{}, err
	}
	// #4233: seed search vector scope state exactly once at startup, after
	// schema apply and before the SearchVectorBuildRunner starts. Gated on the
	// same condition that wires the runner (disabled when vectors are off).
	if err := seedSearchVectorScopeState(ctx, searchVectorBuildRunner, database, logger); err != nil {
		return reducer.Service{}, err
	}
	codeCallEdgeBatchSize, codeCallEdgeGroupBatchSize := loadCodeCallEdgeWriterTuning(getenv)
	inheritanceEdgeGroupBatchSize, sqlRelationshipEdgeGroupBatchSize := loadSharedEdgeWriterGroupTuning(getenv)
	repoDependencyRetractStatementTiming := loadRepoDependencyRetractStatementTiming(getenv)
	serviceMaterializationWriter := serviceMaterializationWriterFor(database)
	serviceDocumentationEvidenceLoader := serviceDocumentationEvidenceLoaderFor(database)
	serviceIncidentEvidenceLoader := serviceIncidentEvidenceLoaderFor(database)
	nornicDBGroupedWrites, err := resolveNornicDBGroupedWrites(getenv, graphBackend, logger)
	if err != nil {
		return reducer.Service{}, err
	}

	// Two independent permit pools bound reducer graph writes: a canonical gate
	// and a semantic gate (#3560, #3652, #4448; see reducerGraphWriteGate).
	// Splitting by class prevents a slow write on one class from exhausting the
	// other class's permits (head-of-line blocking). rawNeo4jExec is the
	// unbounded base for the semantic path's outside-the-timeout permit;
	// neo4jExec and cypherExec are wrapped with the canonical gate.
	// ifa fault injection (#4580 P6 S4, seam fixed by #5048): wraps neo4jExec
	// (whose adapter holds the reducer's persistent RetryingExecutor) before
	// it feeds both the canonical/edge and semantic paths. Under the
	// ifafaultinjection tag it also arms a decorator below that RetryingExecutor
	// so the executor-retry lane retries in place; a no-op otherwise. See
	// ifa_fault_wiring.go / ifa_fault_wiring_off.go.
	neo4jExec, err = wrapIfaFaultExecutor(neo4jExec, getenv, logger)
	if err != nil {
		return reducer.Service{}, err
	}
	graphWriteGate := newReducerGraphWriteGate(getenv, instruments)
	rawNeo4jExec := neo4jExec
	neo4jExec = graphWriteGate.boundExecutor(neo4jExec)
	cypherExec = graphWriteGate.boundCypherExecutor(cypherExec)

	edgeWriterForHandlers := newHandlerEdgeWriter(neo4jExec, neo4jBatchSize(getenv), instruments, logger, inheritanceEdgeGroupBatchSize, sqlRelationshipEdgeGroupBatchSize)
	edgeWriterForHandlers.SQLRelationshipSequentialWrites = graphBackend == runtimecfg.GraphBackendNornicDB
	// #5007: gate the canonical cloud/EC2/K8s node writers on the Postgres owner
	// ledger so cross-scope same-uid nodes resolve deterministically to the
	// max-(observed_at, source_fact_id) contributor. A database that does not
	// expose a transaction beginner yields a pass-through gate (prior behavior).
	ownerGate := graphowner.NewGate(reducerBeginner(database))
	ownerGate.Instruments = instruments
	// #5062: lockGate serializes posture/exposure writers against ownerGate's same-uid base-property writes (same advisory lock, no ledger row).
	lockGate := graphowner.NewLockOnlyGate(reducerBeginner(database))
	graphWriters := newCanonicalGraphWriters(neo4jExec, graphReader, neo4jBatchSize(getenv), ownerGate, lockGate)
	secretsIAMGraphWriter, err := secretsIAMGraphProjectionWriter(getenv, neo4jExec, neo4jBatchSize(getenv), logger)
	if err != nil {
		return reducer.Service{}, err
	}
	presence := newEndpointPresenceWirings(getenv, secretsIAMGraphWriter != nil, database)
	relationshipStore := postgres.NewRelationshipStore(database)
	relationshipGenerationActive := postgres.NewRelationshipGenerationActiveLookup(relationshipStore)
	factStore := postgres.NewFactStore(database)
	if identityCache != nil {
		factStore = postgres.NewFactStoreWithIdentityCache(database, identityCache)
	}
	admissionDecisionWriter := newAdmissionDecisionWriter(database)
	codeCallIntentWriter := postgres.NewCodeCallIntentWriterWithInstruments(database, instruments)
	repoDependencyIntentWriter := postgres.NewSharedIntentAcceptanceWriterWithInstruments(database, instruments)
	acceptedGenerationPrefetch := postgres.NewAcceptedGenerationPrefetch(database)
	graphProjectionStateStore := postgres.NewGraphProjectionPhaseStateStore(database)
	clk := clock.System() // injected queue/lease/reap clock (#4121); == time.Now() in prod
	graphProjectionRepairQueue := configureGraphProjectionRepairQueue(database, clk)
	graphProjectionReadinessLookup := postgres.NewGraphProjectionReadinessLookup(database)
	graphProjectionReadinessPrefetch := postgres.NewGraphProjectionReadinessPrefetch(database)
	generationRetentionRunner := generationRetentionRunnerFor(database, generationRetentionCfg)
	if generationRetentionRunner != nil {
		generationRetentionRunner.Instruments = instruments
		generationRetentionRunner.Logger = logger
	}
	generationLivenessRunner := generationLivenessRunnerFor(database, generationLivenessCfg)
	if generationLivenessRunner != nil {
		generationLivenessRunner.Instruments = instruments
		generationLivenessRunner.Logger = logger
	}
	poisonLivenessRunner := poisonLivenessRunnerFor(database, poisonLivenessCfg)
	if poisonLivenessRunner != nil {
		poisonLivenessRunner.Instruments = instruments
		poisonLivenessRunner.Logger = logger
	}
	graphOrphanSweepRunner := graphOrphanSweepRunnerFor(neo4jExec, graphReader, intentStore, graphOrphanSweepCfg)
	if graphOrphanSweepRunner != nil {
		graphOrphanSweepRunner.Logger = logger
	}
	codeValueFlowStaleCleanupRunner := codeValueFlowStaleCleanupRunnerFor(
		database,
		graphWriters.codeTaintEvidence,
		graphWriters.codeTaintEvidence,
		graphWriters.codeInterprocEvidence,
		graphWriters.codeInterprocEvidence,
		intentStore,
		codeValueFlowStaleCleanupCfg,
	)
	if codeValueFlowStaleCleanupRunner != nil {
		codeValueFlowStaleCleanupRunner.Logger = logger
	}
	// Seed the projected-edge ledger from existing graph TAINT_FLOWS_TO edges so
	// the ledger is a superset of graph edges at deploy time (one-time,
	// idempotent backfill). Must run before stale-cleanup or fixpoint projection
	// start retracting. graphReader is the query.GraphQuery read port; the
	// ledger uses the same postgres-backed store wired for the runtime.
	backfillStateMarker := postgres.NewCodeValueFlowBackfillStateStore(database)
	backfiller := reducer.CodeInterprocProjectedEdgeBackfiller{
		Reader:      reducer.CodeInterprocProjectedEdgeBackfillReader{Graph: graphReader},
		Ledger:      postgres.NewCodeInterprocProjectedEdgeStore(database),
		StateMarker: backfillStateMarker,
		EvidenceSources: []string{
			reducer.CodeInterprocEvidenceSource(),
			reducer.CodeInterprocFixpointEvidenceSource(),
		},
	}
	if err := backfiller.Run(context.Background()); err != nil {
		return reducer.Service{}, fmt.Errorf("code interproc projected edge backfill: %w", err)
	}
	// Seed the projected-node ledger from existing graph CodeTaintEvidence nodes
	// so the ledger is a superset of graph nodes at deploy time (one-time,
	// idempotent backfill). Mirrors the interproc edge backfill above.
	taintNodeBackfiller := reducer.CodeTaintEvidenceProjectedNodeBackfiller{
		Reader:      reducer.CodeTaintEvidenceProjectedNodeBackfillReader{Graph: graphReader},
		Ledger:      postgres.NewCodeTaintEvidenceProjectedNodeStore(database),
		StateMarker: backfillStateMarker,
		EvidenceSources: []string{
			reducer.CodeTaintEvidenceSource(),
		},
	}
	if err := taintNodeBackfiller.Run(context.Background()); err != nil {
		return reducer.Service{}, fmt.Errorf("code taint evidence projected node backfill: %w", err)
	}
	// Seed the projected-source-edge ledger from existing graph source edges
	// (AWS/Azure/GCP relationship edges, observability coverage edges, and
	// security-group reachability edges) so the ledger is a superset of graph
	// edges at deploy time (one-time, idempotent backfill). Must run before any
	// ledger-anchored RetractXxxByUIDs call in the AWS/Azure/GCP/observability/
	// security-group-reachability materialization handlers wired below, so the
	// first post-deploy retract is not a no-op that orphans pre-ledger edges.
	// projectedSourceEdgeStore is constructed once here and reused as
	// DefaultHandlers.ProjectedSourceLedger below so both the backfill and the
	// runtime handlers share one store.
	projectedSourceEdgeStore := postgres.NewProjectedSourceEdgeStore(database)
	projectedSourceEdgeBackfiller := reducer.ProjectedSourceEdgeBackfiller{
		Reader:      reducer.ProjectedSourceEdgeBackfillReader{Graph: graphReader},
		Ledger:      projectedSourceEdgeStore,
		StateMarker: backfillStateMarker,
		EvidenceSources: []string{
			reducer.AWSRelationshipEvidenceSource(),
			reducer.AzureRelationshipEvidenceSource(),
			reducer.GCPRelationshipEvidenceSource(),
			reducer.ObservabilityCoverageEvidenceSource(),
			reducer.SecurityGroupReachabilityEvidenceSource(),
		},
	}
	if err := projectedSourceEdgeBackfiller.Run(context.Background()); err != nil {
		return reducer.Service{}, fmt.Errorf("projected source edge backfill: %w", err)
	}
	// Semantic path: permit gate OUTSIDE the write timeout (#3652 P1); see
	// boundSemanticEntityExecutor.
	semanticEntityExecutor := graphWriteGate.boundSemanticEntityExecutor(
		rawNeo4jExec,
		graphBackend,
		nornicDBCanonicalWriteTimeout(getenv),
		nornicDBGroupedWrites,
	)
	semanticEntityWriter, err := semanticEntityWriterForGraphBackend(semanticEntityExecutor, neo4jBatchSize(getenv), graphBackend, getenv)
	if err != nil {
		return reducer.Service{}, err
	}
	retryCfg, err := loadReducerQueueConfig(getenv)
	if err != nil {
		return reducer.Service{}, err
	}
	projectorDrainGate, err := loadReducerProjectorDrainGate(getenv, graphBackend)
	if err != nil {
		return reducer.Service{}, fmt.Errorf("load reducer projector drain gate: %w", err)
	}
	if projectorDrainGate && logger != nil {
		logger.Info(
			"reducer claims will wait for source-local projection drain",
			"graph_backend", string(graphBackend),
			"query_profile", string(query.ProfileLocalAuthoritative),
		)
	}
	claimDomains, err := loadReducerClaimDomains(getenv)
	if err != nil {
		return reducer.Service{}, fmt.Errorf("load reducer claim domains: %w", err)
	}
	if len(claimDomains) > 0 && logger != nil {
		logger.Info(
			"reducer claims restricted to domains",
			"domains", reducerDomainStrings(claimDomains),
		)
	}
	workQueue := configureReducerQueue(database, retryCfg, claimDomains, projectorDrainGate, getenv, graphBackend, clk, instruments, logger)

	executor, err := reducer.NewDefaultRuntime(reducer.DefaultHandlers{
		DeployableUnitCorrelationHandler: reducer.DeployableUnitCorrelationHandler{
			FactLoader:              factStore,
			ResolvedLoader:          relationshipStore,
			PhasePublisher:          graphProjectionStateStore,
			EdgeWriter:              edgeWriterForHandlers,
			AdmissionDecisionWriter: admissionDecisionWriter,
		},
		WorkloadProjectionInputLoader: reducer.CorrelatedWorkloadProjectionInputLoader{
			FactLoader:     factStore,
			ResolvedLoader: relationshipStore,
			ScopeResolver:  postgres.RepoScopeResolver{DB: database},
		},
		WorkloadDependencyLookup:           neo4jWorkloadDependencyLookup{reader: graphReader},
		InstanceRetractionLookup:           neo4jWorkloadInstanceRetractionLookup{reader: graphReader},
		WorkloadIdentityWriter:             reducer.PostgresWorkloadIdentityWriter{DB: database},
		CloudAssetResolutionWriter:         reducer.PostgresCloudAssetResolutionWriter{DB: database},
		PlatformMaterializationWriter:      reducer.PostgresPlatformMaterializationWriter{DB: database},
		PlatformGraphLocker:                platformGraphLockerForReducer(database),
		WorkloadMaterializationReplayer:    workQueue,
		WorkloadMaterializer:               reducer.NewWorkloadMaterializer(cypherExec),
		InfrastructurePlatformMaterializer: reducer.NewInfrastructurePlatformMaterializer(cypherExec),
		InfrastructurePlatformLookup:       reducer.GraphInfrastructurePlatformLookup{Graph: graphReader},
		FactLoader:                         factStore,
		AdmissionDecisionWriter:            admissionDecisionWriter,
		CodeCallIntentWriter:               codeCallIntentWriter,
		GraphProjectionPhasePublisher:      graphProjectionStateStore,
		GraphProjectionRepairQueue:         graphProjectionRepairQueue,
		ReadinessLookup:                    graphProjectionReadinessLookup,
		ReadinessPrefetch:                  graphProjectionReadinessPrefetch,
		SemanticEntityWriter:               semanticEntityWriter,
		SQLRelationshipEdgeWriter:          edgeWriterForHandlers,
		// Inheritance edges ride the shared-projection intent path (#2867): the
		// handler emits file-scoped per-edge intents plus a per-repo refresh intent
		// to the same shared intent acceptance writer CALLS-adjacent domains use,
		// and the partitioned runner + #2898 refresh fence project them.
		InheritanceIntentWriter: repoDependencyIntentWriter,
		// SQL relationship edges ride the same shared-projection intent path (#2868):
		// the promoted handler emits file-scoped per-edge intents plus a per-repo
		// refresh intent to the shared intent acceptance writer, and the partitioned
		// runner + #2898 refresh fence project them.
		SQLRelationshipIntentWriter: repoDependencyIntentWriter,
		ShellExecIntentWriter:       repoDependencyIntentWriter,
		// Rationale EXPLAINS edges ride the same shared-projection intent path
		// (#2869): the promoted handler emits file-scoped per-edge intents plus a
		// per-repo refresh intent to the shared intent acceptance writer, and the
		// partitioned runner + #2898 refresh fence project them.
		RationaleEdgeIntentWriter:             repoDependencyIntentWriter,
		DocumentationEdgeWriter:               edgeWriterForHandlers,
		CodeownersOwnershipEdgeWriter:         edgeWriterForHandlers,
		RationaleEdgeWriter:                   edgeWriterForHandlers,
		EvidenceFactLoader:                    relationshipStore,
		AssertionLoader:                       relationshipStore,
		ResolutionPersister:                   relationshipStore,
		ResolvedRelationshipLoader:            relationshipStore,
		RepoDependencyIntentWriter:            repoDependencyIntentWriter,
		RepoDependencyEdgeWriter:              edgeWriterForHandlers,
		WorkloadDependencyEdgeWriter:          edgeWriterForHandlers,
		SubmodulePinEdgeWriter:                edgeWriterForHandlers,
		GenerationCheck:                       postgres.NewGenerationFreshnessCheck(database),
		PriorGenerationCheck:                  postgres.NewPriorGenerationCheck(database),
		Tracer:                                tracer,
		Instruments:                           instruments,
		CloudResourceNodeWriter:               graphWriters.cloudResourceNode,
		EC2InstanceNodeWriter:                 graphWriters.ec2InstanceNode,
		CloudResourceEdgeWriter:               graphWriters.cloudResourceEdge,
		CloudResourceContainerImageEdgeWriter: graphWriters.cloudResourceContainerImageEdge,
		GCPCloudResourceEdgeWriter:            graphWriters.gcpCloudResourceEdge,
		AzureCloudResourceEdgeWriter:          graphWriters.azureCloudResourceEdge,
		WorkloadCloudRelationshipEdgeWriter:   graphWriters.workloadCloudRelationshipEdge,
		SecurityGroupEndpointNodeWriter:       graphWriters.securityGroupEndpointNode,
		SecurityGroupRuleNodeWriter:           graphWriters.securityGroupReachability,
		SecurityGroupReachabilityWriter:       graphWriters.securityGroupReachability,
		IAMEscalationEdgeWriter:               graphWriters.iamEscalationEdge,
		IAMCanPerformEdgeWriter:               graphWriters.iamCanPerformEdge,
		ObservabilityCoverageEdgeWriter:       graphWriters.observabilityCoverageEdge,
		// ProjectedSourceLedger (issue #4858, #4881) is shared by the AWS,
		// Azure, GCP relationship, observability-coverage, and
		// security-group-reachability handlers above; each handler keys its
		// own ledger rows by its distinct evidence_source string, so one store
		// instance is enough. This is the same projectedSourceEdgeStore
		// instance the ProjectedSourceEdgeBackfiller above seeded at startup,
		// mirroring the code-interproc ledger wiring
		// (postgres.NewCodeInterprocProjectedEdgeStore) constructed earlier.
		ProjectedSourceLedger:              projectedSourceEdgeStore,
		IAMCanAssumeEdgeWriter:             graphWriters.iamCanAssumeEdge,
		S3LogsToEdgeWriter:                 graphWriters.s3LogsToEdge,
		S3ExternalPrincipalGrantWriter:     graphWriters.s3ExternalPrincipalGrant,
		RDSPostureNodeWriter:               graphWriters.rdsPostureNode,
		EC2UsesProfileEdgeWriter:           graphWriters.ec2UsesProfileEdge,
		IAMInstanceProfileRoleEdgeWriter:   graphWriters.iamInstanceProfileRoleEdge,
		EC2InternetExposureNodeWriter:      graphWriters.ec2InternetExposureNode,
		EC2BlockDeviceKMSPostureNodeWriter: graphWriters.ec2BlockDeviceKMSPostureNode,
		S3InternetExposureNodeWriter:       graphWriters.s3InternetExposureNode,
		ContainerImageIdentityWriter: reducer.PostgresContainerImageIdentityWriter{
			DB: database,
		},
		// PackageProvenanceEdgeWriter / ContainerImageProvenanceEdgeWriter
		// (issue #5457) share one ProvenanceEdgeWriter instance -- it
		// implements both narrower reducer interfaces.
		PackageProvenanceEdgeWriter:        graphWriters.provenanceEdge,
		ContainerImageProvenanceEdgeWriter: graphWriters.provenanceEdge,
		CICDRunCorrelationWriter: reducer.PostgresCICDRunCorrelationWriter{
			DB: database,
		},
		ServiceCatalogCorrelationWriter: reducer.PostgresServiceCatalogCorrelationWriter{
			DB: database,
		},
		ServiceMaterializationWriter:       serviceMaterializationWriter,
		ServiceDocumentationEvidenceLoader: serviceDocumentationEvidenceLoader,
		ServiceIncidentEvidenceLoader:      serviceIncidentEvidenceLoader,
		// ServiceRuntimeInstanceLoader sources the runtime evidence family (#1986)
		// from the canonical graph's WorkloadInstance/Platform nodes for each
		// correlated service's repository. It is wired only alongside
		// ServiceMaterializationWriter so the runtime family stays purely additive
		// to the ownership/deployment lineage; the loader anchors on the
		// workload_instance_repo_id index and runs once per
		// service-catalog-correlation intent.
		ServiceRuntimeInstanceLoader: reducer.GraphServiceRuntimeInstanceLoader{Graph: graphReader},
		// ServiceVulnerabilityAdvisoryLoader sources the vulnerabilities evidence
		// family (#1990, #2127) from active reducer_supply_chain_impact_finding facts
		// on each correlated service's repository (repository_id-scoped, additive).
		ServiceVulnerabilityAdvisoryLoader: factStore,
		ObservabilityCoverageCorrelationWriter: reducer.PostgresObservabilityCoverageCorrelationWriter{
			DB: database,
		},
		PackageCorrelationWriter: reducer.PostgresPackageCorrelationWriter{
			DB: database,
		},
		DriftHandlers:               buildReducerDriftHandlers(database, tracer, instruments, logger, getenv),
		SearchDocumentHandlers:      buildReducerSearchDocumentHandlers(database, instruments, tracer, logger),
		CloudInventoryHandlers:      buildReducerCloudInventoryHandlers(database, logger),
		KubernetesHandlers:          buildReducerKubernetesHandlers(database, graphWriters),
		CrossplaneHandlers:          buildReducerCrossplaneHandlers(graphWriters),
		SupplyChainSecurityHandlers: buildReducerSupplyChainSecurityHandlers(database, *factStore, secretsIAMGraphWriter, presence),
		IncidentRoutingHandlers:     buildReducerIncidentRoutingHandlers(database, *factStore, graphWriters),
		CodeEvidenceHandlers:        buildReducerCodeEvidenceHandlers(database, *factStore, graphWriters, graphReader, logger),
	})
	if err != nil {
		return reducer.Service{}, err
	}

	edgeWriter := sourcecypher.NewEdgeWriter(neo4jExec, neo4jBatchSize(getenv))
	// Shell-exec orphan ShellCommand cleanup runs a Go-side anti-join (S1
	// candidate keys, S2 connected keys) rather than a relationship-existence
	// predicate, which is mis-evaluated on the pinned NornicDB backends
	// (#5310; see docs/public/reference/nornicdb-pitfalls.md). graphReader
	// already satisfies OrphanSweepReader (query.GraphQuery.Run has the same
	// shape) and is the same read port graphOrphanSweepRunnerFor uses.
	edgeWriter.Reader = graphReader
	edgeWriter.Instruments = instruments
	edgeWriter.Logger = logger
	edgeWriter.CodeCallBatchSize = codeCallEdgeBatchSize
	edgeWriter.CodeCallGroupBatchSize = codeCallEdgeGroupBatchSize
	edgeWriter.InheritanceGroupBatchSize = inheritanceEdgeGroupBatchSize
	edgeWriter.SQLRelationshipGroupBatchSize = sqlRelationshipEdgeGroupBatchSize
	edgeWriter.SQLRelationshipSequentialWrites = graphBackend == runtimecfg.GraphBackendNornicDB
	edgeWriter.RepoDependencyRetractStatementTiming = repoDependencyRetractStatementTiming

	reducerGraphDrain := reducerGraphDrainFor(projectorDrainGate, database)

	workers := loadReducerWorkerCount(getenv, graphBackend)
	return reducer.Service{
		PollInterval:               time.Second,
		WorkSource:                 workQueue,
		Executor:                   executor,
		WorkSink:                   workQueue,
		Heartbeater:                workQueue,
		HeartbeatInterval:          workQueue.LeaseDuration / 2,
		SharedProjectionEdgeWriter: edgeWriter,
		SharedProjectionRunner: &reducer.SharedProjectionRunner{
			IntentReader:        intentStore,
			LeaseManager:        intentStore,
			EdgeWriter:          edgeWriter,
			AcceptedGen:         postgres.NewAcceptedGenerationLookup(database),
			AcceptedGenPrefetch: acceptedGenerationPrefetch,
			ReadinessLookup:     graphProjectionReadinessLookup,
			ReadinessPrefetch:   graphProjectionReadinessPrefetch,
			// handles_route gate (#2809) — independent of the secrets/IAM lookup.
			EndpointPresenceLookup: presence.handlesRouteLookup,
			// repo-wide-retract fence (#2898/#2910): the intent store records the
			// per-repo refresh completion the worker holds per-edge writes behind.
			RefreshFenceLookup: intentStore,
			// first-projection retract skip (#3624): a scope whose only
			// generation is the current one has zero prior edges, so its
			// whole-scope retract (a NornicDB full-store scan) is a guaranteed
			// no-op and is skipped. Nil leaves the retract running, byte-identical.
			FirstProjectionLookup: intentStore,
			Config:                sharedCfg,
			Tracer:                tracer,
			Instruments:           instruments,
			Logger:                logger,
		},
		SupplyChainImpactWinnersMaintainer: &reducer.SupplyChainImpactWinnersMaintainer{
			Rebuilder:    postgres.NewSupplyChainImpactWinnersStore(database),
			LeaseManager: intentStore,
			LeaseOwner:   defaultSupplyChainImpactWinnersLeaseOwner(),
			Logger:       logger,
		},
		CollectorEvidenceSummaryMaintainer: &reducer.CollectorEvidenceSummaryMaintainer{
			Rebuilder:    postgres.NewCollectorEvidenceSummaryStore(database),
			Freshness:    postgres.NewCollectorEvidenceSummaryStore(database),
			LeaseManager: intentStore,
			LeaseOwner:   defaultCollectorEvidenceSummaryLeaseOwner(),
			Logger:       logger,
		},
		CodeCallProjectionRunner: &reducer.CodeCallProjectionRunner{
			IntentReader:        intentStore,
			LeaseManager:        intentStore,
			EdgeWriter:          edgeWriter,
			AcceptedGen:         postgres.NewAcceptedGenerationLookup(database),
			AcceptedGenPrefetch: acceptedGenerationPrefetch,
			ReadinessLookup:     graphProjectionReadinessLookup,
			ReadinessPrefetch:   graphProjectionReadinessPrefetch,
			ReducerGraphDrain:   reducerGraphDrain,
			Config:              codeCallCfg,
			Tracer:              tracer,
			Instruments:         instruments,
			Logger:              logger,
		},
		RepoDependencyProjectionRunner: newRepoDependencyProjectionRunner(
			intentStore, database, edgeWriter, workQueue,
			relationshipGenerationActive, acceptedGenerationPrefetch,
			repoDependencyCfg, tracer, instruments, logger,
		),
		CodeReachabilityProjectionRunner: codeReachabilityProjectionRunnerFor(database, sharedCfg, workers, logger),
		GraphProjectionPhaseRepairer: graphProjectionPhaseRepairerFor(
			graphProjectionRepairQueue, database, graphProjectionStateStore, repairCfg, clk, instruments, logger,
		),
		GenerationRetentionRunner:       generationRetentionRunner,
		GenerationLivenessRunner:        generationLivenessRunner,
		PoisonLivenessRunner:            poisonLivenessRunner,
		GraphOrphanSweepRunner:          graphOrphanSweepRunner,
		CodeValueFlowStaleCleanupRunner: codeValueFlowStaleCleanupRunner,
		SearchVectorBuildRunner:         searchVectorBuildRunner,
		QuarantineWriter:                postgres.NewReducerInputInvalidFactStore(database),
		Workers:                         workers,
		BatchClaimSize:                  loadReducerBatchClaimSize(getenv, workers, graphBackend),
		Tracer:                          tracer,
		Instruments:                     instruments,
		Logger:                          logger,
	}, nil
}

// reducerDomainStrings lives in main_helpers.go to keep this file within the
// repo file-size budget.
