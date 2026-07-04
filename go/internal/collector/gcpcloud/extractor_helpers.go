// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"sort"
	"strings"
)

// cmekKeyFullResourceName normalizes a customer-managed-encryption CryptoKey
// reference into a Cloud KMS CAI full resource name shared by every extractor
// whose CMEK field carries a bare relative key name (projects/.../cryptoKeys/...)
// or an already-normalized CAI full name (//cloudkms.googleapis.com/...).
//
// It applies the strict-domain contract:
//
//   - a blank reference yields "" so the caller emits no encryption edge;
//   - an absolute name (//-prefixed) is returned unchanged ONLY when it carries
//     the Cloud KMS prefix; an absolute name for any other service domain is
//     rejected with "" so a typo or malformed asset can never poison the anchor
//     or edge with a non-KMS endpoint;
//   - a bare relative name is prefixed with the Cloud KMS resource-name prefix,
//     trimming a single leading "/" so the prefix is never doubled.
//
// For every valid CAI input — a bare relative name or a proper
// //cloudkms.googleapis.com/ full name — the result is identical to the
// per-extractor copies this helper replaces; only a malformed or wrong-domain
// absolute name, which real Cloud Asset Inventory never emits for a CMEK field,
// is now rejected rather than surfaced. Callers that layer additional shape
// validation on a bare name (see sqlInstanceKMSKeyFullName) wrap this helper
// rather than reimplement the prefix logic.
//
// This is distinct from the self-link-parsing kmsCryptoKeyFullName in
// extractor_disk.go, which extracts a projects/ segment out of a compute
// self-link and strips a trailing /cryptoKeyVersions/ suffix; that helper serves
// a different input contract and is intentionally not merged here.
func cmekKeyFullResourceName(kmsKeyName string) string {
	trimmed := strings.TrimSpace(kmsKeyName)
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(trimmed, "//") {
		if strings.HasPrefix(trimmed, cloudKMSResourceNamePrefix) {
			return trimmed
		}
		return ""
	}
	return cloudKMSResourceNamePrefix + strings.TrimPrefix(trimmed, "/")
}

// dedupeSortedNonEmpty trims, drops blanks, deduplicates, and sorts a string
// slice so an attribute set (for example the Pub/Sub message-storage regions or
// a BigQuery dataset's bounded role/principal summaries) is deterministic
// regardless of the order the API reported it. It composes the shared
// dedupeNonEmpty helper with a final sort so the dedup contract stays in one
// place.
func dedupeSortedNonEmpty(in []string) []string {
	out := dedupeNonEmpty(in)
	sort.Strings(out)
	return out
}
