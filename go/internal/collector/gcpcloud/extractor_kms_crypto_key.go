// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"fmt"
	"strings"
)

// assetTypeKMSKeyRing is the Cloud Asset Inventory asset type for a Cloud KMS
// KeyRing, the CryptoKey's typed parent. assetTypeKMSCryptoKey and
// cloudKMSResourceNamePrefix are declared by the BigQuery Table extractor in
// this package.
const assetTypeKMSKeyRing = "cloudkms.googleapis.com/KeyRing"

// relationshipTypeCryptoKeyInKeyRing is the bounded provider relationship type
// for the edge from a CryptoKey to its containing KeyRing. Cloud KMS does not
// report the KeyRing as a separate field on the CryptoKey's resource.data; it
// is derived from the CryptoKey's own resource-name path.
const relationshipTypeCryptoKeyInKeyRing = "kms_crypto_key_in_key_ring"

func init() {
	RegisterAssetExtractor(assetTypeKMSCryptoKey, extractKMSCryptoKey)
}

// kmsCryptoKeyData is the bounded view of a CAI cloudkms.googleapis.com/CryptoKey
// resource.data blob. Only redaction-safe control-plane metadata is decoded;
// Cloud KMS never returns key material on a CryptoKey resource (key material is
// not exportable for software/HSM keys and lives out-of-band for external keys),
// but this view deliberately omits any key-material-shaped field so a stray one
// in the blob cannot be surfaced.
type kmsCryptoKeyData struct {
	Name             string `json:"name"`
	Purpose          string `json:"purpose"`
	CreateTime       string `json:"createTime"`
	RotationPeriod   string `json:"rotationPeriod"`
	NextRotationTime string `json:"nextRotationTime"`
	VersionTemplate  *struct {
		ProtectionLevel string `json:"protectionLevel"`
		Algorithm       string `json:"algorithm"`
	} `json:"versionTemplate"`
	Primary *struct {
		State string `json:"state"`
	} `json:"primary"`
}

// extractKMSCryptoKey extracts bounded, redaction-safe typed depth for one Cloud
// KMS CryptoKey CAI asset. It surfaces purpose, protection level, algorithm,
// rotation schedule, primary-version state, and creation time; derives the
// containing KeyRing from the CryptoKey's own resource-name path (Cloud KMS
// reports no separate KeyRing field); and emits the typed
// kms_crypto_key_in_key_ring edge with the KeyRing full resource name as the
// correlation anchor. Cloud KMS never returns key material or key state history
// on this resource, and none is read here.
func extractKMSCryptoKey(ctx ExtractContext) (AttributeExtraction, error) {
	var data kmsCryptoKeyData
	if err := json.Unmarshal(ctx.Data, &data); err != nil {
		return AttributeExtraction{}, fmt.Errorf("decode kms crypto key data: %w", err)
	}

	attrs := kmsCryptoKeyAttributes(data)

	var anchors []string
	var rels []RelationshipObservation
	if keyRing := kmsKeyRingRelativeNameFromCryptoKeyName(data.Name); keyRing != "" {
		attrs["key_ring"] = keyRing
		keyRingFullName := cloudKMSResourceNamePrefix + keyRing
		anchors = append(anchors, keyRingFullName)
		rels = append(rels, RelationshipObservation{
			SourceFullResourceName: ctx.FullResourceName,
			SourceAssetType:        ctx.AssetType,
			RelationshipType:       relationshipTypeCryptoKeyInKeyRing,
			TargetFullResourceName: keyRingFullName,
			TargetAssetType:        assetTypeKMSKeyRing,
			SupportState:           RelationshipSupportSupported,
		})
	}

	return AttributeExtraction{
		Attributes:         attrs,
		CorrelationAnchors: dedupeNonEmpty(anchors),
		Relationships:      rels,
	}, nil
}

// kmsCryptoKeyAttributes assembles the bounded attribute map. Empty or absent
// fields are omitted rather than written as zero values so a partial CAI page
// does not fabricate a posture (for example an empty primary_state when the key
// has no primary version yet, or a rotation schedule on a purpose that does not
// rotate).
func kmsCryptoKeyAttributes(data kmsCryptoKeyData) map[string]any {
	attrs := map[string]any{}

	if v := strings.TrimSpace(data.Purpose); v != "" {
		attrs["purpose"] = v
	}
	if data.VersionTemplate != nil {
		if v := strings.TrimSpace(data.VersionTemplate.ProtectionLevel); v != "" {
			attrs["protection_level"] = v
		}
		if v := strings.TrimSpace(data.VersionTemplate.Algorithm); v != "" {
			attrs["algorithm"] = v
		}
	}
	// rotationPeriod/nextRotationTime only apply to ENCRYPT_DECRYPT keys; a
	// signing/MAC key that reports neither must not fabricate a rotation
	// schedule.
	if v := strings.TrimSpace(data.RotationPeriod); v != "" {
		attrs["rotation_period"] = v
	}
	if v, ok := normalizeRFC3339(data.NextRotationTime); ok {
		attrs["next_rotation_time"] = v
	}
	if data.Primary != nil {
		if v := strings.TrimSpace(data.Primary.State); v != "" {
			attrs["primary_state"] = v
		}
	}
	if v, ok := normalizeRFC3339(data.CreateTime); ok {
		attrs["creation_time"] = v
	}
	return attrs
}

// kmsKeyRingRelativeNameFromCryptoKeyName derives the bare, relative KeyRing
// resource name ("projects/<p>/locations/<l>/keyRings/<r>", with no CAI
// "//cloudkms.googleapis.com/" scheme prefix — the shape Terraform import and
// every sibling extractor's *_key_name/*_ring attribute uses) from a CryptoKey
// resource name (relative or already a CAI full resource name), by trimming
// the trailing /cryptoKeys/<id> segment. The caller prefixes this value with
// cloudKMSResourceNamePrefix only when building the anchor/edge target, never
// when storing it in attrs. It returns "" when the name is blank or does not
// contain a keyRings segment, so the caller emits no KeyRing edge, anchor, or
// attribute for a malformed or partial name.
func kmsKeyRingRelativeNameFromCryptoKeyName(name string) string {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return ""
	}
	relative := strings.TrimPrefix(trimmed, cloudKMSResourceNamePrefix)
	relative = strings.TrimPrefix(relative, "/")

	idx := strings.Index(relative, "/cryptoKeys/")
	if idx < 0 || !strings.Contains(relative[:idx], "/keyRings/") {
		return ""
	}
	return relative[:idx]
}
