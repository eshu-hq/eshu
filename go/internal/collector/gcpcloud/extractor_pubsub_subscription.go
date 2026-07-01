// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// assetTypePubSubSubscription is the CAI asset type for a Pub/Sub Subscription.
// Its edge targets reuse asset-type constants already declared in this package:
// assetTypePubSubTopic (Secret Manager Secret extractor), assetTypeBigQueryTable
// and assetTypeStorageBucket plus their //-name prefixes (BigQuery Table
// extractor).
const assetTypePubSubSubscription = "pubsub.googleapis.com/Subscription"

// pubSubDeletedTopicSentinel is the value Pub/Sub reports for a subscription's
// topic when the topic has been deleted while the subscription still exists. It
// names no resolvable Topic resource, so it yields no edge.
const pubSubDeletedTopicSentinel = "_deleted-topic_"

// Bounded provider relationship types for Pub/Sub Subscription edges. Each is a
// stable string carried on a gcp_cloud_relationship fact; the reducer
// materializes an edge only when both endpoints resolve exactly.
const (
	relationshipTypeSubscriptionSubscribesToTopic      = "subscription_subscribes_to_topic"
	relationshipTypeSubscriptionDeadLettersToTopic     = "subscription_dead_letters_to_topic"
	relationshipTypeSubscriptionExportsToBigQueryTable = "subscription_exports_to_bigquery_table"
	relationshipTypeSubscriptionExportsToStorageBucket = "subscription_exports_to_storage_bucket"
)

func init() {
	RegisterAssetExtractor(assetTypePubSubSubscription, extractPubSubSubscription)
}

// pubSubSubscriptionData is the bounded view of a CAI
// pubsub.googleapis.com/Subscription resource.data blob. Only redaction-safe
// control-plane metadata and resource references are decoded. The push endpoint
// is decoded but never persisted raw: its path and query (which can carry OIDC
// tokens or auth secrets) are dropped, the scheme is kept as posture, and the
// host is reduced to a deterministic fingerprint.
type pubSubSubscriptionData struct {
	Topic                     string `json:"topic"`
	AckDeadlineSeconds        int    `json:"ackDeadlineSeconds"`
	RetainAckedMessages       bool   `json:"retainAckedMessages"`
	MessageRetentionDuration  string `json:"messageRetentionDuration"`
	EnableExactlyOnceDelivery bool   `json:"enableExactlyOnceDelivery"`
	Filter                    string `json:"filter"`
	State                     string `json:"state"`
	PushConfig                *struct {
		PushEndpoint string `json:"pushEndpoint"`
	} `json:"pushConfig"`
	ExpirationPolicy *struct {
		TTL string `json:"ttl"`
	} `json:"expirationPolicy"`
	DeadLetterPolicy *struct {
		DeadLetterTopic     string `json:"deadLetterTopic"`
		MaxDeliveryAttempts int    `json:"maxDeliveryAttempts"`
	} `json:"deadLetterPolicy"`
	BigQueryConfig *struct {
		Table string `json:"table"`
	} `json:"bigqueryConfig"`
	CloudStorageConfig *struct {
		Bucket string `json:"bucket"`
	} `json:"cloudStorageConfig"`
}

