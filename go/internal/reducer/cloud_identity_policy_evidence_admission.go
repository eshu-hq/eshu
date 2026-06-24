// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"sort"

	"github.com/eshu-hq/eshu/go/internal/correlation/cloudinventory"
)

// maxCloudIdentityPolicyEvidencePerResource caps the bounded identity-policy
// evidence carried on one canonical CloudResource read-model row.
const maxCloudIdentityPolicyEvidencePerResource = 25

// CloudIdentityPolicyEvidenceRecord is one provider identity-policy source fact
// projected into the fields the shared admission path needs to attach it to a
// canonical CloudResource identity. RawIdentity is the provider resource
// identity; principal, client, object, and tenant values are keyed
// fingerprints, never raw GUIDs.
type CloudIdentityPolicyEvidenceRecord struct {
	// Provider is the source provider token.
	Provider string
	// RawIdentity is the provider raw identity that owns the policy evidence.
	RawIdentity string
	// EvidenceKey is a stable source-fact key used for deterministic ordering
	// and deduplication. It must not contain raw principal values.
	EvidenceKey string
	// IdentityType is the bounded identity or assignment type.
	IdentityType string
	// RoleClass is a bounded role/action class when present.
	RoleClass string
	// PrincipalFingerprint is the keyed principal marker.
	PrincipalFingerprint string
	// ClientFingerprint is the keyed client marker.
	ClientFingerprint string
	// ObjectFingerprint is the keyed object marker.
	ObjectFingerprint string
	// TenantFingerprint is the keyed tenant marker.
	TenantFingerprint string
}

// CloudIdentityPolicyEvidence is the bounded identity-policy evidence persisted
// on an admitted CloudResource read-model row.
type CloudIdentityPolicyEvidence struct {
	// EvidenceKey is a stable, non-secret source evidence key.
	EvidenceKey string
	// IdentityType is the bounded identity or assignment type.
	IdentityType string
	// RoleClass is a bounded role/action class when present.
	RoleClass string
	// PrincipalFingerprint is the keyed principal marker.
	PrincipalFingerprint string
	// ClientFingerprint is the keyed client marker.
	ClientFingerprint string
	// ObjectFingerprint is the keyed object marker.
	ObjectFingerprint string
	// TenantFingerprint is the keyed tenant marker.
	TenantFingerprint string
}

// CloudIdentityPolicyEvidenceLoader loads identity-policy source facts for one
// scope generation. Implementations must bound the load to the supplied scope
// and generation so stale generations cannot leak policy evidence into a newer
// admission.
type CloudIdentityPolicyEvidenceLoader interface {
	// LoadCloudIdentityPolicyEvidence returns identity-policy evidence records in
	// scope for the generation.
	LoadCloudIdentityPolicyEvidence(
		ctx context.Context,
		scopeID string,
		generationID string,
	) ([]CloudIdentityPolicyEvidenceRecord, error)
}

// attachCloudIdentityPolicyEvidence merges identity-policy evidence onto the
// admitted resource that shares each record's cloud_resource_uid. Identity
// policy facts are not identity: a record whose uid was not admitted from
// resource evidence is dropped, so policy evidence cannot fabricate a canonical
// resource.
func attachCloudIdentityPolicyEvidence(
	resources []AdmittedCloudResource,
	records []CloudIdentityPolicyEvidenceRecord,
) {
	if len(resources) == 0 || len(records) == 0 {
		return
	}
	byUID := make(map[string]int, len(resources))
	for i := range resources {
		byUID[resources[i].CloudResourceUID] = i
	}
	for _, record := range records {
		evidence := cloudIdentityPolicyEvidenceFromRecord(record)
		if evidence.IdentityType == "" || !cloudIdentityPolicyEvidenceHasFingerprint(evidence) {
			continue
		}
		resolution := cloudinventory.ResolveProviderIdentity(record.Provider, record.RawIdentity)
		if resolution.Outcome != cloudinventory.ResolutionOutcomeAdmitted {
			continue
		}
		idx, ok := byUID[resolution.CloudResourceUID]
		if !ok {
			continue
		}
		resources[idx].IdentityPolicyEvidence = append(resources[idx].IdentityPolicyEvidence, evidence)
	}
	for i := range resources {
		normalizeCloudIdentityPolicyEvidence(&resources[i])
	}
}

func cloudIdentityPolicyEvidenceFromRecord(record CloudIdentityPolicyEvidenceRecord) CloudIdentityPolicyEvidence {
	return CloudIdentityPolicyEvidence{
		EvidenceKey:          record.EvidenceKey,
		IdentityType:         record.IdentityType,
		RoleClass:            record.RoleClass,
		PrincipalFingerprint: record.PrincipalFingerprint,
		ClientFingerprint:    record.ClientFingerprint,
		ObjectFingerprint:    record.ObjectFingerprint,
		TenantFingerprint:    record.TenantFingerprint,
	}
}

func normalizeCloudIdentityPolicyEvidence(resource *AdmittedCloudResource) {
	if len(resource.IdentityPolicyEvidence) == 0 {
		return
	}
	sort.Slice(resource.IdentityPolicyEvidence, func(i, j int) bool {
		return cloudIdentityPolicyEvidenceLess(resource.IdentityPolicyEvidence[i], resource.IdentityPolicyEvidence[j])
	})
	deduped := resource.IdentityPolicyEvidence[:0]
	seen := map[string]struct{}{}
	for _, evidence := range resource.IdentityPolicyEvidence {
		key := cloudIdentityPolicyEvidenceDedupKey(evidence)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		deduped = append(deduped, evidence)
	}
	if len(deduped) > maxCloudIdentityPolicyEvidencePerResource {
		resource.IdentityPolicyEvidence = deduped[:maxCloudIdentityPolicyEvidencePerResource]
		resource.IdentityPolicyEvidenceTruncated = true
		return
	}
	resource.IdentityPolicyEvidence = deduped
}

func cloudIdentityPolicyEvidenceLess(a, b CloudIdentityPolicyEvidence) bool {
	return cloudIdentityPolicyEvidenceDedupKey(a) < cloudIdentityPolicyEvidenceDedupKey(b)
}

func cloudIdentityPolicyEvidenceDedupKey(evidence CloudIdentityPolicyEvidence) string {
	if evidence.EvidenceKey != "" {
		return evidence.EvidenceKey
	}
	return evidence.IdentityType + "\x00" +
		evidence.RoleClass + "\x00" +
		evidence.PrincipalFingerprint + "\x00" +
		evidence.ClientFingerprint + "\x00" +
		evidence.ObjectFingerprint + "\x00" +
		evidence.TenantFingerprint
}

func cloudIdentityPolicyEvidenceHasFingerprint(evidence CloudIdentityPolicyEvidence) bool {
	return evidence.PrincipalFingerprint != "" ||
		evidence.ClientFingerprint != "" ||
		evidence.ObjectFingerprint != "" ||
		evidence.TenantFingerprint != ""
}
