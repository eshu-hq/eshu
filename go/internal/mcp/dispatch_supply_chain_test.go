package mcp

import "testing"

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