// extractPubSubSubscription extracts bounded, redaction-safe typed depth for one
// Pub/Sub Subscription CAI asset. It returns the Terraform/drift/monitoring
// attribute set (lifecycle state, delivery type, push scheme/host-fingerprint,
// ack deadline, retention, expiration, exactly-once, filter and dead-letter
// posture) and the typed subscribes-to-topic, dead-letters-to-topic,
// exports-to-BigQuery-table, and exports-to-storage-bucket edges.
func extractPubSubSubscription(ctx ExtractContext) (AttributeExtraction, error) {
	var data pubSubSubscriptionData
	if err := json.Unmarshal(ctx.Data, &data); err != nil {
		return AttributeExtraction{}, fmt.Errorf("decode pub/sub subscription data: %w", err)
	}

	attrs := pubSubSubscriptionAttributes(data)

	var anchors []string
	var rels []RelationshipObservation
	if topic := pubSubTopicRefFullName(data.Topic); topic != "" {
		rels = append(rels, pubSubSubscriptionEdge(ctx, relationshipTypeSubscriptionSubscribesToTopic, topic, assetTypePubSubTopic))
	}
	if data.DeadLetterPolicy != nil {
		if dlt := pubSubTopicRefFullName(data.DeadLetterPolicy.DeadLetterTopic); dlt != "" {
			anchors = append(anchors, dlt)
			rels = append(rels, pubSubSubscriptionEdge(ctx, relationshipTypeSubscriptionDeadLettersToTopic, dlt, assetTypePubSubTopic))
		}
	}
	if data.BigQueryConfig != nil {
		if tbl := pubSubBigQueryConfigTableFullName(data.BigQueryConfig.Table); tbl != "" {
			anchors = append(anchors, tbl)
			rels = append(rels, pubSubSubscriptionEdge(ctx, relationshipTypeSubscriptionExportsToBigQueryTable, tbl, assetTypeBigQueryTable))
		}
	}
	if data.CloudStorageConfig != nil {
		if bucket := strings.TrimSpace(data.CloudStorageConfig.Bucket); bucket != "" {
			name := storageBucketResourceNamePrefixFmt + bucket
			anchors = append(anchors, name)
			rels = append(rels, pubSubSubscriptionEdge(ctx, relationshipTypeSubscriptionExportsToStorageBucket, name, assetTypeStorageBucket))
		}
	}

	return AttributeExtraction{
		Attributes:         attrs,
		CorrelationAnchors: dedupeNonEmpty(anchors),
		Relationships:      rels,
	}, nil
}

// pubSubSubscriptionAttributes assembles the bounded attribute map. Empty,
// absent, or default-valued fields are omitted rather than written as zero
// values so a partial CAI page does not fabricate a posture.
func pubSubSubscriptionAttributes(data pubSubSubscriptionData) map[string]any {
	attrs := map[string]any{}

	if v := strings.TrimSpace(data.State); v != "" {
		attrs["state"] = v
	}
	// delivery_type is only meaningful for a real subscription, which always names
	// a topic; an empty/partial blob with no topic must not fabricate a "pull"
	// posture.
	if strings.TrimSpace(data.Topic) != "" {
		attrs["delivery_type"] = pubSubSubscriptionDeliveryType(data)
	}
	if data.PushConfig != nil {
		if scheme, hostFP := pubSubPushEndpointPosture(data.PushConfig.PushEndpoint); scheme != "" {
			attrs["push_endpoint_scheme"] = scheme
			if hostFP != "" {
				attrs["push_endpoint_host_fingerprint"] = hostFP
			}
		}
	}
	if data.AckDeadlineSeconds > 0 {
		attrs["ack_deadline_seconds"] = data.AckDeadlineSeconds
	}
	if data.RetainAckedMessages {
		attrs["retain_acked_messages"] = true
	}
	if v := strings.TrimSpace(data.MessageRetentionDuration); v != "" {
		attrs["message_retention_duration"] = v
	}
	if data.ExpirationPolicy != nil {
		if v := strings.TrimSpace(data.ExpirationPolicy.TTL); v != "" {
			attrs["expiration_ttl"] = v
		}
	}
	if data.DeadLetterPolicy != nil && data.DeadLetterPolicy.MaxDeliveryAttempts > 0 {
		attrs["dead_letter_max_delivery_attempts"] = data.DeadLetterPolicy.MaxDeliveryAttempts
	}
	if data.EnableExactlyOnceDelivery {
		attrs["exactly_once_delivery"] = true
	}
	if strings.TrimSpace(data.Filter) != "" {
		// The filter expression can reference message attribute names/values, so
		// only its presence is recorded, never the expression itself.
		attrs["filter_present"] = true
	}
	return attrs
}

