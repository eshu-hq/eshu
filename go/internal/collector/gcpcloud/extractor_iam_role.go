// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// iamRoleAssetType is the Cloud Asset Inventory asset type for a GCP custom IAM
// Role. It is declared here so the extractor registration stays self-contained
// in this file.
const iamRoleAssetType = "iam.googleapis.com/Role"

// sensitiveIAMPermissions is the bounded set of exact IAM permission strings that
// grant privilege escalation directly (service-account impersonation, key
// minting, and role authoring). Any permission ending in ".setIamPolicy" is also
// treated as sensitive; that suffix check lives in isSensitiveIAMPermission so
// the exact-match set stays small and auditable.
var sensitiveIAMPermissions = map[string]struct{}{
	"iam.serviceAccounts.actAs":              {},
	"iam.serviceAccounts.getAccessToken":     {},
	"iam.serviceAccounts.getOpenIdToken":     {},
	"iam.serviceAccounts.implicitDelegation": {},
	"iam.serviceAccounts.signBlob":           {},
	"iam.serviceAccounts.signJwt":            {},
	"iam.serviceAccountKeys.create":          {},
	"iam.roles.create":                       {},
	"iam.roles.update":                       {},
}

func init() {
	RegisterAssetExtractor(iamRoleAssetType, extractIAMRole)
}

// iamRoleData is the bounded view of a CAI iam.googleapis.com/Role resource.data
// blob. Only redaction-safe control-plane metadata is decoded. The opaque etag is
// reduced to a stable fingerprint and never persisted raw; the permission list is
// reduced to bounded counts and flags rather than surfaced verbatim. Deleted is a
// pointer so a present `false` (an active role, useful posture) is distinguishable
// from an absent field.
type iamRoleData struct {
	Title               string   `json:"title"`
	Stage               string   `json:"stage"`
	IncludedPermissions []string `json:"includedPermissions"`
	Etag                string   `json:"etag"`
	Deleted             *bool    `json:"deleted"`
}

// extractIAMRole extracts bounded, redaction-safe typed depth for one CAI custom
// IAM Role asset. It surfaces the Terraform/drift/monitoring attribute set (title,
// launch stage, included-permission count, sensitive-permission count and a
// privilege-escalation flag, deleted posture, and a fingerprinted etag).
//
// The role's graph edges — the members bound to it and its owning project/org —
// are inbound and owned by the IAM/binding and ancestry layers, which join onto
// this resource node through the role identity and the ancestry already carried
// on the base observation. The extractor therefore derives no outbound
// relationships or anchors from the resource's own data. The raw etag never
// reaches the output.
func extractIAMRole(ctx ExtractContext) (AttributeExtraction, error) {
	var data iamRoleData
	if err := json.Unmarshal(ctx.Data, &data); err != nil {
		return AttributeExtraction{}, fmt.Errorf("decode iam role data: %w", err)
	}

	attrs := map[string]any{}
	if v := strings.TrimSpace(data.Title); v != "" {
		attrs["title"] = v
	}
	if v := strings.TrimSpace(data.Stage); v != "" {
		attrs["stage"] = v
	}
	// Emit the count only when permissions are present; a zero count on a partial
	// CAI page would fabricate a "0 permissions" posture.
	if n := len(data.IncludedPermissions); n > 0 {
		attrs["included_permission_count"] = n
		if sensitive := countSensitiveIAMPermissions(data.IncludedPermissions); sensitive > 0 {
			attrs["sensitive_permission_count"] = sensitive
			attrs["grants_privilege_escalation"] = true
		}
	}
	if data.Deleted != nil {
		attrs["deleted"] = *data.Deleted
	}
	if fp := iamRoleEtagFingerprint(data.Etag); fp != "" {
		attrs["etag_fingerprint"] = fp
	}

	return AttributeExtraction{Attributes: attrs}, nil
}

// countSensitiveIAMPermissions returns the number of privilege-escalation-relevant
// permissions in the role's permission list.
func countSensitiveIAMPermissions(permissions []string) int {
	count := 0
	for _, perm := range permissions {
		if isSensitiveIAMPermission(perm) {
			count++
		}
	}
	return count
}

// isSensitiveIAMPermission reports whether a single IAM permission grants
// privilege escalation. It matches the bounded exact-string set plus any
// permission that sets an IAM policy (the ".setIamPolicy" suffix), which lets a
// principal grant itself further access.
func isSensitiveIAMPermission(permission string) bool {
	trimmed := strings.TrimSpace(permission)
	if trimmed == "" {
		return false
	}
	if _, ok := sensitiveIAMPermissions[trimmed]; ok {
		return true
	}
	return strings.HasSuffix(trimmed, ".setIamPolicy")
}

// iamRoleEtagFingerprint reduces the opaque role etag to a stable, redaction-safe
// digest. The etag is a concurrency token, not sensitive, but it is opaque and
// carries no operator value verbatim; fingerprinting it keeps drift-detectable
// change signal without persisting the raw token. It returns "" for a blank etag
// so the caller omits the attribute.
func iamRoleEtagFingerprint(etag string) string {
	trimmed := strings.TrimSpace(etag)
	if trimmed == "" {
		return ""
	}
	return "sha256:" + facts.StableID("GCPCloudIAMRoleEtag", map[string]any{
		"etag": trimmed,
	})
}
