// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_answer_truth

// Live proof of the reducer's id-anchor census wiring on a real Neo4j (#7212):
// the production startIDAnchorCensus path, with the raw session runner the
// reducer wires and a real OpenTelemetry SDK reader, takes its startup pass
// against a seeded graph and must record the gauge, the last-success time, the
// pass counter, and the `id anchor census` log line. Run it with, for example:
//
//	docker compose -p eshu-7212-live -f docker-compose.live-backend-neo4j.yml up -d --wait
//	cd go && ESHU_NEO4J_URI=bolt://127.0.0.1:7687 ESHU_LIVE_GRAPH_BACKEND=neo4j \
//	  ESHU_LIVE_GRAPH_DATABASE=neo4j go test ./cmd/reducer \
//	  -tags live_nornicdb_answer_truth -run TestLiveIDAnchorCensusStartup -count=1 -v
package main

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

const censusLiveStartupPrefix = "anchor-census-startup-live:"

func TestLiveIDAnchorCensusStartup(t *testing.T) {
	uri := strings.TrimSpace(os.Getenv("ESHU_NEO4J_URI"))
	if uri == "" {
		t.Fatal("ESHU_NEO4J_URI is required")
	}
	if backend := strings.ToLower(strings.TrimSpace(os.Getenv("ESHU_LIVE_GRAPH_BACKEND"))); backend != "neo4j" {
		t.Skip("the id-anchor census runs on Neo4j only (#7212)")
	}
	database := strings.TrimSpace(os.Getenv("ESHU_LIVE_GRAPH_DATABASE"))
	if database == "" {
		database = "neo4j"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	driver, err := neo4jdriver.NewDriverWithContext(uri, neo4jdriver.NoAuth())
	if err != nil {
		t.Fatalf("open driver: %v", err)
	}
	defer func() { _ = driver.Close(context.Background()) }()
	write := func(cypher string) {
		t.Helper()
		if _, err := neo4jdriver.ExecuteQuery(ctx, driver, cypher, nil,
			neo4jdriver.EagerResultTransformer, neo4jdriver.ExecuteQueryWithDatabase(database)); err != nil {
			t.Fatalf("write %q: %v", cypher, err)
		}
	}
	cleanup := func() {
		write(`MATCH (n) WHERE n.id STARTS WITH '` + censusLiveStartupPrefix + `' DETACH DELETE n`)
	}
	cleanup()
	defer cleanup()

	// The reducer wires the raw session runner as its census read port.
	runner := neo4jSessionRunner{Driver: driver, DatabaseName: database}
	census := func() (gauge int64, lastSuccess int64, okPasses int64, logs string) {
		reader := sdkmetric.NewManualReader()
		provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
		inst, err := telemetry.NewInstruments(provider.Meter("live"))
		if err != nil {
			t.Fatalf("NewInstruments: %v", err)
		}
		buf := &bytes.Buffer{}
		logger := slog.New(slog.NewTextHandler(buf, nil))
		runCtx, stop := context.WithCancel(ctx)
		cfg := idAnchorCensusConfig{Enabled: true, PollInterval: time.Hour, Timeout: time.Minute}
		wait := startIDAnchorCensus(runCtx, cfg, runtimecfg.GraphBackendNeo4j, idAnchorCensusReader(runner, nil), inst, logger)
		deadline := time.After(time.Minute)
		for {
			if v := readCensusMetrics(t, reader); v.okPasses > 0 {
				gauge, lastSuccess, okPasses = v.gauge, v.lastSuccess, v.okPasses
				break
			}
			select {
			case <-deadline:
				t.Fatal("the startup census pass did not finish")
			case <-time.After(50 * time.Millisecond):
			}
		}
		stop()
		wait()
		return gauge, lastSuccess, okPasses, buf.String()
	}

	baseGauge, baseLast, basePasses, baseLogs := census()
	t.Logf("startup pass, graph before seed: gauge=%d last_success_unixtime=%d ok_passes=%d", baseGauge, baseLast, basePasses)
	t.Logf("startup log line: %s", strings.TrimSpace(baseLogs))

	write(`CREATE (:Function {id: '` + censusLiveStartupPrefix + `fn', uid: '` + censusLiveStartupPrefix + `fn'})`)
	write(`CREATE (:Repository {id: '` + censusLiveStartupPrefix + `repo'})`)
	cleanGauge, _, _, _ := census()
	if cleanGauge != baseGauge {
		t.Fatalf("gauge moved from %d to %d on canonical seeds", baseGauge, cleanGauge)
	}

	write(`CREATE (:Unconstrained {id: '` + censusLiveStartupPrefix + `planted'})`)
	plantedGauge, plantedLast, _, plantedLogs := census()
	t.Logf("startup pass, one id-only node planted: gauge=%d last_success_unixtime=%d", plantedGauge, plantedLast)
	t.Logf("startup log line: %s", strings.TrimSpace(plantedLogs))
	if plantedGauge != baseGauge+1 {
		t.Fatalf("gauge = %d after planting one unreachable node, want %d", plantedGauge, baseGauge+1)
	}
	for _, want := range []string{"id anchor census", "snapshot=true", "first_pass=true", "unreachable_nodes=", "level=WARN"} {
		if !strings.Contains(plantedLogs, want) {
			t.Errorf("planted startup line lacks %q:\n%s", want, plantedLogs)
		}
	}
	if baseLast <= 0 || plantedLast <= 0 {
		t.Errorf("last-success gauge not recorded: %d, %d", baseLast, plantedLast)
	}
}

type censusMetricValues struct{ gauge, lastSuccess, okPasses int64 }

func readCensusMetrics(t *testing.T, reader *sdkmetric.ManualReader) censusMetricValues {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	var out censusMetricValues
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			switch m.Name {
			case "eshu_dp_graph_id_anchor_unreachable_nodes":
				out.gauge = m.Data.(metricdata.Gauge[int64]).DataPoints[0].Value
			case "eshu_dp_graph_id_anchor_census_last_success_unixtime":
				out.lastSuccess = m.Data.(metricdata.Gauge[int64]).DataPoints[0].Value
			case "eshu_dp_graph_id_anchor_census_passes_total":
				for _, p := range m.Data.(metricdata.Sum[int64]).DataPoints {
					for _, a := range p.Attributes.ToSlice() {
						if string(a.Key) == telemetry.MetricDimensionOutcome && a.Value.AsString() == "ok" {
							out.okPasses += p.Value
						}
					}
				}
			}
		}
	}
	return out
}
