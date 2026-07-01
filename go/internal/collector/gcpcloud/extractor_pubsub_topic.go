// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// assetTypePubSubSchema is the CAI asset type for a Pub/Sub Schema, the target
// of a topic's schema edge. The Topic asset type (assetTypePubSubTopic) and the
// //pubsub.googleapis.com/ full-resource-name prefix (pubSubResourceNamePrefix)
// are already declared by the Secret Manager Secret extractor in this package;
// the KMS CryptoKey asset type and prefix come from the BigQuery Table extractor.
const assetTypePubSubSchema = "pubsub.googleapis.com/Schema"

// pubSubDeletedSchemaSentinel is the value Pub/Sub reports for schemaSettings.schema
// when the referenced schema has been deleted while still attached to the topic.
// It names no resolvable Schema resource, so it yields no edge or anchor.
const pubSubDeletedSchemaSentinel = "_deleted-schema_"

// Bounded provider relationship types for Pub/Sub Topic edges. Each is a stable,
// bounded string carried on a gcp_cloud_relationship fact; the reducer
// materializes an edge only when both endpoints resolve exactly. Subscription
// and IAM publisher/subscriber edges are intentionally absent: a Topic's own
// resource.data carries neither its subscriptions (which reference the topic on
// their own asset) nor its IAM policy (never persisted per the collector
// contract), so those joins belong to the subscription asset and IAM-policy
// paths, not to this metadata-only extractor.
const (
	relationshipTypeTopicEncryptedByKMSKey = "topic_encrypted_by_kms_key"
	relationshipTypeTopicUsesSchema        = "topic_uses_schema"
)

func init() {
	RegisterAssetExtractor(assetTypePubSubTopic, extractPubSubTopic)
}

// pubSubTopicData is the bounded view of a CAI pubsub.googleapis.com/Topic
// resource.data blob. Only redaction-safe control-plane metadata and resource
// references are decoded; a topic carries no message payloads, so there is
// nothing data-plane to omit here.
type pubSubTopicData struct {
	KMSKeyName           string `json:"kmsKeyName"`
	MessageStoragePolicy *struct {
		AllowedPersistenceRegions []string `json:"allowedPersistenceRegions"`
		EnforceInTransit          bool     `json:"enforceInTransit"`
	} `json:"messageStoragePolicy"`
	SchemaSettings *struct {
		Schema   string `json:"schema"`
		Encoding string `json:"encoding"`
	} `json:"schemaSettings"`
	MessageRetentionDuration string `json:"messageRetentionDuration"`
	State                    string `json:"state"`
}

// extractPubSubTopic extracts bounded, redaction-safe typed depth for one Pub/Sub
// Topic CAI asset. It returns the Terraform/drift/monitoring attribute set
// (lifecycle state, encryption posture, message-storage region residency and
// in-transit enforcement, schema encoding, and retention), the CMEK CryptoKey
// and message-schema resource names as cross-source correlation anchors, and the
// typed topic_encrypted_by_kms_key and topic_uses_schema edges.
func extractPubSubTopic(ctx ExtractContext) (AttributeExtraction, error) {
	var data pubSubTopicData
	if err := json.Unmarshal(ctx.Data, &data); err != nil {
		return AttributeExtraction{}, fmt.Errorf("decode pub/sub topic data: %w", err)
	}

	attrs := pubSubTopicAttributes(data)

	var anchors []string
	var rels []RelationshipObservation
	if kmsName := pubSubTopicKMSKeyFullName(data.KMSKeyName); kmsName != "" {
		anchors = append(anchors, kmsName)
		rels = append(rels, pubSubTopicEdge(ctx, relationshipTypeTopicEncryptedByKMSKey, kmsName, assetTypeKMSCryptoKey))
	}
	if data.SchemaSettings != nil {
		if schemaName := pubSubTopicSchemaFullName(data.SchemaSettings.Schema); schemaName != "" {
			anchors = append(anchors, schemaName)
			rels = append(rels, pubSubTopicEdge(ctx, relationshipTypeTopicUsesSchema, schemaName, assetTypePubSubSchema))
		}
	}

	return AttributeExtraction{
		Attributes:         attrs,
		CorrelationAnchors: dedupeNonEmpty(anchors),
		Relationships:      rels,
	}, nil
}

