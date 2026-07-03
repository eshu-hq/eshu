// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"fmt"
	"strings"
)

func init() {
	RegisterAssetExtractor(assetTypeKMSKeyRing, extractKMSKeyRing)
}

// kmsKeyRingData is the bounded view of a CAI cloudkms.googleapis.com/KeyRing
// resource.data blob. Per Cloud KMS v1's projects.locations.keyRings REST
// reference, the KeyRing resource carries exactly two fields — name and
// createTime — and no encryption, label, or child-key field of its own;
// CryptoKeys are a separate CAI asset type (cloudkms.googleapis.com/CryptoKey,
// handled by extractor_kms_crypto_key.go) that reference their KeyRing
// parent, not the reverse.
type kmsKeyRingData struct {
	Name       string `json:"name"`
	CreateTime string `json:"createTime"`
}

// extractKMSKeyRing extracts bounded, redaction-safe typed depth for one
// Cloud KMS KeyRing CAI asset. It surfaces the location derived from the
// resource-name path (projects/*/locations/*/keyRings/*; Cloud KMS reports no
// separate location field) and creation time, falling back to the asset's
// own ctx.FullResourceName when resource.data.name is blank or fails to
// parse a keyRings segment (a sparse or stripped CAI data blob must not
// silently drop the location), mirroring the CryptoKey extractor's
// KeyRing-derivation fallback. It emits no outbound edges or correlation
// anchors: the KeyRing's graph value is inbound only — every contained
// CryptoKey already resolves the kms_crypto_key_in_key_ring edge toward this
// asset type from the CryptoKey side, mirroring the Custom IAM Role and SSL
// Certificate extractors' inbound-only edge shape.
func extractKMSKeyRing(ctx ExtractContext) (AttributeExtraction, error) {
	var data kmsKeyRingData
	if err := json.Unmarshal(ctx.Data, &data); err != nil {
		return AttributeExtraction{}, fmt.Errorf("decode kms key ring data: %w", err)
	}

	attrs := map[string]any{}

	location := kmsKeyRingLocationFromName(data.Name)
	if location == "" {
		location = kmsKeyRingLocationFromName(ctx.FullResourceName)
	}
	if location != "" {
		attrs["location"] = location
	}
	if v, ok := normalizeRFC3339(data.CreateTime); ok {
		attrs["creation_time"] = v
	}

	return AttributeExtraction{Attributes: attrs}, nil
}

// kmsKeyRingLocationFromName derives the location segment
// ("projects/<p>/locations/<l>/keyRings/<r>" -> "<l>") from a KeyRing
// resource name (relative or already a CAI full resource name). It returns
// "" when the name is blank or does not contain a recognizable
// locations/.../keyRings path, so the caller emits no location attribute for
// a malformed or partial name rather than fabricating one.
func kmsKeyRingLocationFromName(name string) string {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return ""
	}
	relative := strings.TrimPrefix(trimmed, cloudKMSResourceNamePrefix)
	relative = strings.TrimPrefix(relative, "/")

	const locationsSegment = "/locations/"
	locIdx := strings.Index(relative, locationsSegment)
	if locIdx < 0 {
		return ""
	}
	rest := relative[locIdx+len(locationsSegment):]

	const keyRingsSegment = "/keyRings/"
	keyRingIdx := strings.Index(rest, keyRingsSegment)
	if keyRingIdx < 0 {
		return ""
	}
	return rest[:keyRingIdx]
}
