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

	return collector.Service{
		Source: &collector.GitSource{
			Component: "collector-git",
			Selector:  collector.NativeRepositorySelector{Config: config},
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
		PollInterval: defaultCollectorPollInterval,
		Tracer:       tracer,
		Instruments:  instruments,
		Logger:       logger,
	}, nil
}
