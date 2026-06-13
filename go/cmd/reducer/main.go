package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/app"
	"github.com/eshu-hq/eshu/go/internal/graphschemacompat"
	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/relationships/tfstatebackend"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func run(parent context.Context) error {
	// Initialize telemetry
	bootstrap, err := telemetry.NewBootstrap("reducer")
	if err != nil {
		return fmt.Errorf("telemetry bootstrap: %w", err)
	}
	providers, err := telemetry.NewProviders(parent, bootstrap)
	if err != nil {
		return fmt.Errorf("telemetry providers: %w", err)
	}
	defer func() {
		_ = providers.Shutdown(context.Background())
	}()

	logger := telemetry.NewLogger(bootstrap, "reducer", "reducer")
	tracer := providers.TracerProvider.Tracer(telemetry.DefaultSignalName)
	meter := providers.MeterProvider.Meter(telemetry.DefaultSignalName)
	instruments, err := telemetry.NewInstruments(meter)
	if err != nil {
		return fmt.Errorf("telemetry instruments: %w", err)
	}

	logger.Info("starting reducer")

	pprofSrv, err := runtimecfg.NewPprofServer(os.Getenv)
	if err != nil {
		return fmt.Errorf("pprof server: %w", err)
	}
	if pprofSrv != nil {
		if err := pprofSrv.Start(parent); err != nil {
			return fmt.Errorf("pprof server start: %w", err)
		}
		logger.Info("pprof server listening", "addr", pprofSrv.Addr())
		defer func() {
			_ = pprofSrv.Stop(context.Background())
		}()
	}

	db, err := runtimecfg.OpenPostgres(parent, os.Getenv)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	if _, err := graphschemacompat.RequireCompatibleForRuntime(parent, postgres.SQLQueryer{DB: db}, os.Getenv); err != nil {
		return err
	}

	neo4jExecutor, cypherExecutor, neo4jReader, graphReader, neo4jCloser, err := openReducerNeo4jAdapters(parent, os.Getenv)
	if err != nil {
		return err
	}
	defer func() { _ = neo4jCloser.Close() }()

	serviceRunner, err := buildObservedReducerService(db, neo4jExecutor, cypherExecutor, neo4jReader, graphReader, os.Getenv, tracer, instruments, meter, logger)
	if err != nil {
		return err
	}
	retryPolicy, err := loadReducerQueueConfig(os.Getenv)
	if err != nil {
		return err
	}
	statusReader := statuspkg.WithRetryPolicies(
		postgres.NewStatusStore(postgres.SQLQueryer{DB: db}),
		statuspkg.MergeRetryPolicies(
			statuspkg.DefaultRetryPolicies(),
			statuspkg.RetryPolicySummary{
				Stage:       "reducer",
				MaxAttempts: retryPolicy.MaxAttempts,
				RetryDelay:  retryPolicy.RetryDelay,
			},
		)...,
	)
	service, err := app.NewHostedWithStatusServer(
		"reducer",
		serviceRunner,
		statusReader,
		runtimecfg.WithPrometheusHandler(providers.PrometheusHandler),
	)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	defer stop()

	return service.Run(ctx)
}

