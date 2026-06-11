package main

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/azurecloud"
	"github.com/eshu-hq/eshu/go/internal/collector/azurecloud/azureruntime"
	"github.com/eshu-hq/eshu/go/internal/redact"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// redactionKeyFileEnv names the env var holding the path to the read-only
// redaction key material used to fingerprint azure_tag_observation values. When
// unset, the collector runs with a zero key and emits no tag observation facts,
// so tag values are never fingerprinted or carried without an operator key.
const redactionKeyFileEnv = "ESHU_AZURE_REDACTION_KEY_FILE"

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
	redactionKey, err := loadRedactionKey(getenv(redactionKeyFileEnv))
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
			RedactionKey:    redactionKey,
			Tracer:          tracer,
			Logger:          logger,
		},
		Committer:    committer,
		PollInterval: config.PollInterval,
		Tracer:       tracer,
		Logger:       logger,
	}, nil
}

// loadRedactionKey reads the read-only redaction key material from the file at
// path. An empty path returns a zero key, which disables azure_tag_observation
// emission (tag values are never fingerprinted or carried without a key). A
// configured-but-unreadable or blank key file is a hard error so the collector
// never silently runs keyless when a key was intended. The material is never
// logged.
func loadRedactionKey(path string) (redact.Key, error) {
	if strings.TrimSpace(path) == "" {
		return redact.Key{}, nil
	}
	material, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return redact.Key{}, fmt.Errorf("read azure redaction key file: %w", err)
	}
	key, err := redact.NewKey(material)
	if err != nil {
		return redact.Key{}, fmt.Errorf("azure redaction key: %w", err)
	}
	return key, nil
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
