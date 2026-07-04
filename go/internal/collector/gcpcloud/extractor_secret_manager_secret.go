// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Asset type and full-resource-name prefix for the Pub/Sub topic endpoint a
// Secret Manager Secret derives from its rotation notification topics. The KMS
// CryptoKey endpoint reuses assetTypeKMSCryptoKey and cloudKMSResourceNamePrefix
// already declared by the BigQuery Table extractor in this package.
const (
	assetTypePubSubTopic     = "pubsub.googleapis.com/Topic"
	pubSubResourceNamePrefix = "//pubsub.googleapis.com/"
)

// Bounded provider relationship types for Secret Manager Secret edges. Each is a
// stable, bounded string carried on a gcp_cloud_relationship fact; the reducer
// materializes an edge only when both endpoints resolve exactly.
const (
	relationshipTypeSecretEncryptedByKMSKey = "secret_encrypted_by_kms_key"
	relationshipTypeSecretNotifiesTopic     = "secret_notifies_topic"
)

func init() {
	RegisterAssetExtractor(secretManagerSecretAssetType, extractSecretManagerSecret)
}

// secretManagerSecretData is the bounded view of a CAI
// secretmanager.googleapis.com/Secret resource.data blob. Only redaction-safe
// control-plane metadata and resource references are decoded. A CAI Secret asset
// carries no secret payload (payloads live on separate SecretVersion resources
// and are never read); this view deliberately omits any payload/value field so a
// stray one in the blob cannot be surfaced.
type secretManagerSecretData struct {
	Replication struct {
		Automatic *struct {
			CustomerManagedEncryption *secretCMEK `json:"customerManagedEncryption"`
		} `json:"automatic"`
		UserManaged *struct {
			Replicas []struct {
				Location                  string      `json:"location"`
				CustomerManagedEncryption *secretCMEK `json:"customerManagedEncryption"`
			} `json:"replicas"`
		} `json:"userManaged"`
	} `json:"replication"`
	CreateTime string `json:"createTime"`
	ExpireTime string `json:"expireTime"`
	TTL        string `json:"ttl"`
	Rotation   *struct {
		NextRotationTime string `json:"nextRotationTime"`
		RotationPeriod   string `json:"rotationPeriod"`
	} `json:"rotation"`
	Topics []struct {
		Name string `json:"name"`
	} `json:"topics"`
	VersionAliases map[string]string `json:"versionAliases"`
}

// secretCMEK is the bounded customer-managed encryption reference. Only the KMS
// key resource name is decoded; it is a control-plane identifier, not a key or
// secret value.
type secretCMEK struct {
	KMSKeyName string `json:"kmsKeyName"`
}

// extractSecretManagerSecret extracts bounded, redaction-safe typed depth for one
// Secret Manager Secret CAI asset. It returns the Terraform/drift/monitoring
// attribute set (replication posture, encryption posture, rotation, expiration,
// creation, and bounded counts), the CMEK CryptoKey and rotation-notification
// Pub/Sub topics as cross-source correlation anchors, and the typed
// secret_encrypted_by_kms_key and secret_notifies_topic edges. Secret payloads
// are never read.
func extractSecretManagerSecret(ctx ExtractContext) (AttributeExtraction, error) {
	var data secretManagerSecretData
	if err := json.Unmarshal(ctx.Data, &data); err != nil {
		return AttributeExtraction{}, fmt.Errorf("decode secret manager secret data: %w", err)
	}

	attrs := secretManagerSecretAttributes(data)

	var anchors []string
	var rels []RelationshipObservation
	for _, kmsName := range secretManagerKMSKeyFullNames(data) {
		anchors = append(anchors, kmsName)
		rels = append(rels, secretManagerSecretEdge(ctx, relationshipTypeSecretEncryptedByKMSKey, kmsName, assetTypeKMSCryptoKey))
	}
	for _, topicName := range secretManagerTopicFullNames(data) {
		anchors = append(anchors, topicName)
		rels = append(rels, secretManagerSecretEdge(ctx, relationshipTypeSecretNotifiesTopic, topicName, assetTypePubSubTopic))
	}

	return AttributeExtraction{
		Attributes:         attrs,
		CorrelationAnchors: dedupeNonEmpty(anchors),
		Relationships:      rels,
	}, nil
}

