// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/projector/canonical"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestCanonicalNodeWriterSkipsOversizedIndexKeys proves one oversized indexed
// value no longer reaches the backend: the #7058 trial dead-lettered a whole
// repository because Neo4j rejected a 32,473-byte Module.name inside the atomic
// canonical write. The writer must drop that row (and the Function with an
// oversized name), write everything else, and say so in a metric and a WARN.
func TestCanonicalNodeWriterSkipsOversizedIndexKeys(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	executor := &recordingGroupExecutor{}
	writer := NewCanonicalNodeWriter(executor, 500, instruments)

	bigModule := "this.basePage);" + strings.Repeat("x", 32473-len("this.basePage);"))
	bigFunction := strings.Repeat("f", canonical.MaxIndexedKeyBytes+1)
	err = writer.Write(context.Background(), canonical.CanonicalMaterialization{
		ScopeID:         "scope-1",
		GenerationID:    "gen-1",
		RepoID:          "repo-1",
		RepoPath:        "/repo",
		FirstGeneration: true,
		Repository:      &canonical.RepositoryRow{RepoID: "repo-1", Name: "repo", Path: "/repo"},
		Files: []canonical.FileRow{
			{Path: "/repo/a.ts", RelativePath: "a.ts", Name: "a.ts", Language: "typescript", RepoID: "repo-1"},
		},
		Entities: []canonical.EntityRow{
			{EntityID: "content-entity:e_000000000001", Label: "Function", EntityName: "render", FilePath: "/repo/a.ts", RepoID: "repo-1"},
			{EntityID: "content-entity:e_000000000002", Label: "Function", EntityName: bigFunction, FilePath: "/repo/a.ts", RepoID: "repo-1"},
		},
		Modules: []canonical.ModuleRow{
			{Name: "react", Language: "typescript"},
			{Name: bigModule, Language: "typescript"},
		},
		Imports: []canonical.ImportRow{
			{FilePath: "/repo/a.ts", ModuleName: "react", ModuleLanguage: "typescript"},
			{FilePath: "/repo/a.ts", ModuleName: bigModule, ModuleLanguage: "typescript"},
		},
	})
	if err != nil {
		t.Fatalf("Write() error = %v, want nil: an oversized value must not fail the repository write", err)
	}

	var sawReact, sawRender bool
	for _, group := range executor.groupCalls {
		for _, stmt := range group {
			encoded, err := json.Marshal(stmt.Parameters)
			if err != nil {
				t.Fatalf("marshal parameters: %v", err)
			}
			text := string(encoded)
			if strings.Contains(text, bigModule) || strings.Contains(text, bigFunction) {
				t.Fatalf("statement for phase %v still carries an oversized value", stmt.Parameters[StatementMetadataPhaseKey])
			}
			sawReact = sawReact || strings.Contains(text, `"react"`)
			sawRender = sawRender || strings.Contains(text, `"render"`)
		}
	}
	if !sawReact || !sawRender {
		t.Fatalf("normal rows missing from the write: react=%v render=%v", sawReact, sawRender)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	const name = "eshu_dp_graph_oversized_index_keys_skipped_total"
	if got := metricCounterValue(t, rm, name, telemetry.MetricDimensionNodeLabel, "Module"); got != 1 {
		t.Fatalf("%s{node_label=Module} = %d, want 1", name, got)
	}
	if got := metricCounterValue(t, rm, name, telemetry.MetricDimensionNodeLabel, "Function"); got != 1 {
		t.Fatalf("%s{node_label=Function} = %d, want 1", name, got)
	}

	var warns []map[string]any
	for _, line := range bytes.Split(bytes.TrimSpace(logs.Bytes()), []byte("\n")) {
		var entry map[string]any
		if err := json.Unmarshal(line, &entry); err != nil {
			t.Fatalf("decode log line %q: %v", line, err)
		}
		if entry["msg"] == "canonical row skipped: indexed value exceeds key size limit" {
			warns = append(warns, entry)
		}
	}
	if len(warns) != 2 {
		t.Fatalf("skip WARN lines = %d, want 2; logs=%s", len(warns), logs.String())
	}
	module := warns[0]
	for key, want := range map[string]any{
		"scope_id": "scope-1", "repo_id": "repo-1", "generation_id": "gen-1",
		"node_label": "Module", "property": "name",
		"key_bytes": float64(32473), "limit_bytes": float64(canonical.MaxIndexedKeyBytes),
	} {
		if module[key] != want {
			t.Fatalf("module WARN %s = %v, want %v", key, module[key], want)
		}
	}
	if prefix, _ := module["value_prefix"].(string); !strings.HasPrefix(prefix, "this.basePage);") {
		t.Fatalf("module WARN value_prefix = %q, want the value's leading bytes", prefix)
	}
	if warns[1]["entity_id"] != "content-entity:e_000000000002" || warns[1]["file_path"] != "/repo/a.ts" {
		t.Fatalf("function WARN = %v, want entity_id and file_path of the skipped entity", warns[1])
	}
}
