package main

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

func TestBuildProjectorRuntimeWiresPersistentStorageAdapters(t *testing.T) {
	t.Parallel()

	runtime, err := buildProjectorRuntime(postgres.SQLDB{}, &noopCanonicalWriter{}, nil, nil, func(string) string { return "" })
	if err != nil {
		t.Fatalf("buildProjectorRuntime() error = %v, want nil", err)
	}

	if runtime.CanonicalWriter == nil {
		t.Fatal("CanonicalWriter = nil, want non-nil")
	}
	if _, ok := runtime.ContentWriter.(postgres.ContentWriter); !ok {
		t.Fatalf("ContentWriter type = %T, want %T", runtime.ContentWriter, postgres.ContentWriter{})
	}
}

func TestBuildProjectorServiceWiresRetryInjectorFromEnv(t *testing.T) {
	t.Parallel()

	service, err := buildProjectorService(
		postgres.SQLDB{},
		&noopCanonicalWriter{},
		func(name string) string {
			if name == projectorRetryOnceScopeGenerationEnv {
				return "scope-123:generation-456"
			}
			return ""
		},
	)
	if err != nil {
		t.Fatalf("buildProjectorService() error = %v, want nil", err)
	}

	runtime, ok := service.Runner.(projector.Runtime)
	if !ok {
		t.Fatalf("Runner type = %T, want projector.Runtime", service.Runner)
	}
	if runtime.RetryInjector == nil {
		t.Fatal("RetryInjector = nil, want configured injector")
	}
}

func TestBuildProjectorServiceWiresRetryPolicyFromEnv(t *testing.T) {
	t.Parallel()

	service, err := buildProjectorService(
		postgres.SQLDB{},
		&noopCanonicalWriter{},
		func(name string) string {
			switch name {
			case "ESHU_PROJECTOR_MAX_ATTEMPTS":
				return "5"
			case "ESHU_PROJECTOR_RETRY_DELAY":
				return "45s"
			default:
				return ""
			}
		},
	)
	if err != nil {
		t.Fatalf("buildProjectorService() error = %v, want nil", err)
	}

	workSource, ok := service.WorkSource.(postgres.ProjectorQueue)
	if !ok {
		t.Fatalf("WorkSource type = %T, want postgres.ProjectorQueue", service.WorkSource)
	}
	if got, want := workSource.MaxAttempts, 5; got != want {
		t.Fatalf("MaxAttempts = %d, want %d", got, want)
	}
	if got, want := workSource.RetryDelay, 45*time.Second; got != want {
		t.Fatalf("RetryDelay = %v, want %v", got, want)
	}
}

func TestCloseProjectorNeo4jDriverAllowsNilDriver(t *testing.T) {
	t.Parallel()

	if err := closeProjectorNeo4jDriver(nil); err != nil {
		t.Fatalf("closeProjectorNeo4jDriver(nil) error = %v, want nil", err)
	}
}

func TestNeo4jBatchSizeReturnsZeroWhenEmpty(t *testing.T) {
	t.Parallel()

	getenv := func(string) string { return "" }
	if got := neo4jBatchSize(getenv); got != 0 {
		t.Fatalf("neo4jBatchSize() = %d, want 0", got)
	}
}

func TestNeo4jBatchSizeParsesValidInteger(t *testing.T) {
	t.Parallel()

	getenv := func(string) string { return "500" }
	if got, want := neo4jBatchSize(getenv), 500; got != want {
		t.Fatalf("neo4jBatchSize() = %d, want %d", got, want)
	}
}

func TestNeo4jBatchSizeReturnsZeroForInvalidInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value string
	}{
		{"negative", "-10"},
		{"zero", "0"},
		{"non-numeric", "invalid"},
		{"whitespace", "   "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			getenv := func(string) string { return tt.value }
			if got := neo4jBatchSize(getenv); got != 0 {
				t.Fatalf("neo4jBatchSize(%q) = %d, want 0", tt.value, got)
			}
		})
	}
}

// noopCanonicalWriter is a no-op canonical writer for tests.
type noopCanonicalWriter struct{}

func (*noopCanonicalWriter) Write(_ context.Context, _ projector.CanonicalMaterialization) error {
	return nil
}
