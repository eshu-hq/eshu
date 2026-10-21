// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main //nolint:filelength // 503 lines: reducer service assembly; the #8211 handler-timeout wiring tipped it over the cap.

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/cicdrun"
	worker "github.com/eshu-hq/eshu/go/internal/reducer/intents/shared/worker"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"

	"github.com/eshu-hq/eshu/go/internal/reducer/servicecatalog"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher/canonical/node"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"

	_ "github.com/jackc/pgx/v5/stdlib"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/clock"
	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/reducer/cloudasset"
	"github.com/eshu-hq/eshu/go/internal/reducer/code/call/projection"
	"github.com/eshu-hq/eshu/go/internal/reducer/containerimage"
	"github.com/eshu-hq/eshu/go/internal/reducer/maintenance/evidence"
	"github.com/eshu-hq/eshu/go/internal/reducer/obscoverage"
	"github.com/eshu-hq/eshu/go/internal/reducer/packages/correlation"
	"github.com/eshu-hq/eshu/go/internal/reducer/platformfam"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	producerreadiness "github.com/eshu-hq/eshu/go/internal/storage/postgres/readiness/producer"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/scope/completion"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func buildReducerService(
	ctx context.Context,
	database db.ExecQueryer,
	neo4jExec sourcecypher.Executor,
	cypherExec reducer.CypherExecutor,
	intentStore *postgres.SharedIntentStore,
	neo4jReader node.CypherReader,
	graphReader query.GraphQuery,
	getenv func(string) string,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
	identityCache *postgres.IdentityEpochCache,
) (reducer.Service, error) {
	graphBackend, searchKey, err := loadReducerGraphBackend(getenv)
	if err != nil {
		return reducer.Service{}, err
	}
	sharedCfg := worker.LoadConfig(getenv)
	sharedCfg.LeaseOwner = loadSharedProjectionLeaseOwner(getenv)
	codeCallCfg := loadCodeCallProjectionConfig(getenv)
	repoDependencyCfg := loadRepoDependencyProjectionConfig(getenv, graphBackend)
	repairCfg := loadGraphProjectionPhaseRepairConfig(getenv)
	generationRetentionCfg := loadGenerationRetentionConfig(getenv)
	if err := validateGenerationRetentionConfig(getenv, generationRetentionCfg); err != nil {
		return reducer.Service{}, err
	}
	logGenerationRetentionHardCeilingLift(ctx, logger, generationRetentionCfg)
	generationLivenessCfg := loadGenerationLivenessConfig(getenv)
	warnGenerationLivenessProgressWindowClamp(ctx, logger, generationLivenessCfg)
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

	graphChain, err := buildReducerGraphWriteChain(neo4jExec, cypherExec, getenv, instruments, logger)
	if err != nil {
		return reducer.Service{}, err
	}
	graphWriteGate := graphChain.gate
	rawNeo4jExec := graphChain.raw
	neo4jExec = graphChain.canonical
	cypherExec = graphChain.cypher
	leaseExpiryBarrier := graphChain.barrier

	edgeWriterForHandlers := newHandlerEdgeWriter(neo4jExec, neo4jBatchSize(getenv), instruments, logger, inheritanceEdgeGroupBatchSize, sqlRelationshipEdgeGroupBatchSize)
	edgeWriterForHandlers.SQLRelationshipSequentialWrites = graphBackend == runtimecfg.GraphBackendNornicDB
	graphWriters := newReducerCanonicalGraphWriters(neo4jExec, graphReader, neo4jBatchSize(getenv), database, instruments, searchKey)
	secretsIAMGraphWriter, err := secretsIAMGraphProjectionWriter(getenv, neo4jExec, neo4jBatchSize(getenv), logger)
	if err != nil {
		return reducer.Service{}, err
	}
	presence := newEndpointPresenceWirings(getenv, secretsIAMGraphWriter != nil, database)
	relationshipStore := postgres.NewRelationshipStore(database).WithInstruments(instruments)
	relationshipGenerationActive := postgres.NewRelationshipGenerationActiveLookup(relationshipStore)
	// Corpus-wide fence for derivation that merges foreign resolved reads
	// (workload materialization, deployable-unit correlation): those loaders
	// must not succeed while any active scope's current relationship
	// generation is inactive (#6184).
	resolutionsComplete := postgres.NewRelationshipGenerationsCompleteLookup(relationshipStore)
	// Best-effort holder list for fence deferral errors (#6730).
	incompleteScopes := postgres.NewRelationshipGenerationsIncompleteScopesLookup(relationshipStore)
	// #7597: a closed own gate reads its deployment_mapping producer.
	resolutionProducer := postgres.NewResolutionProducerLookup(database)
	factStore := postgres.NewFactStore(database).WithInstruments(instruments)
	if identityCache != nil {
		factStore = postgres.NewFactStoreWithIdentityCache(database, identityCache).WithInstruments(instruments)
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
	generationRetentionRunner := generationRetentionRunnerFor(database, generationRetentionCfg, logger)
	if generationRetentionRunner != nil {
		generationRetentionRunner.Instruments = instruments
		generationRetentionRunner.Logger = logger
	}
	generationLivenessRunner := generationLivenessRunnerFor(database, generationLivenessCfg)
	if generationLivenessRunner != nil {
		generationLivenessRunner.Instruments = instruments
		generationLivenessRunner.Logger = logger
	}
	runners, err := activationRunnersFor(getenv, database, tracer, instruments, logger)
	if err != nil {
		return reducer.Service{}, err
	}
	poisonLivenessRunner := poisonLivenessRunnerFor(database, poisonLivenessCfg)
	if poisonLivenessRunner != nil {
		poisonLivenessRunner.Instruments = instruments
		poisonLivenessRunner.Logger = logger
	}
	graphOrphanSweepRunner := graphOrphanSweepRunnerFor(neo4jExec, graphReader, intentStore, graphOrphanSweepCfg, relationshipStore)
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
	// Seed the projected-edge/node/source-edge ledgers from existing graph state
	// (one-time, idempotent backfills; see seedReducerProjectedSourceLedgers'
	// doc in canonical_graph_writers.go). Must run before stale-cleanup, fixpoint
	// projection, or any ledger-anchored RetractXxxByUIDs call in the
	// AWS/Azure/GCP/observability/security-group-reachability materialization
	// handlers wired below start retracting, so the first post-deploy retract is
	// not a no-op that orphans pre-ledger edges. projectedSourceEdgeStore is
	// reused as DefaultHandlers.ProjectedSourceLedger below so both the backfill
	// and the runtime handlers share one store.
	projectedSourceEdgeStore, err := seedReducerProjectedSourceLedgers(database, graphReader)
	if err != nil {
		return reducer.Service{}, err
	}

	// Semantic path: permit gate OUTSIDE the write timeout (#3652 P1); see
	// boundSemanticEntityExecutor.
	semanticEntityExecutor := graphWriteGate.boundSemanticEntityExecutor(
		rawNeo4jExec,
		graphBackend,
		reducerTransactionTimeout(graphBackend, getenv),
		nornicDBGroupedWrites,
	)
	semanticEntityWriter, err := semanticEntityWriterForGraphBackend(semanticEntityExecutor, neo4jBatchSize(getenv), graphBackend, getenv, postgres.NewGenerationFreshnessCheck(database))
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
	handlerTimeout, err := loadReducerHandlerTimeout(getenv)
	if err != nil {
		return reducer.Service{}, fmt.Errorf("load reducer handler timeout: %w", err)
	}
	handlerTimeoutOverrides, err := loadReducerHandlerTimeoutOverrides(getenv)
	if err != nil {
		return reducer.Service{}, fmt.Errorf("load reducer handler timeout overrides: %w", err)
	}
	if (handlerTimeout > 0 || len(handlerTimeoutOverrides) > 0) && logger != nil {
		logger.Warn(
			"reducer handler timeouts are recorded but not enforced yet",
			"default", handlerTimeout.String(),
			"overrides", len(handlerTimeoutOverrides),
		)
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
			FactLoader:                factStore,
			ResolvedLoader:            relationshipStore,
			PhasePublisher:            graphProjectionStateStore,
			EdgeWriter:                edgeWriterForHandlers,
			AdmissionDecisionWriter:   admissionDecisionWriter,
			ResolutionActiveLookup:    relationshipGenerationActive,
			ResolutionsCompleteLookup: resolutionsComplete,
			IncompleteScopesLookup:    incompleteScopes,
			ResolutionProducerLookup:  resolutionProducer,
			CanonicalQuiescence:       postgres.NewReducerGraphDrain(database),
			ReopenFingerprintRecorder: relationshipStore,
			EdgeVerifier:              edgeWriterForHandlers,
			Logger:                    logger,
		},
		WorkloadProjectionInputLoader: reducer.CorrelatedWorkloadProjectionInputLoader{
			FactLoader:                factStore,
			ResolvedLoader:            relationshipStore,
			ScopeResolver:             postgres.RepoScopeResolver{DB: database},
			ResolutionActiveLookup:    relationshipGenerationActive,
			ResolutionsCompleteLookup: resolutionsComplete,
			IncompleteScopesLookup:    incompleteScopes,
			ResolutionProducerLookup:  resolutionProducer,
		},
		WorkloadDependencyLookup:        neo4jWorkloadDependencyLookup{reader: graphReader},
		InstanceRetractionLookup:        neo4jWorkloadInstanceRetractionLookup{reader: graphReader},
		RepositoryEdgeReader:            graphReader,
		WorkloadIdentityWriter:          reducer.PostgresWorkloadIdentityWriter{DB: database},
		CloudAssetResolutionWriter:      cloudasset.PostgresCloudAssetResolutionWriter{DB: database},
		PlatformMaterializationWriter:   platformfam.PostgresPlatformMaterializationWriter{DB: database},
		PlatformGraphLocker:             platformGraphLockerForReducer(database),
		WorkloadMaterializationReplayer: workQueue,
		WorkloadMaterializer:            newProbedWorkloadMaterializer(cypherExec, logger, instruments, neo4jWorkloadSiblingLookup{reader: graphReader}),
		// WorkloadSiblingLookup feeds the #5385 handle ladder in the four
		// provider-asserted consumers (cloud USES, documentation,
		// service-catalog, supply-chain impact). A nil graphReader lists
		// nothing, so every selector resolves as a miss and each writer's
		// exact MATCH stays a no-op — the same posture as the materializer
		// above.
		WorkloadSiblingLookup:              neo4jWorkloadSiblingLookup{reader: graphReader},
		InfrastructurePlatformMaterializer: reducer.NewInfrastructurePlatformMaterializer(cypherExec),
		InfrastructurePlatformLookup:       reducer.GraphInfrastructurePlatformLookup{Graph: graphReader},
		// The value-flow refresh emit gate (#6785) reads each producer's
		// committed edges through the shared graph query runner. A nil
		// graphReader (binaries without a graph read path) leaves the field
		// nil and every gate failed open.
		RefreshAffectedGraph:          graphReader,
		FactLoader:                    factStore,
		CrossScopeHandlers:            buildReducerCrossScopeHandlers(database, factStore, graphReader, logger),
		AdmissionDecisionWriter:       admissionDecisionWriter,
		CodeCallIntentWriter:          codeCallIntentWriter,
		GraphProjectionPhasePublisher: graphProjectionStateStore,
		GraphProjectionRepairQueue:    graphProjectionRepairQueue,
		ReadinessLookup:               graphProjectionReadinessLookup,
		ReadinessPrefetch:             graphProjectionReadinessPrefetch,
		SemanticEntityWriter:          semanticEntityWriter,
		SQLRelationshipEdgeWriter:     edgeWriterForHandlers,
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
		RationaleEdgeIntentWriter:     repoDependencyIntentWriter,
		DocumentationEdgeWriter:       edgeWriterForHandlers,
		CodeownersOwnershipEdgeWriter: edgeWriterForHandlers,
		RationaleEdgeWriter:           edgeWriterForHandlers,
		EvidenceFactLoader:            relationshipStore,
		AssertionLoader:               relationshipStore,
		ResolutionPersister:           relationshipStore,
		// #8294 completion-driven wake: the same store owns the reopened
		// queue rows and the fingerprint table the wake predicate reads.
		DeployableUnitCorrelationWaker:        relationshipStore,
		ScopeRepos:                            relationshipStore,
		ResolvedRelationshipLoader:            relationshipStore,
		RepoDependencyIntentWriter:            repoDependencyIntentWriter,
		RepoDependencyEdgeWriter:              edgeWriterForHandlers,
		WorkloadDependencyEdgeWriter:          edgeWriterForHandlers,
		SubmodulePinEdgeWriter:                edgeWriterForHandlers,
		GenerationCheck:                       postgres.NewGenerationFreshnessCheck(database),
		PriorGenerationCheck:                  postgres.NewPriorGenerationCheck(database),
		PriorGeneration:                       postgres.NewPriorGenerationID(database),
		Tracer:                                tracer,
		Instruments:                           instruments,
		CloudResourceNodeWriter:               graphWriters.cloudResourceNode,
		EC2InstanceNodeWriter:                 graphWriters.ec2InstanceNode,
		CloudResourceNodeRetracter:            graphWriters.cloudResourceRetracter,
		EC2InstanceNodeRetracter:              graphWriters.ec2InstanceRetracter,
		CloudResourceEdgeWriter:               graphWriters.cloudResourceEdge,
		CloudResourceContainerImageEdgeWriter: graphWriters.cloudResourceContainerImageEdge,
		ContainerImageExistence:               containerimage.GraphContainerImageExistenceLookup{Graph: graphReader},
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
		// (taintstore.NewCodeInterprocProjectedEdgeStore) constructed earlier.
		ProjectedSourceLedger:                 projectedSourceEdgeStore,
		IAMCanAssumeEdgeWriter:                graphWriters.iamCanAssumeEdge,
		S3LogsToEdgeWriter:                    graphWriters.s3LogsToEdge,
		S3ExternalPrincipalGrantWriter:        graphWriters.s3ExternalPrincipalGrant,
		RDSPostureNodeWriter:                  graphWriters.rdsPostureNode,
		EC2InstanceIdentityNodeWriter:         graphWriters.ec2InstanceIdentityNode,
		EC2UsesProfileEdgeWriter:              graphWriters.ec2UsesProfileEdge,
		IAMInstanceProfileRoleEdgeWriter:      graphWriters.iamInstanceProfileRoleEdge,
		EC2InternetExposureNodeWriter:         graphWriters.ec2InternetExposureNode,
		EC2BlockDeviceKMSPostureNodeWriter:    graphWriters.ec2BlockDeviceKMSPostureNode,
		S3InternetExposureNodeWriter:          graphWriters.s3InternetExposureNode,
		ContainerImageIdentityWriter:          containerImageIdentityWriterFor(database),
		ContainerImageIdentityGenerationCheck: postgres.NewGenerationFreshnessCheck(database),
		// PackageProvenanceEdgeWriter / ContainerImageProvenanceEdgeWriter
		// (issue #5457) and ContainerImageDerivedFromEdgeWriter (issue #5460)
		// share one ProvenanceEdgeWriter instance -- it implements all three
		// narrower reducer interfaces.
		PackageProvenanceEdgeWriter:         graphWriters.provenanceEdge,
		ContainerImageProvenanceEdgeWriter:  graphWriters.provenanceEdge,
		ContainerImageDerivedFromEdgeWriter: graphWriters.provenanceEdge,
		CICDRunCorrelationWriter: cicdrun.PostgresCICDRunCorrelationWriter{
			DB: database,
		},
		ServiceCatalogCorrelationWriter: servicecatalog.PostgresServiceCatalogCorrelationWriter{
			DB: database,
		},
		ServiceMaterializationWriter:       serviceMaterializationWriter,
		ServiceDocumentationEvidenceLoader: serviceDocumentationEvidenceLoader,
		ServiceIncidentEvidenceLoader:      serviceIncidentEvidenceLoader,
		// ServiceRuntimeInstanceLoader sources the runtime evidence family (#1986)
		// from the graph's WorkloadInstance/Platform nodes per correlated
		// service's repository. It is wired only alongside
		// ServiceMaterializationWriter so the runtime family stays additive; it
		// anchors on workload_instance_repo_id, once per correlation intent.
		ServiceRuntimeInstanceLoader: reducer.GraphServiceRuntimeInstanceLoader{Graph: graphReader},
		// ServiceRuntimeProducerReadiness fences that read on in-flight
		// workload_materialization work at each repository's active generation (#7816).
		ServiceRuntimeProducerReadiness: producerreadiness.Store{DB: database},
		// ServiceVulnerabilityAdvisoryLoader sources the vulnerabilities evidence
		// family (#1990, #2127) from active reducer_supply_chain_impact_finding facts
		// on each correlated service's repository (repository_id-scoped, additive).
		ServiceVulnerabilityAdvisoryLoader: factStore,
		ObservabilityCoverageCorrelationWriter: obscoverage.PostgresObservabilityCoverageCorrelationWriter{
			DB: database,
		},
		PackageWriter: correlation.PostgresPackageWriter{
			DB: database,
		},
		DriftHandlers:               buildReducerDriftHandlers(database, tracer, instruments, logger, getenv),
		SearchDocumentHandlers:      buildReducerSearchDocumentHandlers(database, instruments, tracer, logger),
		CloudInventoryHandlers:      buildReducerCloudInventoryHandlers(database, logger),
		KubernetesHandlers:          buildReducerKubernetesHandlers(database, graphWriters),
		CrossplaneHandlers:          buildReducerCrossplaneHandlers(database, graphWriters, graphReader),
		SupplyChainSecurityHandlers: buildReducerSupplyChainSecurityHandlers(database, *factStore, secretsIAMGraphWriter, presence),
		IncidentRoutingHandlers:     buildReducerIncidentRoutingHandlers(database, *factStore, graphWriters),
		CodeEvidenceHandlers:        buildReducerCodeEvidenceHandlers(database, *factStore, graphWriters, graphReader, logger),
	})
	if err != nil {
		return reducer.Service{}, err
	}
	serviceExecutor := leaseExpiryBarrier.wrapReducer(executor)

	// graphReader already satisfies OrphanSweepReader (query.GraphQuery.Run has
	// the same shape) and is the same read port graphOrphanSweepRunnerFor uses.
	edgeWriter := newSharedProjectionEdgeWriter(neo4jExec, neo4jBatchSize(getenv),
		instruments, logger, graphReader, codeCallEdgeBatchSize, codeCallEdgeGroupBatchSize,
		inheritanceEdgeGroupBatchSize, sqlRelationshipEdgeGroupBatchSize)
	edgeWriter.SQLRelationshipSequentialWrites = graphBackend == runtimecfg.GraphBackendNornicDB
	edgeWriter.RepoDependencyRetractStatementTiming = repoDependencyRetractStatementTiming

	reducerGraphDrain := reducerGraphDrainFor(projectorDrainGate, database)

	configArtifactWalker, err := configArtifactWalkerRunnerFor(ctx, getenv, database, tracer, instruments, logger)
	if err != nil {
		return reducer.Service{}, err
	}

	workers := loadReducerWorkerCount(getenv, graphBackend)
	return reducer.Service{
		PollInterval:               time.Second,
		WorkSource:                 workQueue,
		Executor:                   serviceExecutor,
		WorkSink:                   workQueue,
		Heartbeater:                workQueue,
		HeartbeatInterval:          reducerHeartbeatInterval,
		HandlerTimeout:             handlerTimeout,
		HandlerTimeoutOverrides:    handlerTimeoutOverrides,
		SharedProjectionEdgeWriter: edgeWriter,
		SharedProjectionRunner: &worker.Runner{
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
			// #5984: durable record of intents no edge write could route. The
			// worker persists these BEFORE completing the intent, and fails the
			// cycle if the write fails -- completion is permanent, so this row
			// is the only lasting evidence that the intent produced no edge.
			UnroutableWriter: postgres.NewSharedProjectionUnroutableIntentStore(database),
			Config:           sharedCfg,
			Tracer:           tracer,
			Instruments:      instruments,
			Logger:           logger,
		},
		SupplyChainImpactWinnersMaintainer: &supplychaincore.SupplyChainImpactWinnersMaintainer{
			Rebuilder:    postgres.NewSupplyChainImpactWinnersStore(database),
			LeaseManager: intentStore,
			LeaseOwner:   defaultSupplyChainImpactWinnersLeaseOwner(),
			Logger:       logger,
		},
		CollectorEvidenceSummaryMaintainer: &evidence.Maintainer{
			Rebuilder:    postgres.NewCollectorEvidenceSummaryStore(database),
			Freshness:    postgres.NewCollectorEvidenceSummaryStore(database),
			LeaseManager: intentStore,
			LeaseOwner:   defaultCollectorEvidenceSummaryLeaseOwner(),
			Logger:       logger,
		},
		CodeCallProjectionRunner: &projection.Runner{
			IntentReader:        intentStore,
			LeaseManager:        intentStore,
			EdgeWriter:          edgeWriter,
			AcceptedGen:         postgres.NewAcceptedGenerationLookup(database),
			AcceptedGenPrefetch: acceptedGenerationPrefetch,
			ReadinessLookup:     graphProjectionReadinessLookup,
			ReadinessPrefetch:   graphProjectionReadinessPrefetch,
			ReducerGraphDrain:   reducerGraphDrain,
			// #6184: cross-repo CALLS loss happens on every backend/profile,
			// so quiescence is wired unconditionally while the contention
			// half of the drain stays behind the projector drain gate.
			CanonicalQuiescence: postgres.NewReducerGraphDrain(database),
			Config:              codeCallCfg,
			Tracer:              tracer,
			Instruments:         instruments,
			Logger:              logger,
		},
		RepoDependencyProjectionRunner: newRepoDependencyProjectionRunner(
			intentStore, database, edgeWriter, workQueue,
			relationshipGenerationActive, acceptedGenerationPrefetch,
			graphProjectionReadinessPrefetch,
			repoDependencyCfg, tracer, instruments, logger,
		),
		CodeReachabilityProjectionRunner: codeReachabilityProjectionRunnerFor(database, sharedCfg, workers, logger),
		GraphProjectionPhaseRepairer: graphProjectionPhaseRepairerFor(
			graphProjectionRepairQueue, database, graphProjectionStateStore, repairCfg, clk, instruments, logger,
		),
		GenerationRetentionRunner:       generationRetentionRunner,
		ChangedSinceLinkRunner:          changedSinceLinkRunnerFor(getenv, database, tracer, instruments, logger),
		InfraInventoryReconcileRunner:   infraInventoryReconcileRunnerFor(getenv, database, tracer, instruments, logger),
		ConfigArtifactWalkerRunner:      configArtifactWalker,
		GenerationLivenessRunner:        generationLivenessRunner,
		PoisonLivenessRunner:            poisonLivenessRunner,
		ActivationObligationRunner:      runners.obligation,
		ProducerActivationRunner:        runners.producer,
		GraphOrphanSweepRunner:          graphOrphanSweepRunner,
		CodeValueFlowStaleCleanupRunner: codeValueFlowStaleCleanupRunner,
		SearchVectorBuildRunner:         searchVectorBuildRunner,
		CrossScopeCompletionRunner: &reducer.CrossScopeCompletionRunner{
			Queue:      completionstore.NewCrossScopeCompletionStore(database),
			LeaseOwner: defaultCrossScopeCompletionLeaseOwner(),
			Logger:     logger,
		},
		QuarantineWriter:       postgres.NewReducerInputInvalidFactStore(database),
		IsDeferredFailureClass: postgres.IsNonCountingReducerRetryFailureClass,
		Workers:                workers,
		BatchClaimSize:         loadReducerBatchClaimSize(getenv, workers, graphBackend),
		Tracer:                 tracer,
		Instruments:            instruments,
		Logger:                 logger,
	}, nil
}

// reducerDomainStrings lives in main_helpers.go to keep this file within the
// repo file-size budget.
