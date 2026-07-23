// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
)

func TestResolveRouteMapsSBOMAttestationAttachmentsToBoundedQuery(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("list_sbom_attestation_attachments", map[string]any{
		"after_attachment_id": "attachment-1",
		"attachment_status":   "attached_verified",
		"digest":              "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		"limit":               float64(25),
		"repository_id":       "repo://example/api",
		"service_id":          "service:example-api",
		"subject_digest":      "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"workload_id":         "workload:example-api",
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "GET"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/supply-chain/sbom-attestations/attachments"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
	if got, want := route.query["subject_digest"], "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"; got != want {
		t.Fatalf("route.query[subject_digest] = %#v, want %#v", got, want)
	}
	if got, want := route.query["digest"], "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"; got != want {
		t.Fatalf("route.query[digest] = %#v, want %#v", got, want)
	}
	if got, want := route.query["repository_id"], "repo://example/api"; got != want {
		t.Fatalf("route.query[repository_id] = %#v, want %#v", got, want)
	}
	if got, want := route.query["workload_id"], "workload:example-api"; got != want {
		t.Fatalf("route.query[workload_id] = %#v, want %#v", got, want)
	}
	if got, want := route.query["service_id"], "service:example-api"; got != want {
		t.Fatalf("route.query[service_id] = %#v, want %#v", got, want)
	}
	if got, want := route.query["attachment_status"], "attached_verified"; got != want {
		t.Fatalf("route.query[attachment_status] = %#v, want %#v", got, want)
	}
	if got, want := route.query["limit"], "25"; got != want {
		t.Fatalf("route.query[limit] = %#v, want %#v", got, want)
	}
}

func TestResolveRouteForwardsSBOMRepositoryScopeToHTTPContract(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		tool string
		args map[string]any
	}{
		{
			tool: "list_sbom_attestation_attachments",
			args: map[string]any{"repository_id": "repo://example/api", "limit": float64(25)},
		},
		{
			tool: "count_sbom_attestation_attachments",
			args: map[string]any{"repository_id": "repo://example/api"},
		},
		{
			tool: "get_sbom_attestation_attachment_inventory",
			args: map[string]any{"repository_id": "repo://example/api", "limit": float64(25)},
		},
	} {
		tc := tc
		t.Run(tc.tool, func(t *testing.T) {
			t.Parallel()

			route, err := resolveRoute(tc.tool, tc.args)
			if err != nil {
				t.Fatalf("resolveRoute() error = %v, want nil", err)
			}
			if got, want := route.query["repository_id"], "repo://example/api"; got != want {
				t.Fatalf("route.query[repository_id] = %#v, want %#v", got, want)
			}
		})
	}
}

func TestDispatchSBOMAggregateRepositoryScopeReturnsScopedCount(t *testing.T) {
	t.Parallel()

	store := &recordingMCPAggregateStore{
		count: query.SBOMAttestationAttachmentAggregateCount{
			TotalAttachments:   0,
			ByAttachmentStatus: map[string]int{},
			ByArtifactKind:     map[string]int{},
		},
	}
	handler := &query.SupplyChainHandler{SBOMAttachmentAggregates: store}
	mux := httpServeMux(handler)

	result, err := dispatchTool(
		context.Background(),
		mux,
		"count_sbom_attestation_attachments",
		map[string]any{"repository_id": "repo://example/api"},
		"",
		slog.Default(),
	)
	if err != nil {
		t.Fatalf("dispatchTool() error = %v, want nil", err)
	}
	if store.countCalls != 1 {
		t.Fatalf("CountSBOMAttestationAttachments called %d times, want 1", store.countCalls)
	}
	if got, want := store.lastFilter.RepositoryID, "repo://example/api"; got != want {
		t.Fatalf("RepositoryID = %q, want %q", got, want)
	}
	envelope, ok := result.Value.(*query.ResponseEnvelope)
	if !ok {
		t.Fatalf("result.Value = %T, want query.ResponseEnvelope", result.Value)
	}
	data, ok := envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("envelope.Data = %T, want map[string]any", envelope.Data)
	}
	scope, ok := data["scope"].(map[string]any)
	if !ok {
		t.Fatalf("scope = %T, want map[string]any; data=%#v", data["scope"], data)
	}
	if got, want := scope["repository_id"], "repo://example/api"; got != want {
		t.Fatalf("scope.repository_id = %q, want %q", got, want)
	}
}

