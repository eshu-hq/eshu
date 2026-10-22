// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud //nolint:dirgate // #6696 wave-1: flat extractor moves to extractor/<group> in a later slice

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/gcpcloud/catalog"
)

// Family constants for this extractor now live in catalog/gcp_security.go (#6696 HK slice); uses below name catalog.X.

func init() {
	RegisterAssetExtractor(catalog.SecretVersionAssetType, extractSecretVersion)
}

// secretVersionData is the bounded view of a CAI
// secretmanager.googleapis.com/SecretVersion resource.data blob. Only
// redaction-safe control-plane posture is decoded; the secret payload is never a
// field so a stray one cannot be surfaced. Only the CMEK KMS key version resource
// name is pulled from the replication status — no key material.
type secretVersionData struct {
	State             string `json:"state"`
	CreateTime        string `json:"createTime"`
	DestroyTime       string `json:"destroyTime"`
	ReplicationStatus struct {
		Automatic *struct {
			CustomerManagedEncryption *secretVersionCMEK `json:"customerManagedEncryption"`
		} `json:"automatic"`
		UserManaged *struct {
			Replicas []struct {
				Location                  string             `json:"location"`
				CustomerManagedEncryption *secretVersionCMEK `json:"customerManagedEncryption"`
			} `json:"replicas"`
		} `json:"userManaged"`
	} `json:"replicationStatus"`
	// CustomerManagedEncryption is the top-level CMEK status for a regional
	// Secret Manager version. Regional versions report CMEK here rather than
	// under replicationStatus (which is global-secret only), so both are decoded.
	CustomerManagedEncryption *secretVersionCMEK `json:"customerManagedEncryption"`
}

// secretVersionCMEK is the bounded customer-managed encryption reference. Only the
// KMS key version resource name is decoded; it is a control-plane identifier, not
// key material.
type secretVersionCMEK struct {
	KMSKeyVersionName string `json:"kmsKeyVersionName"`
}

// extractSecretVersion extracts bounded, redaction-safe typed depth for one CAI
// Secret Manager Secret Version asset. It surfaces the lifecycle state, create and
// destroy times, replication type, and CMEK posture; derives the parent Secret
// from the version's own full resource name and emits the typed
// secret_version_of_secret edge to it plus a
// secret_version_encrypted_by_kms_key_version edge to each CMEK CryptoKeyVersion,
// with those key version resource names as correlation anchors. The secret payload
// is never read.
func extractSecretVersion(ctx ExtractContext) (AttributeExtraction, error) {
	var data secretVersionData
	if err := json.Unmarshal(ctx.Data, &data); err != nil {
		return AttributeExtraction{}, fmt.Errorf("decode secret version data: %w", err)
	}

	attrs := map[string]any{}
	if v := strings.TrimSpace(data.State); v != "" {
		attrs["state"] = v
	}
	if v, ok := normalizeRFC3339(data.CreateTime); ok {
		attrs["creation_time"] = v
	}
	if v, ok := normalizeRFC3339(data.DestroyTime); ok {
		attrs["destroy_time"] = v
	}
	switch {
	case data.ReplicationStatus.UserManaged != nil:
		attrs["replication_type"] = "user_managed"
	case data.ReplicationStatus.Automatic != nil:
		attrs["replication_type"] = "automatic"
	}

	keyVersions := secretVersionKMSKeyVersionFullNames(data)
	if len(keyVersions) > 0 {
		attrs["customer_managed_encryption"] = true
	}

	var rels []RelationshipObservation
	if parent := parentSecretFullName(ctx.FullResourceName); parent != "" {
		rels = append(rels, secretVersionEdge(ctx, catalog.RelationshipTypeSecretVersionOfSecret, parent, secretManagerSecretAssetType))
	}
	for _, keyVersion := range keyVersions {
		rels = append(rels, secretVersionEdge(ctx, catalog.RelationshipTypeSecretVersionEncryptedByKMSKeyVersion, keyVersion, catalog.AssetTypeKMSCryptoKeyVersion))
	}

	return AttributeExtraction{
		Attributes:         attrs,
		CorrelationAnchors: keyVersions,
		Relationships:      rels,
	}, nil
}

