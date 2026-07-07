// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationships

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	gcpv1 "github.com/eshu-hq/eshu/sdk/go/factschema/gcp/v1"
)

// emitterGCPRelationshipEnvelope builds a gcp_cloud_relationship envelope whose
// payload is produced by the SAME contracts-module encoder the collector emitter
// calls (factschema.EncodeGCPCloudRelationship in
// gcpcloud.NewCloudRelationshipEnvelope), so the test feeds the real emitter
// payload shape — the flat identity keys PLUS the collector's merged control-plane
// Attributes (collector_instance_id, parent_scope_kind, project ids,
// redaction_policy_version) — rather than a hand-built fixture. schemaVersion lets
// a caller pin the envelope's schema version to exercise the typed decode seam's
// major dispatch.
func emitterGCPRelationshipEnvelope(t *testing.T, schemaVersion string, rel gcpv1.Relationship) facts.Envelope {
	t.Helper()
	payload, err := factschema.EncodeGCPCloudRelationship(rel)
	if err != nil {
		t.Fatalf("EncodeGCPCloudRelationship: %v", err)
	}
	return facts.Envelope{
		FactKind:      facts.GCPCloudRelationshipFactKind,
		SchemaVersion: schemaVersion,
		ScopeID:       "gcp:project:demo:relationship:global",
		StableFactKey: "gcp-rel-emitter-1",
		Payload:       payload,
	}
}

func strptr(s string) *string { return &s }

// TestDiscoverGCPCloudRelationshipEvidenceFromRealEmitterEnvelope feeds the real
// collector-emitted payload shape (built through factschema.EncodeGCPCloudRelationship,
// carrying the merged control-plane Attributes the collector adds) and asserts the
// typed decode seam still extracts the correct source/target evidence and details.
// It proves the W2d conversion reads through DecodeGCPCloudRelationship against the
// genuine wire shape, not just the slim hand-built fixture the older tests used.
func TestDiscoverGCPCloudRelationshipEvidenceFromRealEmitterEnvelope(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		emitterGCPRelationshipEnvelope(t, facts.GCPCloudRelationshipSchemaVersion, gcpv1.Relationship{
			SourceFullResourceName: "//run.googleapis.com/projects/demo/locations/us-central1/services/order-gateway",
			TargetFullResourceName: "//secretmanager.googleapis.com/projects/demo/secrets/payments-service",
			RelationshipType:       "run_service_uses_secret",
			SourceAssetType:        strptr("run.googleapis.com/Service"),
			TargetAssetType:        strptr("secretmanager.googleapis.com/Secret"),
			SupportState:           strptr("supported"),
			Attributes: map[string]any{
				"collector_instance_id":    "collector-1",
				"parent_scope_kind":        "project",
				"source_project_id":        "demo",
				"target_project_id":        "demo",
				"redaction_policy_version": "1.0.0",
			},
		}),
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
	if got.SourceRepoID != "repo-orders" || got.TargetRepoID != "repo-payments" {
		t.Fatalf("source/target = %q/%q, want repo-orders/repo-payments", got.SourceRepoID, got.TargetRepoID)
	}
	if got.Details["gcp_relationship_type"] != "run_service_uses_secret" {
		t.Fatalf("gcp_relationship_type = %#v", got.Details["gcp_relationship_type"])
	}
	if got.Details["source_asset_type"] != "run.googleapis.com/Service" {
		t.Fatalf("source_asset_type = %#v, want run.googleapis.com/Service", got.Details["source_asset_type"])
	}
	if got.Details["target_asset_type"] != "secretmanager.googleapis.com/Secret" {
		t.Fatalf("target_asset_type = %#v, want secretmanager.googleapis.com/Secret", got.Details["target_asset_type"])
	}
}

// TestDiscoverGCPCloudRelationshipEvidenceRejectsUnsupportedSchemaMajor is the
// failing-first differentiator that proves the typed decode seam is exercised: a
// well-formed emitter payload carrying an unsupported schema major ("2.0.0")
// produces NO evidence, because DecodeGCPCloudRelationship rejects the major
// through decodeLatestMajor rather than best-effort reading the payload keys. A
// raw payloadString read ignores schema_version entirely and would emit evidence,
// so this test fails against the pre-conversion raw reads and passes only once the
// reads route through the contracts seam. The corpus carries only 1.0.0 gcp
// relationship facts, so this rejection changes no golden truth; it only stops a
// future unrecognized-major payload from being decoded as speculative evidence.
func TestDiscoverGCPCloudRelationshipEvidenceRejectsUnsupportedSchemaMajor(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		emitterGCPRelationshipEnvelope(t, "2.0.0", gcpv1.Relationship{
			SourceFullResourceName: "//run.googleapis.com/projects/demo/locations/us-central1/services/order-gateway",
			TargetFullResourceName: "//secretmanager.googleapis.com/projects/demo/secrets/payments-service",
			RelationshipType:       "run_service_uses_secret",
			SupportState:           strptr("supported"),
		}),
	}
	catalog := []CatalogEntry{
		{RepoID: "repo-orders", Aliases: []string{"order-gateway"}},
		{RepoID: "repo-payments", Aliases: []string{"payments-service"}},
	}

	if evidence := DiscoverEvidence(envelopes, catalog); len(evidence) != 0 {
		t.Fatalf("len = %d, want 0 for unsupported schema major: %#v", len(evidence), evidence)
	}
}

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

