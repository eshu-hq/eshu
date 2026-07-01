// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// Asset type constants for the compute Persistent Disk typed-depth extractor and
// the relationship endpoints it derives. Each target asset type names the CAI
// asset type of an edge target so the reducer can resolve both endpoints exactly
// before materializing the edge.
//
// assetTypeKMSCryptoKey and cloudKMSResourceNamePrefix are declared by the
// BigQuery Table extractor in this package and reused here for the disk
// encryption-key edge.
const (
	assetTypeComputeDisk     = "compute.googleapis.com/Disk"
	assetTypeComputeInstance = "compute.googleapis.com/Instance"
	assetTypeComputeImage    = "compute.googleapis.com/Image"
	assetTypeComputeSnapshot = "compute.googleapis.com/Snapshot"
)

// Bounded provider relationship types for the Persistent Disk edges carried on
// gcp_cloud_relationship facts. The reducer materializes each edge only when
// both endpoints resolve exactly.
const (
	relationshipTypeDiskAttachedToInstance  = "disk_attached_to_instance"
	relationshipTypeDiskCreatedFromImage    = "disk_created_from_image"
	relationshipTypeDiskCreatedFromSnapshot = "disk_created_from_snapshot"
	relationshipTypeDiskEncryptedByKey      = "disk_encrypted_by_key"
)

func init() {
	RegisterAssetExtractor(assetTypeComputeDisk, extractDisk)
}

// diskData is the bounded view of a CAI compute.googleapis.com/Disk resource.data
// blob. Only redaction-safe control-plane metadata and resource references are
// decoded. Integer fields arrive as JSON strings (the compute API convention) or
// as bare numbers, so they are decoded as raw JSON and normalized. The disk
// encryption key is decoded only for its key *name* (a resource identifier, not
// key material); the sha256/rawKey fields the compute API may return are never
// decoded, per the GCP collector contract Payload Boundaries.
type diskData struct {
	Zone                   string          `json:"zone"`
	Region                 string          `json:"region"`
	SizeGb                 json.RawMessage `json:"sizeGb"`
	Type                   string          `json:"type"`
	Status                 string          `json:"status"`
	Users                  []string        `json:"users"`
	SourceImage            string          `json:"sourceImage"`
	SourceSnapshot         string          `json:"sourceSnapshot"`
	PhysicalBlockSizeBytes json.RawMessage `json:"physicalBlockSizeBytes"`
	CreationTimestamp      string          `json:"creationTimestamp"`
	DiskEncryptionKey      *struct {
		KMSKeyName string `json:"kmsKeyName"`
	} `json:"diskEncryptionKey"`
}

// extractDisk extracts bounded, redaction-safe typed depth for one compute
// Persistent Disk CAI asset. It returns the Terraform/drift/monitoring attribute
// set; the attached instances, source image/snapshot, and KMS CryptoKey as
// cross-source correlation anchors; and the typed attachment, provenance, and
// encryption edges. Attached-instance identities are kept only as counts and
// resolvable resource names, and the KMS key reference is reduced to its
// CryptoKey resource name (any cryptoKeyVersions suffix is stripped).
func extractDisk(ctx ExtractContext) (AttributeExtraction, error) {
	var data diskData
	if err := json.Unmarshal(ctx.Data, &data); err != nil {
		return AttributeExtraction{}, fmt.Errorf("decode disk data: %w", err)
	}

	attrs := diskAttributes(data)

	var anchors []string
	var rels []RelationshipObservation

	for _, user := range data.Users {
		if instanceName := computeResourceFullName(user, "instances"); instanceName != "" {
			anchors = append(anchors, instanceName)
			rels = append(rels, diskEdge(ctx, relationshipTypeDiskAttachedToInstance, instanceName, assetTypeComputeInstance))
		}
	}
	if imageName := computeResourceFullName(data.SourceImage, "images"); imageName != "" {
		anchors = append(anchors, imageName)
		rels = append(rels, diskEdge(ctx, relationshipTypeDiskCreatedFromImage, imageName, assetTypeComputeImage))
	}
	if snapshotName := computeResourceFullName(data.SourceSnapshot, "snapshots"); snapshotName != "" {
		anchors = append(anchors, snapshotName)
		rels = append(rels, diskEdge(ctx, relationshipTypeDiskCreatedFromSnapshot, snapshotName, assetTypeComputeSnapshot))
	}
	if data.DiskEncryptionKey != nil {
		if keyName := kmsCryptoKeyFullName(data.DiskEncryptionKey.KMSKeyName); keyName != "" {
			anchors = append(anchors, keyName)
			rels = append(rels, diskEdge(ctx, relationshipTypeDiskEncryptedByKey, keyName, assetTypeKMSCryptoKey))
		}
	}

	return AttributeExtraction{
		Attributes:         attrs,
		CorrelationAnchors: dedupeNonEmpty(anchors),
		Relationships:      rels,
	}, nil
}

