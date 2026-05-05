package main

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/backendconformance"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
)

func TestReducerNeo4jExecutorRunsBackendConformanceWriteCorpus(t *testing.T) {
	t.Parallel()

	executor := reducerNeo4jExecutor{session: &fakeNeo4jSession{}}
	report, err := backendconformance.RunWriteCorpus(context.Background(), executor, backendconformance.DefaultWriteCorpus())
	if err != nil {
		t.Fatalf("RunWriteCorpus() error = %v", err)
	}
	if got, want := len(report.Results), len(backendconformance.DefaultWriteCorpus()); got != want {
		t.Fatalf("result count = %d, want %d", got, want)
	}
}

func TestNornicDBGroupedConformanceExecutorRunsBackendConformanceWriteCorpus(t *testing.T) {
	t.Parallel()

	inner := &groupCapableReducerExecutor{}
	executor := semanticEntityExecutorForGraphBackend(inner, runtimecfg.GraphBackendNornicDB, 0, true)
	report, err := backendconformance.RunWriteCorpus(context.Background(), executor, backendconformance.DefaultWriteCorpus())
	if err != nil {
		t.Fatalf("RunWriteCorpus() error = %v", err)
	}
	if got, want := len(report.Results), len(backendconformance.DefaultWriteCorpus()); got != want {
		t.Fatalf("result count = %d, want %d", got, want)
	}
	if inner.groupCalls == 0 {
		t.Fatal("NornicDB conformance executor did not exercise grouped writes")
	}
}
