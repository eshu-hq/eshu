// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"bytes"
	"context"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/collector/extensionhost"
	"github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/telemetry/contract"
)

const (
	seedGrantScopeSecret = "SEED-GRANT-SCOPE-b204"
	scorecardProducerID  = "dev.eshu.examples.scorecard"
)

// coordinatorGrantSignals wires the production GrantTelemetry adapter (the
// one cmd/workflow-coordinator builds) to in-memory metric and log sinks, so a
// test reads exactly what an operator would.
type coordinatorGrantSignals struct {
	observer component.GrantObserver
	reader   *sdkmetric.ManualReader
	logs     *bytes.Buffer
}

func newCoordinatorGrantSignals(t *testing.T) coordinatorGrantSignals {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	inst, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}
	logs := &bytes.Buffer{}
	logger := slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	return coordinatorGrantSignals{
		observer: extensionhost.NewGrantTelemetry(inst, logger),
		reader:   reader,
		logs:     logs,
	}
}

// decisions returns one "stage/decision/reason/fact_kind=count" string per
// recorded counter series, sorted, and fails on any label outside the closed
// four-key set.
func (s coordinatorGrantSignals) decisions(t *testing.T) []string {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := s.reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	var out []string
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != contract.MetricProducerGrantDecisions {
				continue
			}
			for _, dp := range m.Data.(metricdata.Sum[int64]).DataPoints {
				labels := map[string]string{}
				for _, kv := range dp.Attributes.ToSlice() {
					labels[string(kv.Key)] = kv.Value.AsString()
				}
				if len(labels) != 4 {
					t.Fatalf("metric labels = %v, want exactly decision, stage, reason, fact_kind", labels)
				}
				for _, value := range labels {
					if strings.Contains(value, scorecardProducerID) || strings.Contains(value, seedGrantScopeSecret) {
						t.Fatalf("metric label value %q leaks a producer id or grant scope", value)
					}
				}
				out = append(out, labels["stage"]+"/"+labels["decision"]+"/"+labels["reason"]+
					"/"+labels["fact_kind"]+"="+itoa(dp.Value))
			}
		}
	}
	sort.Strings(out)
	return out
}

func itoa(n int64) string {
	return strconv.FormatInt(n, 10)
}

func assertDecisions(t *testing.T, got []string, want ...string) {
	t.Helper()
	sort.Strings(want)
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("recorded decisions = %v, want %v", got, want)
	}
}

// TestLoadConfigObservedRecordsAllowAtReadbackAndActivation proves the
// coordinator reports an allow for a granted manifest at exactly the two
// stages it performs: readback (Registry.Readback) and activation (the
// reload that plans a claims-enabled instance).
func TestLoadConfigObservedRecordsAllowAtReadbackAndActivation(t *testing.T) {
	t.Parallel()

	home, _ := grantedCoreKindHome(t)
	signals := newCoordinatorGrantSignals(t)

	cfg, err := LoadConfigObserved(componentCoordinatorEnv(home, nil), signals.observer)
	if err != nil {
		t.Fatalf("LoadConfigObserved() error = %v, want nil", err)
	}
	if got, want := len(cfg.CollectorInstances), 1; got != want {
		t.Fatalf("collector instances = %d, want %d", got, want)
	}
	assertDecisions(t, signals.decisions(t),
		"readback/allow/granted/aws_resource=1",
		"activation/allow/granted/aws_resource=1",
	)
}

