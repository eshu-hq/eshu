// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cloud_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/cloud"
)

func TestGCPFactKindRegistry(t *testing.T) {
	kinds := cloud.GCPFactKinds()
	want := []string{
		cloud.GCPCloudResourceFactKind,
		cloud.GCPCollectionWarningFactKind,
		cloud.GCPCloudRelationshipFactKind,
		cloud.GCPTagObservationFactKind,
		cloud.GCPIAMPolicyObservationFactKind,
		cloud.GCPDNSRecordFactKind,
		cloud.GCPImageReferenceFactKind,
	}
	if len(kinds) != len(want) {
		t.Fatalf("len(cloud.GCPFactKinds()) = %d, want %d", len(kinds), len(want))
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("cloud.GCPFactKinds()[%d] = %q, want %q", i, kinds[i], want[i])
		}
		version, ok := cloud.GCPSchemaVersion(kinds[i])
		if !ok {
			t.Fatalf("cloud.GCPSchemaVersion(%q) ok = false", kinds[i])
		}
		// gcp_cloud_resource is at 1.1.0 since it gained the typed-depth
		// attributes/correlation_anchors fields; the other GCP fact kinds remain
		// at their initial 1.0.0 schema.
		wantVersion := "1.0.0"
		if kinds[i] == cloud.GCPCloudResourceFactKind {
			wantVersion = "1.1.0"
		}
		if version != wantVersion {
			t.Fatalf("cloud.GCPSchemaVersion(%q) = %q, want %q", kinds[i], version, wantVersion)
		}
	}

	kinds[0] = "mutated"
	if got := cloud.GCPFactKinds()[0]; got != cloud.GCPCloudResourceFactKind {
		t.Fatalf("cloud.GCPFactKinds returned mutable backing slice, got first kind %q", got)
	}
}

func TestGCPSchemaVersionUnknownKind(t *testing.T) {
	if _, ok := cloud.GCPSchemaVersion("gcp_not_a_kind"); ok {
		t.Fatal("cloud.GCPSchemaVersion(unknown) ok = true, want false")
	}
}

func TestGCPFactKindsAreCore(t *testing.T) {
	core := facts.CoreFactKinds()
	for _, kind := range cloud.GCPFactKinds() {
		if !slices.Contains(core, kind) {
			t.Fatalf("facts.CoreFactKinds() missing GCP kind %q", kind)
		}
		if !facts.IsCoreFactKind(kind) {
			t.Fatalf("facts.IsCoreFactKind(%q) = false, want true", kind)
		}
	}
}

type gcpFactDecision string

const (
	gcpFactDecisionConsumed       gcpFactDecision = "consumed"
	gcpFactDecisionProvenanceOnly gcpFactDecision = "provenance_only"
)

type gcpFactConsumptionDecision struct {
	FactKind  string
	Decision  gcpFactDecision
	Consumer  string
	Rationale string
}

