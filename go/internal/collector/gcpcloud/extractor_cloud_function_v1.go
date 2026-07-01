// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/secretsiam"
)

// assetTypeCloudFunctionV1 is the CAI asset type for a first-generation Cloud
// Function (the v1 API resource), distinct from the unified/gen2
// cloudfunctions.googleapis.com/Function type. It reuses the Cloud Functions
// relationship types, endpoint builders, and edge helper declared by the gen2
// extractor in this package (extractor_cloud_function.go).
const assetTypeCloudFunctionV1 = "cloudfunctions.googleapis.com/CloudFunction"

func init() {
	RegisterAssetExtractor(assetTypeCloudFunctionV1, extractCloudFunctionV1)
}

// cloudFunctionV1Data is the bounded view of a CAI
// cloudfunctions.googleapis.com/CloudFunction resource.data blob (Cloud Functions
// v1 API shape). All fields sit at the top level. Only redaction-safe
// control-plane metadata and resource references are decoded; the https trigger
// is decoded as presence only (its URL is never read), env values, secret values,
// and the source object path are never surfaced.
type cloudFunctionV1Data struct {
	Status              string    `json:"status"`
	Runtime             string    `json:"runtime"`
	EntryPoint          string    `json:"entryPoint"`
	AvailableMemoryMb   *int      `json:"availableMemoryMb"`
	ServiceAccountEmail string    `json:"serviceAccountEmail"`
	VPCConnector        string    `json:"vpcConnector"`
	VPCConnectorEgress  string    `json:"vpcConnectorEgressSettings"`
	IngressSettings     string    `json:"ingressSettings"`
	SourceArchiveURL    string    `json:"sourceArchiveUrl"`
	UpdateTime          string    `json:"updateTime"`
	HTTPSTrigger        *struct{} `json:"httpsTrigger"`
	EventTrigger        *struct {
		EventType string `json:"eventType"`
		Resource  string `json:"resource"`
	} `json:"eventTrigger"`
	SecretEnvVars []cfSecretRef `json:"secretEnvironmentVariables"`
	SecretVolumes []cfSecretRef `json:"secretVolumes"`
}

// extractCloudFunctionV1 extracts bounded, redaction-safe typed depth for one CAI
// first-generation Cloud Function. It returns the Terraform/drift/monitoring
// attribute set (status, runtime, entry point, available memory, ingress, VPC
// egress, trigger type, event type, secret mount count, update time, and the
// fingerprinted runtime service account), the source bucket, VPC connector,
// mounted secrets, event-trigger topic, and fingerprinted runtime service-account
// email as correlation anchors, and the typed function_source_bucket,
// function_uses_vpc_connector, function_mounts_secret, and
// function_triggered_by_topic edges. The runtime service account is joined via
// its fingerprinted-email digest (the IAM/trust layer owns the inbound edge); the
// https trigger URL, secret values, env values, and source object path are never
// read or persisted.
func extractCloudFunctionV1(ctx ExtractContext) (AttributeExtraction, error) {
	var data cloudFunctionV1Data
	if err := json.Unmarshal(ctx.Data, &data); err != nil {
		return AttributeExtraction{}, fmt.Errorf("decode cloud function v1 data: %w", err)
	}

	saDigest := secretsiam.GCPServiceAccountEmailDigest(cloudFunctionV1ServiceAccountEmail(ctx.ProjectID, data))
	attrs := cloudFunctionV1Attributes(ctx.ProjectID, data, saDigest)

	var anchors []string
	var rels []RelationshipObservation
	if saDigest != "" {
		anchors = append(anchors, saDigest)
	}
	if bucket := cloudFunctionArchiveBucket(data.SourceArchiveURL); bucket != "" {
		full := storageBucketResourceNamePrefixFmt + bucket
		anchors = append(anchors, full)
		rels = append(rels, cloudFunctionEdge(ctx, relationshipTypeFunctionSourceBucket, full, assetTypeStorageBucket))
	}
	if connector := cloudFunctionConnectorFullName(ctx.FullResourceName, data.VPCConnector); connector != "" {
		anchors = append(anchors, connector)
		rels = append(rels, cloudFunctionEdge(ctx, relationshipTypeFunctionUsesVPCConnector, connector, assetTypeVPCAccessConnector))
	}
	for _, secret := range cloudFunctionV1SecretFullNames(ctx.ProjectID, data) {
		anchors = append(anchors, secret)
		rels = append(rels, cloudFunctionEdge(ctx, relationshipTypeFunctionMountsSecret, secret, secretManagerSecretAssetType))
	}
	if data.EventTrigger != nil {
		if topic := secretManagerTopicFullName(data.EventTrigger.Resource); topic != "" {
			anchors = append(anchors, topic)
			rels = append(rels, cloudFunctionEdge(ctx, relationshipTypeFunctionTriggeredByTopic, topic, assetTypePubSubTopic))
		}
	}

	return AttributeExtraction{
		Attributes:         attrs,
		CorrelationAnchors: dedupeNonEmpty(anchors),
		Relationships:      rels,
	}, nil
}

