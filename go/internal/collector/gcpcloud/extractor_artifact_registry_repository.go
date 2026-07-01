// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"fmt"
	"strings"
)

// relationshipTypeArtifactRepoEncryptedByKMSKey is the bounded provider
// relationship type for the Artifact Registry repository CMEK edge. It is the
// only typed edge a repository's own resource.data can resolve: the images and
// packages it contains reference the repository from their own child assets
// (see the DockerImage extractor's docker_image_in_repository edge) rather than
// being named here, and the project is the resource's ancestry. It is a distinct
// type from the Dataform repository CMEK edge so the two repository kinds stay
// unambiguous in graph queries.
const relationshipTypeArtifactRepoEncryptedByKMSKey = "artifact_registry_repository_encrypted_by_kms_key"

func init() {
	RegisterAssetExtractor(assetTypeArtifactRegistryRepository, extractArtifactRegistryRepository)
}

// artifactRegistryRepositoryData is the bounded view of a CAI
// artifactregistry.googleapis.com/Repository resource.data blob. Only
// redaction-safe control-plane metadata and the CMEK key reference are decoded;
// cleanup-policy bodies and remote/virtual upstream configuration details are
// reduced to a bounded count rather than surfaced.
type artifactRegistryRepositoryData struct {
	Format          string                     `json:"format"`
	Mode            string                     `json:"mode"`
	KMSKeyName      string                     `json:"kmsKeyName"`
	SizeBytes       *int64                     `json:"sizeBytes"`
	CreateTime      string                     `json:"createTime"`
	CleanupPolicies map[string]json.RawMessage `json:"cleanupPolicies"`
}

// extractArtifactRegistryRepository extracts bounded, redaction-safe typed depth
// for one Artifact Registry Repository CAI asset. It returns the
// Terraform/drift/monitoring attribute set (format, mode, size, cleanup-policy
// count, CMEK posture, and creation time), the CMEK CryptoKey as a correlation
// anchor, and the optional typed CMEK edge. Only the CryptoKey resource name (a
// control-plane identifier, not key material) leaves the parser.
func extractArtifactRegistryRepository(ctx ExtractContext) (AttributeExtraction, error) {
	var data artifactRegistryRepositoryData
	if err := json.Unmarshal(ctx.Data, &data); err != nil {
		return AttributeExtraction{}, fmt.Errorf("decode artifact registry repository data: %w", err)
	}

	attrs := artifactRegistryRepositoryAttributes(data)

	var anchors []string
	var rels []RelationshipObservation
	if kms := dataformKMSKeyFullName(data.KMSKeyName); kms != "" {
		anchors = append(anchors, kms)
		rels = append(rels, artifactRegistryRepositoryEdge(ctx, relationshipTypeArtifactRepoEncryptedByKMSKey, kms, assetTypeKMSCryptoKey))
	}

	return AttributeExtraction{
		Attributes:         attrs,
		CorrelationAnchors: dedupeNonEmpty(anchors),
		Relationships:      rels,
	}, nil
}

// artifactRegistryRepositoryAttributes assembles the bounded attribute map. Empty
// or absent fields are omitted rather than written as zero values so a partial
// CAI page does not fabricate a posture.
func artifactRegistryRepositoryAttributes(data artifactRegistryRepositoryData) map[string]any {
	attrs := map[string]any{}
	if v := strings.TrimSpace(data.Format); v != "" {
		attrs["format"] = v
	}
	if v := strings.TrimSpace(data.Mode); v != "" {
		attrs["mode"] = v
	}
	if data.SizeBytes != nil {
		attrs["size_bytes"] = *data.SizeBytes
	}
	if n := len(data.CleanupPolicies); n > 0 {
		attrs["cleanup_policy_count"] = n
	}
	if strings.TrimSpace(data.KMSKeyName) != "" {
		attrs["customer_managed_encryption"] = true
	}
	if v, ok := normalizeRFC3339(data.CreateTime); ok {
		attrs["creation_time"] = v
	}
	return attrs
}

// artifactRegistryRepositoryEdge builds one typed provider relationship
// observation anchored on the repository's CAI full resource name.
func artifactRegistryRepositoryEdge(ctx ExtractContext, relationshipType, targetName, targetType string) RelationshipObservation {
	return RelationshipObservation{
		SourceFullResourceName: ctx.FullResourceName,
		SourceAssetType:        ctx.AssetType,
		RelationshipType:       relationshipType,
		TargetFullResourceName: targetName,
		TargetAssetType:        targetType,
		SupportState:           RelationshipSupportSupported,
	}
}