// TestHasSupportedGCPRelationshipFact pins the cheap O(envelopes) guard that lets
// ResolveGCPRelationshipRepoLinks skip the O(catalog) matcher build. The guard
// was dropped by the #3568 squash-merge while its only caller survived, breaking
// the build; this direct, in-package test fails to compile if it is dropped
// again, so a future squash cannot silently remove it.
func TestHasSupportedGCPRelationshipFact(t *testing.T) {
	t.Parallel()

	supported := func() facts.Envelope {
		return facts.Envelope{
			FactKind: facts.GCPCloudRelationshipFactKind,
			Payload: map[string]any{
				"source_full_resource_name": "//run.googleapis.com/projects/demo/locations/us-central1/services/order-gateway",
				"relationship_type":         "run_service_uses_secret",
				"target_full_resource_name": "//secretmanager.googleapis.com/projects/demo/secrets/payments-service",
				"support_state":             "supported",
			},
		}
	}

	tombstone := supported()
	tombstone.IsTombstone = true

	unsupported := supported()
	unsupported.Payload["support_state"] = "unsupported"

	missingSupport := supported()
	delete(missingSupport.Payload, "support_state")

	missingSource := supported()
	missingSource.Payload["source_full_resource_name"] = "   "

	missingTarget := supported()
	missingTarget.Payload["target_full_resource_name"] = ""

	missingType := supported()
	delete(missingType.Payload, "relationship_type")

	wrongKind := supported()
	wrongKind.FactKind = facts.GCPIAMPolicyObservationFactKind

	cases := []struct {
		name      string
		envelopes []facts.Envelope
		want      bool
	}{
		{name: "empty", envelopes: nil, want: false},
		{name: "supported", envelopes: []facts.Envelope{supported()}, want: true},
		{name: "tombstone skipped", envelopes: []facts.Envelope{tombstone}, want: false},
		{name: "unsupported skipped", envelopes: []facts.Envelope{unsupported}, want: false},
		{name: "missing support state skipped", envelopes: []facts.Envelope{missingSupport}, want: false},
		{name: "blank source skipped", envelopes: []facts.Envelope{missingSource}, want: false},
		{name: "blank target skipped", envelopes: []facts.Envelope{missingTarget}, want: false},
		{name: "missing relationship type skipped", envelopes: []facts.Envelope{missingType}, want: false},
		{name: "non relationship kind skipped", envelopes: []facts.Envelope{wrongKind}, want: false},
		{
			name:      "supported after skipped facts",
			envelopes: []facts.Envelope{tombstone, unsupported, supported()},
			want:      true,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := hasSupportedGCPRelationshipFact(tc.envelopes); got != tc.want {
				t.Fatalf("hasSupportedGCPRelationshipFact = %t, want %t", got, tc.want)
			}
		})
	}
}

// TestResolveGCPRelationshipRepoLinksSourceThenTarget pins the source-match-then-
// target-match repo resolution that ResolveGCPRelationshipRepoLinks emits once the
// guard admits a supported fact. It guards both the positive link and the guard's
// short-circuit: a non-GCP envelope set must resolve to no links and never build
// a catalog match.
func TestResolveGCPRelationshipRepoLinksSourceThenTarget(t *testing.T) {
	t.Parallel()

	catalog := []CatalogEntry{
		{RepoID: "repo-orders", Aliases: []string{"order-gateway"}},
		{RepoID: "repo-payments", Aliases: []string{"payments-service"}},
	}
	supported := []facts.Envelope{
		{
			FactKind: facts.GCPCloudRelationshipFactKind,
			Payload: map[string]any{
				"source_full_resource_name": "//run.googleapis.com/projects/demo/locations/us-central1/services/order-gateway",
				"relationship_type":         "run_service_uses_secret",
				"target_full_resource_name": "//secretmanager.googleapis.com/projects/demo/secrets/payments-service",
				"support_state":             "supported",
			},
		},
	}

	links := ResolveGCPRelationshipRepoLinks(supported, catalog)
	if len(links) != 1 {
		t.Fatalf("links = %d, want 1: %#v", len(links), links)
	}
	if links[0].SourceRepoID != "repo-orders" {
		t.Fatalf("source repo = %q, want repo-orders", links[0].SourceRepoID)
	}
	if links[0].TargetRepoID != "repo-payments" {
		t.Fatalf("target repo = %q, want repo-payments", links[0].TargetRepoID)
	}

	noGCP := []facts.Envelope{
		{
			FactKind: facts.GCPIAMPolicyObservationFactKind,
			Payload:  map[string]any{"repo_id": "repo-infra"},
		},
	}
	if links := ResolveGCPRelationshipRepoLinks(noGCP, catalog); len(links) != 0 {
		t.Fatalf("links = %d, want 0 when no supported GCP fact present: %#v", len(links), links)
	}
}