// TestLoadConfigObservedRecordsDenyWithoutActivation proves an ungranted,
// revoked, expired, or scope-mismatched manifest records a deny at readback
// and never reaches the activation stage, because the coordinator skips it.
func TestLoadConfigObservedRecordsDenyWithoutActivation(t *testing.T) {
	t.Parallel()

	future := time.Now().Add(time.Hour)
	revoke := func(t *testing.T, registry component.Registry) {
		t.Helper()
		if err := registry.RevokeGrant(scorecardProducerID, "0.1.0", "aws_resource", "scorecard"); err != nil {
			t.Fatalf("RevokeGrant() error = %v, want nil", err)
		}
	}
	for _, tt := range []struct {
		name   string
		mutate func(t *testing.T, registry component.Registry)
		want   string
	}{
		{"revoked", revoke, "readback/deny/revoked/aws_resource=1"},
		{
			"expired",
			func(t *testing.T, registry component.Registry) {
				t.Helper()
				expired := scorecardAWSResourceGrant("scorecard", "aws_resource", time.Now().Add(-time.Hour))
				if err := registry.RecordGrant(expired); err != nil {
					t.Fatalf("RecordGrant(expired) error = %v, want nil", err)
				}
			},
			"readback/deny/expired/aws_resource=1",
		},
		{
			"revoked then a grant for another kind",
			func(t *testing.T, registry component.Registry) {
				t.Helper()
				revoke(t, registry)
				if err := registry.RecordGrant(scorecardAWSResourceGrant("scorecard", "vpc", future)); err != nil {
					t.Fatalf("RecordGrant(other kind) error = %v, want nil", err)
				}
			},
			"readback/deny/revoked/aws_resource=1",
		},
		{
			"scope mismatch with a seeded scope value",
			func(t *testing.T, registry component.Registry) {
				t.Helper()
				revoke(t, registry)
				if err := registry.RecordGrant(scorecardAWSResourceGrant(seedGrantScopeSecret, "aws_resource", future)); err != nil {
					t.Fatalf("RecordGrant(other scope) error = %v, want nil", err)
				}
			},
			"readback/deny/scope_mismatch/aws_resource=1",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			home, registry := grantedCoreKindHome(t)
			tt.mutate(t, registry)
			signals := newCoordinatorGrantSignals(t)

			cfg, err := LoadConfigObserved(componentCoordinatorEnv(home, map[string]string{
				"ESHU_COLLECTOR_INSTANCES_JSON": staticGitCollectorJSON(),
			}), signals.observer)
			if err != nil {
				t.Fatalf("LoadConfigObserved() error = %v, want nil (skip, not abort)", err)
			}
			if got, want := len(cfg.CollectorInstances), 1; got != want {
				t.Fatalf("collector instances = %d, want only the static instance %d", got, want)
			}
			assertDecisions(t, signals.decisions(t), tt.want)
			if strings.Contains(signals.logs.String(), seedGrantScopeSecret) {
				t.Fatalf("log output leaks the seeded grant scope: %s", signals.logs.String())
			}
		})
	}
}

// TestLoadConfigObservedSkipsActivationForUnclaimedComponent proves a
// component the coordinator reads back but does not plan (no claims-enabled
// activation) records its readback decision and no activation decision, so
// the activation counter means "planned to run".
func TestLoadConfigObservedSkipsActivationForUnclaimedComponent(t *testing.T) {
	t.Parallel()

	home, _ := grantedCoreKindHomeClaims(t, false)
	signals := newCoordinatorGrantSignals(t)

	if _, err := LoadConfigObserved(componentCoordinatorEnv(home, map[string]string{
		"ESHU_COLLECTOR_INSTANCES_JSON": staticGitCollectorJSON(),
	}), signals.observer); err != nil {
		t.Fatalf("LoadConfigObserved() error = %v, want nil", err)
	}
	assertDecisions(t, signals.decisions(t), "readback/allow/granted/aws_resource=1")
}

// TestLoadConfigDoesNotObserveWithoutObserver keeps the observer-free entry
// point a strict no-op so existing callers and the API are unchanged.
func TestLoadConfigDoesNotObserveWithoutObserver(t *testing.T) {
	t.Parallel()

	home, _ := grantedCoreKindHome(t)
	if _, err := LoadConfigObserved(componentCoordinatorEnv(home, nil), nil); err != nil {
		t.Fatalf("LoadConfigObserved(nil) error = %v, want nil", err)
	}
}
