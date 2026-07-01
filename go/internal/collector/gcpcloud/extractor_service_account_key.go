// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/secretsiam"
)

// serviceAccountKeyAssetType is the Cloud Asset Inventory asset type for a GCP
// IAM ServiceAccount key. It is declared here so the extractor registration
// stays self-contained in this file. The parent ServiceAccount endpoint reuses
// serviceAccountAssetType already declared for the ServiceAccount extractor.
const serviceAccountKeyAssetType = "iam.googleapis.com/ServiceAccountKey"

// relationshipTypeServiceAccountKeyOf is the bounded provider relationship type
// for the edge from a key to its parent ServiceAccount. The reducer materializes
// the edge only when both endpoints resolve exactly.
const relationshipTypeServiceAccountKeyOf = "service_account_key_of"

// serviceAccountKeyKeysMarker separates a ServiceAccount full resource name from
// its per-key suffix in a ServiceAccountKey full resource name
// (`.../serviceAccounts/<email>/keys/<id>`).
const serviceAccountKeyKeysMarker = "/keys/"

func init() {
	RegisterAssetExtractor(serviceAccountKeyAssetType, extractServiceAccountKey)
}

// serviceAccountKeyData is the bounded view of a CAI
// iam.googleapis.com/ServiceAccountKey resource.data blob. Only redaction-safe
// control-plane posture is decoded; private and public key material
// (privateKeyData, publicKeyData, privateKeyType) is deliberately never a field
// so a stray one in the blob cannot be surfaced. Disabled is a pointer so a
// present `false` (an active key, useful posture) is distinguishable from an
// absent field.
type serviceAccountKeyData struct {
	KeyType         string `json:"keyType"`
	KeyAlgorithm    string `json:"keyAlgorithm"`
	KeyOrigin       string `json:"keyOrigin"`
	ValidAfterTime  string `json:"validAfterTime"`
	ValidBeforeTime string `json:"validBeforeTime"`
	Disabled        *bool  `json:"disabled"`
}

// extractServiceAccountKey extracts bounded, redaction-safe typed depth for one
// CAI IAM ServiceAccountKey asset. It surfaces the Terraform/drift/monitoring
// attribute set (key type, algorithm, origin, validity window for age/rotation
// posture, disabled posture, and the fingerprinted parent service-account email),
// derives the parent ServiceAccount as the single correlation anchor
// (fingerprinted email) plus the typed service_account_key_of edge to the parent
// ServiceAccount resource. Private and public key material is never read.
func extractServiceAccountKey(ctx ExtractContext) (AttributeExtraction, error) {
	var data serviceAccountKeyData
	if err := json.Unmarshal(ctx.Data, &data); err != nil {
		return AttributeExtraction{}, fmt.Errorf("decode service account key data: %w", err)
	}

	attrs := map[string]any{}
	if v := strings.TrimSpace(data.KeyType); v != "" {
		attrs["key_type"] = v
	}
	if v := strings.TrimSpace(data.KeyAlgorithm); v != "" {
		attrs["key_algorithm"] = v
	}
	if v := strings.TrimSpace(data.KeyOrigin); v != "" {
		attrs["key_origin"] = v
	}
	if v, ok := normalizeRFC3339(data.ValidAfterTime); ok {
		attrs["valid_after_time"] = v
	}
	if v, ok := normalizeRFC3339(data.ValidBeforeTime); ok {
		attrs["valid_before_time"] = v
	}
	if data.Disabled != nil {
		attrs["disabled"] = *data.Disabled
	}

	var anchors []string
	var rels []RelationshipObservation
	if parent := parentServiceAccountFullName(ctx.FullResourceName); parent != "" {
		if digest := secretsiam.GCPServiceAccountEmailDigest(serviceAccountEmailFromFullName(parent)); digest != "" {
			attrs["parent_service_account_email_fingerprint"] = digest
			anchors = append(anchors, digest)
		}
		rels = append(rels, RelationshipObservation{
			SourceFullResourceName: ctx.FullResourceName,
			SourceAssetType:        ctx.AssetType,
			RelationshipType:       relationshipTypeServiceAccountKeyOf,
			TargetFullResourceName: parent,
			TargetAssetType:        serviceAccountAssetType,
			SupportState:           RelationshipSupportSupported,
		})
	}

	return AttributeExtraction{
		Attributes:         attrs,
		CorrelationAnchors: anchors,
		Relationships:      rels,
	}, nil
}

// parentServiceAccountFullName derives the parent ServiceAccount full resource
// name from a ServiceAccountKey full resource name by trimming the `/keys/<id>`
// suffix. It returns "" when the input does not carry a key suffix, or when the
// marker has no key id after it (a malformed `.../keys/` name), so the caller
// emits no parent edge or anchor from a name that does not actually identify a
// key.
func parentServiceAccountFullName(keyFullName string) string {
	trimmed := strings.TrimSpace(keyFullName)
	index := strings.LastIndex(trimmed, serviceAccountKeyKeysMarker)
	if index < 0 || index+len(serviceAccountKeyKeysMarker) >= len(trimmed) {
		return ""
	}
	return trimmed[:index]
}
