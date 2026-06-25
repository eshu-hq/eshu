// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cassette_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/cassette"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

func writeCassetteFile(t *testing.T, f cassette.File) string {
	t.Helper()
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		t.Fatalf("marshal cassette: %v", err)
	}
	path := filepath.Join(t.TempDir(), "cassette.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write cassette file: %v", err)
	}
	return path
}

func minimalCassette(observedAt time.Time) cassette.File {
	return cassette.File{
		Collector:     "kubernetes_live",
		SchemaVersion: "1",
		Scopes: []cassette.Scope{
			{
				ScopeID:       "kubernetes_live:cluster:test-cluster",
				SourceSystem:  "kubernetes_live",
				ScopeKind:     "cluster",
				CollectorKind: "kubernetes_live",
				PartitionKey:  "kubernetes_live:cluster:test-cluster",
				GenerationID:  "cassette-k8s-test-gen1",
				ObservedAt:    observedAt,
				TriggerKind:   "snapshot",
				Facts: []cassette.Fact{
					{
						FactKind:         "kubernetes_live.pod_template",
						StableFactKey:    "kubernetes_live:cluster:test-cluster:deployment:default:demo",
						SchemaVersion:    "1.0.0",
						CollectorKind:    "kubernetes_live",
						FencingToken:     1,
						SourceConfidence: "observed",
						Payload: map[string]any{
							"cluster_id": "test-cluster",
							"namespace":  "default",
							"name":       "demo",
							"kind":       "Deployment",
						},
					},
				},
			},
		},
	}
}

// TestSourceNextEmitsOneGenerationPerScope proves the source yields one
// CollectedGeneration per scope in the cassette, with correct scope/generation
// identity and fact count.
func TestSourceNextEmitsOneGenerationPerScope(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 6, 25, 0, 0, 0, 0, time.UTC)
	path := writeCassetteFile(t, minimalCassette(observedAt))
	src, err := cassette.NewSource(path)
	if err != nil {
		t.Fatalf("NewSource: %v", err)
	}

	gen, ok, err := src.Next(context.Background())
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if !ok {
		t.Fatal("Next ok=false, want true for the first scope")
	}
	if got, want := gen.Scope.ScopeID, "kubernetes_live:cluster:test-cluster"; got != want {
		t.Errorf("ScopeID = %q, want %q", got, want)
	}
	if got, want := gen.Generation.GenerationID, "cassette-k8s-test-gen1"; got != want {
		t.Errorf("GenerationID = %q, want %q", got, want)
	}
	if got, want := gen.FactCount, 1; got != want {
		t.Errorf("FactCount = %d, want %d", got, want)
	}

	// Drain exhausts the scope batch.
	_, ok, err = src.Next(context.Background())
	if err != nil {
		t.Fatalf("Next (drain): %v", err)
	}
	if ok {
		t.Error("Next ok=true after all scopes exhausted, want false")
	}
}

// TestSourceRestartsBatchAfterDrain proves that after all scopes are exhausted
// the source restarts from the first scope on the next poll.
func TestSourceRestartsBatchAfterDrain(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 6, 25, 0, 0, 0, 0, time.UTC)
	path := writeCassetteFile(t, minimalCassette(observedAt))
	src, err := cassette.NewSource(path)
	if err != nil {
		t.Fatalf("NewSource: %v", err)
	}

	// First poll — consume the batch and exhaust.
	if _, ok, err := src.Next(context.Background()); !ok || err != nil {
		t.Fatalf("first Next ok=%v err=%v, want (true, nil)", ok, err)
	}
	if _, ok, err := src.Next(context.Background()); ok || err != nil {
		t.Fatalf("drain Next ok=%v err=%v, want (false, nil)", ok, err)
	}

	// Second poll — should restart and emit again.
	gen, ok, err := src.Next(context.Background())
	if err != nil {
		t.Fatalf("restart Next: %v", err)
	}
	if !ok {
		t.Fatal("restart Next ok=false, want true")
	}
	if got, want := gen.Scope.ScopeID, "kubernetes_live:cluster:test-cluster"; got != want {
		t.Errorf("restart ScopeID = %q, want %q", got, want)
	}
}

// TestSourceFactEnvelopeFields proves the emitted facts.Envelope has the
// expected fields derived from the cassette scope and fact entries.
func TestSourceFactEnvelopeFields(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	path := writeCassetteFile(t, minimalCassette(observedAt))
	src, err := cassette.NewSource(path)
	if err != nil {
		t.Fatalf("NewSource: %v", err)
	}

	gen, ok, err := src.Next(context.Background())
	if !ok || err != nil {
		t.Fatalf("Next ok=%v err=%v, want (true, nil)", ok, err)
	}

	var envs []facts.Envelope
	for e := range gen.Facts {
		envs = append(envs, e)
	}
	if len(envs) != 1 {
		t.Fatalf("got %d envelopes, want 1", len(envs))
	}
	e := envs[0]
	if got, want := e.ScopeID, "kubernetes_live:cluster:test-cluster"; got != want {
		t.Errorf("ScopeID = %q, want %q", got, want)
	}
	if got, want := e.GenerationID, "cassette-k8s-test-gen1"; got != want {
		t.Errorf("GenerationID = %q, want %q", got, want)
	}
	if got, want := e.FactKind, "kubernetes_live.pod_template"; got != want {
		t.Errorf("FactKind = %q, want %q", got, want)
	}
	if got, want := e.SchemaVersion, "1.0.0"; got != want {
		t.Errorf("SchemaVersion = %q, want %q", got, want)
	}
	if got, want := e.CollectorKind, "kubernetes_live"; got != want {
		t.Errorf("CollectorKind = %q, want %q", got, want)
	}
	if got, want := e.FencingToken, int64(1); got != want {
		t.Errorf("FencingToken = %d, want %d", got, want)
	}
	if got, want := e.SourceConfidence, "observed"; got != want {
		t.Errorf("SourceConfidence = %q, want %q", got, want)
	}
	if !e.ObservedAt.Equal(observedAt) {
		t.Errorf("ObservedAt = %v, want %v", e.ObservedAt, observedAt)
	}
	if e.FactID == "" {
		t.Error("FactID is empty, want a derived non-empty value")
	}
}

