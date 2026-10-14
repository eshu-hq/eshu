// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ociruntime

import (
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// ociReferenceDigest is one scanned reference and the manifest digest the
// registry reported for it. Digest digests exist only after buildEnvelopes
// fetches manifests, so the scan records them there and the freshness hint is
// attached to the generation post-scan.
type ociReferenceDigest struct {
	reference string
	digest    string
	// digestWarning is empty when the digest came from the registry response,
	// WarningComputedManifestDigest when it was computed from manifest bytes,
	// or WarningMissingManifestDigest when no digest was available.
	digestWarning string
}

// ociFreshnessHint returns the stable content digest for one repository scan
// (#8003): the sorted reference-to-digest pairs, the per-reference digest
// provenance (so a digest-missing warning cannot masquerade as an unchanged
// tick), and whether the tag list was truncated. It never covers observation
// timestamps, so identical registry state at different ticks shares one hint
// and the store can skip the duplicate generation.
func ociFreshnessHint(references []ociReferenceDigest, tagListTruncated bool) string {
	ordered := slices.Clone(references)
	slices.SortFunc(ordered, func(a, b ociReferenceDigest) int {
		return strings.Compare(a.reference, b.reference)
	})
	entries := make([]map[string]any, 0, len(ordered))
	for _, ref := range ordered {
		entries = append(entries, map[string]any{
			"digest":         ref.digest,
			"digest_warning": ref.digestWarning,
			"reference":      ref.reference,
		})
	}
	return facts.StableID("oci-registry.collection.freshness", map[string]any{
		"references":         entries,
		"tag_list_truncated": tagListTruncated,
	})
}