// pubSubSubscriptionDeliveryType classifies the subscription delivery mechanism
// from whichever delivery config is present, defaulting to pull.
func pubSubSubscriptionDeliveryType(data pubSubSubscriptionData) string {
	switch {
	case data.PushConfig != nil && strings.TrimSpace(data.PushConfig.PushEndpoint) != "":
		return "push"
	case data.BigQueryConfig != nil && strings.TrimSpace(data.BigQueryConfig.Table) != "":
		return "bigquery"
	case data.CloudStorageConfig != nil && strings.TrimSpace(data.CloudStorageConfig.Bucket) != "":
		return "cloud_storage"
	default:
		return "pull"
	}
}

// pubSubPushEndpointPosture reduces a push endpoint URL to its redaction-safe
// posture: the scheme (an http-vs-https alerting signal) and a deterministic
// fingerprint of the host. The path and query are dropped because they can carry
// OIDC tokens or shared secrets, and the raw host is a DNS name that the GCP
// collector contract fingerprints rather than persisting verbatim.
func pubSubPushEndpointPosture(endpoint string) (scheme, hostFingerprint string) {
	trimmed := strings.TrimSpace(endpoint)
	if trimmed == "" {
		return "", ""
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", ""
	}
	return strings.ToLower(parsed.Scheme), pubSubPushEndpointHostFingerprint(parsed.Hostname())
}

// pubSubPushEndpointHostFingerprint returns a stable, case-normalized digest of a
// push endpoint host so subscriptions sharing an endpoint can be correlated
// without persisting the raw DNS name. A blank host fingerprints to "".
func pubSubPushEndpointHostFingerprint(host string) string {
	normalized := strings.ToLower(strings.TrimSpace(host))
	if normalized == "" {
		return ""
	}
	return "sha256:" + facts.StableID("GCPPubSubPushEndpointHost", map[string]any{"host": normalized})
}

// pubSubTopicRefFullName builds the CAI Pub/Sub Topic full resource name from a
// topic reference (projects/.../topics/...). An already-normalized CAI full
// resource name is returned unchanged. It returns "" for a blank reference, the
// deleted-topic sentinel, or a reference that does not name a topic.
func pubSubTopicRefFullName(topicRef string) string {
	trimmed := strings.TrimSpace(topicRef)
	if trimmed == "" || trimmed == pubSubDeletedTopicSentinel || !strings.Contains(trimmed, "/topics/") {
		return ""
	}
	if strings.HasPrefix(trimmed, "//") {
		return trimmed
	}
	return pubSubResourceNamePrefix + strings.TrimPrefix(trimmed, "/")
}

// pubSubBigQueryConfigTableFullName builds the CAI BigQuery Table full resource
// name from a Pub/Sub BigQueryConfig table reference. Pub/Sub reports the target
// as the dotted form "projectId.datasetId.tableId" (a domain-scoped project uses
// "projectId:datasetId.tableId"). It returns "" for any reference that does not
// resolve to exactly a project, dataset, and table so no edge is fabricated.
func pubSubBigQueryConfigTableFullName(tableRef string) string {
	trimmed := strings.TrimSpace(tableRef)
	if trimmed == "" {
		return ""
	}
	// A colon separates a domain-scoped project from its dataset; normalize it to
	// a dot so the reference splits into project/dataset/table uniformly.
	parts := strings.Split(strings.Replace(trimmed, ":", ".", 1), ".")
	if len(parts) != 3 {
		return ""
	}
	for _, p := range parts {
		if strings.TrimSpace(p) == "" {
			return ""
		}
	}
	return fmt.Sprintf("%sprojects/%s/datasets/%s/tables/%s", bigQueryResourceNamePrefix, parts[0], parts[1], parts[2])
}

// pubSubSubscriptionEdge builds one typed provider relationship observation
// anchored on the subscription's CAI full resource name.
func pubSubSubscriptionEdge(ctx ExtractContext, relationshipType, targetName, targetType string) RelationshipObservation {
	return RelationshipObservation{
		SourceFullResourceName: ctx.FullResourceName,
		SourceAssetType:        ctx.AssetType,
		RelationshipType:       relationshipType,
		TargetFullResourceName: targetName,
		TargetAssetType:        targetType,
		SupportState:           RelationshipSupportSupported,
	}
}
