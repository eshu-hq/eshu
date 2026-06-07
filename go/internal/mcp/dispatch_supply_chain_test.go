package mcp

import (
	"context"
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

func TestDispatchSBOMAggregateRepositoryScopeReturnsHTTPContractError(t *testing.T) {
	t.Parallel()

	store := &recordingMCPAggregateStore{
		count: query.SBOMAttestationAttachmentAggregateCount{
			TotalAttachments:   18,
			ByAttachmentStatus: map[string]int{"attached_verified": 18},
			ByArtifactKind:     map[string]int{"sbom": 18},
		},
	}
	handler := &query.SupplyChainHandler{SBOMAttachmentAggregates: store}
	mux := httpServeMux(handler)

	_, err := dispatchTool(
		context.Background(),
		mux,
		"count_sbom_attestation_attachments",
		map[string]any{"repository_id": "repo://example/api"},
		"",
		slog.Default(),
	)
	if err == nil {
		t.Fatal("dispatchTool() error = nil, want repository_id contract error")
	}
	if !strings.Contains(err.Error(), "repository_id") {
		t.Fatalf("dispatchTool() error = %v, want repository_id contract error", err)
	}
	if store.countCalls != 0 {
		t.Fatalf("CountSBOMAttestationAttachments called %d times, want 0", store.countCalls)
	}
}

func TestDispatchToolSBOMAttestationAttachmentsReturnsBoundedWarningPreview(t *testing.T) {
	t.Parallel()

	store := &fakeSBOMAttestationAttachmentStore{
		rows: []query.SBOMAttestationAttachmentRow{
			{
				AttachmentID:       "attachment-many-warnings",
				DocumentID:         "doc-many-warnings",
				AttachmentStatus:   "unparseable",
				ParseStatus:        "parse_failed",
				ArtifactKind:       "sbom",
				WarningSummaries:   repeatedMCPWarningSummaries(256, "lockfile parse warning"),
				SourceFreshness:    "active",
				SourceConfidence:   "reported",
				EvidenceFactIDs:    []string{"warning-fact"},
				MissingEvidence:    []string{"parseable_document"},
				AttachmentScope:    "parse_only_unanchored",
				DocumentDigest:     "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
				SubjectDigest:      "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				VerificationPolicy: "not_configured",
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
	if got, want := store.lastFilter.Limit, 2; got != want {
		t.Fatalf("store filter limit = %d, want %d", got, want)
	}
}

func TestResolveRouteMapsContainerImageIdentitiesToBoundedQuery(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("list_container_image_identities", map[string]any{
		"after_identity_id": "identity-1",
		"digest":            "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"outcome":           "tag_resolved",
		"limit":             float64(25),
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
	if got, want := route.query["after_identity_id"], "identity-1"; got != want {
		t.Fatalf("route.query[after_identity_id] = %#v, want %#v", got, want)
	}
	if got, want := route.query["limit"], "25"; got != want {
		t.Fatalf("route.query[limit] = %#v, want %#v", got, want)
	}
}

type recordingMCPAggregateStore struct {
	count      query.SBOMAttestationAttachmentAggregateCount
	countCalls int
}

func (s *recordingMCPAggregateStore) CountSBOMAttestationAttachments(
	_ context.Context,
	_ query.SBOMAttestationAttachmentAggregateFilter,
) (query.SBOMAttestationAttachmentAggregateCount, error) {
	s.countCalls++
	return s.count, nil
}

func (s *recordingMCPAggregateStore) SBOMAttestationAttachmentInventory(
	context.Context,
	query.SBOMAttestationAttachmentAggregateFilter,
	query.SBOMAttestationAttachmentInventoryDimension,
	int,
	int,
) ([]query.SBOMAttestationAttachmentInventoryRow, error) {
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
