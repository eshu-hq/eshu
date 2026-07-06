// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/content"
	"github.com/eshu-hq/eshu/go/internal/cpubudget"
	"github.com/eshu-hq/eshu/go/internal/projector"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	ingesterCollectorPollInterval        = time.Second
	ingesterConnectionTimeout            = 10 * time.Second
	defaultNornicDBCanonicalWriteTimeout = 30 * time.Second
	defaultNornicDBPhaseGroupStatements  = 500
	// Entity statements carry the heaviest canonical payloads on the current
	// self-repo dogfood lane, so NornicDB needs a lower grouped-transaction cap
	// here than on lighter canonical phases.
	defaultNornicDBEntityPhaseStatements = 25
	// File upserts are lighter than entity rows but huge repos can emit many
	// 500-row file statements. Keep this phase narrow without lowering the
	// global non-entity phase-group cap.
	defaultNornicDBFilePhaseStatements = 5
	// Some repos carry thousands of static/vendor files. Keep NornicDB file
	// upsert row payloads bounded separately from the grouped-statement cap so
	// one huge file statement cannot dominate a Bolt transaction.
	defaultNornicDBFileBatchSize = 100
	// Long-running labels such as Variable need cumulative visibility before
	// the whole entities phase completes, otherwise tuning waits on hour-scale
	// dogfood runs.
	defaultNornicDBEntityLabelSummaryExecutions = 10
	// Normal NornicDB entity upserts stay bounded to 100 rows so we do not send
	// 500-row canonical entity statements through the slower Bolt path.
	defaultNornicDBEntityBatchSize = 100
	// Function entities remain the heaviest row shape inside the broader entity
	// phase, so they get a narrower row cap than other entity labels.
	// Function rows are the highest-cost entity family on the self-repo dogfood
	// lane. The first narrowed 10-row default lowered per-statement cost, but it
	// fragmented the lane too much; 15 rows keeps Function chunks bounded while
	// still reaching Variable at the healthier ~20s band.
	defaultNornicDBFunctionEntityBatchSize = 15
	// Struct entities were the next heavy family on the self-repo dogfood lane
	// once Function rows were narrowed, so they get the next smaller row cap.
	defaultNornicDBStructEntityBatchSize = 50
	// Variable is high-cardinality but not row-heavy after file-scoped entity
	// batching. The 2026-04-27 php-large-repo-b ladder improved from
	// 196.7s at 10 rows to 102.8s at 100 rows with no retries, no singleton
	// fallbacks, and max grouped execution under one second.
	defaultNornicDBVariableEntityBatchSize = 100
	// K8sResource rows can cluster heavily in one Helm/Kustomize YAML file.
	// File-scoped inline containment preserves NornicDB row binding correctness,
	// and full-corpus timing showed even five same-file rows can exceed the
	// 15s write budget under concurrent K8s-heavy projection.
	defaultNornicDBK8sResourceEntityBatchSize = 1
	// Function entity statements remain the slowest grouped transaction shape
	// on the self-repo dogfood lane. Ten-statement groups still drifted into
	// the high-30s seconds, so NornicDB now keeps that family on the same
	// conservative grouped cap as Variable for the built-in default lane.
	defaultNornicDBFunctionEntityPhaseStatements = 5
	// Struct entity statements were the next slowest family after Function, but
	// still lighter than Function rows, so they keep a slightly looser cap.
	defaultNornicDBStructEntityPhaseStatements = 15
	// Variable entities hit the first post-Function repo-scale timeout at the
	// broader entity phase limit, so they need the same conservative grouped
	// statement cap as Function for the current dogfood lane.
	defaultNornicDBVariableEntityPhaseStatements = 5
	// K8sResource rows are individually small, but Helm/Kustomize repos create
	// dense same-label bursts. Keep grouped execution to one statement at a
	// time so NornicDB proves correctness before we widen this hot family.
	defaultNornicDBK8sResourceEntityPhaseStatements = 1
	// nornicDBEntityPhaseConcurrencyCap is the hard upper bound for the
	// ESHU_NORNICDB_ENTITY_PHASE_CONCURRENCY env override and for the
	// CPU-derived default. Each worker holds one Bolt session against
	// NornicDB while a grouped chunk runs, so the cap also bounds peak Bolt
	// session demand from the canonical entity path.
	nornicDBEntityPhaseConcurrencyCap          = 16
	canonicalWriteTimeoutEnv                   = "ESHU_CANONICAL_WRITE_TIMEOUT"
	nornicDBCanonicalGroupedWritesEnv          = "ESHU_NORNICDB_CANONICAL_GROUPED_WRITES"
	nornicDBPhaseGroupStatementsEnv            = "ESHU_NORNICDB_PHASE_GROUP_STATEMENTS"
	nornicDBFilePhaseGroupStatementsEnv        = "ESHU_NORNICDB_FILE_PHASE_GROUP_STATEMENTS"
	nornicDBFileBatchSizeEnv                   = "ESHU_NORNICDB_FILE_BATCH_SIZE"
	nornicDBEntityPhaseStatementsEnv           = "ESHU_NORNICDB_ENTITY_PHASE_GROUP_STATEMENTS"
	nornicDBEntityBatchSizeEnv                 = "ESHU_NORNICDB_ENTITY_BATCH_SIZE"
	nornicDBEntityLabelBatchSizesEnv           = "ESHU_NORNICDB_ENTITY_LABEL_BATCH_SIZES"
	nornicDBEntityLabelPhaseGroupStatementsEnv = "ESHU_NORNICDB_ENTITY_LABEL_PHASE_GROUP_STATEMENTS"
	nornicDBBatchedEntityContainmentEnv        = "ESHU_NORNICDB_BATCHED_ENTITY_CONTAINMENT"
	nornicDBEntityPhaseConcurrencyEnv          = "ESHU_NORNICDB_ENTITY_PHASE_CONCURRENCY"
)

