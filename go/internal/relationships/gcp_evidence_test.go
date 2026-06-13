package relationships

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestDiscoverGCPCloudRelationshipEvidenceFromCatalogAnchoredResourceReference(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactKind:      facts.GCPCloudRelationshipFactKind,
			ScopeID:       "gcp:project:demo:relationship:global",
			StableFactKey: "gcp-rel-1",
			SourceRef: facts.Ref{
				SourceURI:      "gcp://projects/demo/relationships/run_service_uses_secret",
				SourceRecordID: "run-service|secret",
			},
			Payload: map[string]any{
				"source_full_resource_name": "//run.googleapis.com/projects/demo/locations/us-central1/services/order-gateway",
				"source_asset_type":         "run.googleapis.com/Service",
				"relationship_type":         "run_service_uses_secret",
				"target_full_resource_name": "//secretmanager.googleapis.com/projects/demo/secrets/payments-service",
				"target_asset_type":         "secretmanager.googleapis.com/Secret",
				"support_state":             "supported",
			},
		},
	}
	catalog := []CatalogEntry{
		{RepoID: "repo-orders", Aliases: []string{"order-gateway"}},
		{RepoID: "repo-payments", Aliases: []string{"payments-service"}},
	}

	evidence := DiscoverEvidence(envelopes, catalog)

	if len(evidence) != 1 {
		t.Fatalf("len = %d, want 1: %#v", len(evidence), evidence)
	}
	got := evidence[0]
	if got.EvidenceKind != EvidenceKindGCPCloudRelationship {
		t.Fatalf("kind = %q, want %q", got.EvidenceKind, EvidenceKindGCPCloudRelationship)
	}
	if got.RelationshipType != RelDependsOn {
		t.Fatalf("relationship = %q, want %q", got.RelationshipType, RelDependsOn)
	}
	if got.SourceRepoID != "repo-orders" {
		t.Fatalf("source = %q, want repo-orders", got.SourceRepoID)
	}
	if got.TargetRepoID != "repo-payments" {
		t.Fatalf("target = %q, want repo-payments", got.TargetRepoID)
	}
	if got.Confidence < DefaultConfidenceThreshold {
		t.Fatalf("confidence = %f, want at least %f", got.Confidence, DefaultConfidenceThreshold)
	}
	if got.Details["extractor"] != "gcp-cloud-relationship" {
		t.Fatalf("extractor = %#v, want gcp-cloud-relationship", got.Details["extractor"])
	}
	if got.Details["gcp_relationship_type"] != "run_service_uses_secret" {
		t.Fatalf("gcp_relationship_type = %#v", got.Details["gcp_relationship_type"])
	}

	_, resolved := Resolve(DedupeEvidenceFacts(evidence), nil, DefaultConfidenceThreshold)
	if len(resolved) != 1 {
		t.Fatalf("resolved len = %d, want 1: %#v", len(resolved), resolved)
	}
}

func TestDiscoverGCPEvidenceRequiresSourceAndTargetCatalogMatches(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactKind: facts.GCPCloudRelationshipFactKind,
			ScopeID:  "gcp:project:demo:relationship:global",
			Payload: map[string]any{
				"source_full_resource_name": "//run.googleapis.com/projects/demo/locations/us-central1/services/unmatched-runtime",
				"relationship_type":         "run_service_uses_secret",
				"target_full_resource_name": "//secretmanager.googleapis.com/projects/demo/secrets/payments-service",
				"support_state":             "supported",
			},
		},
	}
	catalog := []CatalogEntry{
		{RepoID: "repo-payments", Aliases: []string{"payments-service"}},
	}

	evidence := DiscoverEvidence(envelopes, catalog)
	if len(evidence) != 0 {
		t.Fatalf("len = %d, want 0 for one-sided GCP catalog match: %#v", len(evidence), evidence)
	}
}

