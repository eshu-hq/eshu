package mcp

import "testing"

func TestResolveRouteMapsSBOMAttestationAttachmentsToBoundedQuery(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("list_sbom_attestation_attachments", map[string]any{
		"after_attachment_id": "attachment-1",
		"attachment_status":   "attached_verified",
		"limit":               float64(25),
		"subject_digest":      "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
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
	if got, want := route.query["attachment_status"], "attached_verified"; got != want {
		t.Fatalf("route.query[attachment_status] = %#v, want %#v", got, want)
	}
	if got, want := route.query["limit"], "25"; got != want {
		t.Fatalf("route.query[limit] = %#v, want %#v", got, want)
	}
}