func buildIngesterService(
	database postgres.ExecQueryer,
	canonicalWriter projector.CanonicalWriter,
	getenv func(string) string,
	getwd func() (string, error),
	environ func() []string,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
) (compositeRunner, error) {
	collectorSvc, err := buildIngesterCollectorService(database, getenv, getwd, environ, tracer, instruments, logger)
	if err != nil {
		return compositeRunner{}, fmt.Errorf("build ingester collector: %w", err)
	}

	projectorSvc, err := buildIngesterProjectorService(database, canonicalWriter, getenv, tracer, instruments, logger)
	if err != nil {
		return compositeRunner{}, fmt.Errorf("build ingester projector: %w", err)
	}

	return newCompositeRunner(logger, collectorSvc, projectorSvc), nil
}

func buildIngesterCollectorService(
	database postgres.ExecQueryer,
	getenv func(string) string,
	getwd func() (string, error),
	environ func() []string,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
) (collector.Service, error) {
	config, err := collector.LoadRepoSyncConfig("ingester", getenv)
	if err != nil {
		return collector.Service{}, err
	}
	discoveryOptions, err := collector.LoadDiscoveryOptionsFromEnv(getenv)
	if err != nil {
		return collector.Service{}, err
	}
	committer := postgres.NewIngestionStore(database)
	committer.Logger = logger
	committer.Instruments = instruments
	// The ingester runs the corpus-wide deferred relationship backfill as a
	// separate batch phase, so per-commit backfill would be duplicate work
	// (#4451, § T8).
	committer.SkipRelationshipBackfill = true

	scheduledSyncConfig, err := collector.LoadScheduledSyncConfig(getenv)
	if err != nil {
		return collector.Service{}, err
	}

	// committer doubles as the delta-baseline resolver: it reads the last
	// projected commit per scope from scope_generations so git delta syncs
	// baseline on a durable commit instead of the local working-copy HEAD
	// (epic #2340).
	nativeSelector := collector.NativeRepositorySelector{
		Config:           config,
		Logger:           logger,
		BaselineResolver: committer,
		Instruments:      instruments,
	}
	selector := collector.RepositorySelector(nativeSelector)
	handoffConfig := collector.LoadWebhookTriggerHandoffConfig("ingester", getenv)
	if !scheduledSyncConfig.Enabled && !handoffConfig.Enabled {
		return collector.Service{}, errors.New("ESHU_REPO_SCHEDULED_SYNC_ENABLED=false requires ESHU_WEBHOOK_TRIGGER_HANDOFF_ENABLED=true")
	}
	if handoffConfig.Enabled {
		webhookSelector := collector.WebhookTriggerRepositorySelector{
			Config:           config,
			Store:            postgres.NewWebhookTriggerStore(database),
			Owner:            handoffConfig.Owner,
			ClaimLimit:       handoffConfig.ClaimLimit,
			Logger:           logger,
			BaselineResolver: committer,
			Instruments:      instruments,
		}
		if scheduledSyncConfig.Enabled {
			selector = collector.PriorityRepositorySelector{Selectors: []collector.RepositorySelector{
				webhookSelector,
				nativeSelector,
			}}
		} else {
			selector = webhookSelector
		}
	}

	return collector.Service{
		Source: &collector.GitSource{
			Component: "ingester",
			Selector:  selector,
			Snapshotter: collector.NativeRepositorySnapshotter{
				SCIP:             collector.LoadSnapshotSCIPConfig(getenv),
				ParseWorkers:     config.ParseWorkers,
				DiscoveryOptions: discoveryOptions,
				Tracer:           tracer,
				Instruments:      instruments,
				Logger:           logger,
			},
			SnapshotWorkers:        config.SnapshotWorkers,
			LargeRepoThreshold:     config.LargeRepoThreshold,
			LargeRepoMaxConcurrent: config.LargeRepoMaxConcurrent,
			StreamBuffer:           config.StreamBuffer,
			Tracer:                 tracer,
			Instruments:            instruments,
			Logger:                 logger,
		},
		Committer:    committer,
		DeadLetters:  postgres.NewCollectorGenerationDeadLetterStore(database),
		PollInterval: ingesterCollectorPollInterval,
		AfterBatchDrained: ingesterDeferredRelationshipMaintenance(committer, postgres.DeferredMaintenanceBarrierConfig{
			ShardCount: config.RepoShardCount,
			ShardIndex: config.RepoShardIndex,
		}, tracer, instruments, logger),
		AfterEmptyBatchDrained: config.RepoShardCount > 1,
		Tracer:                 tracer,
		Instruments:            instruments,
		Logger:                 logger,
	}, nil
}

