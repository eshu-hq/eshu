// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

type recordingPackageRegistryCorrelationStore struct {
	rows       []PackageRegistryCorrelationRow
	lastFilter PackageRegistryCorrelationFilter
}

// ListPackageRegistryCorrelations is deliberately a thin handler-plumbing
// fake: it computes truncated := len(rows) > filter.Limit directly against
// its pre-decoded rows slice rather than modeling
// PostgresPackageRegistryCorrelationStore's real "+1 lookahead fetch, then
// decode only the visible window" contract (buildPackageRegistryCorrelationPage
// takes a raw fetchLimit and derives Truncated from the RAW fetched fact
// count, not from a comparison against the already-decoded row slice). That
// is fine for the handler-wiring tests this fake serves -- request
// parsing, filter threading, cursor round-tripping, response shape -- but it
// cannot exercise the decode-drop-inside-the-window failure class (a fact
// present in the raw fetch that fails typed decode) because it has no raw,
// undecoded facts to drop. rawFactPackageRegistryCorrelationStore
// (package_registry_correlations_pagination_test.go) and
// candidateFactPackageRegistryCorrelationStore
// (package_registry_scoped_access_windowfactcount_test.go) are the faithful
// fakes for pagination and authz-gate semantics: both hold raw fact bytes and
// route them through the real buildPackageRegistryCorrelationPage. Prefer
// those whenever a test needs to prove Truncated/NextCursorCorrelationID/
// WindowFactCount behavior rather than just handler plumbing. WindowFactCount
// is set to len(rows) (the post-truncation row count): this fake holds
// already-decoded rows with no undecodable facts, so the raw fetched count
// and the decoded row count are truthfully identical here (#5461/#5816
// WindowFactCount finding).
func (s *recordingPackageRegistryCorrelationStore) ListPackageRegistryCorrelations(
	_ context.Context,
	filter PackageRegistryCorrelationFilter,
) (PackageRegistryCorrelationPage, error) {
	s.lastFilter = filter
	rows := append([]PackageRegistryCorrelationRow(nil), s.rows...)
	truncated := filter.Limit > 0 && len(rows) > filter.Limit
	if truncated {
		rows = rows[:filter.Limit]
	}
	page := PackageRegistryCorrelationPage{Rows: rows, Truncated: truncated, WindowFactCount: len(rows)}
	if truncated && len(rows) > 0 {
		page.NextCursorCorrelationID = rows[len(rows)-1].CorrelationID
	}
	return page, nil
}

func TestPackageRegistryListCorrelationsRequiresScopeAndLimit(t *testing.T) {
	t.Parallel()

	handler := &PackageRegistryHandler{Correlations: &recordingPackageRegistryCorrelationStore{}}
	mux := http.NewServeMux()
	handler.Mount(mux)

	for _, target := range []string{
		"/api/v0/package-registry/correlations?limit=10",
		"/api/v0/package-registry/correlations?package_id=pkg:npm://registry.example/team-api",
	} {
		target := target
		t.Run(target, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodGet, target, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if got, want := w.Code, http.StatusBadRequest; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
			}
		})
	}
}

