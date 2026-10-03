// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"regexp"
	"testing"
	"time"

	runtimepostgres "github.com/eshu-hq/eshu/go/internal/runtime/postgres"
	"github.com/jackc/pgx/v5"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

var memberLossContainerName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]*$`)

// requireOwnedMemberLossFixture checks the direct hosts against three running,
// task-labeled Docker containers before this test creates a database or kills
// any backend. An opt-in string and DSNs alone do not establish ownership.
func requireOwnedMemberLossFixture(t *testing.T, dsns []string) {
	t.Helper()
	names := []string{
		os.Getenv("ESHU_READER_TEST_PRIMARY_CONTAINER"),
		os.Getenv("ESHU_READER_TEST_FIRST_READER_CONTAINER"),
		os.Getenv("ESHU_READER_TEST_SECOND_READER_CONTAINER"),
	}
	for _, name := range names {
		if name == "" {
			t.Skip("owned physical reader container names not configured")
		}
	}
	if len(dsns) != len(names) {
		t.Fatal("fixture DSN/container count mismatch")
	}
	seenNames := map[string]bool{}
	seenHosts := map[string]bool{}
	for index, name := range names {
		if !memberLossContainerName.MatchString(name) || seenNames[name] {
			t.Fatal("invalid or duplicate owned fixture container name")
		}
		seenNames[name] = true
		cfg, err := pgx.ParseConfig(dsns[index])
		if err != nil || net.ParseIP(cfg.Host) == nil || seenHosts[cfg.Host] {
			t.Fatal("fixture DSNs must name distinct direct IP addresses")
		}
		seenHosts[cfg.Host] = true
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		output, err := exec.CommandContext(ctx, "docker", "inspect", "--type", "container", "--format", "{{json .}}", name).Output()
		cancel()
		if err != nil {
			t.Fatalf("inspect owned fixture container: %v", err)
		}
		var container struct {
			Config          struct{ Labels map[string]string }
			State           struct{ Running bool }
			NetworkSettings struct {
				Networks map[string]struct{ IPAddress string }
			}
		}
		if err := json.Unmarshal(output, &container); err != nil {
			t.Fatalf("decode owned fixture identity: %v", err)
		}
		if !container.State.Running || container.Config.Labels["eshu.goal"] != "7033-midread" {
			t.Fatal("fixture container is not running with the expected disposable goal label")
		}
		matched := false
		for _, network := range container.NetworkSettings.Networks {
			matched = matched || network.IPAddress == cfg.Host
		}
		if !matched {
			t.Fatal("fixture DSN does not resolve to its labeled direct container")
		}
	}
}

func assertMemberLossReservationsZero(t *testing.T, access *runtimepostgres.Access) {
	t.Helper()
	manual := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(manual))
	defer func() { _ = provider.Shutdown(context.Background()) }()
	registration, err := runtimepostgres.RegisterPoolMetrics(provider.Meter("member-loss-proof"), access)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = registration.Unregister() }()
	var metrics metricdata.ResourceMetrics
	if err := manual.Collect(t.Context(), &metrics); err != nil {
		t.Fatal(err)
	}
	for _, scope := range metrics.ScopeMetrics {
		for _, measurement := range scope.Metrics {
			if measurement.Name != runtimepostgres.MetricReaderMemberReservations {
				continue
			}
			gauge, ok := measurement.Data.(metricdata.Gauge[int64])
			if !ok || len(gauge.DataPoints) != 2 {
				t.Fatalf("reader reservation metric shape: %T", measurement.Data)
			}
			for _, point := range gauge.DataPoints {
				if point.Value != 0 {
					t.Fatalf("reader reservation leaked: %d", point.Value)
				}
			}
			return
		}
	}
	t.Fatal("reader reservation metric missing")
}
