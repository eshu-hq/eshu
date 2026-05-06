package main

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

func TestBuildIngesterServiceRejectsMissingBridgeRepoRoot(t *testing.T) {
	t.Parallel()

	runner, err := buildIngesterService(
		postgres.SQLDB{},
		&noopCanonicalWriter{},
		func(string) string { return "" },
		func() (string, error) { return "/tmp/does-not-exist", nil },
		func() []string { return nil },
		nil, // tracer
		nil, // instruments
		nil, // logger
	)
	if err != nil {
		t.Fatalf("buildIngesterService() error = %v, want nil", err)
	}
	if got, want := len(runner.runners), 2; got != want {
		t.Fatalf("len(buildIngesterService().runners) = %d, want %d", got, want)
	}
}

// noopCanonicalWriter satisfies projector.CanonicalWriter for tests that
// don't exercise Neo4j.
type noopCanonicalWriter struct{}

func (*noopCanonicalWriter) Write(_ context.Context, _ projector.CanonicalMaterialization) error {
	return nil
}
