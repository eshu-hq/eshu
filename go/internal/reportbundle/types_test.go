// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reportbundle

import (
	"encoding/json"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
)

// TestBundle_SchemaRoundTrip proves the wrong_answer_report.v1 schema is
// byte-stable across an encode -> decode -> re-encode cycle: no field is lost
// or reordered nondeterministically by the standard json package.
func TestBundle_SchemaRoundTrip(t *testing.T) {
	t.Parallel()

	bundle := Bundle{
		SchemaVersion: SchemaVersion,
		BundleID:      "deadbeef",
		CreatedAt:     "2026-07-10T00:00:00Z",
		ReporterNote:  "expected the owning team, got an empty list",
		Query: CapturedQuery{
			Surface: "api",
			Target:  "/api/v0/services/checkout/story",
			Method:  "GET",
			Params:  map[string]any{"repo": "demo/service"},
			Profile: "local_authoritative",
		},
		Response: CapturedResponse{
			Truth: &query.TruthEnvelope{
				Level:   query.TruthLevelExact,
				Basis:   query.TruthBasisAuthoritativeGraph,
				Backend: query.GraphBackendNornicDB,
			},
			Truncated:  false,
			Data:       json.RawMessage(`{"owner":"platform-team"}`),
			DataDigest: "abc123",
		},
		Evidence: EvidenceContext{
			Citations: []CitationRef{
				{Kind: "file", RepoID: "demo/service", RelativePath: "main.go", CitationID: "citation:abc"},
			},
			FactRefs:      []FactRef{{FactID: "f1", StableFactKey: "k1", FactKind: "repository", ScopeID: "s1", GenerationID: "g1"}},
			FactRefsState: "resolved",
		},
		Redaction: RedactionProfile{
			Profile: ProfilePublic,
			Rules:   []string{"api_key"},
		},
		Validation: Validation{
			Status: "passed",
			Checks: append([]string(nil), ValidationChecks...),
		},
	}

	first, err := json.Marshal(bundle)
	if err != nil {
		t.Fatalf("marshal bundle: %v", err)
	}

	var decoded Bundle
	if err := json.Unmarshal(first, &decoded); err != nil {
		t.Fatalf("unmarshal bundle: %v", err)
	}

	second, err := json.Marshal(decoded)
	if err != nil {
		t.Fatalf("re-marshal bundle: %v", err)
	}

	if string(first) != string(second) {
		t.Fatalf("bundle is not byte-stable across round trip:\nfirst:  %s\nsecond: %s", first, second)
	}

	if decoded.SchemaVersion != SchemaVersion {
		t.Fatalf("decoded schema_version = %q, want %q", decoded.SchemaVersion, SchemaVersion)
	}
	if decoded.Query.Surface != "api" || decoded.Query.Target != "/api/v0/services/checkout/story" {
		t.Fatalf("decoded query mismatch: %+v", decoded.Query)
	}
	if decoded.Response.Truth == nil || decoded.Response.Truth.Level != query.TruthLevelExact {
		t.Fatalf("decoded truth envelope mismatch: %+v", decoded.Response.Truth)
	}
	if decoded.Payloads != nil {
		t.Fatalf("decoded payloads = %+v, want nil for a public-profile bundle", decoded.Payloads)
	}
}

// TestBundle_UnknownSchemaVersionFailsClosed proves Validate rejects any
// schema_version other than the current wrong_answer_report.v1, mirroring the
// evidencebundle fail-closed schema check (evidencebundle/validate.go:14-15).
func TestBundle_UnknownSchemaVersionFailsClosed(t *testing.T) {
	t.Parallel()

	bundle := minimalPublicBundle(t)
	bundle.SchemaVersion = "wrong_answer_report.v2"

	if err := Validate(bundle, ValidateOptions{}); err == nil {
		t.Fatalf("Validate() error = nil, want a schema_version mismatch error")
	}
}
