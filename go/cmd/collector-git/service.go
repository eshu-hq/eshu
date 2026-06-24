// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"log/slog"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"go.opentelemetry.io/otel/trace"
)

const defaultCollectorPollInterval = time.Second

func buildCollectorService(
	database postgres.SQLDB,
	getenv func(string) string,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
) (collector.Service, error) {
	config, err := collector.LoadRepoSyncConfig("collector-git", getenv)
	if err != nil {
		return collector.Service{}, err
	}
	discoveryOptions, err := collector.LoadDiscoveryOptionsFromEnv(getenv)
	if err != nil {
		return collector.Service{}, err
	}

	committer := postgres.NewIngestionStore(database)
	committer.Logger = logger

	// committer doubles as the delta-baseline resolver so git delta syncs
	// baseline on the last projected commit per scope rather than local HEAD
	// (epic #2340).
	selector := collector.RepositorySelector(collector.NativeRepositorySelector{
		Config:           config,
		Logger:           logger,
		BaselineResolver: committer,
		Instruments:      instruments,
	})
	handoffConfig := collector.LoadWebhookTriggerHandoffConfig("collector-git", getenv)
	if handoffConfig.Enabled {
		selector = collector.PriorityRepositorySelector{Selectors: []collector.RepositorySelector{
			collector.WebhookTriggerRepositorySelector{
				Config:           config,
				Store:            postgres.NewWebhookTriggerStore(database),
				Owner:            handoffConfig.Owner,
				ClaimLimit:       handoffConfig.ClaimLimit,
				Logger:           logger,
				BaselineResolver: committer,
				Instruments:      instruments,
			},
			selector,
		}}
	}

	return collector.Service{
		Source: &collector.GitSource{
			Component: "collector-git",
			Selector:  selector,
			Snapshotter: collector.NativeRepositorySnapshotter{
				SCIP:             collector.LoadSnapshotSCIPConfig(getenv),
				ParseWorkers:     config.ParseWorkers,
				DiscoveryOptions: discoveryOptions,
				EmitDataflow:     collector.LoadEmitDataflowGate(getenv),
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
		PollInterval: defaultCollectorPollInterval,
		Tracer:       tracer,
		Instruments:  instruments,
		Logger:       logger,
	}, nil
}
