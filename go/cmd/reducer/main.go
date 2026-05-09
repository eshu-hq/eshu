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
	"github.com/eshu-hq/eshu/go/internal/buildinfo"
	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func main() {
	if handled, err := buildinfo.PrintVersionFlag(os.Args[1:], os.Stdout, "eshu-reducer"); handled {
		if err != nil {
			_, _ = fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	if err := run(context.Background()); err != nil {
		slog.Error("reducer failed", "error", err)
		os.Exit(1)
	}
}

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

	db, err := runtimecfg.OpenPostgres(parent, os.Getenv)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	queueObserver := postgres.NewQueueObserverStore(postgres.SQLQueryer{DB: db})
	if err := telemetry.RegisterObservableGauges(instruments, meter, queueObserver, nil); err != nil {
		return fmt.Errorf("register observable gauges: %w", err)
	}

	neo4jExecutor, cypherExecutor, neo4jReader, graphReader, neo4jCloser, err := openReducerNeo4jAdapters(parent, os.Getenv)
	if err != nil {
		return err
	}
	defer func() { _ = neo4jCloser.Close() }()

	instrumentedDB := &postgres.InstrumentedDB{
		Inner:       postgres.SQLDB{DB: db},
		Tracer:      tracer,
		Instruments: instruments,
		StoreName:   "reducer",
	}
	instrumentedNeo4j := &sourcecypher.InstrumentedExecutor{
		Inner:       neo4jExecutor,
		Tracer:      tracer,
		Instruments: instruments,
	}
	intentStore := postgres.NewSharedIntentStore(instrumentedDB)
	serviceRunner, err := buildReducerService(instrumentedDB, instrumentedNeo4j, cypherExecutor, intentStore, neo4jReader, graphReader, os.Getenv, tracer, instruments, logger)
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
	codeCallEdgeBatchSize, codeCallEdgeGroupBatchSize := loadCodeCallEdgeWriterTuning(getenv)
	inheritanceEdgeGroupBatchSize, sqlRelationshipEdgeGroupBatchSize := loadSharedEdgeWriterGroupTuning(getenv)
	graphBackend, err := runtimecfg.LoadGraphBackend(getenv)
	if err != nil {
		return reducer.Service{}, err
	}
	nornicDBGroupedWrites := false
	if graphBackend == runtimecfg.GraphBackendNornicDB {
		nornicDBGroupedWrites, err = nornicDBCanonicalGroupedWrites(getenv)
		if err != nil {
			return reducer.Service{}, err
		}
		if nornicDBGroupedWrites {
			logger.Warn("NornicDB semantic grouped writes enabled for conformance",
				"graph_backend", string(graphBackend),
				"grouped_writes", true,
				"env_var", nornicDBCanonicalGroupedWritesEnv)
		}
	}

	edgeWriterForHandlers := sourcecypher.NewEdgeWriter(neo4jExec, neo4jBatchSize(getenv))
	edgeWriterForHandlers.Instruments = instruments
	edgeWriterForHandlers.Logger = logger
	edgeWriterForHandlers.InheritanceGroupBatchSize = inheritanceEdgeGroupBatchSize
	edgeWriterForHandlers.SQLRelationshipGroupBatchSize = sqlRelationshipEdgeGroupBatchSize
	relationshipStore := postgres.NewRelationshipStore(database)
	factStore := postgres.NewFactStore(database)
	codeCallIntentWriter := postgres.NewCodeCallIntentWriterWithInstruments(database, instruments)
	repoDependencyIntentWriter := postgres.NewSharedIntentAcceptanceWriterWithInstruments(database, instruments)
	acceptedGenerationPrefetch := postgres.NewAcceptedGenerationPrefetch(database)
	graphProjectionStateStore := postgres.NewGraphProjectionPhaseStateStore(database)
	graphProjectionRepairQueue := postgres.NewGraphProjectionPhaseRepairQueueStore(database)
	graphProjectionReadinessLookup := postgres.NewGraphProjectionReadinessLookup(database)
	graphProjectionReadinessPrefetch := postgres.NewGraphProjectionReadinessPrefetch(database)
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
	claimDomain, err := loadReducerClaimDomain(getenv)
	if err != nil {
		return reducer.Service{}, fmt.Errorf("load reducer claim domain: %w", err)
	}
	if claimDomain != "" && logger != nil {
		logger.Info("reducer claims restricted to domain",
			"domain", string(claimDomain),
		)
	}
	workQueue := postgres.NewReducerQueue(database, "reducer", time.Minute)
	workQueue.RetryDelay = retryCfg.RetryDelay
	workQueue.MaxAttempts = retryCfg.MaxAttempts
	workQueue.ClaimDomain = claimDomain
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
		Workers:        workers,
		BatchClaimSize: loadReducerBatchClaimSize(getenv, workers, graphBackend),
		Tracer:         tracer,
		Instruments:    instruments,
		Logger:         logger,
	}, nil
}
