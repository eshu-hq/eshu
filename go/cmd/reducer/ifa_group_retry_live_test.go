// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build ifafaultinjection

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/replay/faultreplay"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const ifaGroupRetryLiveEnv = "ESHU_REPO_DEPENDENCY_CONCURRENCY_PROVE_LIVE"

func TestReducerGroupedRetrySeamLiveNornicDB(t *testing.T) {
	if strings.TrimSpace(os.Getenv(ifaGroupRetryLiveEnv)) != "1" {
		t.Skipf("set %s=1 and real NornicDB connection variables", ifaGroupRetryLiveEnv)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	driver, cfg, err := runtimecfg.OpenNeo4jDriver(ctx, os.Getenv)
	if err != nil {
		t.Fatalf("open Bolt driver: %v", err)
	}
	t.Cleanup(func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer closeCancel()
		_ = driver.Close(closeCtx)
	})

	runner := neo4jSessionRunner{
		Driver:       driver,
		DatabaseName: cfg.DatabaseName,
		TxTimeout:    30 * time.Second,
	}
	marker := fmt.Sprintf("ifa-group-retry-live-%d-%d", os.Getpid(), time.Now().UnixNano())
	cleanupIfaGroupRetryProbe(ctx, t, runner, marker)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		cleanupIfaGroupRetryProbe(cleanupCtx, t, runner, marker)
	})

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(provider.Meter("ifa-group-retry-live"))
	if err != nil {
		t.Fatalf("new instruments: %v", err)
	}
	instrumented := &sourcecypher.InstrumentedExecutor{
		Inner: newReducerNeo4jExecutor(runner, instruments),
	}
	ordinal := 1
	script := faultreplay.Script{
		Version: faultreplay.CurrentVersion,
		Faults: []faultreplay.FaultOp{{
			Kind:    faultreplay.KindFailGraphWriteOnceThenSucceed,
			Trigger: faultreplay.Trigger{StatementOrdinal: &ordinal},
			Target:  faultreplay.Target{Lane: faultreplay.LaneExecutorRetry},
		}},
	}
	raw, err := json.Marshal(script)
	if err != nil {
		t.Fatalf("marshal fault script: %v", err)
	}
	path := filepath.Join(t.TempDir(), "group-retry-live.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write fault script: %v", err)
	}
	wrapped, err := wrapIfaFaultExecutor(
		instrumented,
		getenvMap(map[string]string{ifaFaultScriptEnv: path}),
		nil,
	)
	if err != nil {
		t.Fatalf("wrap fault executor: %v", err)
	}
	faulting, ok := wrapped.(*sourcecypher.FaultingExecutor)
	if !ok {
		t.Fatalf("wrapped executor type = %T, want *cypher.FaultingExecutor", wrapped)
	}
	grouped, ok := wrapped.(sourcecypher.GroupExecutor)
	if !ok {
		t.Fatalf("wrapped executor type = %T, want cypher.GroupExecutor", wrapped)
	}

	statements := make([]sourcecypher.Statement, 0, 2)
	for i, id := range []string{"probe-a", "probe-b"} {
		statements = append(statements, sourcecypher.Statement{
			Operation: sourcecypher.OperationCanonicalUpsert,
			Cypher: `MERGE (probe:IfaGroupRetryProbe {id: $id})
				SET probe.marker = $marker, probe.ordinal = $ordinal`,
			Parameters: map[string]any{"id": id, "marker": marker, "ordinal": i + 1},
		})
	}
	if err := grouped.ExecuteGroup(ctx, statements); err != nil {
		t.Fatalf("execute grouped retry probe: %v", err)
	}
	if !faulting.OnceThenSucceedFired() {
		t.Fatal("executor-retry fault did not fire")
	}

	var metrics metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &metrics); err != nil {
		t.Fatalf("collect retry metric: %v", err)
	}
	if got, want := deadlockRetryCounterValue(t, metrics), int64(1); got != want {
		t.Fatalf("group retry counter = %d, want %d", got, want)
	}
	if got, want := countIfaGroupRetryProbes(ctx, t, driver, cfg.DatabaseName, marker), int64(2); got != want {
		t.Fatalf("persisted group probes = %d, want %d", got, want)
	}
}

func cleanupIfaGroupRetryProbe(
	ctx context.Context,
	t *testing.T,
	runner neo4jSessionRunner,
	marker string,
) {
	t.Helper()
	if err := runner.RunCypher(
		ctx,
		`MATCH (probe:IfaGroupRetryProbe {marker: $marker}) DETACH DELETE probe`,
		map[string]any{"marker": marker},
	); err != nil {
		t.Fatalf("cleanup grouped retry probes: %v", err)
	}
}

func countIfaGroupRetryProbes(
	ctx context.Context,
	t *testing.T,
	driver neo4jdriver.DriverWithContext,
	database string,
	marker string,
) int64 {
	t.Helper()
	session := driver.NewSession(ctx, neo4jdriver.SessionConfig{
		AccessMode:   neo4jdriver.AccessModeRead,
		DatabaseName: database,
	})
	defer func() { _ = session.Close(ctx) }()
	result, err := session.Run(
		ctx,
		`MATCH (probe:IfaGroupRetryProbe {marker: $marker}) RETURN count(probe)`,
		map[string]any{"marker": marker},
	)
	if err != nil {
		t.Fatalf("query grouped retry probes: %v", err)
	}
	if !result.Next(ctx) {
		t.Fatalf("query grouped retry probes returned no row: %v", result.Err())
	}
	count, ok := result.Record().Values[0].(int64)
	if !ok {
		t.Fatalf("grouped retry probe count type = %T, want int64", result.Record().Values[0])
	}
	if _, err := result.Consume(ctx); err != nil {
		t.Fatalf("consume grouped retry probe count: %v", err)
	}
	return count
}