func buildIngesterProjectorService(
	database postgres.ExecQueryer,
	canonicalWriter projector.CanonicalWriter,
	getenv func(string) string,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
) (projector.Service, error) {
	projectorQueue := postgres.NewProjectorQueue(database, "ingester", 5*time.Minute)
	reducerWriter, err := ingesterReducerIntentWriter(database, getenv, instruments, logger)
	if err != nil {
		return projector.Service{}, err
	}
	retryInjector, err := loadIngesterRetryInjector(getenv)
	if err != nil {
		return projector.Service{}, err
	}
	retryPolicy, err := loadIngesterRetryPolicy(getenv)
	if err != nil {
		return projector.Service{}, err
	}
	projectorQueue.RetryDelay, projectorQueue.MaxAttempts = retryPolicy.RetryDelay, retryPolicy.MaxAttempts
	// Exponential backoff + jitter (#4450): without these, same-instant
	// failures reconverge on one visible_at and self-reinforce into a retry
	// storm. See runtime.RetryPolicyConfig's doc comment for the formula.
	projectorQueue.MaxRetryDelay = retryPolicy.MaxRetryDelay
	projectorQueue.JitterFraction = retryPolicy.JitterFraction
	projectorQueue.Instruments = instruments
	runner, err := buildIngesterProjectorRuntime(database, canonicalWriter, reducerWriter, retryInjector, getenv, tracer, instruments, logger)
	if err != nil {
		return projector.Service{}, err
	}

	svc := projector.Service{
		PollInterval:          time.Second,
		WorkSource:            projectorQueue,
		FactStore:             postgres.NewFactStore(database),
		Runner:                runner,
		WorkSink:              projectorQueue,
		Heartbeater:           projectorQueue,
		HeartbeatInterval:     projectorHeartbeatInterval(projectorQueue.LeaseDuration),
		Tracer:                tracer,
		Instruments:           instruments,
		Logger:                logger,
		Workers:               projectorWorkerCount(getenv),
		FactCounter:           postgres.NewFactStore(database),
		LargeGenThreshold:     largeGenThreshold(getenv),
		LargeGenMaxConcurrent: largeGenMaxConcurrent(getenv),
	}
	svc.InitLargeGenSemaphore()
	return svc, nil
}

