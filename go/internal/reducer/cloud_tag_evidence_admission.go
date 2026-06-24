// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/correlation/cloudinventory"
)

// CloudTagEvidenceRecord is one tag-evidence source fact (e.g.
// azure_tag_observation) projected into the fields the shared admission path
// needs to attach its fingerprints to a canonical CloudResource identity.
// RawIdentity is the provider raw identity (the tag subject) that resolves into
// the same cloud_resource_uid as the resource it describes.
type CloudTagEvidenceRecord struct {
	// Provider is the source provider token (aws, gcp, azure, ...).
	Provider string
	// RawIdentity is the tag subject's provider raw identity, resolved to a uid.
	RawIdentity string
	// TagValueFingerprints maps tag key -> keyed value fingerprint marker. Tag
	// value text is never present; the collector already fingerprinted it.
	TagValueFingerprints map[string]string
}

// CloudTagEvidenceLoader loads tag-evidence source facts for one scope
// generation. Implementations must bound the load to the supplied scope and
// generation so stale generations cannot leak tag evidence into a newer
// admission.
type CloudTagEvidenceLoader interface {
	// LoadCloudTagEvidence returns the tag-evidence records in scope for the
	// generation.
	LoadCloudTagEvidence(
		ctx context.Context,
		scopeID string,
		generationID string,
	) ([]CloudTagEvidenceRecord, error)
}

// attachCloudTagEvidence merges tag-evidence fingerprints onto the admitted
// resource that shares each record's cloud_resource_uid. Tag evidence is never
// identity: a record whose uid was not admitted from resource evidence is
// dropped, so tags can never fabricate a canonical resource. Records that fail
// to resolve a uid (ambiguous/unsupported/unresolved) are skipped. When several
// records share a uid, their fingerprint maps merge (later keys win), which is
// deterministic because the loader returns rows in a stable order.
func attachCloudTagEvidence(resources []AdmittedCloudResource, records []CloudTagEvidenceRecord) {
	if len(resources) == 0 || len(records) == 0 {
		return
	}
	byUID := make(map[string]int, len(resources))
	for i := range resources {
		byUID[resources[i].CloudResourceUID] = i
	}
	for _, record := range records {
		if len(record.TagValueFingerprints) == 0 {
			continue
		}
		resolution := cloudinventory.ResolveProviderIdentity(record.Provider, record.RawIdentity)
		if resolution.Outcome != cloudinventory.ResolutionOutcomeAdmitted {
			continue
		}
		idx, ok := byUID[resolution.CloudResourceUID]
		if !ok {
			// No resource fact admitted this identity; tags are not identity.
			continue
		}
		if resources[idx].TagValueFingerprints == nil {
			resources[idx].TagValueFingerprints = make(map[string]string, len(record.TagValueFingerprints))
		}
		for key, marker := range record.TagValueFingerprints {
			resources[idx].TagValueFingerprints[key] = marker
		}
	}
}
