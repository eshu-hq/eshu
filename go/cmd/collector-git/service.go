package main

import (
	"log/slog"
	"strconv"
	"strings"
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

	selector := collector.RepositorySelector(collector.NativeRepositorySelector{Config: config})
	if webhookTriggerHandoffEnabled(getenv) {
		selector = collector.PriorityRepositorySelector{Selectors: []collector.RepositorySelector{
			collector.WebhookTriggerRepositorySelector{
				Config:     config,
				Store:      postgres.NewWebhookTriggerStore(database),
				Owner:      webhookTriggerHandoffOwner("collector-git", getenv),
				ClaimLimit: webhookTriggerHandoffLimit(getenv),
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

func webhookTriggerHandoffEnabled(getenv func(string) string) bool {
	value := strings.TrimSpace(strings.ToLower(getenv("ESHU_WEBHOOK_TRIGGER_HANDOFF_ENABLED")))
	return value == "1" || value == "true" || value == "yes" || value == "on"
}

func webhookTriggerHandoffOwner(defaultOwner string, getenv func(string) string) string {
	if owner := strings.TrimSpace(getenv("ESHU_WEBHOOK_TRIGGER_HANDOFF_OWNER")); owner != "" {
		return owner
	}
	return defaultOwner
}

func webhookTriggerHandoffLimit(getenv func(string) string) int {
	raw := strings.TrimSpace(getenv("ESHU_WEBHOOK_TRIGGER_CLAIM_LIMIT"))
	if raw == "" {
		return 0
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit <= 0 {
		return 0
	}
	return limit
}