// pubSubTopicAttributes assembles the bounded attribute map. Empty, absent, or
// default-valued fields are omitted rather than written as zero values so a
// partial CAI page does not fabricate a posture (for example a false
// enforce-in-transit flag or an empty regions list).
func pubSubTopicAttributes(data pubSubTopicData) map[string]any {
	attrs := map[string]any{}

	if v := strings.TrimSpace(data.State); v != "" {
		attrs["state"] = v
	}
	if strings.TrimSpace(data.KMSKeyName) != "" {
		attrs["customer_managed_encryption"] = true
	}
	if p := data.MessageStoragePolicy; p != nil {
		if regions := dedupeSortedNonEmpty(p.AllowedPersistenceRegions); len(regions) > 0 {
			attrs["message_storage_regions"] = regions
		}
		if p.EnforceInTransit {
			attrs["message_storage_enforced"] = true
		}
	}
	if s := data.SchemaSettings; s != nil {
		if v := strings.TrimSpace(s.Encoding); v != "" {
			attrs["schema_encoding"] = v
		}
	}
	if v := strings.TrimSpace(data.MessageRetentionDuration); v != "" {
		attrs["message_retention_duration"] = v
	}
	return attrs
}

// pubSubTopicKMSKeyFullName builds the CAI CryptoKey full resource name from a
// relative KMS key name (projects/.../cryptoKeys/...). An already-normalized CAI
// full resource name (//cloudkms.googleapis.com/...) is returned unchanged so the
// prefix is never doubled. It returns "" for a blank reference so the caller
// emits no encryption edge.
func pubSubTopicKMSKeyFullName(kmsKeyName string) string {
	trimmed := strings.TrimSpace(kmsKeyName)
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(trimmed, "//") {
		return trimmed
	}
	return cloudKMSResourceNamePrefix + strings.TrimPrefix(trimmed, "/")
}

// pubSubTopicSchemaFullName builds the CAI Pub/Sub Schema full resource name from
// a schema reference (projects/.../schemas/...). An already-normalized CAI full
// resource name (//pubsub.googleapis.com/...) is returned unchanged so the prefix
// is never doubled. It returns "" for a blank reference, the deleted-schema
// sentinel, or a reference that does not name a schema, so the caller emits no
// schema edge.
func pubSubTopicSchemaFullName(schemaRef string) string {
	trimmed := strings.TrimSpace(schemaRef)
	if trimmed == "" || trimmed == pubSubDeletedSchemaSentinel || !strings.Contains(trimmed, "/schemas/") {
		return ""
	}
	if strings.HasPrefix(trimmed, "//") {
		return trimmed
	}
	return pubSubResourceNamePrefix + strings.TrimPrefix(trimmed, "/")
}

// pubSubTopicEdge builds one typed provider relationship observation anchored on
// the topic's CAI full resource name.
func pubSubTopicEdge(ctx ExtractContext, relationshipType, targetName, targetType string) RelationshipObservation {
	return RelationshipObservation{
		SourceFullResourceName: ctx.FullResourceName,
		SourceAssetType:        ctx.AssetType,
		RelationshipType:       relationshipType,
		TargetFullResourceName: targetName,
		TargetAssetType:        targetType,
		SupportState:           RelationshipSupportSupported,
	}
}

// dedupeSortedNonEmpty trims, drops blanks, deduplicates, and sorts a string
// slice so an attribute like the message-storage region set is deterministic
// regardless of the order the API reported it.
func dedupeSortedNonEmpty(in []string) []string {
	out := dedupeNonEmpty(in)
	sort.Strings(out)
	return out
}
