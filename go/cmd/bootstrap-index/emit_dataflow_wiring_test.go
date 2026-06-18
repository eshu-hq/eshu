package main

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector"
)

func TestBuildBootstrapCollectorWiresEmitDataflowGate(t *testing.T) {
	t.Parallel()

	deps, err := buildBootstrapCollector(
		context.Background(),
		&fakeBootstrapSQLDB{},
		func(key string) string {
			if key == "ESHU_EMIT_DATAFLOW" {
				return "true"
			}
			return ""
		},
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("buildBootstrapCollector() error = %v, want nil", err)
	}

	source := deps.source.(*collector.GitSource)
	snapshotter := source.Snapshotter.(collector.NativeRepositorySnapshotter)
	if !snapshotter.EmitDataflow {
		t.Fatal("EmitDataflow = false, want true when ESHU_EMIT_DATAFLOW=true")
	}
}
