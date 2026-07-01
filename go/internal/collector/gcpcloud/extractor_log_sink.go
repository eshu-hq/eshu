// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/secretsiam"
)

// logSinkAssetType is the Cloud Asset Inventory asset type for a GCP Logging Log
// Sink. Destination endpoint asset types (Storage Bucket, BigQuery Dataset,
// Pub/Sub Topic, Log Bucket) reuse constants declared elsewhere in this package.
const logSinkAssetType = "logging.googleapis.com/LogSink"

// Bounded provider relationship types for the sink's export destination.
const (
	relationshipTypeLogSinkExportsToBucket    = "log_sink_exports_to_bucket"
	relationshipTypeLogSinkExportsToDataset   = "log_sink_exports_to_dataset"
	relationshipTypeLogSinkExportsToTopic     = "log_sink_exports_to_topic"
	relationshipTypeLogSinkExportsToLogBucket = "log_sink_exports_to_log_bucket"
)

const logSinkWriterIdentityPrefix = "serviceAccount:"

func init() {
	RegisterAssetExtractor(logSinkAssetType, extractLogSink)
}

// logSinkData is the bounded view of a CAI logging.googleapis.com/LogSink
// resource.data blob. The filter expression (which can reference internal log,
// project, and resource names) is reduced to a presence flag, and the writer
// identity email is reduced to a digest — neither raw value leaves the parser.
// Disabled is a pointer so a present `false` is distinguishable from an absent
// field.
type logSinkData struct {
	Destination    string            `json:"destination"`
	Filter         string            `json:"filter"`
	WriterIdentity string            `json:"writerIdentity"`
	Disabled       *bool             `json:"disabled"`
	Exclusions     []json.RawMessage `json:"exclusions"`
	CreateTime     string            `json:"createTime"`
}

// extractLogSink extracts bounded, redaction-safe typed depth for one CAI Logging
// Log Sink asset. It surfaces the destination type, filter presence, disabled
// posture, exclusion count, creation time, and the fingerprinted writer-identity
// service-account email; emits the typed export edge to the destination resource
// (Storage Bucket, BigQuery Dataset, Pub/Sub Topic, or Log Bucket); and surfaces
// the destination resource name and writer-identity digest as correlation
// anchors. The raw filter expression and writer email never leave the parser.
func extractLogSink(ctx ExtractContext) (AttributeExtraction, error) {
	var data logSinkData
	if err := json.Unmarshal(ctx.Data, &data); err != nil {
		return AttributeExtraction{}, fmt.Errorf("decode log sink data: %w", err)
	}

	attrs := map[string]any{}
	var anchors []string
	var rels []RelationshipObservation

	if target, assetType, relType, destType := logSinkDestination(data.Destination); destType != "" {
		attrs["destination_type"] = destType
		if target != "" {
			anchors = append(anchors, target)
			rels = append(rels, RelationshipObservation{
				SourceFullResourceName: ctx.FullResourceName,
				SourceAssetType:        ctx.AssetType,
				RelationshipType:       relType,
				TargetFullResourceName: target,
				TargetAssetType:        assetType,
				SupportState:           RelationshipSupportSupported,
			})
		}
	}
	if strings.TrimSpace(data.Filter) != "" {
		attrs["filter_present"] = true
	}
	if data.Disabled != nil {
		attrs["disabled"] = *data.Disabled
	}
	if n := len(data.Exclusions); n > 0 {
		attrs["exclusion_count"] = n
	}
	if v, ok := normalizeRFC3339(data.CreateTime); ok {
		attrs["creation_time"] = v
	}
	if digest := logSinkWriterIdentityDigest(data.WriterIdentity); digest != "" {
		attrs["writer_identity_email_fingerprint"] = digest
		anchors = append(anchors, digest)
	}

	return AttributeExtraction{
		Attributes:         attrs,
		CorrelationAnchors: anchors,
		Relationships:      rels,
	}, nil
}

// logSinkDestination classifies the sink destination string and returns the
// resolvable CAI full resource name, its asset type, the bounded export
// relationship type, and a bounded destination-type tag. An unrecognized
// destination service yields empty strings so the caller emits no edge or type.
func logSinkDestination(destination string) (target, assetType, relType, destType string) {
	trimmed := strings.TrimSpace(destination)
	if trimmed == "" {
		return "", "", "", ""
	}
	switch {
	case strings.HasPrefix(trimmed, "storage.googleapis.com/"):
		return "//" + trimmed, assetTypeStorageBucket, relationshipTypeLogSinkExportsToBucket, "storage"
	case strings.HasPrefix(trimmed, "bigquery.googleapis.com/"):
		return "//" + trimmed, assetTypeBigQueryDataset, relationshipTypeLogSinkExportsToDataset, "bigquery"
	case strings.HasPrefix(trimmed, "pubsub.googleapis.com/"):
		return "//" + trimmed, assetTypePubSubTopic, relationshipTypeLogSinkExportsToTopic, "pubsub"
	case strings.HasPrefix(trimmed, "logging.googleapis.com/"):
		return "//" + trimmed, logBucketAssetType, relationshipTypeLogSinkExportsToLogBucket, "logging"
	default:
		return "", "", "", ""
	}
}

// logSinkWriterIdentityDigest reduces the sink writer identity (a
// "serviceAccount:<email>" member string) to the redaction-safe email digest so
// the IAM/trust layer can join without the raw email being persisted.
func logSinkWriterIdentityDigest(writerIdentity string) string {
	trimmed := strings.TrimSpace(writerIdentity)
	if !strings.HasPrefix(trimmed, logSinkWriterIdentityPrefix) {
		return ""
	}
	email := strings.TrimPrefix(trimmed, logSinkWriterIdentityPrefix)
	return secretsiam.GCPServiceAccountEmailDigest(email)
}
