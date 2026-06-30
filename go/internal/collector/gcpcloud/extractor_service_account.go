// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/secretsiam"
)

func init() {
	RegisterAssetExtractor(serviceAccountAssetType, extractServiceAccount)
}

// serviceAccountData is the bounded view of a CAI
// iam.googleapis.com/ServiceAccount resource.data blob. Only redaction-safe
// control-plane metadata is decoded; the raw email is normalized to a digest and
// never persisted, and any key material in an (atypical) keys array is reduced to
// a bounded count. Disabled is a pointer so a present `false` (an active account,
// useful posture) is distinguishable from an absent field.
type serviceAccountData struct {
	UniqueID       string            `json:"uniqueId"`
	Email          string            `json:"email"`
	DisplayName    string            `json:"displayName"`
	OAuth2ClientID string            `json:"oauth2ClientId"`
	Disabled       *bool             `json:"disabled"`
	Keys           []json.RawMessage `json:"keys"`
}

// extractServiceAccount extracts bounded typed depth for one CAI IAM
// ServiceAccount asset. It surfaces the Terraform/drift/monitoring attribute set
// (unique id, fingerprinted email, display name, OAuth2 client id, disabled
// posture, and a bounded key count when present) and the fingerprinted email as
// the single cross-source correlation anchor.
//
// The service account's graph edges — impersonation trust, IAM member bindings,
// "resources running as it", and key sub-resources — are inbound and owned by the
// IAM/trust and image-identity layers; those layers join onto this resource node
// through the same fingerprinted-email digest. The extractor therefore derives no
// outbound relationships from the resource's own data. Raw email and any key
// material never reach the output.
func extractServiceAccount(ctx ExtractContext) (AttributeExtraction, error) {
	var data serviceAccountData
	if err := json.Unmarshal(ctx.Data, &data); err != nil {
		return AttributeExtraction{}, fmt.Errorf("decode service account data: %w", err)
	}

	attrs := map[string]any{}
	if v := strings.TrimSpace(data.UniqueID); v != "" {
		attrs["unique_id"] = v
	}
	emailDigest := secretsiam.GCPServiceAccountEmailDigest(data.Email)
	if emailDigest != "" {
		attrs["email_fingerprint"] = emailDigest
	}
	if v := strings.TrimSpace(data.DisplayName); v != "" {
		attrs["display_name"] = v
	}
	if v := strings.TrimSpace(data.OAuth2ClientID); v != "" {
		attrs["oauth2_client_id"] = v
	}
	if data.Disabled != nil {
		attrs["disabled"] = *data.Disabled
	}
	if n := len(data.Keys); n > 0 {
		attrs["key_count"] = n
	}

	var anchors []string
	if emailDigest != "" {
		anchors = []string{emailDigest}
	}

	return AttributeExtraction{
		Attributes:         attrs,
		CorrelationAnchors: anchors,
	}, nil
}
