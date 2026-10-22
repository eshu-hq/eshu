// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud //nolint:dirgate // #6696 wave-1: flat extractor moves to extractor/<group> in a later slice

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/gcpcloud/catalog"
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
	if kmsName := cmekKeyFullResourceName(data.KMSKeyName); kmsName != "" {
		// Derive the CMEK posture from the normalized key so the attribute agrees
		// with the edge: a wrong-domain kmsKeyName that the strict normalizer
		// rejects must not report customer_managed_encryption=true while emitting
		// no encryption edge.
		attrs["customer_managed_encryption"] = true
		anchors = append(anchors, kmsName)
		rels = append(rels, pubSubTopicEdge(ctx, catalog.RelationshipTypeTopicEncryptedByKMSKey, kmsName, assetTypeKMSCryptoKey))
	}
	if data.SchemaSettings != nil {
		if schemaName := pubSubTopicSchemaFullName(data.SchemaSettings.Schema); schemaName != "" {
			anchors = append(anchors, schemaName)
			rels = append(rels, pubSubTopicEdge(ctx, catalog.RelationshipTypeTopicUsesSchema, schemaName, catalog.AssetTypePubSubSchema))
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
	if p := data.MessageStoragePolicy; p != nil {
		if regions := dedupeSortedNonEmpty(p.AllowedPersistenceRegions); len(regions) > 0 {
			attrs["message_storage_regions"] = regions
		}
		if p.EnforceInTransit {
			attrs["message_storage_enforced"] = true
		}
	}
	if s := data.SchemaSettings; s != nil {
		// The encoding is retained even when the schema name is the deleted-schema
		// sentinel (which emits no edge): it records the wire encoding the topic is
		// still configured for, which drives drift and monitoring independent of
		// whether the referenced Schema resource currently resolves.
		if v := strings.TrimSpace(s.Encoding); v != "" {
			attrs["schema_encoding"] = v
		}
	}
	if v := strings.TrimSpace(data.MessageRetentionDuration); v != "" {
		attrs["message_retention_duration"] = v
	}
	return attrs
}

// pubSubTopicSchemaFullName builds the CAI Pub/Sub Schema full resource name from
// a schema reference (projects/.../schemas/...). An already-normalized CAI full
// resource name (//pubsub.googleapis.com/...) is returned unchanged so the prefix
// is never doubled. It returns "" for a blank reference, the deleted-schema
// sentinel, or a reference that does not name a schema, so the caller emits no
// schema edge.
func pubSubTopicSchemaFullName(schemaRef string) string {
	trimmed := strings.TrimSpace(schemaRef)
	if trimmed == "" || trimmed == catalog.PubSubDeletedSchemaSentinel || !strings.Contains(trimmed, "/schemas/") {
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
