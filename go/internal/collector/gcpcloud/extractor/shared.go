// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extractor

import (
	"sort"
	"strings"
	"time"
)

// CloudKMSResourceNamePrefix is the CAI full-resource-name prefix for Cloud
// KMS keys. A bare relative CMEK reference (projects/.../cryptoKeys/...) is
// normalized by prefixing it with this value.
const CloudKMSResourceNamePrefix = "//cloudkms.googleapis.com/"

// Asset types shared by extractors across service groups. Each names the CAI
// asset type of a typed edge endpoint so reducers can resolve both endpoints
// exactly. They live in this leaf because extractors in several groups derive
// edges to them.
const (
	// AssetTypeKMSCryptoKey is the CAI asset type for a Cloud KMS CryptoKey,
	// the target of CMEK encryption edges from many groups.
	AssetTypeKMSCryptoKey = "cloudkms.googleapis.com/CryptoKey"
	// AssetTypeStorageBucket is the CAI asset type for a Cloud Storage bucket,
	// the target of source-bucket and DAG-bucket edges.
	AssetTypeStorageBucket = "storage.googleapis.com/Bucket"
	// AssetTypeRunService is the CAI asset type for a Cloud Run service, the
	// target of trigger and revision-parent edges.
	AssetTypeRunService = "run.googleapis.com/Service"
	// AssetTypeRunJob is the CAI asset type for a Cloud Run job, handled
	// alongside services for container image references.
	AssetTypeRunJob = "run.googleapis.com/Job"
)

// StorageBucketResourceNamePrefixFmt is a plain string prefix, not a
// fmt.Sprintf format string, despite the "Fmt" suffix: it carries no `%` verb.
// Every caller builds a bucket full resource name by concatenating the bucket
// name directly onto this prefix, never via fmt.Sprintf.
const StorageBucketResourceNamePrefixFmt = "//storage.googleapis.com/projects/_/buckets/"

// CMEKKeyFullResourceName normalizes a customer-managed-encryption CryptoKey
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
func CMEKKeyFullResourceName(kmsKeyName string) string {
	trimmed := strings.TrimSpace(kmsKeyName)
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(trimmed, "//") {
		if strings.HasPrefix(trimmed, CloudKMSResourceNamePrefix) {
			return trimmed
		}
		return ""
	}
	return CloudKMSResourceNamePrefix + strings.TrimPrefix(trimmed, "/")
}

// DedupeNonEmpty trims, drops blanks, and deduplicates a string slice,
// preserving first-seen order. A nil or all-blank input yields nil.
func DedupeNonEmpty(input []string) []string {
	if len(input) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(input))
	out := make([]string, 0, len(input))
	for _, v := range input {
		trimmed := strings.TrimSpace(v)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// DedupeSortedNonEmpty trims, drops blanks, deduplicates, and sorts a string
// slice so an attribute set (for example the Pub/Sub message-storage regions or
// a BigQuery dataset's bounded role/principal summaries) is deterministic
// regardless of the order the API reported it. It composes the shared
// DedupeNonEmpty helper with a final sort so the dedup contract stays in one
// place.
func DedupeSortedNonEmpty(in []string) []string {
	out := DedupeNonEmpty(in)
	sort.Strings(out)
	return out
}

// FirstNonEmpty returns the first trimmed non-blank value, or "" when every
// value is blank.
func FirstNonEmpty(values ...string) string {
	for _, v := range values {
		if trimmed := strings.TrimSpace(v); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// ImageDigestFromReference extracts the digest from a container image reference
// of the form name@sha256:digest. A reference without a sha256: digest yields
// "" so the caller emits no digest anchor for untagged or tag-only references.
func ImageDigestFromReference(imageReference string) string {
	_, digest, ok := strings.Cut(strings.TrimSpace(imageReference), "@")
	if !ok {
		return ""
	}
	digest = strings.TrimSpace(digest)
	if strings.HasPrefix(digest, "sha256:") {
		return digest
	}
	return ""
}

// NormalizeRFC3339 parses a compute API RFC3339 timestamp (which carries a zone
// offset and optional fractional seconds) and returns it as an RFC3339 UTC
// string. A blank or unparseable value yields ok=false so the attribute is
// omitted rather than written as the zero time.
func NormalizeRFC3339(value string) (string, bool) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", false
	}
	parsed, err := time.Parse(time.RFC3339, trimmed)
	if err != nil {
		// Some GCP APIs (e.g. Dataplex) report sub-second createTime; RFC3339Nano
		// accepts the fractional-second form that strict RFC3339 rejects. The
		// output is still normalized to whole-second RFC3339.
		parsed, err = time.Parse(time.RFC3339Nano, trimmed)
		if err != nil {
			return "", false
		}
	}
	return parsed.UTC().Format(time.RFC3339), true
}