// cloudFunctionV1ServiceAccountEmail returns the function's runtime
// service-account email, deriving the gen1 default identity
// ({projectId}@appspot.gserviceaccount.com) when the field is empty, so a
// function running as the default identity still fingerprints and correlates
// like the explicitly-configured case. It returns "" only when neither an
// explicit email nor a project to derive the default from is available.
func cloudFunctionV1ServiceAccountEmail(projectID string, data cloudFunctionV1Data) string {
	if v := strings.TrimSpace(data.ServiceAccountEmail); v != "" {
		return v
	}
	// Derive the default identity only for a function that is actually present
	// (every real gen1 function reports a runtime); a blank blob fabricates no
	// default-SA posture.
	if strings.TrimSpace(data.Runtime) == "" {
		return ""
	}
	if p := strings.TrimSpace(projectID); p != "" {
		return p + "@appspot.gserviceaccount.com"
	}
	return ""
}

// cloudFunctionV1Attributes assembles the bounded attribute map. Absent fields
// are omitted rather than written as zero values.
func cloudFunctionV1Attributes(projectID string, data cloudFunctionV1Data, saDigest string) map[string]any {
	attrs := map[string]any{}
	if v := strings.TrimSpace(data.Status); v != "" {
		attrs["status"] = v
	}
	if v := strings.TrimSpace(data.Runtime); v != "" {
		attrs["runtime"] = v
	}
	if v := strings.TrimSpace(data.EntryPoint); v != "" {
		attrs["entry_point"] = v
	}
	if data.AvailableMemoryMb != nil {
		attrs["available_memory_mb"] = *data.AvailableMemoryMb
	}
	if v := strings.TrimSpace(data.IngressSettings); v != "" {
		attrs["ingress_settings"] = v
	}
	if v := strings.TrimSpace(data.VPCConnectorEgress); v != "" {
		attrs["vpc_egress"] = v
	}
	switch {
	case data.EventTrigger != nil:
		attrs["trigger_type"] = "event"
		if v := strings.TrimSpace(data.EventTrigger.EventType); v != "" {
			attrs["event_type"] = v
		}
	case data.HTTPSTrigger != nil:
		attrs["trigger_type"] = "https"
	}
	if n := cloudFunctionV1DistinctSecretCount(projectID, data); n > 0 {
		attrs["secret_mount_count"] = n
	}
	if v, ok := normalizeRFC3339(data.UpdateTime); ok {
		attrs["update_time"] = v
	}
	if saDigest != "" {
		attrs["service_account_fingerprint"] = saDigest
	}
	return attrs
}

// cloudFunctionV1SecretRefs returns every secret reference the function mounts,
// from both env secretKeyRef sources and secret volumes, in observation order.
func cloudFunctionV1SecretRefs(data cloudFunctionV1Data) []cfSecretRef {
	return append(append([]cfSecretRef{}, data.SecretEnvVars...), data.SecretVolumes...)
}

// cloudFunctionV1SecretFullNames returns the deduplicated mounted-secret full
// resource names, reusing the shared per-reference resolver.
func cloudFunctionV1SecretFullNames(fallbackProject string, data cloudFunctionV1Data) []string {
	refs := cloudFunctionV1SecretRefs(data)
	if len(refs) == 0 {
		return nil
	}
	names := make([]string, 0, len(refs))
	for _, ref := range refs {
		if name := cloudFunctionSecretFullName(fallbackProject, ref); name != "" {
			names = append(names, name)
		}
	}
	return dedupeNonEmpty(names)
}

// cloudFunctionV1DistinctSecretCount counts distinct mounted secrets, deduping by
// resolved full resource name (so the same secret referenced twice counts once)
// and skipping an unresolved absolute (wrong-domain) reference; a bare id with no
// project falls back to its raw reference.
func cloudFunctionV1DistinctSecretCount(fallbackProject string, data cloudFunctionV1Data) int {
	seen := map[string]struct{}{}
	for _, ref := range cloudFunctionV1SecretRefs(data) {
		trimmed := strings.TrimSpace(ref.Secret)
		if trimmed == "" {
			continue
		}
		key := cloudFunctionSecretFullName(fallbackProject, ref)
		if key == "" {
			if strings.HasPrefix(trimmed, "//") {
				continue
			}
			key = trimmed
		}
		seen[key] = struct{}{}
	}
	return len(seen)
}
