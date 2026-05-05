package main

import (
	"log/slog"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	noopmetric "go.opentelemetry.io/otel/metric/noop"
	nooptrace "go.opentelemetry.io/otel/trace/noop"
)

func TestBuildCollectorServiceUsesIngestionStoreBoundary(t *testing.T) {
	t.Parallel()

	service, err := buildCollectorService(
		postgres.SQLDB{},
		func(string) string { return "" },
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("buildCollectorService() error = %v, want nil", err)
	}
	if service.Source == nil {
		t.Fatal("buildCollectorService() source = nil, want non-nil")
	}
	if _, ok := service.Source.(*collector.GitSource); !ok {
		t.Fatalf(
			"buildCollectorService() source type = %T, want *collector.GitSource",
			service.Source,
		)
	}
	source := service.Source.(*collector.GitSource)
	if _, ok := source.Selector.(collector.NativeRepositorySelector); !ok {
		t.Fatalf("buildCollectorService() selector type = %T, want collector.NativeRepositorySelector", source.Selector)
	}
	if _, ok := source.Snapshotter.(collector.NativeRepositorySnapshotter); !ok {
		t.Fatalf("buildCollectorService() snapshotter type = %T, want collector.NativeRepositorySnapshotter", source.Snapshotter)
	}
	snapshotter := source.Snapshotter.(collector.NativeRepositorySnapshotter)
	if snapshotter.SCIP.Enabled {
		t.Fatal("buildCollectorService() SCIP enabled by default, want false")
	}
	if service.PollInterval <= 0 {
		t.Fatalf(
			"buildCollectorService() poll interval = %v, want positive",
			service.PollInterval,
		)
	}
	if _, ok := service.Committer.(postgres.IngestionStore); !ok {
		t.Fatalf(
			"buildCollectorService() committer type = %T, want postgres.IngestionStore",
			service.Committer,
		)
	}
}

func TestBuildCollectorServiceDoesNotRequireBridgeRepoRoot(t *testing.T) {
	t.Parallel()

	service, err := buildCollectorService(
		postgres.SQLDB{},
		func(string) string { return "" },
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("buildCollectorService() error = %v, want nil", err)
	}
	source, ok := service.Source.(*collector.GitSource)
	if !ok {
		t.Fatalf("buildCollectorService() source type = %T, want *collector.GitSource", service.Source)
	}
	if _, ok := source.Selector.(collector.NativeRepositorySelector); !ok {
		t.Fatalf("buildCollectorService() selector type = %T, want collector.NativeRepositorySelector", source.Selector)
	}
}

func TestBuildCollectorServiceWiresSCIPEnvironment(t *testing.T) {
	t.Parallel()

	service, err := buildCollectorService(
		postgres.SQLDB{},
		func(key string) string {
			switch key {
			case "SCIP_INDEXER":
				return "true"
			case "SCIP_LANGUAGES":
				return "python,go"
			default:
				return ""
			}
		},
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("buildCollectorService() error = %v, want nil", err)
	}

	source := service.Source.(*collector.GitSource)
	snapshotter := source.Snapshotter.(collector.NativeRepositorySnapshotter)
	if !snapshotter.SCIP.Enabled {
		t.Fatal("SCIP enabled = false, want true")
	}
	if got, want := snapshotter.SCIP.Languages, []string{"python", "go"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("SCIP languages = %#v, want %#v", got, want)
	}
}

func TestBuildCollectorServiceWiresDiscoveryPathGlobOverlay(t *testing.T) {
	t.Parallel()

	service, err := buildCollectorService(
		postgres.SQLDB{},
		func(key string) string {
			if key == "ESHU_DISCOVERY_IGNORED_PATH_GLOBS" {
				return "generated/**=generated-template"
			}
			return ""
		},
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("buildCollectorService() error = %v, want nil", err)
	}

	source := service.Source.(*collector.GitSource)
	snapshotter := source.Snapshotter.(collector.NativeRepositorySnapshotter)
	if got, want := len(snapshotter.DiscoveryOptions.IgnoredPathGlobs), 1; got != want {
		t.Fatalf("IgnoredPathGlobs length = %d, want %d", got, want)
	}
}

func TestBuildCollectorServiceWiresTelemetryIntoSourceAndService(t *testing.T) {
	t.Parallel()

	meter := noopmetric.NewMeterProvider().Meter("test")
	instruments, err := telemetry.NewInstruments(meter)
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}
	tracer := nooptrace.NewTracerProvider().Tracer("test")
	logger := slog.New(slog.NewJSONHandler(&discardWriter{}, nil))

	service, err := buildCollectorService(
		postgres.SQLDB{},
		func(string) string { return "" },
		tracer,
		instruments,
		logger,
	)
	if err != nil {
		t.Fatalf("buildCollectorService() error = %v, want nil", err)
	}

	source, ok := service.Source.(*collector.GitSource)
	if !ok {
		t.Fatalf("buildCollectorService() source type = %T, want *collector.GitSource", service.Source)
	}
	if source.Tracer == nil || service.Tracer == nil {
		t.Fatal("collector tracer wiring = nil, want non-nil")
	}
	if source.Instruments == nil || service.Instruments == nil {
		t.Fatal("collector instruments wiring = nil, want non-nil")
	}
	if source.Logger == nil || service.Logger == nil {
		t.Fatal("collector logger wiring = nil, want non-nil")
	}
}

type discardWriter struct{}

func (*discardWriter) Write(p []byte) (int, error) {
	return len(p), nil
}