func TestPackageRegistryListCorrelationsUsesBoundedPostgresStore(t *testing.T) {
	t.Parallel()

	store := &recordingPackageRegistryCorrelationStore{
		rows: []PackageRegistryCorrelationRow{
			{
				CorrelationID:    "correlation-1",
				RelationshipKind: "publication",
				PackageID:        "pkg:npm://registry.example/team-api",
				VersionID:        "pkg:npm://registry.example/team-api@1.2.0",
				RepositoryID:     "repo-team-api",
				RepositoryName:   "team-api",
				Outcome:          "exact",
				Reason:           "source hint matches repository remote exactly",
				ProvenanceOnly:   true,
			},
			{CorrelationID: "correlation-2", RelationshipKind: "ownership"},
		},
	}
	handler := &PackageRegistryHandler{Correlations: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/package-registry/correlations?package_id=pkg:npm://registry.example/team-api&relationship_kind=publication&limit=1",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if got, want := store.lastFilter.PackageID, "pkg:npm://registry.example/team-api"; got != want {
		t.Fatalf("PackageID = %q, want %q", got, want)
	}
	if got, want := store.lastFilter.RelationshipKind, "publication"; got != want {
		t.Fatalf("RelationshipKind = %q, want %q", got, want)
	}
	if got, want := store.lastFilter.Limit, 1; got != want {
		t.Fatalf("Limit = %d, want %d (the handler no longer pre-adds a +1 lookahead; the store computes it internally)", got, want)
	}

	var resp struct {
		Correlations []PackageRegistryCorrelationResult `json:"correlations"`
		Count        int                                `json:"count"`
		Limit        int                                `json:"limit"`
		Truncated    bool                               `json:"truncated"`
		NextCursor   map[string]string                  `json:"next_cursor"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got, want := len(resp.Correlations), 1; got != want {
		t.Fatalf("len(correlations) = %d, want %d", got, want)
	}
	if got, want := resp.Correlations[0].VersionID, "pkg:npm://registry.example/team-api@1.2.0"; got != want {
		t.Fatalf("VersionID = %q, want %q", got, want)
	}
	if got, want := resp.Correlations[0].RepositoryID, "repo-team-api"; got != want {
		t.Fatalf("RepositoryID = %q, want %q", got, want)
	}
	if !resp.Truncated {
		t.Fatal("truncated = false, want true")
	}
	if got, want := resp.NextCursor["after_correlation_id"], "correlation-1"; got != want {
		t.Fatalf("next_cursor.after_correlation_id = %q, want %q", got, want)
	}
}

func TestPackageRegistryCorrelationQueryExcludesTombstones(t *testing.T) {
	t.Parallel()

	if !strings.Contains(listPackageRegistryCorrelationsQuery, "fact.is_tombstone = FALSE") {
		t.Fatalf("listPackageRegistryCorrelationsQuery must exclude tombstone facts:\n%s", listPackageRegistryCorrelationsQuery)
	}
}

func TestPackageRegistryCorrelationQuerySupportsBatchedPackageIDs(t *testing.T) {
	t.Parallel()

	if !strings.Contains(listPackageRegistryCorrelationsQuery, "fact.payload->>'package_id' = ANY($9::text[])") {
		t.Fatalf("listPackageRegistryCorrelationsQuery must batch on package_id = ANY for the dependency-chain publisher read:\n%s", listPackageRegistryCorrelationsQuery)
	}
}

func TestPackageRegistryCorrelationQuerySupportsRelationshipKindsFilter(t *testing.T) {
	t.Parallel()

	// The $10 relationship_kind filter must appear BEFORE the LIMIT clause so
	// that the bounded page for the dependency-chain phase-2 publisher read
	// contains only publisher-kind rows (publication/ownership). Without this
	// WHERE predicate, a popular package with many consumer rows could fill the
	// page before any publisher rows appear, silently dropping them.
	if !strings.Contains(listPackageRegistryCorrelationsQuery, "fact.payload->>'relationship_kind' = ANY($10::text[])") {
		t.Fatalf("listPackageRegistryCorrelationsQuery must filter on relationship_kind = ANY($10) before LIMIT:\n%s", listPackageRegistryCorrelationsQuery)
	}
}

func TestPackageRegistryCorrelationQueryIncludesPublicationFacts(t *testing.T) {
	t.Parallel()

	if !stringSliceContains(packageRegistryCorrelationFactKinds(), packagePublicationCorrelationFactKind) {
		t.Fatalf("packageRegistryCorrelationFactKinds() = %#v, want publication facts", packageRegistryCorrelationFactKinds())
	}
}

// TestPackageRegistryCorrelationQuerySelectsFactKindAndSchemaVersion proves
// the SELECT list carries fact.fact_kind and fact.schema_version alongside
// fact.payload — the typed decode path (#5461) needs the kind to dispatch to
// the matching factschema Decode* seam and the schema_version to thread
// through packageCorrelationSchemaEnvelope so a future-major fact
// dead-letters instead of silently decoding as v1.
func TestPackageRegistryCorrelationQuerySelectsFactKindAndSchemaVersion(t *testing.T) {
	t.Parallel()

	if !strings.Contains(listPackageRegistryCorrelationsQuery, "fact.fact_kind") {
		t.Fatalf("listPackageRegistryCorrelationsQuery must select fact.fact_kind for the typed decode dispatch:\n%s", listPackageRegistryCorrelationsQuery)
	}
	if !strings.Contains(listPackageRegistryCorrelationsQuery, "fact.schema_version") {
		t.Fatalf("listPackageRegistryCorrelationsQuery must select fact.schema_version for the typed decode seam:\n%s", listPackageRegistryCorrelationsQuery)
	}
}

// TestDecodePackageRegistryCorrelationRowTypedSeam proves
// decodePackageRegistryCorrelationRow decodes each of the three governed
// package correlation kinds through the typed factschema seam
// (factschema_decode_package_correlations.go) into exactly the fields the
// pre-existing raw StringVal/BoolVal/IntVal/StringSliceVal path used to
// produce (#5461, output-preserving refactor). Each want value below is the
// same field set decodePackageRegistryCorrelationRow read from the raw
// payload map before this change; a field the raw path never read for a
// given kind (for example Ecosystem on an ownership row) stays at its zero
// value here too, matching the raw path's StringVal("")/nil-slice fallback
// for an absent key.
func TestDecodePackageRegistryCorrelationRowTypedSeam(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		factID   string
		factKind string
		payload  map[string]any
		want     PackageRegistryCorrelationRow
	}{
		{
			name:     "ownership",
			factID:   "fact-ownership-1",
			factKind: packageOwnershipCorrelationFactKind,
			payload: map[string]any{
				"package_id":               "pkg:npm://registry.example/team-api",
				"relationship_kind":        "ownership",
				"version_id":               "pkg:npm://registry.example/team-api@1.2.0",
				"hint_kind":                "repository_field",
				"source_url":               "https://github.com/example/team-api",
				"repository_id":            "repo-team-api",
				"repository_name":          "team-api",
				"candidate_repository_ids": []string{"repo-team-api", "repo-team-api-mirror"},
				"outcome":                  "exact",
				"reason":                   "source hint matches repository remote exactly",
				"provenance_only":          true,
				"canonical_writes":         2,
				"evidence_fact_ids":        []string{"fact-1", "fact-2"},
			},
			want: PackageRegistryCorrelationRow{
				CorrelationID:          "fact-ownership-1",
				RelationshipKind:       "ownership",
				PackageID:              "pkg:npm://registry.example/team-api",
				VersionID:              "pkg:npm://registry.example/team-api@1.2.0",
				RepositoryID:           "repo-team-api",
				RepositoryName:         "team-api",
				SourceURL:              "https://github.com/example/team-api",
				CandidateRepositoryIDs: []string{"repo-team-api", "repo-team-api-mirror"},
				Outcome:                "exact",
				Reason:                 "source hint matches repository remote exactly",
				ProvenanceOnly:         true,
				CanonicalWrites:        2,
				EvidenceFactIDs:        []string{"fact-1", "fact-2"},
			},
		},
		{
			name:     "consumption",
			factID:   "fact-consumption-1",
			factKind: packageConsumptionCorrelationFactKind,
			payload: map[string]any{
				"package_id":        "pkg:npm://registry.example/team-api",
				"relationship_kind": "consumption",
				"ecosystem":         "npm",
				"package_name":      "team-api",
				"repository_id":     "repo-consumer",
				"repository_name":   "consumer-service",
				"relative_path":     "package.json",
				"manifest_section":  "dependencies",
				"dependency_range":  "^1.2.0",
				"outcome":           "exact",
				"reason":            "manifest range admits published version",
				"provenance_only":   false,
				"canonical_writes":  1,
				"evidence_fact_ids": []string{"fact-3"},
			},
			want: PackageRegistryCorrelationRow{
				CorrelationID:    "fact-consumption-1",
				RelationshipKind: "consumption",
				PackageID:        "pkg:npm://registry.example/team-api",
				Ecosystem:        "npm",
				PackageName:      "team-api",
				RepositoryID:     "repo-consumer",
				RepositoryName:   "consumer-service",
				RelativePath:     "package.json",
				ManifestSection:  "dependencies",
				DependencyRange:  "^1.2.0",
				Outcome:          "exact",
				Reason:           "manifest range admits published version",
				ProvenanceOnly:   false,
				CanonicalWrites:  1,
				EvidenceFactIDs:  []string{"fact-3"},
			},
		},
		{
			name:     "publication",
			factID:   "fact-publication-1",
			factKind: packagePublicationCorrelationFactKind,
			payload: map[string]any{
				"package_id":               "pkg:npm://registry.example/team-api",
				"relationship_kind":        "publication",
				"version_id":               "pkg:npm://registry.example/team-api@1.2.0",
				"version":                  "1.2.0",
				"published_at":             "2026-01-02T03:04:05Z",
				"source_url":               "https://github.com/example/team-api",
				"repository_id":            "repo-team-api",
				"repository_name":          "team-api",
				"candidate_repository_ids": []string{"repo-team-api"},
				"outcome":                  "exact",
				"reason":                   "publish metadata source URL matches repository remote",
				"provenance_only":          true,
				"canonical_writes":         0,
				"evidence_fact_ids":        []string{"fact-4"},
			},
			want: PackageRegistryCorrelationRow{
				CorrelationID:          "fact-publication-1",
				RelationshipKind:       "publication",
				PackageID:              "pkg:npm://registry.example/team-api",
				VersionID:              "pkg:npm://registry.example/team-api@1.2.0",
				Version:                "1.2.0",
				PublishedAt:            "2026-01-02T03:04:05Z",
				RepositoryID:           "repo-team-api",
				RepositoryName:         "team-api",
				SourceURL:              "https://github.com/example/team-api",
				CandidateRepositoryIDs: []string{"repo-team-api"},
				Outcome:                "exact",
				Reason:                 "publish metadata source URL matches repository remote",
				ProvenanceOnly:         true,
				CanonicalWrites:        0,
				EvidenceFactIDs:        []string{"fact-4"},
			},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			payloadBytes, err := json.Marshal(tc.payload)
			if err != nil {
				t.Fatalf("json.Marshal: %v", err)
			}

			got, ok, err := decodePackageRegistryCorrelationRow(tc.factID, tc.factKind, "1.0.0", payloadBytes)
			if err != nil {
				t.Fatalf("decodePackageRegistryCorrelationRow: unexpected error = %v", err)
			}
			if !ok {
				t.Fatalf("decodePackageRegistryCorrelationRow: ok = false, want true")
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("decodePackageRegistryCorrelationRow() = %#v, want %#v", got, tc.want)
			}
		})
	}
}

// TestDecodePackageRegistryCorrelationRowDefaultsEmptySchemaVersion proves an
// empty schema_version (a version-less legacy row) still decodes through the
// typed seam by normalizing to queryDefaultSchemaMajorVersion, matching every
// other package_registry_correlations query decode wrapper's documented
// default.
func TestDecodePackageRegistryCorrelationRowDefaultsEmptySchemaVersion(t *testing.T) {
	t.Parallel()

	payloadBytes, err := json.Marshal(map[string]any{
		"package_id":        "pkg:npm://registry.example/team-api",
		"relationship_kind": "ownership",
	})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	got, ok, err := decodePackageRegistryCorrelationRow("fact-ownership-2", packageOwnershipCorrelationFactKind, "", payloadBytes)
	if err != nil {
		t.Fatalf("decodePackageRegistryCorrelationRow: unexpected error = %v", err)
	}
	if !ok {
		t.Fatalf("decodePackageRegistryCorrelationRow: ok = false, want true")
	}
	if got.PackageID != "pkg:npm://registry.example/team-api" {
		t.Fatalf("PackageID = %q, want %q", got.PackageID, "pkg:npm://registry.example/team-api")
	}
}

// TestDecodePackageRegistryCorrelationRowDropsMissingPackageID proves a fact
// missing its required package_id identity field is dropped (ok=false,
// err=nil) rather than decoded into an empty-identity row. This is new
// behavior versus the pre-#5461 raw StringVal path, which had no concept of a
// "required" field and would have silently returned PackageID="" — the
// #4784 ADR's "missing required fields dead-letter, they never silently zero
// out" rule applied to this read path for the first time.
func TestDecodePackageRegistryCorrelationRowDropsMissingPackageID(t *testing.T) {
	t.Parallel()

	payloadBytes, err := json.Marshal(map[string]any{
		"relationship_kind": "ownership",
		"repository_id":     "repo-team-api",
	})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	got, ok, err := decodePackageRegistryCorrelationRow("fact-missing-package-id", packageOwnershipCorrelationFactKind, "1.0.0", payloadBytes)
	if err != nil {
		t.Fatalf("decodePackageRegistryCorrelationRow: unexpected error = %v", err)
	}
	if ok {
		t.Fatalf("decodePackageRegistryCorrelationRow: ok = true, want false for a fact missing package_id; got = %#v", got)
	}
}