// secretManagerSecretAttributes assembles the bounded attribute map. Empty or
// absent fields are omitted rather than written as zero values so a partial CAI
// page does not fabricate a posture (for example a false CMEK flag or a "0
// versions" count that was simply not reported).
func secretManagerSecretAttributes(data secretManagerSecretData) map[string]any {
	attrs := map[string]any{}

	switch {
	case data.Replication.UserManaged != nil:
		attrs["replication_type"] = "user_managed"
		// Emit the count only when replicas are present; a zero count on a
		// partial CAI page would fabricate a "0 locations" posture.
		if n := len(data.Replication.UserManaged.Replicas); n > 0 {
			attrs["replication_location_count"] = n
		}
	case data.Replication.Automatic != nil:
		attrs["replication_type"] = "automatic"
	}
	if secretHasCMEK(data) {
		attrs["customer_managed_encryption"] = true
	}
	if v, ok := normalizeRFC3339(data.CreateTime); ok {
		attrs["creation_time"] = v
	}
	// expireTime and ttl are the two arms of the expiration oneof; keep whichever
	// the API reported. expireTime is a timestamp; ttl is a duration string.
	if v, ok := normalizeRFC3339(data.ExpireTime); ok {
		attrs["expire_time"] = v
	} else if v := strings.TrimSpace(data.TTL); v != "" {
		attrs["ttl"] = v
	}
	if data.Rotation != nil {
		if v, ok := normalizeRFC3339(data.Rotation.NextRotationTime); ok {
			attrs["next_rotation_time"] = v
		}
		if v := strings.TrimSpace(data.Rotation.RotationPeriod); v != "" {
			attrs["rotation_period"] = v
		}
	}
	if n := len(data.Topics); n > 0 {
		attrs["topic_count"] = n
	}
	if n := len(data.VersionAliases); n > 0 {
		attrs["version_alias_count"] = n
	}
	return attrs
}

// secretHasCMEK reports whether the secret declares customer-managed encryption
// that resolves to at least one Cloud KMS CryptoKey. It is derived from the same
// normalized key set that drives the encryption edges (secretManagerKMSKeyFullNames)
// so the customer_managed_encryption attribute and the emitted edges always
// agree: a wrong-domain kmsKeyName the strict normalizer rejects sets neither.
func secretHasCMEK(data secretManagerSecretData) bool {
	return len(secretManagerKMSKeyFullNames(data)) > 0
}

// secretManagerKMSKeyFullNames returns the deduplicated CMEK CryptoKey full
// resource names referenced by the secret's replication policy.
func secretManagerKMSKeyFullNames(data secretManagerSecretData) []string {
	var keys []string
	if a := data.Replication.Automatic; a != nil && a.CustomerManagedEncryption != nil {
		if name := cmekKeyFullResourceName(a.CustomerManagedEncryption.KMSKeyName); name != "" {
			keys = append(keys, name)
		}
	}
	if u := data.Replication.UserManaged; u != nil {
		for _, r := range u.Replicas {
			if r.CustomerManagedEncryption == nil {
				continue
			}
			if name := cmekKeyFullResourceName(r.CustomerManagedEncryption.KMSKeyName); name != "" {
				keys = append(keys, name)
			}
		}
	}
	return dedupeNonEmpty(keys)
}

// secretManagerTopicFullNames returns the deduplicated Pub/Sub topic full
// resource names for the secret's rotation-notification topics.
func secretManagerTopicFullNames(data secretManagerSecretData) []string {
	if len(data.Topics) == 0 {
		return nil
	}
	names := make([]string, 0, len(data.Topics))
	for _, topic := range data.Topics {
		if name := secretManagerTopicFullName(topic.Name); name != "" {
			names = append(names, name)
		}
	}
	return dedupeNonEmpty(names)
}

// secretManagerTopicFullName builds the CAI Pub/Sub topic full resource name from
// a topic reference (projects/.../topics/...). An already-normalized CAI full
// resource name (//pubsub.googleapis.com/...) is returned unchanged so the prefix
// is never doubled. It returns "" for a blank reference or one that does not name
// a topic so the caller emits no topic edge.
func secretManagerTopicFullName(topicRef string) string {
	trimmed := strings.TrimSpace(topicRef)
	if trimmed == "" || !strings.Contains(trimmed, "/topics/") {
		return ""
	}
	if strings.HasPrefix(trimmed, "//") {
		return trimmed
	}
	return pubSubResourceNamePrefix + strings.TrimPrefix(trimmed, "/")
}

func secretManagerSecretEdge(ctx ExtractContext, relationshipType, targetName, targetType string) RelationshipObservation {
	return RelationshipObservation{
		SourceFullResourceName: ctx.FullResourceName,
		SourceAssetType:        ctx.AssetType,
		RelationshipType:       relationshipType,
		TargetFullResourceName: targetName,
		TargetAssetType:        targetType,
		SupportState:           RelationshipSupportSupported,
	}
}