func TestDispatchToolSBOMAttestationAttachmentsReturnsBoundedWarningPreview(t *testing.T) {
	t.Parallel()

	store := &fakeSBOMAttestationAttachmentStore{
		rows: []query.SBOMAttestationAttachmentRow{
			{
				AttachmentID:               "attachment-many-warnings",
				DocumentID:                 "doc-many-warnings",
				AttachmentStatus:           "unparseable",
				ParseStatus:                "parse_failed",
				ArtifactKind:               "sbom",
				WarningSummaries:           repeatedMCPWarningSummaries(256, "lockfile parse warning"),
				SourceFreshness:            "active",
				SourceConfidence:           "reported",
				EvidenceFactIDs:            []string{"warning-fact"},
				MissingEvidence:            []string{"parseable_document"},
				AttachmentScope:            "parse_only_unanchored",
				ComponentCount:             1,
				ComponentEvidence:          []query.ComponentEvidenceRow{{ComponentID: "component-warning", FactID: "component-fact"}},
				ComponentEvidenceTruncated: false,
				DocumentDigest:             "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
				SubjectDigest:              "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				VerificationPolicy:         "not_configured",
			},
		},
	}
	mux := http.NewServeMux()
	handler := &query.SupplyChainHandler{
		SBOMAttachments: store,
		Profile:         query.ProfileProduction,
	}
	handler.Mount(mux)

	result, err := dispatchTool(
		context.Background(),
		mux,
		"list_sbom_attestation_attachments",
		map[string]any{
			"document_id": "doc-many-warnings",
			"limit":       float64(1),
		},
		"",
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err != nil {
		t.Fatalf("dispatchTool() error = %v, want nil", err)
	}
	if result.Envelope == nil {
		t.Fatal("dispatchTool() envelope is nil, want SBOM attachment envelope")
	}
	data, ok := result.Envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("envelope.Data = %T, want map[string]any", result.Envelope.Data)
	}
	attachments, ok := data["attachments"].([]any)
	if !ok {
		t.Fatalf("attachments = %T, want []any", data["attachments"])
	}
	if got, want := len(attachments), 1; got != want {
		t.Fatalf("len(attachments) = %d, want %d", got, want)
	}
	row, ok := attachments[0].(map[string]any)
	if !ok {
		t.Fatalf("attachment = %T, want map[string]any", attachments[0])
	}
	warnings, ok := row["warning_summaries"].([]any)
	if !ok {
		t.Fatalf("warning_summaries = %T, want []any", row["warning_summaries"])
	}
	if got, want := len(warnings), 1; got != want {
		t.Fatalf("len(warning_summaries) = %d, want %d", got, want)
	}
	if got, want := warnings[0], "lockfile parse warning"; got != want {
		t.Fatalf("warning_summaries[0] = %#v, want %#v", got, want)
	}
	if got, want := row["warning_summary_count"], float64(256); got != want {
		t.Fatalf("warning_summary_count = %#v, want %#v", got, want)
	}
	if got, want := row["warning_summaries_truncated"], true; got != want {
		t.Fatalf("warning_summaries_truncated = %#v, want %#v", got, want)
	}
	if got, want := row["component_evidence_truncated"], false; got != want {
		t.Fatalf("component_evidence_truncated = %#v, want %#v", got, want)
	}
	if got, want := store.lastFilter.Limit, 2; got != want {
		t.Fatalf("store filter limit = %d, want %d", got, want)
	}
}