func projectorHeartbeatInterval(leaseDuration time.Duration) time.Duration {
	if leaseDuration <= 0 {
		return time.Minute
	}
	interval := leaseDuration / 3
	if interval <= 0 {
		return time.Second
	}
	if interval > time.Minute {
		return time.Minute
	}
	return interval
}

func projectorWorkerCount(getenv func(string) string) int {
	if raw := strings.TrimSpace(getenv("ESHU_PROJECTOR_WORKERS")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			return n
		}
	}
	if strings.TrimSpace(getenv("ESHU_QUERY_PROFILE")) == "local_authoritative" &&
		strings.TrimSpace(getenv("ESHU_GRAPH_BACKEND")) == string(runtimecfg.GraphBackendNornicDB) {
		return cpubudget.UsableCPUs()
	}
	n := cpubudget.UsableCPUs()
	if n > 8 {
		n = 8
	}
	if n < 1 {
		n = 1
	}
	return n
}

func largeGenThreshold(getenv func(string) string) int {
	if raw := strings.TrimSpace(getenv("ESHU_LARGE_GEN_THRESHOLD")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			return n
		}
	}
	return 10000
}

func largeGenMaxConcurrent(getenv func(string) string) int {
	if raw := strings.TrimSpace(getenv("ESHU_LARGE_GEN_MAX_CONCURRENT")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			return n
		}
	}
	if strings.TrimSpace(getenv("ESHU_QUERY_PROFILE")) == "local_authoritative" {
		return 4
	}
	return 2
}

func buildIngesterProjectorRuntime(
	database postgres.ExecQueryer,
	canonicalWriter projector.CanonicalWriter,
	intentWriter projector.ReducerIntentWriter,
	retryInjector projector.RetryInjector,
	getenv func(string) string,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
) (projector.Runtime, error) {
	contentConfig, err := content.LoadWriterConfig(getenv)
	if err != nil {
		return projector.Runtime{}, err
	}
	contentWriter := postgres.NewContentWriter(database).
		WithLogger(logger).
		WithEntityBatchSize(contentConfig.EntityBatchSize)

	return projector.Runtime{
		CanonicalWriter:               canonicalWriter,
		ContentWriter:                 contentWriter,
		IntentWriter:                  intentWriter,
		PhasePublisher:                postgres.NewGraphProjectionPhaseStateStore(database),
		RepairQueue:                   postgres.NewGraphProjectionPhaseRepairQueueStore(database),
		RetryInjector:                 retryInjector,
		PackageRegistryIdentityLocker: packageRegistryIdentityLocker(database),
		ContentBeforeCanonical:        ingesterContentBeforeCanonical(getenv),
		Tracer:                        tracer,
		Instruments:                   instruments,
		Logger:                        logger,
	}, nil
}