// secretVersionKMSKeyVersionFullNames returns the deduplicated CMEK
// CryptoKeyVersion full resource names referenced by the version's replication
// status.
func secretVersionKMSKeyVersionFullNames(data secretVersionData) []string {
	var names []string
	if a := data.ReplicationStatus.Automatic; a != nil && a.CustomerManagedEncryption != nil {
		if name := kmsKeyVersionFullName(a.CustomerManagedEncryption.KMSKeyVersionName); name != "" {
			names = append(names, name)
		}
	}
	if u := data.ReplicationStatus.UserManaged; u != nil {
		for _, r := range u.Replicas {
			if r.CustomerManagedEncryption == nil {
				continue
			}
			if name := kmsKeyVersionFullName(r.CustomerManagedEncryption.KMSKeyVersionName); name != "" {
				names = append(names, name)
			}
		}
	}
	// Regional versions carry CMEK at the top level rather than under
	// replicationStatus.
	if c := data.CustomerManagedEncryption; c != nil {
		if name := kmsKeyVersionFullName(c.KMSKeyVersionName); name != "" {
			names = append(names, name)
		}
	}
	return dedupeNonEmpty(names)
}

// kmsKeyVersionFullName builds the CAI CryptoKeyVersion full resource name from a
// relative KMS key version name. An already-normalized CAI full resource name is
// returned unchanged so the prefix is never doubled; a blank reference yields "".
func kmsKeyVersionFullName(kmsKeyVersionName string) string {
	trimmed := strings.TrimSpace(kmsKeyVersionName)
	if trimmed == "" {
		return ""
	}
	trimmed = normalizeKMSCryptoKeyVersionPath(trimmed)
	if strings.HasPrefix(trimmed, "//") {
		return trimmed
	}
	return cloudKMSResourceNamePrefix + strings.TrimPrefix(trimmed, "/")
}

// normalizeKMSCryptoKeyVersionPath rewrites the shortened
// `.../cryptoKeys/<key>/versions/<v>` KMS version form to the canonical CAI
// `.../cryptoKeys/<key>/cryptoKeyVersions/<v>` form so the emitted endpoint
// resolves by exact `full_resource_name` against the scanned CryptoKeyVersion
// resource. The canonical API form already uses `cryptoKeyVersions`; a name that
// already carries it is returned unchanged.
func normalizeKMSCryptoKeyVersionPath(name string) string {
	if strings.Contains(name, "/cryptoKeyVersions/") {
		return name
	}
	if i := strings.LastIndex(name, catalog.SecretVersionVersionsMarker); i >= 0 && strings.Contains(name[:i], "/cryptoKeys/") {
		return name[:i] + "/cryptoKeyVersions/" + name[i+len(catalog.SecretVersionVersionsMarker):]
	}
	return name
}

// parentSecretFullName derives the parent Secret full resource name from a
// SecretVersion full resource name by trimming the `/versions/<id>` suffix. It
// returns "" when the input carries no version suffix or the marker has no id
// after it, so the caller emits no parent edge from a name that does not identify
// a version.
func parentSecretFullName(versionFullName string) string {
	trimmed := strings.TrimSpace(versionFullName)
	index := strings.LastIndex(trimmed, catalog.SecretVersionVersionsMarker)
	if index < 0 || index+len(catalog.SecretVersionVersionsMarker) >= len(trimmed) {
		return ""
	}
	return trimmed[:index]
}

func secretVersionEdge(ctx ExtractContext, relationshipType, targetName, targetType string) RelationshipObservation {
	return RelationshipObservation{
		SourceFullResourceName: ctx.FullResourceName,
		SourceAssetType:        ctx.AssetType,
		RelationshipType:       relationshipType,
		TargetFullResourceName: targetName,
		TargetAssetType:        targetType,
		SupportState:           RelationshipSupportSupported,
	}
}
