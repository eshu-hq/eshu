// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package schema_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/replay/cassette"
	"github.com/eshu-hq/eshu/go/internal/replay/schema"
)

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path) // #nosec G304 -- test reads repo-shipped cassette fixtures
	if err != nil {
		t.Fatalf("read %q: %v", path, err)
	}
	return data
}

// validCassetteBytes builds a minimal valid cassette document by marshaling the
// real cassette structs, so the baseline is guaranteed to satisfy the loader.
func validCassetteBytes(t *testing.T) []byte {
	t.Helper()
	f := cassette.File{
		Collector:     "kubernetes_live",
		SchemaVersion: cassette.SchemaVersionV1,
		Scopes: []cassette.Scope{{
			ScopeID:       "cluster-a",
			SourceSystem:  "kubernetes_live",
			ScopeKind:     "cluster",
			CollectorKind: "kubernetes_live",
			GenerationID:  "gen-1",
			ObservedAt:    time.Date(2026, 6, 25, 0, 0, 0, 0, time.UTC),
			Facts: []cassette.Fact{{
				FactKind:      "kubernetes_workload",
				StableFactKey: "ns/app",
				SchemaVersion: "1",
				Payload:       map[string]any{"name": "app"},
			}},
		}},
	}
	data, err := json.Marshal(f)
	if err != nil {
		t.Fatalf("marshal valid cassette: %v", err)
	}
	return data
}

// withInjectedKey decodes the document, sets a raw key at the requested path,
// and re-encodes it — used to simulate a field-name typo that JSON decoding
// into the structs would otherwise silently drop.
func withInjectedKey(t *testing.T, data []byte, mutate func(root map[string]any)) []byte {
	t.Helper()
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatalf("unmarshal for mutation: %v", err)
	}
	mutate(root)
	out, err := json.Marshal(root)
	if err != nil {
		t.Fatalf("re-marshal mutated cassette: %v", err)
	}
	return out
}

func TestValidateCassetteBytesAcceptsValid(t *testing.T) {
	t.Parallel()
	if err := schema.ValidateCassetteBytes("valid", validCassetteBytes(t)); err != nil {
		t.Fatalf("ValidateCassetteBytes(valid) = %v, want nil", err)
	}
}

func TestValidateCassetteBytesRejectsStructural(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		mutate  func(root map[string]any)
		wantSub string
	}{
		{
			name:    "missing schema_version",
			mutate:  func(r map[string]any) { delete(r, "schema_version") },
			wantSub: "schema_version",
		},
		{
			name:    "wrong schema_version",
			mutate:  func(r map[string]any) { r["schema_version"] = "9" },
			wantSub: "schema_version",
		},
		{
			name:    "no scopes",
			mutate:  func(r map[string]any) { r["scopes"] = []any{} },
			wantSub: "at least 1 item",
		},
		{
			name: "missing scope_id",
			mutate: func(r map[string]any) {
				scope := r["scopes"].([]any)[0].(map[string]any)
				delete(scope, "scope_id")
			},
			wantSub: "scope_id",
		},
		{
			name: "missing fact_kind",
			mutate: func(r map[string]any) {
				scope := r["scopes"].([]any)[0].(map[string]any)
				fact := scope["facts"].([]any)[0].(map[string]any)
				delete(fact, "fact_kind")
			},
			wantSub: "fact_kind",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			data := withInjectedKey(t, validCassetteBytes(t), tc.mutate)
			err := schema.ValidateCassetteBytes(tc.name, data)
			if err == nil {
				t.Fatalf("ValidateCassetteBytes(%s) = nil, want error containing %q", tc.name, tc.wantSub)
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("error = %q, want it to contain %q", err, tc.wantSub)
			}
		})
	}
}

