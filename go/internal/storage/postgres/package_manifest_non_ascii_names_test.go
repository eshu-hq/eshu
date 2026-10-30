// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/testfixtures"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func nonASCIIManifestEnvelope(factID, name, packageManager string) facts.Envelope {
	return manifestConsumptionTestEnvelope(factID, false, map[string]any{
		"entity_type": "Variable",
		"repo_id":     "repository:app",
		"entity_name": name,
		"entity_metadata": map[string]any{
			"config_kind":     "dependency",
			"package_manager": packageManager,
		},
	})
}

// The sidecar writer must key a lookalike manifest name under its own
// spelling, never under the ASCII package's key, and report it by ecosystem
// so the ingester can count it (#9584).
func TestWritePackageManifestConsumptionKeysKeepsLookalikeAndCountsIt(t *testing.T) {
	t.Parallel()

	database := &manifestConsumptionExecRecorder{}
	counts, err := writePackageManifestConsumptionKeys(context.Background(), database, []facts.Envelope{
		nonASCIIManifestEnvelope("ascii", "Serilog", "nuget"),
		nonASCIIManifestEnvelope("lookalike", "Ser\u0130log", "nuget"),
		nonASCIIManifestEnvelope("kelvin", "\u212Aafka-node", "npm"),
	})
	if err != nil {
		t.Fatalf("writePackageManifestConsumptionKeys() error = %v", err)
	}
	if got := counts["nuget"]; got != 1 {
		t.Fatalf("non-ASCII nuget count = %d, want 1 (counts %v)", got, counts)
	}
	if got := counts["npm"]; got != 1 {
		t.Fatalf("non-ASCII npm count = %d, want 1 (counts %v)", got, counts)
	}
	keysByFact := map[string][]string{}
	for _, exec := range database.execs[1:] {
		for i := 0; i+columnsPerPackageManifestConsumptionKeyRow <= len(exec.args); i += columnsPerPackageManifestConsumptionKeyRow {
			factID, _ := exec.args[i].(string)
			name, _ := exec.args[i+5].(string)
			keysByFact[factID] = append(keysByFact[factID], name)
		}
	}
	for _, name := range keysByFact["lookalike"] {
		if name == "serilog" {
			t.Fatalf("lookalike fact was keyed as %q, the ASCII package's key", name)
		}
	}
	if len(keysByFact["lookalike"]) == 0 || keysByFact["lookalike"][0] != "ser\u0130log" {
		t.Fatalf("lookalike keys = %q, want [%q]", keysByFact["lookalike"], "ser\u0130log")
	}
	if got := keysByFact["ascii"]; len(got) != 1 || got[0] != "serilog" {
		t.Fatalf("ASCII keys = %q, want [serilog]", got)
	}
	if counts, err := writePackageManifestConsumptionKeys(context.Background(), &manifestConsumptionExecRecorder{},
		[]facts.Envelope{nonASCIIManifestEnvelope("plain", "Newtonsoft.Json", "nuget")}); err != nil || counts != nil {
		t.Fatalf("ASCII-only batch counts = %v, err = %v; want nil, nil", counts, err)
	}
}

func TestRecordPackageManifestNonASCIINamesEmitsCounterByEcosystem(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(provider.Meter("postgres-non-ascii-names"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	recordPackageManifestNonASCIINames(context.Background(), instruments, "scope-1", "gen-1", map[string]int64{"nuget": 2, "npm": 1, "pypi": 0})
	recordPackageManifestNonASCIINames(context.Background(), nil, "scope-1", "gen-1", map[string]int64{"nuget": 5})

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	got := map[string]int64{}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_package_manifest_non_ascii_names_total" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("metric data = %T, want Sum[int64]", m.Data)
			}
			for _, point := range sum.DataPoints {
				value, _ := point.Attributes.Value(telemetry.MetricDimensionEcosystem)
				got[value.AsString()] += point.Value
			}
		}
	}
	if got["nuget"] != 2 || got["npm"] != 1 || len(got) != 2 {
		t.Fatalf("eshu_dp_package_manifest_non_ascii_names_total = %v, want nuget=2 npm=1", got)
	}
}

// TestCommitScopeGenerationCountsNonASCIIManifestNames drives the real
// IngestionStore.CommitScopeGeneration batch callback and requires the
// eshu_dp_package_manifest_non_ascii_names_total counter it emits (#9584).
func TestCommitScopeGenerationCountsNonASCIIManifestNames(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(provider.Meter("postgres-non-ascii-commit"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	now := time.Date(2026, time.October, 29, 12, 0, 0, 0, time.UTC)
	store := NewIngestionStore(&countingCatalogDB{})
	store.Now = func() time.Time { return now }
	store.Instruments = instruments

	envelopes := []facts.Envelope{
		nonASCIIManifestEnvelope("ascii", "Serilog", "nuget"),
		nonASCIIManifestEnvelope("lookalike", "Ser\u0130log", "nuget"),
	}
	for index := range envelopes {
		envelopes[index].ScopeID = "scope-app"
		envelopes[index].GenerationID = "gen-app-1"
		envelopes[index].ObservedAt = now.Add(-time.Minute)
		envelopes[index].StableFactKey = "content_entity:" + envelopes[index].FactID
	}
	if err := store.CommitScopeGeneration(
		context.Background(),
		testfixtures.CatalogScope("scope-app", "repository:app"),
		testfixtures.CatalogGeneration("scope-app", "gen-app-1", now),
		testfixtures.FactChannel(envelopes),
	); err != nil {
		t.Fatalf("CommitScopeGeneration() error = %v", err)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	var nuget int64 = -1
	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, record := range scopeMetrics.Metrics {
			if record.Name != "eshu_dp_package_manifest_non_ascii_names_total" {
				continue
			}
			sum, ok := record.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("metric data = %T, want metricdata.Sum[int64]", record.Data)
			}
			for _, point := range sum.DataPoints {
				if value, ok := point.Attributes.Value(telemetry.MetricDimensionEcosystem); ok && value.AsString() == "nuget" {
					nuget = point.Value
				}
			}
		}
	}
	if nuget != 1 {
		t.Fatalf("eshu_dp_package_manifest_non_ascii_names_total{ecosystem=nuget} = %d, want 1", nuget)
	}
}
