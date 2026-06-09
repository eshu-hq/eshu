package main

import (
	"log/slog"

	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/azurecloud"
	"github.com/eshu-hq/eshu/go/internal/collector/azurecloud/azureruntime"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// buildCollectorService constructs the non-claimed Azure cloud collector
// service from declarative environment configuration. It selects the
// file-backed offline page provider when ESHU_AZURE_FIXTURE_PAGES_JSON is set,
// and otherwise selects the gated live seam so production wiring never issues a
// live Azure call by default.
func buildCollectorService(
	database postgres.ExecQueryer,
	getenv func(string) string,
	tracer trace.Tracer,
	meter metric.Meter,
	logger *slog.Logger,
) (collector.Service, error) {
	config, err := loadRuntimeConfig(getenv)
	if err != nil {
		return collector.Service{}, err
	}
	factory, err := buildProviderFactory(getenv)
	if err != nil {
		return collector.Service{}, err
	}
	metrics, err := azurecloud.NewMetrics(meter)
	if err != nil {
		return collector.Service{}, err
	}
	committer := postgres.NewIngestionStore(database)
	committer.Logger = logger
	return collector.Service{
		Source: &azureruntime.Source{
			Config:          config,
			ProviderFactory: factory,
			Metrics:         metrics,
			Tracer:          tracer,
			Logger:          logger,
		},
		Committer:    committer,
		PollInterval: config.PollInterval,
		Tracer:       tracer,
		Logger:       logger,
	}, nil
}

// buildProviderFactory selects the page provider seam. The file-backed offline
// provider is for local proof and smoke tests only; the default is the gated
// live seam, which is inert until a real read-only adapter is injected.
func buildProviderFactory(getenv func(string) string) (azureruntime.PageProviderFactory, error) {
	fixture, ok, err := loadFixturePagesConfig(getenv)
	if err != nil {
		return nil, err
	}
	if !ok {
		return azureruntime.LiveProviderFactory{}, nil
	}
	provider, err := azureruntime.NewFixturePageProviderFromFiles(
		azurecloud.ScopeAccess{
			Partial:             fixture.Partial,
			HiddenResourceCount: fixture.HiddenResourceCount,
			Reason:              fixture.Reason,
			Message:             fixture.Message,
		},
		fixture.PagePaths...,
	)
	if err != nil {
		return nil, err
	}
	return azureruntime.StaticFixtureFactory(provider), nil
}
