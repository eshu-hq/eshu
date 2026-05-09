package main

import (
	"log/slog"
	"testing"
	"time"

	"go.opentelemetry.io/otel/trace/noop"

	confluencecollector "github.com/eshu-hq/eshu/go/internal/collector/confluence"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

func TestBuildCollectorServiceWiresConfluenceSource(t *testing.T) {
	t.Parallel()

	service, err := buildCollectorService(
		postgres.SQLDB{},
		func(key string) string {
			values := map[string]string{
				"ESHU_CONFLUENCE_BASE_URL":      "https://example.atlassian.net/wiki",
				"ESHU_CONFLUENCE_SPACE_ID":      "100",
				"ESHU_CONFLUENCE_API_TOKEN":     "token",
				"ESHU_CONFLUENCE_EMAIL":         "bot@example.com",
				"ESHU_CONFLUENCE_POLL_INTERVAL": "20m",
			}
			return values[key]
		},
		noop.NewTracerProvider().Tracer("test"),
		nil,
		slog.Default(),
	)
	if err != nil {
		t.Fatalf("buildCollectorService() error = %v, want nil", err)
	}
	if got, want := service.PollInterval, 20*time.Minute; got != want {
		t.Fatalf("PollInterval = %v, want %v", got, want)
	}
	if _, ok := service.Source.(*confluencecollector.Source); !ok {
		t.Fatalf("Source type = %T, want *confluence.Source", service.Source)
	}
}