// TestDispatchToolSBOMAttestationAttachmentsSurfacesComponentEvidenceTruncation
// proves the MCP envelope retains the reducer/query component preview's full
// count, bounded rows, and overflow signal without a separate MCP-only shape.
func TestDispatchToolSBOMAttestationAttachmentsSurfacesComponentEvidenceTruncation(t *testing.T) {
	t.Parallel()

	components := make([]query.ComponentEvidenceRow, 100)
	for i := range components {
		components[i] = query.ComponentEvidenceRow{
			ComponentID: fmt.Sprintf("component-%03d", i),
			FactID:      fmt.Sprintf("component-fact-%03d", i),
		}
	}
	store := &fakeSBOMAttestationAttachmentStore{
		rows: []query.SBOMAttestationAttachmentRow{
			{
				AttachmentID:               "attachment-many-components",
				DocumentID:                 "doc-many-components",
				AttachmentStatus:           "attached_parse_only",
				ArtifactKind:               "sbom",
				ComponentCount:             101,
				ComponentEvidence:          components,
				ComponentEvidenceTruncated: true,
			},
		},
	}
	handler := &query.SupplyChainHandler{
		SBOMAttachments: store,
		Profile:         query.ProfileProduction,
	}
	mux := httpServeMux(handler)

	result, err := dispatchTool(
		context.Background(),
		mux,
		"list_sbom_attestation_attachments",
		map[string]any{
			"document_id": "doc-many-components",
			"limit":       float64(1),
		},
		"",
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err != nil {
		t.Fatalf("dispatchTool() error = %v, want nil", err)
	}
	if result.Envelope == nil {
		t.Fatal("dispatchTool() envelope is nil, want SBOM attachment envelope")
	}
	data, ok := result.Envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("envelope.Data = %T, want map[string]any", result.Envelope.Data)
	}
	attachments, ok := data["attachments"].([]any)
	if !ok || len(attachments) != 1 {
		t.Fatalf("attachments = %#v, want one row", data["attachments"])
	}
	row, ok := attachments[0].(map[string]any)
	if !ok {
		t.Fatalf("attachment = %T, want map[string]any", attachments[0])
	}
	componentEvidence, ok := row["component_evidence"].([]any)
	if !ok || len(componentEvidence) != 100 {
		t.Fatalf("component_evidence len = %d, want 100", len(componentEvidence))
	}
	if got, want := row["component_count"], float64(101); got != want {
		t.Fatalf("component_count = %#v, want %#v", got, want)
	}
	if got, want := row["component_evidence_truncated"], true; got != want {
		t.Fatalf("component_evidence_truncated = %#v, want %#v", got, want)
	}
}

// TestDispatchToolSBOMAttestationAttachmentsSurfacesSLSAProvenance proves the
// MCP tool response surfaces the joined attestation.slsa_provenance evidence
// (#5371) through the same query.SBOMAttestationAttachmentResult conversion
// the HTTP route uses, so the MCP and HTTP surfaces agree on this field.
func TestDispatchToolSBOMAttestationAttachmentsSurfacesSLSAProvenance(t *testing.T) {
	t.Parallel()

	store := &fakeSBOMAttestationAttachmentStore{
		rows: []query.SBOMAttestationAttachmentRow{
			{
				AttachmentID:                "attachment-slsa",
				DocumentID:                  "stmt-slsa",
				AttachmentStatus:            "attached_verified",
				ArtifactKind:                "attestation",
				SLSAProvenancePredicateType: "https://slsa.dev/provenance/v1",
				SLSAProvenanceBuilderID:     "https://github.com/actions/runner/v1",
			},
		},
	}
	mux := http.NewServeMux()
	handler := &query.SupplyChainHandler{
		SBOMAttachments: store,
		Profile:         query.ProfileProduction,
	}
	handler.Mount(mux)

	result, err := dispatchTool(
		context.Background(),
		mux,
		"list_sbom_attestation_attachments",
		map[string]any{
			"document_id": "stmt-slsa",
			"limit":       float64(1),
		},
		"",
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err != nil {
		t.Fatalf("dispatchTool() error = %v, want nil", err)
	}
	if result.Envelope == nil {
		t.Fatal("dispatchTool() envelope is nil, want SBOM attachment envelope")
	}
	data, ok := result.Envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("envelope.Data = %T, want map[string]any", result.Envelope.Data)
	}
	attachments, ok := data["attachments"].([]any)
	if !ok {
		t.Fatalf("attachments = %T, want []any", data["attachments"])
	}
	if got, want := len(attachments), 1; got != want {
		t.Fatalf("len(attachments) = %d, want %d", got, want)
	}
	row, ok := attachments[0].(map[string]any)
	if !ok {
		t.Fatalf("attachment = %T, want map[string]any", attachments[0])
	}
	if got, want := row["slsa_provenance_predicate_type"], "https://slsa.dev/provenance/v1"; got != want {
		t.Fatalf("slsa_provenance_predicate_type = %#v, want %#v", got, want)
	}
	if got, want := row["slsa_provenance_builder_id"], "https://github.com/actions/runner/v1"; got != want {
		t.Fatalf("slsa_provenance_builder_id = %#v, want %#v", got, want)
	}
}