// diskAttributes assembles the bounded attribute map. Empty or absent fields are
// omitted rather than written as zero values so a partial CAI page does not
// fabricate a posture (for example a zero size or an empty status). Zone and
// region are placement metadata kept as attributes, not edges, because CAI does
// not track zones/regions as resolvable resources.
func diskAttributes(data diskData) map[string]any {
	attrs := map[string]any{}
	if v := computeZoneName(data.Zone); v != "" {
		attrs["zone"] = v
	}
	if v := computeRegionName(data.Region); v != "" {
		attrs["region"] = v
	}
	if v, ok := parseFlexibleInt64(data.SizeGb); ok {
		attrs["size_gb"] = v
	}
	if v := diskTypeName(data.Type); v != "" {
		attrs["disk_type"] = v
	}
	if v := strings.TrimSpace(data.Status); v != "" {
		attrs["status"] = v
	}
	if n := countResolvableInstances(data.Users); n > 0 {
		attrs["attached_instance_count"] = n
	}
	if v, ok := parseFlexibleInt64(data.PhysicalBlockSizeBytes); ok {
		attrs["physical_block_size_bytes"] = v
	}
	if v, ok := normalizeRFC3339(data.CreationTimestamp); ok {
		attrs["creation_time"] = v
	}
	return attrs
}

// countResolvableInstances counts the disk users that name a resolvable compute
// instance, matching the number of attachment edges emitted so the count and the
// edges never disagree.
func countResolvableInstances(users []string) int {
	count := 0
	for _, user := range users {
		if computeResourceFullName(user, "instances") != "" {
			count++
		}
	}
	return count
}

// diskTypeName extracts the bare disk-type name (for example pd-ssd) from a disk
// type reference, which CAI may report as a compute self-link, a partial path,
// or the bare name itself.
func diskTypeName(typeRef string) string {
	trimmed := strings.TrimSpace(typeRef)
	if trimmed == "" {
		return ""
	}
	if idx := strings.LastIndex(trimmed, "/diskTypes/"); idx >= 0 {
		return strings.TrimSpace(trimmed[idx+len("/diskTypes/"):])
	}
	if strings.Contains(trimmed, "/") {
		return ""
	}
	return trimmed
}

// computeZoneName extracts the bare zone name from a zone reference, which CAI
// may report as a compute self-link, a partial path, or the bare zone name.
func computeZoneName(zoneRef string) string {
	trimmed := strings.TrimSpace(zoneRef)
	if trimmed == "" {
		return ""
	}
	if idx := strings.LastIndex(trimmed, "/zones/"); idx >= 0 {
		return strings.TrimSpace(trimmed[idx+len("/zones/"):])
	}
	if strings.Contains(trimmed, "/") {
		// A path that does not name a zone segment is not a bare zone name.
		return ""
	}
	return trimmed
}

// computeResourceFullName derives a compute Engine CAI full resource name from a
// resource reference (a full self-link or partial path) that must name the given
// resource segment (for example "instances", "images", "snapshots"). It returns
// "" when the reference is blank, has no projects/ anchor, or does not name the
// requested segment, so the caller emits no edge for an ambiguous reference.
func computeResourceFullName(ref, segment string) string {
	trimmed := strings.TrimSpace(ref)
	if trimmed == "" {
		return ""
	}
	idx := strings.Index(trimmed, "projects/")
	if idx < 0 {
		return ""
	}
	path := trimmed[idx:]
	if !strings.Contains(path, "/"+segment+"/") {
		return ""
	}
	return computeResourceNamePrefix + path
}

// kmsCryptoKeyFullName derives the Cloud KMS CryptoKey CAI full resource name
// from a disk's kmsKeyName, stripping any trailing cryptoKeyVersions segment so
// the edge points at the CryptoKey rather than a specific version. It returns ""
// when the reference is blank or does not name a cryptoKeys segment.
func kmsCryptoKeyFullName(kmsKeyName string) string {
	trimmed := strings.TrimSpace(kmsKeyName)
	if trimmed == "" {
		return ""
	}
	idx := strings.Index(trimmed, "projects/")
	if idx < 0 {
		return ""
	}
	path := trimmed[idx:]
	if !strings.Contains(path, "/cryptoKeys/") {
		return ""
	}
	if vIdx := strings.Index(path, "/cryptoKeyVersions/"); vIdx >= 0 {
		path = path[:vIdx]
	}
	return cloudKMSResourceNamePrefix + path
}

// parseFlexibleInt64 parses a control-plane integer that CAI may render either as
// a JSON string (the compute API int64 convention) or as a bare JSON number. It
// returns ok=false for a blank, null, or non-numeric value so the attribute is
// omitted rather than fabricated as zero.
func parseFlexibleInt64(raw json.RawMessage) (int64, bool) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return 0, false
	}
	if len(trimmed) >= 2 && trimmed[0] == '"' && trimmed[len(trimmed)-1] == '"' {
		trimmed = trimmed[1 : len(trimmed)-1]
	}
	trimmed = strings.TrimSpace(trimmed)
	if trimmed == "" {
		return 0, false
	}
	value, err := strconv.ParseInt(trimmed, 10, 64)
	if err != nil {
		return 0, false
	}
	return value, true
}

// diskEdge builds a supported typed relationship observation rooted at the disk.
func diskEdge(ctx ExtractContext, relationshipType, targetName, targetType string) RelationshipObservation {
	return RelationshipObservation{
		SourceFullResourceName: ctx.FullResourceName,
		SourceAssetType:        ctx.AssetType,
		RelationshipType:       relationshipType,
		TargetFullResourceName: targetName,
		TargetAssetType:        targetType,
		SupportState:           RelationshipSupportSupported,
	}
}
