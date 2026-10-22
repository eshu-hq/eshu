// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/gcpcloud/catalog"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

// The redaction policy version and reasons (catalog leaf, #6696 slice L)
// identify the GCP cloud collector redaction policy attached to every emitted
// fact payload. The rotation invariant lives with the constant: rotating the
// underlying redact.Key material MUST bump catalog.RedactionPolicyVersion.

// FingerprintLabelValues returns a copy of labels with the values of keys named
// in fingerprintKeys replaced by deterministic keyed redaction markers. Label
// values outside fingerprintKeys are preserved verbatim as bounded source
// evidence. The raw value is never retained.
func FingerprintLabelValues(labels map[string]string, fingerprintKeys []string, key redact.Key) map[string]string {
	if len(labels) == 0 {
		return nil
	}
	fingerprint := make(map[string]struct{}, len(fingerprintKeys))
	for _, k := range fingerprintKeys {
		fingerprint[k] = struct{}{}
	}
	out := make(map[string]string, len(labels))
	for k, v := range labels {
		if _, ok := fingerprint[k]; ok {
			out[k] = redact.String(v, catalog.RedactionReasonLabel, "gcp_label:"+k, key).Marker
			continue
		}
		out[k] = v
	}
	return out
}

// MemberClass returns the bounded principal class for a Cloud IAM member binding
// such as "user:alice@example.com" or "serviceAccount:svc@proj.iam". Public and
// authenticated special members map to "public" and "authenticated". An
// unrecognized or blank member maps to "unknown". The class is safe as a
// telemetry label.
func MemberClass(member string) string {
	trimmed := strings.TrimSpace(member)
	if trimmed == "" {
		return catalog.UnknownLabel
	}
	if after, ok := strings.CutPrefix(trimmed, "deleted:"); ok {
		trimmed = after
	}
	switch trimmed {
	case "allUsers":
		return "public"
	case "allAuthenticatedUsers":
		return "authenticated"
	}
	prefix, _, ok := strings.Cut(trimmed, ":")
	if !ok || prefix == "" {
		return catalog.UnknownLabel
	}
	switch prefix {
	case "user", "group", "serviceAccount", "domain", "principal", "principalSet":
		return prefix
	default:
		return catalog.UnknownLabel
	}
}

// FingerprintMember returns a deterministic keyed redaction marker for a Cloud
// IAM member identity. The raw user, group, domain, or service-account identity
// is never retained; reducers join on the class plus fingerprint, never on the
// raw email.
func FingerprintMember(member string, key redact.Key) string {
	return redact.String(strings.TrimSpace(member), catalog.RedactionReasonMember, "gcp_iam_member:"+MemberClass(member), key).Marker
}