// TestValidateCassetteBytesRejectsUnknownField is the typo-catching gate: a
// misspelled optional field is silently dropped by struct decoding (so the
// loader alone passes it), but additionalProperties:false rejects it with a
// field-level path. This is the behavior the JSON Schema buys over the loader.
func TestValidateCassetteBytesRejectsUnknownField(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		mutate  func(root map[string]any)
		wantSub string
	}{
		{
			name:    "unknown root field",
			mutate:  func(r map[string]any) { r["collectr"] = "typo" },
			wantSub: `(root): unknown field "collectr"`,
		},
		{
			name: "unknown scope field",
			mutate: func(r map[string]any) {
				scope := r["scopes"].([]any)[0].(map[string]any)
				scope["scope_knd"] = "typo"
			},
			wantSub: `scopes[0]: unknown field "scope_knd"`,
		},
		{
			name: "unknown fact field (misspelled source_uri)",
			mutate: func(r map[string]any) {
				scope := r["scopes"].([]any)[0].(map[string]any)
				fact := scope["facts"].([]any)[0].(map[string]any)
				fact["source_ur"] = "file://x"
			},
			wantSub: `scopes[0].facts[0]: unknown field "source_ur"`,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			data := withInjectedKey(t, validCassetteBytes(t), tc.mutate)
			err := schema.ValidateCassetteBytes(tc.name, data)
			if err == nil {
				t.Fatalf("ValidateCassetteBytes(%s) = nil, want unknown-field error", tc.name)
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("error = %q, want it to contain %q", err, tc.wantSub)
			}
		})
	}
}

// TestValidateCassetteBytesEnforcesSchemaOnlyConstraints proves the validator
// enforces constraints the permissive Go loader never checks but the published
// JSON Schema declares — so the author-time gate cannot accept a cassette the
// schema rejects (regression for the codex P2 on #4105).
func TestValidateCassetteBytesEnforcesSchemaOnlyConstraints(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		mutate  func(root map[string]any)
		wantSub string
	}{
		{
			name: "negative fencing_token",
			mutate: func(r map[string]any) {
				fact := r["scopes"].([]any)[0].(map[string]any)["facts"].([]any)[0].(map[string]any)
				fact["fencing_token"] = -1
			},
			wantSub: "fencing_token: must be >= 0",
		},
		{
			name: "null metadata",
			mutate: func(r map[string]any) {
				scope := r["scopes"].([]any)[0].(map[string]any)
				scope["metadata"] = nil
			},
			wantSub: "metadata: must be an object",
		},
		{
			name: "null partition_key",
			mutate: func(r map[string]any) {
				scope := r["scopes"].([]any)[0].(map[string]any)
				scope["partition_key"] = nil
			},
			wantSub: "partition_key: must be a string",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			data := withInjectedKey(t, validCassetteBytes(t), tc.mutate)
			// The permissive loader alone must NOT catch these (that is the
			// drift the schema pass closes): prove the loader passes the doc.
			if _, err := cassette.ParseAndValidate(data); err != nil {
				t.Fatalf("precondition: loader rejected %s (%v); schema-only case is moot", tc.name, err)
			}
			err := schema.ValidateCassetteBytes(tc.name, data)
			if err == nil {
				t.Fatalf("ValidateCassetteBytes(%s) = nil, want schema error containing %q", tc.name, tc.wantSub)
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("error = %q, want it to contain %q", err, tc.wantSub)
			}
		})
	}
}

// TestCommittedCassettesValid runs every cassette committed under
// testdata/cassettes through the validator. This is the offline author-time
// gate: it loads no Docker, no graph, and finishes in milliseconds.
func TestCommittedCassettesValid(t *testing.T) {
	t.Parallel()
	root := filepath.Join("..", "..", "..", "..", "testdata", "cassettes")
	matches, err := filepath.Glob(filepath.Join(root, "*", "*.json"))
	if err != nil {
		t.Fatalf("glob committed cassettes: %v", err)
	}
	if len(matches) == 0 {
		t.Fatalf("no committed cassettes found under %s", root)
	}
	for _, path := range matches {
		path := path
		t.Run(filepath.Base(filepath.Dir(path))+"/"+filepath.Base(path), func(t *testing.T) {
			t.Parallel()
			data := readFile(t, path)
			if err := schema.ValidateCassetteBytes(path, data); err != nil {
				t.Fatalf("committed cassette failed validation: %v", err)
			}
		})
	}
}