func gcpFactConsumptionDecisions() []gcpFactConsumptionDecision {
	return []gcpFactConsumptionDecision{
		{
			FactKind:  cloud.GCPCloudResourceFactKind,
			Decision:  gcpFactDecisionConsumed,
			Consumer:  "cloud inventory, runtime drift, and gcp_resource_materialization reducers",
			Rationale: "resource identity is admitted into the shared cloud_resource_uid keyspace before graph nodes or readback truth are presented",
		},
		{
			FactKind:  cloud.GCPCollectionWarningFactKind,
			Decision:  gcpFactDecisionProvenanceOnly,
			Consumer:  "fact-store audit evidence and collector telemetry counters",
			Rationale: "collection warnings explain partial or unsupported provider coverage and must not mint graph, inventory, DNS, or IAM truth",
		},
		{
			FactKind:  cloud.GCPCloudRelationshipFactKind,
			Decision:  gcpFactDecisionConsumed,
			Consumer:  "gcp_relationship_materialization reducer",
			Rationale: "relationships are written only after both endpoints resolve to committed GCP CloudResource nodes in the allowed scope",
		},
		{
			FactKind:  cloud.GCPTagObservationFactKind,
			Decision:  gcpFactDecisionConsumed,
			Consumer:  "cloud tag evidence loader and cloud inventory readback",
			Rationale: "tag evidence attaches only to already-admitted CloudResource identities and never admits a resource by itself",
		},
		{
			FactKind:  cloud.GCPIAMPolicyObservationFactKind,
			Decision:  gcpFactDecisionProvenanceOnly,
			Consumer:  "fact-store audit evidence; secrets/IAM reducers consume derived GCP IAM source facts",
			Rationale: "raw IAM binding observations stay policy provenance while gcp_iam_principal, gcp_iam_permission_policy, and gcp_iam_trust_policy carry admitted correlation input",
		},
		{
			FactKind:  facts.GCPIAMPrincipalFactKind,
			Decision:  gcpFactDecisionConsumed,
			Consumer:  "secrets_iam_trust_chain reducer",
			Rationale: "service-account principal evidence joins GCP permission and trust facts through a redaction-safe member fingerprint",
		},
		{
			FactKind:  facts.GCPIAMTrustPolicyFactKind,
			Decision:  gcpFactDecisionConsumed,
			Consumer:  "secrets_iam_trust_chain reducer",
			Rationale: "service-account impersonation trust evidence is admitted only when downstream GCP or Kubernetes anchors prove the chain",
		},
		{
			FactKind:  facts.GCPIAMPermissionPolicyFactKind,
			Decision:  gcpFactDecisionConsumed,
			Consumer:  "secrets_iam_trust_chain reducer",
			Rationale: "permission grants provide the role and resource capability side of admitted GCP secrets/IAM trust paths",
		},
		{
			FactKind:  cloud.GCPDNSRecordFactKind,
			Decision:  gcpFactDecisionProvenanceOnly,
			Consumer:  "fact-store audit evidence",
			Rationale: "DNS records remain redaction-safe provenance until a DNS read model or resolver contract admits them",
		},
		{
			FactKind:  cloud.GCPImageReferenceFactKind,
			Decision:  gcpFactDecisionConsumed,
			Consumer:  "container_image_identity reducer",
			Rationale: "digest-first image evidence can join existing OCI tag evidence before entering deployment or vulnerability paths",
		},
	}
}

func TestGCPFactKindsHaveConsumptionDecisions(t *testing.T) {
	decisions := gcpFactConsumptionDecisions()
	kinds := gcpFactKindsRequiringConsumptionDecision()
	if got, want := len(decisions), len(kinds); got != want {
		t.Fatalf("gcpFactConsumptionDecisions() len = %d, want %d", got, want)
	}

	seen := map[string]gcpFactConsumptionDecision{}
	for _, decision := range decisions {
		if !slices.Contains(kinds, decision.FactKind) {
			t.Fatalf("consumption decision covers unknown GCP fact kind %q", decision.FactKind)
		}
		if decision.Consumer == "" {
			t.Fatalf("consumption decision for %q has empty consumer", decision.FactKind)
		}
		if decision.Rationale == "" {
			t.Fatalf("consumption decision for %q has empty rationale", decision.FactKind)
		}
		switch decision.Decision {
		case gcpFactDecisionConsumed, gcpFactDecisionProvenanceOnly:
		default:
			t.Fatalf("consumption decision for %q has invalid decision %q", decision.FactKind, decision.Decision)
		}
		if _, ok := seen[decision.FactKind]; ok {
			t.Fatalf("duplicate consumption decision for %q", decision.FactKind)
		}
		seen[decision.FactKind] = decision
	}
	for _, kind := range kinds {
		if _, ok := seen[kind]; !ok {
			t.Fatalf("missing consumption decision for GCP fact kind %q", kind)
		}
	}

	for _, kind := range []string{cloud.GCPIAMPolicyObservationFactKind, cloud.GCPDNSRecordFactKind} {
		if got := seen[kind].Decision; got != gcpFactDecisionProvenanceOnly {
			t.Fatalf("%s decision = %q, want %q", kind, got, gcpFactDecisionProvenanceOnly)
		}
	}
}

func gcpFactKindsRequiringConsumptionDecision() []string {
	kinds := cloud.GCPFactKinds()
	for _, kind := range facts.SecretsIAMFactKinds() {
		if strings.HasPrefix(kind, "gcp_") && !slices.Contains(kinds, kind) {
			kinds = append(kinds, kind)
		}
	}
	return kinds
}
