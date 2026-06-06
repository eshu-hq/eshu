package mcp

import (
	"context"
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
