package main

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"go.opentelemetry.io/otel/trace/noop"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/awsruntime"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestLoadFixtureConfigParsesEstate proves the declarative fixture document
// parses into a runtime config with the resolved poll interval and bounded
// scopes, with no AWS credentials.
func TestLoadFixtureConfigParsesEstate(t *testing.T) {
	t.Parallel()

	cfg, pollInterval, err := loadFixtureConfig("testdata/fixture-estate.json")
	if err != nil {
		t.Fatalf("loadFixtureConfig() error = %v, want nil", err)
	}
	if cfg.CollectorInstanceID != "aws-fixture-instance" {
		t.Fatalf("collector instance id = %q, want aws-fixture-instance", cfg.CollectorInstanceID)
	}
	if got, want := pollInterval, 30*time.Minute; got != want {
		t.Fatalf("poll interval = %v, want %v", got, want)
	}
	if len(cfg.Scopes) != 1 {
		t.Fatalf("scope count = %d, want 1", len(cfg.Scopes))
	}
	scope := cfg.Scopes[0]
	if len(scope.Resources) != 2 {
		t.Fatalf("resource count = %d, want 2", len(scope.Resources))
	}
	if len(scope.Relationships) != 1 {
		t.Fatalf("relationship count = %d, want 1", len(scope.Relationships))
	}
}

// TestLoadFixtureConfigRejectsEmpty proves an empty config document is rejected
// rather than silently producing a no-op fixture source.
func TestLoadFixtureConfigRejectsEmpty(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := dir + "/empty.json"
	if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
		t.Fatalf("write empty config: %v", err)
	}
	if _, _, err := loadFixtureConfig(path); err == nil {
		t.Fatalf("loadFixtureConfig() error = nil, want validation error for empty config")
	}
}

// TestBuildCollectorServiceWiresFixtureSource proves the binary constructs a
// fixture-backed awsruntime.FixtureSource from the declarative config and that
// the source replays the expected offline facts with no credentials.
func TestBuildCollectorServiceWiresFixtureSource(t *testing.T) {
	t.Parallel()

	service, err := buildCollectorService(
		postgres.SQLDB{},
		"testdata/fixture-estate.json",
		noop.NewTracerProvider().Tracer("test"),
		(*telemetry.Instruments)(nil),
		slog.Default(),
	)
	if err != nil {
		t.Fatalf("buildCollectorService() error = %v, want nil", err)
	}
	if got, want := service.PollInterval, 30*time.Minute; got != want {
		t.Fatalf("poll interval = %v, want %v", got, want)
	}
	source, ok := service.Source.(*awsruntime.FixtureSource)
	if !ok {
		t.Fatalf("source type = %T, want *awsruntime.FixtureSource", service.Source)
	}

	collected, ok, err := source.Next(context.Background())
	if err != nil || !ok {
		t.Fatalf("source.Next() ok=%v err=%v", ok, err)
	}
	resources := 0
	relationships := 0
	for env := range collected.Facts {
		switch env.FactKind {
		case facts.AWSResourceFactKind:
			resources++
		case facts.AWSRelationshipFactKind:
			relationships++
		}
	}
	if resources != 2 {
		t.Fatalf("resource fact count = %d, want 2", resources)
	}
	if relationships != 1 {
		t.Fatalf("relationship fact count = %d, want 1", relationships)
	}
}
