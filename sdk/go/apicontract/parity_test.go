// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package apicontract

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// canonicalTruthEnvelope is the SDK-side construction of the wire sample in
// testdata/golden/envelope_truth.json. Every field is a literal: the test
// fails when a struct tag or Go type drifts from the golden bytes the core
// types produced.
func canonicalTruthEnvelope() ResponseEnvelope {
	return ResponseEnvelope{
		Data: map[string]any{"truncated": true, "rows": []any{"a", "b"}},
		Truth: &TruthEnvelope{
			Level:      TruthLevelDerived,
			Capability: "search_code",
			Profile:    ProfileProduction,
			Basis:      TruthBasisHybrid,
			Backend:    GraphBackendNeo4j,
			Freshness: TruthFreshness{
				State:      FreshnessStale,
				ObservedAt: "2026-10-29T00:00:00Z",
				Detail:     "reducer backlog",
				Cause:      FreshnessCauseReducerBacklog,
				NextCheck: &FreshnessNextCheck{
					Tool:   "get_reducer_status",
					Route:  "/api/v0/admin/reducer/status",
					Reason: "drain state",
					Params: map[string]string{"scope": "repo"},
				},
			},
			Reason:    "hybrid evidence",
			Truncated: true,
			Omissions: []TruthOmission{
				{Section: "citations", Detail: "handles", Total: 7},
			},
		},
	}
}

// canonicalErrorEnvelope mirrors testdata/golden/envelope_error.json.
func canonicalErrorEnvelope() ResponseEnvelope {
	return ResponseEnvelope{
		Error: &ErrorEnvelope{
			Code:       ErrorCodeNotFound,
			Message:    "no such service",
			Capability: "service_story",
			Profiles: &ErrorProfiles{
				Current:  ProfileProduction,
				Required: ProfileLocalAuthoritative,
			},
			Details: map[string]any{"service": "api"},
		},
	}
}

func readGolden(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "golden", name))
	if err != nil {
		t.Fatalf("read golden %s: %v", name, err)
	}
	return raw
}

func marshalCanonical(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatalf("marshal canonical: %v", err)
	}
	return append(raw, '\n')
}

// TestGoldenEnvelopeBytes pins the wire contract: the SDK shapes marshal to
// the exact bytes the core querycontract types produced. A renamed JSON tag,
// a retyped field, or a dropped omitempty fails here before any consumer can
// pin the module.
func TestGoldenEnvelopeBytes(t *testing.T) {
	t.Parallel()
	cases := map[string]ResponseEnvelope{
		"envelope_truth.json": canonicalTruthEnvelope(),
		"envelope_error.json": canonicalErrorEnvelope(),
	}
	for name, canonical := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			want := readGolden(t, name)
			if got := marshalCanonical(t, canonical); string(got) != string(want) {
				t.Fatalf("SDK marshal differs from golden %s:\n got: %s\nwant: %s", name, got, want)
			}
			var decoded ResponseEnvelope
			if err := json.Unmarshal(want, &decoded); err != nil {
				t.Fatalf("unmarshal golden %s into SDK types: %v", name, err)
			}
			if got := marshalCanonical(t, decoded); string(got) != string(want) {
				t.Fatalf("golden %s does not round-trip through SDK types:\n got: %s\nwant: %s", name, got, want)
			}
		})
	}
}

// TestGoldenToolDefinition pins the MCP tool-definition wire shape against
// the bytes the core toolcontract type produced.
func TestGoldenToolDefinition(t *testing.T) {
	t.Parallel()
	want := readGolden(t, "tool_definition.json")
	canonical := ToolDefinition{
		Name:        "search_code",
		Description: "Search code across repositories",
		InputSchema: map[string]any{"type": "object"},
	}
	if got := marshalCanonical(t, canonical); string(got) != string(want) {
		t.Fatalf("SDK ToolDefinition marshal differs:\n got: %s\nwant: %s", got, want)
	}
	var decoded ToolDefinition
	if err := json.Unmarshal(want, &decoded); err != nil {
		t.Fatalf("unmarshal tool golden into SDK type: %v", err)
	}
	if !reflect.DeepEqual(decoded, canonical) {
		t.Fatalf("tool golden round-trip mismatch:\n got: %+v\nwant: %+v", decoded, canonical)
	}
}

// TestClientEntriesMatchGolden pins the published client variable table to
// the envregistry subset the core generator snapshotted. Adding, dropping, or
// retyping an entry fails here; the core drift test fails first when the
// registry itself moves.
func TestClientEntriesMatchGolden(t *testing.T) {
	t.Parallel()
	want := readGolden(t, "env_client_entries.json")
	var golden []Entry
	if err := json.Unmarshal(want, &golden); err != nil {
		t.Fatalf("unmarshal entries golden: %v", err)
	}
	if got := ClientEntries(); !reflect.DeepEqual(got, golden) {
		t.Fatalf("ClientEntries differs from golden:\n got: %+v\nwant: %+v", got, golden)
	}
}