func buildReducerService(
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
) (reducer.Service, error) {
	sharedCfg := reducer.LoadSharedProjectionConfig(getenv)
	codeCallCfg := loadCodeCallProjectionConfig(getenv)
	repoDependencyCfg := loadRepoDependencyProjectionConfig(getenv)
	repairCfg := loadGraphProjectionPhaseRepairConfig(getenv)
	generationRetentionCfg := loadGenerationRetentionConfig(getenv)
	graphOrphanSweepCfg := loadGraphOrphanSweepConfig(getenv)
	codeCallEdgeBatchSize, codeCallEdgeGroupBatchSize := loadCodeCallEdgeWriterTuning(getenv)
	inheritanceEdgeGroupBatchSize, sqlRelationshipEdgeGroupBatchSize := loadSharedEdgeWriterGroupTuning(getenv)
	serviceMaterializationWriter := serviceMaterializationWriterFor(database)
	serviceDocumentationEvidenceLoader := serviceDocumentationEvidenceLoaderFor(database)
	graphBackend, err := runtimecfg.LoadGraphBackend(getenv)
	if err != nil {
		return reducer.Service{}, err
	}
	nornicDBGroupedWrites, err := resolveNornicDBGroupedWrites(getenv, graphBackend, logger)
	if err != nil {
		return reducer.Service{}, err
	}

	edgeWriterForHandlers := newHandlerEdgeWriter(neo4jExec, neo4jBatchSize(getenv), instruments, logger, inheritanceEdgeGroupBatchSize, sqlRelationshipEdgeGroupBatchSize)
	cloudResourceNodeWriter := sourcecypher.NewCloudResourceNodeWriter(neo4jExec, neo4jBatchSize(getenv))
	ec2InstanceNodeWriter := sourcecypher.NewEC2InstanceNodeWriter(neo4jExec, neo4jBatchSize(getenv))
	cloudResourceEdgeWriter := sourcecypher.NewCloudResourceEdgeWriter(neo4jExec, neo4jBatchSize(getenv))
	workloadCloudRelationshipEdgeWriter := sourcecypher.NewWorkloadCloudRelationshipWriter(neo4jExec, neo4jBatchSize(getenv))
	kubernetesWorkloadNodeWriter := sourcecypher.NewKubernetesWorkloadNodeWriter(neo4jExec, neo4jBatchSize(getenv))
	securityGroupEndpointNodeWriter := sourcecypher.NewSecurityGroupEndpointNodeWriter(neo4jExec, neo4jBatchSize(getenv))
	securityGroupReachabilityWriter := sourcecypher.NewSecurityGroupReachabilityWriter(neo4jExec, neo4jBatchSize(getenv))
	kubernetesCorrelationEdgeWriter := sourcecypher.NewKubernetesCorrelationEdgeWriter(neo4jExec, neo4jBatchSize(getenv))
	iamEscalationEdgeWriter := sourcecypher.NewIAMEscalationEdgeWriter(neo4jExec, neo4jBatchSize(getenv))
	iamCanPerformEdgeWriter := sourcecypher.NewIAMCanPerformEdgeWriter(neo4jExec, neo4jBatchSize(getenv))
	secretsIAMGraphWriter, err := secretsIAMGraphProjectionWriter(getenv, neo4jExec, neo4jBatchSize(getenv), logger)
	if err != nil {
		return reducer.Service{}, err
	}
	endpointPresenceWriter, endpointPresenceLookup := endpointPresenceWiring(secretsIAMGraphWriter != nil, database)
	observabilityCoverageEdgeWriter := sourcecypher.NewObservabilityCoverageEdgeWriter(neo4jExec, neo4jBatchSize(getenv))
	incidentRoutingEvidenceWriter := sourcecypher.NewIncidentRoutingEvidenceWriter(neo4jExec, neo4jBatchSize(getenv))
	iamCanAssumeEdgeWriter := sourcecypher.NewIAMCanAssumeEdgeWriter(neo4jExec, neo4jBatchSize(getenv))
	s3LogsToEdgeWriter := sourcecypher.NewS3LogsToEdgeWriter(neo4jExec, neo4jBatchSize(getenv))
	s3ExternalPrincipalGrantWriter := sourcecypher.NewS3ExternalPrincipalGrantWriter(neo4jExec, neo4jBatchSize(getenv))
	rdsPostureNodeWriter := sourcecypher.NewRDSPostureNodeWriter(neo4jExec, neo4jBatchSize(getenv))
	ec2UsesProfileEdgeWriter := sourcecypher.NewEC2UsesProfileEdgeWriter(neo4jExec, neo4jBatchSize(getenv))
	iamInstanceProfileRoleEdgeWriter := sourcecypher.NewIAMInstanceProfileRoleEdgeWriter(neo4jExec, neo4jBatchSize(getenv))
	ec2InternetExposureNodeWriter := sourcecypher.NewEC2InternetExposureNodeWriter(neo4jExec, neo4jBatchSize(getenv))
	ec2BlockDeviceKMSPostureNodeWriter := sourcecypher.NewEC2BlockDeviceKMSPostureNodeWriter(neo4jExec, neo4jBatchSize(getenv))
	s3InternetExposureNodeWriter := sourcecypher.NewS3InternetExposureNodeWriter(neo4jExec, neo4jBatchSize(getenv))
	relationshipStore := postgres.NewRelationshipStore(database)
	factStore := postgres.NewFactStore(database)
	codeCallIntentWriter := postgres.NewCodeCallIntentWriterWithInstruments(database, instruments)
	repoDependencyIntentWriter := postgres.NewSharedIntentAcceptanceWriterWithInstruments(database, instruments)
	acceptedGenerationPrefetch := postgres.NewAcceptedGenerationPrefetch(database)
	graphProjectionStateStore := postgres.NewGraphProjectionPhaseStateStore(database)
	graphProjectionRepairQueue := postgres.NewGraphProjectionPhaseRepairQueueStore(database)
	graphProjectionReadinessLookup := postgres.NewGraphProjectionReadinessLookup(database)
	graphProjectionReadinessPrefetch := postgres.NewGraphProjectionReadinessPrefetch(database)
	generationRetentionRunner := generationRetentionRunnerFor(database, generationRetentionCfg)
	if generationRetentionRunner != nil {
		generationRetentionRunner.Instruments = instruments
		generationRetentionRunner.Logger = logger
	}
	graphOrphanSweepRunner := graphOrphanSweepRunnerFor(neo4jExec, graphReader, intentStore, graphOrphanSweepCfg)
	if graphOrphanSweepRunner != nil {
		graphOrphanSweepRunner.Logger = logger
	}
	cloudInventoryEvidenceLoader, cloudInventoryAdmissionWriter, cloudInventoryGenerationCheck, cloudInventoryTagEvidenceLoader := cloudInventoryAdmissionWiring(database, logger)
	multiCloudRuntimeDriftEvidenceLoader, multiCloudRuntimeDriftWriter, multiCloudRuntimeDriftLogger := multiCloudRuntimeDriftWiring(database, tracer, instruments, logger)
	incidentRepoCorrelationLoader, incidentRepoCorrelationResolver, incidentRepoCorrelationWriter := incidentRepositoryCorrelationWiring(database)
	semanticEntityExecutor := semanticEntityExecutorForGraphBackend(
		neo4jExec,
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
		logger.Info("reducer claims will wait for source-local projection drain",
			"graph_backend", string(graphBackend),
			"query_profile", string(query.ProfileLocalAuthoritative),
		)
	}
	claimDomains, err := loadReducerClaimDomains(getenv)
	if err != nil {
		return reducer.Service{}, fmt.Errorf("load reducer claim domains: %w", err)
	}
	if len(claimDomains) > 0 && logger != nil {
		logger.Info("reducer claims restricted to domains",
			"domains", reducerDomainStrings(claimDomains),
		)
	}
	workQueue := postgres.NewReducerQueue(database, "reducer", time.Minute)
	workQueue.RetryDelay = retryCfg.RetryDelay
	workQueue.MaxAttempts = retryCfg.MaxAttempts
	workQueue.ClaimDomains = claimDomains
	workQueue.RequireProjectorDrainBeforeClaim = projectorDrainGate
	workQueue.ExpectedSourceLocalProjectors = loadReducerExpectedSourceLocalProjectors(getenv)
	workQueue.SemanticEntityClaimLimit = loadReducerSemanticEntityClaimLimit(getenv, graphBackend)
	if workQueue.ExpectedSourceLocalProjectors > 0 && logger != nil {
		logger.Info("semantic reducers will wait for expected source-local projectors",
			"expected_source_local_projectors", workQueue.ExpectedSourceLocalProjectors,
		)
	}
	if projectorDrainGate && logger != nil {
		logger.Info("semantic reducer claim limit configured",
			"semantic_entity_claim_limit", workQueue.SemanticEntityClaimLimit,
		)
	}

	executor, err := reducer.NewDefaultRuntime(reducer.DefaultHandlers{
		DeployableUnitCorrelationHandler: reducer.DeployableUnitCorrelationHandler{
			FactLoader:     factStore,
			ResolvedLoader: relationshipStore,
			PhasePublisher: graphProjectionStateStore,
		},
		WorkloadProjectionInputLoader: reducer.CorrelatedWorkloadProjectionInputLoader{
			FactLoader:     factStore,
			ResolvedLoader: relationshipStore,
			ScopeResolver:  postgres.RepoScopeResolver{DB: database},
		},
		WorkloadDependencyLookup:           neo4jWorkloadDependencyLookup{reader: graphReader},
		WorkloadIdentityWriter:             reducer.PostgresWorkloadIdentityWriter{DB: database},
		CloudAssetResolutionWriter:         reducer.PostgresCloudAssetResolutionWriter{DB: database},
		PlatformMaterializationWriter:      reducer.PostgresPlatformMaterializationWriter{DB: database},
		PlatformGraphLocker:                platformGraphLockerForReducer(database),
		WorkloadMaterializationReplayer:    workQueue,
		WorkloadMaterializer:               reducer.NewWorkloadMaterializer(cypherExec),
		InfrastructurePlatformMaterializer: reducer.NewInfrastructurePlatformMaterializer(cypherExec),
		InfrastructurePlatformLookup:       reducer.GraphInfrastructurePlatformLookup{Graph: graphReader},
		FactLoader:                         factStore,
		CodeCallIntentWriter:               codeCallIntentWriter,
		GraphProjectionPhasePublisher:      graphProjectionStateStore,
		GraphProjectionRepairQueue:         graphProjectionRepairQueue,
		ReadinessLookup:                    graphProjectionReadinessLookup,
		ReadinessPrefetch:                  graphProjectionReadinessPrefetch,
		SemanticEntityWriter:               semanticEntityWriter,
		SQLRelationshipEdgeWriter:          edgeWriterForHandlers,
		InheritanceEdgeWriter:              edgeWriterForHandlers,
		DocumentationEdgeWriter:            edgeWriterForHandlers,
		RationaleEdgeWriter:                edgeWriterForHandlers,
		EvidenceFactLoader:                 relationshipStore,
		AssertionLoader:                    relationshipStore,
		ResolutionPersister:                relationshipStore,
		ResolvedRelationshipLoader:         relationshipStore,
		RepoDependencyIntentWriter:         repoDependencyIntentWriter,
		RepoDependencyEdgeWriter:           edgeWriterForHandlers,
		WorkloadDependencyEdgeWriter:       edgeWriterForHandlers,
		GenerationCheck:                    postgres.NewGenerationFreshnessCheck(database),
		PriorGenerationCheck:               postgres.NewPriorGenerationCheck(database),
		Tracer:                             tracer,
		Instruments:                        instruments,
		// Terraform config-vs-state drift adapters (issue #163). All three
		// must be non-nil for the reducer registry to register
		// DomainConfigStateDrift (see internal/reducer/defaults.go:202-213).
		TerraformBackendResolver: tfstatebackend.NewResolver(
			postgres.PostgresTerraformBackendQuery{DB: database},
		),
		DriftEvidenceLoader: postgres.PostgresDriftEvidenceLoader{
			DB:               database,
			Tracer:           tracer,
			Logger:           logger,
			PriorConfigDepth: parsePriorConfigDepth(getenv(driftPriorConfigDepthEnv), logger),
			// Instruments drives eshu_dp_drift_unresolved_module_calls_total
			// for the module-aware drift join (issue #169). Nil-safe in the
			// loader itself; passing it here keeps the counter wired in
			// production runs.
			Instruments: instruments,
		},
		DriftLogger: logger,
		// AWS runtime drift joins current AWS resource facts to active
		// Terraform-state resources by ARN, then resolves the state backend to
		// the owning config snapshot before classifying unmanaged resources.
		AWSCloudRuntimeDriftEvidenceLoader: postgres.PostgresAWSCloudRuntimeDriftEvidenceLoader{
			DB: database,
			ConfigResolver: tfstatebackend.NewResolver(
				postgres.PostgresTerraformBackendQuery{DB: database},
			),
			Tracer:      tracer,
			Logger:      logger,
			Instruments: instruments,
		},
		AWSCloudRuntimeDriftWriter: reducer.PostgresAWSCloudRuntimeDriftWriter{DB: database},
		AWSCloudRuntimeDriftLogger: logger,
		// Multi-cloud runtime drift wiring (issues #1997, #1998); see
		// multiCloudRuntimeDriftWiring for the uid-keyed join contract.
		MultiCloudRuntimeDriftEvidenceLoader: multiCloudRuntimeDriftEvidenceLoader,
		MultiCloudRuntimeDriftWriter:         multiCloudRuntimeDriftWriter,
		MultiCloudRuntimeDriftLogger:         multiCloudRuntimeDriftLogger,
		CloudInventoryEvidenceLoader:         cloudInventoryEvidenceLoader,
		CloudInventoryAdmissionWriter:        cloudInventoryAdmissionWriter,
		CloudInventoryGenerationCheck:        cloudInventoryGenerationCheck,
		CloudInventoryTagEvidenceLoader:      cloudInventoryTagEvidenceLoader,
		CloudResourceNodeWriter:              cloudResourceNodeWriter,
		EC2InstanceNodeWriter:                ec2InstanceNodeWriter,
		CloudResourceEdgeWriter:              cloudResourceEdgeWriter,
		WorkloadCloudRelationshipEdgeWriter:  workloadCloudRelationshipEdgeWriter,
		KubernetesWorkloadNodeWriter:         kubernetesWorkloadNodeWriter,
		SecurityGroupEndpointNodeWriter:      securityGroupEndpointNodeWriter,
		SecurityGroupRuleNodeWriter:          securityGroupReachabilityWriter,
		SecurityGroupReachabilityWriter:      securityGroupReachabilityWriter,
		KubernetesCorrelationEdgeWriter:      kubernetesCorrelationEdgeWriter,
		IAMEscalationEdgeWriter:              iamEscalationEdgeWriter,
		IAMCanPerformEdgeWriter:              iamCanPerformEdgeWriter,
		SecretsIAMGraphWriter:                secretsIAMGraphWriter,
		EndpointPresenceWriter:               endpointPresenceWriter,
		EndpointPresenceLookup:               endpointPresenceLookup,
		ObservabilityCoverageEdgeWriter:      observabilityCoverageEdgeWriter,
		IAMCanAssumeEdgeWriter:               iamCanAssumeEdgeWriter,
		S3LogsToEdgeWriter:                   s3LogsToEdgeWriter,
		S3ExternalPrincipalGrantWriter:       s3ExternalPrincipalGrantWriter,
		RDSPostureNodeWriter:                 rdsPostureNodeWriter,
		EC2UsesProfileEdgeWriter:             ec2UsesProfileEdgeWriter,
		IAMInstanceProfileRoleEdgeWriter:     iamInstanceProfileRoleEdgeWriter,
		EC2InternetExposureNodeWriter:        ec2InternetExposureNodeWriter,
		EC2BlockDeviceKMSPostureNodeWriter:   ec2BlockDeviceKMSPostureNodeWriter,
		S3InternetExposureNodeWriter:         s3InternetExposureNodeWriter,
		ContainerImageIdentityWriter: reducer.PostgresContainerImageIdentityWriter{
			DB: database,
		},
		CICDRunCorrelationWriter: reducer.PostgresCICDRunCorrelationWriter{
			DB: database,
		},
		ServiceCatalogCorrelationWriter: reducer.PostgresServiceCatalogCorrelationWriter{
			DB: database,
		},
		ServiceMaterializationWriter:       serviceMaterializationWriter,
		ServiceDocumentationEvidenceLoader: serviceDocumentationEvidenceLoader,
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
		KubernetesCorrelationWriter: reducer.PostgresKubernetesCorrelationWriter{
			DB: database,
		},
		SBOMAttestationAttachmentWriter: reducer.PostgresSBOMAttestationAttachmentWriter{
			DB: database,
		},
		SupplyChainImpactWriter: reducer.PostgresSupplyChainImpactWriter{
			DB: database,
		},
		SecurityAlertReconciliationWriter: reducer.PostgresSecurityAlertReconciliationWriter{
			DB: database,
		},
		PackageCorrelationWriter: reducer.PostgresPackageCorrelationWriter{
			DB: database,
		},
		SecretsIAMTrustChainEvidenceLoader: factStore,
		SecretsIAMTrustChainWriter: reducer.PostgresSecretsIAMTrustChainWriter{
			DB: database,
		},
		IncidentRoutingEvidenceLoader: factStore,
		IncidentRoutingEvidenceWriter: incidentRoutingEvidenceWriter,
		// Durable incident -> repository correlation (#2161); see helper for rationale.
		AppliedPagerDutyServiceRoutingLoader: incidentRepoCorrelationLoader,
		BackendRepositoryResolver:            incidentRepoCorrelationResolver,
		IncidentRepositoryCorrelationWriter:  incidentRepoCorrelationWriter,
	})
	if err != nil {
		return reducer.Service{}, err
	}

	edgeWriter := sourcecypher.NewEdgeWriter(neo4jExec, neo4jBatchSize(getenv))
	edgeWriter.Instruments = instruments
	edgeWriter.Logger = logger
	edgeWriter.CodeCallBatchSize = codeCallEdgeBatchSize
	edgeWriter.CodeCallGroupBatchSize = codeCallEdgeGroupBatchSize
	edgeWriter.InheritanceGroupBatchSize = inheritanceEdgeGroupBatchSize
	edgeWriter.SQLRelationshipGroupBatchSize = sqlRelationshipEdgeGroupBatchSize

	var reducerGraphDrain reducer.ReducerGraphDrain
	if projectorDrainGate {
		reducerGraphDrain = postgres.NewReducerGraphDrain(database)
	}

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
			Config:              sharedCfg,
			Tracer:              tracer,
			Instruments:         instruments,
			Logger:              logger,
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
		RepoDependencyProjectionRunner: &reducer.RepoDependencyProjectionRunner{
			IntentReader:                    intentStore,
			LeaseManager:                    intentStore,
			EdgeWriter:                      edgeWriter,
			WorkloadMaterializationReplayer: workQueue,
			AcceptedGen:                     postgres.NewAcceptedGenerationLookup(database),
			AcceptedGenPrefetch:             acceptedGenerationPrefetch,
			Config:                          repoDependencyCfg,
			Tracer:                          tracer,
			Instruments:                     instruments,
			Logger:                          logger,
		},
		GraphProjectionPhaseRepairer: &reducer.GraphProjectionPhaseRepairer{
			Queue:       graphProjectionRepairQueue,
			AcceptedGen: postgres.NewAcceptedGenerationLookup(database),
			StateLookup: graphProjectionStateStore,
			Publisher:   graphProjectionStateStore,
			Config:      repairCfg,
			Instruments: instruments,
			Logger:      logger,
		},
		GenerationRetentionRunner: generationRetentionRunner,
		GraphOrphanSweepRunner:    graphOrphanSweepRunner,
		Workers:                   workers,
		BatchClaimSize:            loadReducerBatchClaimSize(getenv, workers, graphBackend),
		Tracer:                    tracer,
		Instruments:               instruments,
		Logger:                    logger,
	}, nil
}

// reducerDomainStrings lives in main_helpers.go to keep this file within the
// repo file-size budget.