func TestResolveRouteMapsContainerImageIdentitiesToBoundedQuery(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("list_container_image_identities", map[string]any{
		"after_identity_id":    "identity-1",
		"digest":               "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"outcome":              "tag_resolved",
		"source_repository_id": "repo://example/api",
		"limit":                float64(25),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "GET"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/supply-chain/container-images/identities"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
	if got, want := route.query["digest"], "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"; got != want {
		t.Fatalf("route.query[digest] = %#v, want %#v", got, want)
	}
	if got, want := route.query["outcome"], "tag_resolved"; got != want {
		t.Fatalf("route.query[outcome] = %#v, want %#v", got, want)
	}
	if got, want := route.query["source_repository_id"], "repo://example/api"; got != want {
		t.Fatalf("route.query[source_repository_id] = %#v, want %#v", got, want)
	}
	if got, want := route.query["after_identity_id"], "identity-1"; got != want {
		t.Fatalf("route.query[after_identity_id] = %#v, want %#v", got, want)
	}
	if got, want := route.query["limit"], "25"; got != want {
		t.Fatalf("route.query[limit] = %#v, want %#v", got, want)
	}
}

func TestContainerImageIdentityToolSchemaAdvertisesSourceRepositoryScope(t *testing.T) {
	t.Parallel()

	var tool ToolDefinition
	for _, candidate := range supplyChainTools() {
		if candidate.Name == "list_container_image_identities" {
			tool = candidate
			break
		}
	}
	if tool.Name == "" {
		t.Fatal("list_container_image_identities tool not found")
	}
	schema := tool.InputSchema.(map[string]any)
	properties := schema["properties"].(map[string]any)
	sourceRepository := properties["source_repository_id"].(map[string]any)
	description := sourceRepository["description"].(string)
	for _, want := range []string{"source repository", "not an OCI"} {
		if !strings.Contains(description, want) {
			t.Fatalf("source_repository_id description = %q, want %q", description, want)
		}
	}
}

type recordingMCPAggregateStore struct {
	count      query.SBOMAttestationAttachmentAggregateCount
	lastFilter query.SBOMAttestationAttachmentAggregateFilter
	countCalls int
}

func (s *recordingMCPAggregateStore) CountSBOMAttestationAttachments(
	_ context.Context,
	filter query.SBOMAttestationAttachmentAggregateFilter,
) (query.SBOMAttestationAttachmentAggregateCount, error) {
	s.countCalls++
	s.lastFilter = filter
	return s.count, nil
}

func (s *recordingMCPAggregateStore) SBOMAttestationAttachmentInventory(
	_ context.Context,
	filter query.SBOMAttestationAttachmentAggregateFilter,
	_ query.SBOMAttestationAttachmentInventoryDimension,
	_ int,
	_ int,
) ([]query.SBOMAttestationAttachmentInventoryRow, error) {
	s.lastFilter = filter
	return nil, nil
}

func httpServeMux(handler *query.SupplyChainHandler) *http.ServeMux {
	mux := http.NewServeMux()
	handler.Mount(mux)
	return mux
}

type fakeSBOMAttestationAttachmentStore struct {
	rows       []query.SBOMAttestationAttachmentRow
	lastFilter query.SBOMAttestationAttachmentFilter
}

func (s *fakeSBOMAttestationAttachmentStore) ListSBOMAttestationAttachments(
	_ context.Context,
	filter query.SBOMAttestationAttachmentFilter,
) (query.SBOMAttestationAttachmentPage, error) {
	s.lastFilter = filter
	return query.SBOMAttestationAttachmentPage{
		Attachments: append([]query.SBOMAttestationAttachmentRow(nil), s.rows...),
	}, nil
}

func repeatedMCPWarningSummaries(count int, summary string) []string {
	warnings := make([]string, count)
	for i := range warnings {
		warnings[i] = summary
	}
	return warnings
}