// TestSourceScopeKindAndCollectorKind proves the IngestionScope carries the
// correct typed scope/collector kinds from the cassette.
func TestSourceScopeKindAndCollectorKind(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 6, 25, 0, 0, 0, 0, time.UTC)
	path := writeCassetteFile(t, minimalCassette(observedAt))
	src, err := cassette.NewSource(path)
	if err != nil {
		t.Fatalf("NewSource: %v", err)
	}

	gen, _, _ := src.Next(context.Background())
	if got, want := gen.Scope.ScopeKind, scope.KindCluster; got != want {
		t.Errorf("ScopeKind = %q, want %q", got, want)
	}
	if got, want := gen.Scope.CollectorKind, scope.CollectorKubernetesLive; got != want {
		t.Errorf("CollectorKind = %q, want %q", got, want)
	}
}

// TestSourceMultipleScopes proves each scope is emitted as a separate
// CollectedGeneration in document order.
func TestSourceMultipleScopes(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 6, 25, 0, 0, 0, 0, time.UTC)
	f := cassette.File{
		Collector:     "kubernetes_live",
		SchemaVersion: "1",
		Scopes: []cassette.Scope{
			{
				ScopeID:       "kubernetes_live:cluster:cluster-a",
				SourceSystem:  "kubernetes_live",
				ScopeKind:     "cluster",
				CollectorKind: "kubernetes_live",
				GenerationID:  "gen-cluster-a",
				ObservedAt:    observedAt,
				Facts: []cassette.Fact{
					{
						FactKind:      "kubernetes_live.pod_template",
						StableFactKey: "kubernetes_live:cluster:cluster-a:deployment:default:demo-a",
						SchemaVersion: "1.0.0",
						Payload:       map[string]any{"name": "demo-a"},
					},
				},
			},
			{
				ScopeID:       "kubernetes_live:cluster:cluster-b",
				SourceSystem:  "kubernetes_live",
				ScopeKind:     "cluster",
				CollectorKind: "kubernetes_live",
				GenerationID:  "gen-cluster-b",
				ObservedAt:    observedAt,
				Facts: []cassette.Fact{
					{
						FactKind:      "kubernetes_live.pod_template",
						StableFactKey: "kubernetes_live:cluster:cluster-b:deployment:default:demo-b",
						SchemaVersion: "1.0.0",
						Payload:       map[string]any{"name": "demo-b"},
					},
				},
			},
		},
	}
	path := writeCassetteFile(t, f)
	src, err := cassette.NewSource(path)
	if err != nil {
		t.Fatalf("NewSource: %v", err)
	}

	genA, ok, err := src.Next(context.Background())
	if !ok || err != nil {
		t.Fatalf("first Next: ok=%v err=%v", ok, err)
	}
	genB, ok, err := src.Next(context.Background())
	if !ok || err != nil {
		t.Fatalf("second Next: ok=%v err=%v", ok, err)
	}
	_, ok, err = src.Next(context.Background())
	if ok || err != nil {
		t.Fatalf("drain Next: ok=%v err=%v, want (false, nil)", ok, err)
	}

	if got, want := genA.Scope.ScopeID, "kubernetes_live:cluster:cluster-a"; got != want {
		t.Errorf("first scope ScopeID = %q, want %q", got, want)
	}
	if got, want := genB.Scope.ScopeID, "kubernetes_live:cluster:cluster-b"; got != want {
		t.Errorf("second scope ScopeID = %q, want %q", got, want)
	}
}

// TestLoadFileRejectsInvalidCassette proves that LoadFile returns a meaningful
// error for files that fail schema validation.
func TestLoadFileRejectsInvalidCassette(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		mutate      func(*cassette.File)
		wantErrFrag string
	}{
		{
			name:        "wrong schema version",
			mutate:      func(f *cassette.File) { f.SchemaVersion = "99" },
			wantErrFrag: "schema_version",
		},
		{
			name:        "no scopes",
			mutate:      func(f *cassette.File) { f.Scopes = nil },
			wantErrFrag: "at least one scope",
		},
		{
			name: "missing scope_id",
			mutate: func(f *cassette.File) {
				f.Scopes[0].ScopeID = ""
			},
			wantErrFrag: "scope_id",
		},
		{
			name: "missing generation_id",
			mutate: func(f *cassette.File) {
				f.Scopes[0].GenerationID = ""
			},
			wantErrFrag: "generation_id",
		},
		{
			name: "missing fact_kind",
			mutate: func(f *cassette.File) {
				f.Scopes[0].Facts[0].FactKind = ""
			},
			wantErrFrag: "fact_kind",
		},
	}

	observedAt := time.Date(2026, 6, 25, 0, 0, 0, 0, time.UTC)
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := minimalCassette(observedAt)
			tc.mutate(&f)
			path := writeCassetteFile(t, f)
			_, err := cassette.NewSource(path)
			if err == nil {
				t.Fatalf("NewSource succeeded, want error containing %q", tc.wantErrFrag)
			}
			if !strings.Contains(err.Error(), tc.wantErrFrag) {
				t.Errorf("error = %q, want it to contain %q", err, tc.wantErrFrag)
			}
		})
	}
}