func TestDiscoverGCPCloudRelationshipSkipsAmbiguousOrSelfMatches(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactKind: facts.GCPCloudRelationshipFactKind,
			ScopeID:  "gcp:project:demo:relationship:global",
			Payload: map[string]any{
				"source_full_resource_name": "//run.googleapis.com/projects/demo/locations/us-central1/services/shared-api",
				"relationship_type":         "run_service_uses_secret",
				"target_full_resource_name": "//secretmanager.googleapis.com/projects/demo/secrets/payments-service",
				"support_state":             "supported",
			},
		},
		{
			FactKind: facts.GCPCloudRelationshipFactKind,
			ScopeID:  "gcp:project:demo:relationship:global",
			Payload: map[string]any{
				"source_full_resource_name": "//run.googleapis.com/projects/demo/locations/us-central1/services/payments-service",
				"relationship_type":         "run_service_uses_secret",
				"target_full_resource_name": "//secretmanager.googleapis.com/projects/demo/secrets/payments-service",
				"support_state":             "supported",
			},
		},
	}
	catalog := []CatalogEntry{
		{RepoID: "repo-shared-a", Aliases: []string{"shared-api"}},
		{RepoID: "repo-shared-b", Aliases: []string{"shared-api"}},
		{RepoID: "repo-payments", Aliases: []string{"payments-service"}},
	}

	evidence := DiscoverEvidence(envelopes, catalog)
	if len(evidence) != 0 {
		t.Fatalf("len = %d, want 0 for ambiguous or self GCP matches: %#v", len(evidence), evidence)
	}
}

func TestDiscoverGCPNonResolverFactsDoNotEmitRelationshipEvidence(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactKind: facts.GCPIAMPolicyObservationFactKind,
			ScopeID:  "gcp:project:demo:iam:global",
			Payload: map[string]any{
				"repo_id":            "repo-infra",
				"role":               "roles/secretmanager.secretAccessor",
				"member_fingerprint": "payments-service",
			},
		},
		{
			FactKind: facts.GCPDNSRecordFactKind,
			ScopeID:  "gcp:project:demo:dns:global",
			Payload: map[string]any{
				"repo_id":                 "repo-infra",
				"record_name_fingerprint": "payments-service",
				"target_fingerprints":     []any{"payments-service"},
			},
		},
		{
			FactKind: facts.GCPImageReferenceFactKind,
			ScopeID:  "gcp:project:demo:image:global",
			Payload: map[string]any{
				"repo_id":               "repo-infra",
				"image_reference":       "us-docker.pkg.dev/demo/prod/payments-service@sha256:0123456789abcdef",
				"tag_digest_confidence": "digest",
			},
		},
		{
			FactKind: facts.GCPCloudRelationshipFactKind,
			ScopeID:  "gcp:project:demo:relationship:global",
			Payload: map[string]any{
				"source_full_resource_name": "//run.googleapis.com/projects/demo/locations/us-central1/services/order-gateway",
				"relationship_type":         "premium_only",
				"target_full_resource_name": "//secretmanager.googleapis.com/projects/demo/secrets/payments-service",
				"support_state":             "unsupported",
			},
		},
		{
			FactKind: facts.GCPCloudRelationshipFactKind,
			ScopeID:  "gcp:project:demo:relationship:global",
			Payload: map[string]any{
				"source_full_resource_name": "//run.googleapis.com/projects/demo/locations/us-central1/services/order-gateway",
				"relationship_type":         "run_service_uses_secret",
				"target_full_resource_name": "//secretmanager.googleapis.com/projects/demo/secrets/payments-service",
			},
		},
	}
	catalog := []CatalogEntry{
		{RepoID: "repo-orders", Aliases: []string{"order-gateway"}},
		{RepoID: "repo-payments", Aliases: []string{"payments-service"}},
	}

	evidence := DiscoverEvidence(envelopes, catalog)
	if len(evidence) != 0 {
		t.Fatalf("len = %d, want 0 for non-resolver GCP facts: %#v", len(evidence), evidence)
	}
}
