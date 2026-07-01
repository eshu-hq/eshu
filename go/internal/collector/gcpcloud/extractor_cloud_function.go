// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/secretsiam"
)

// assetTypeCloudFunction is the CAI asset type for a Cloud Functions function
// (gen1 and gen2 share this type; the environment field distinguishes them). The
// VPC connector, Secret Manager, Pub/Sub Topic, and Storage Bucket endpoints
// reuse the asset-type constants and full-resource-name builders already declared
// in this package.
const assetTypeCloudFunction = "cloudfunctions.googleapis.com/Function"

// Bounded provider relationship types for Cloud Functions edges. Each is a
// stable, bounded string carried on a gcp_cloud_relationship fact; the reducer
// materializes an edge only when both endpoints resolve exactly.
const (
	relationshipTypeFunctionUsesVPCConnector = "function_uses_vpc_connector"
	// #nosec G101 -- bounded gcp_cloud_relationship type label, not a credential
	relationshipTypeFunctionMountsSecret     = "function_mounts_secret"
	relationshipTypeFunctionTriggeredByTopic = "function_triggered_by_topic"
	relationshipTypeFunctionSourceBucket     = "function_source_bucket"
)

func init() {
	RegisterAssetExtractor(assetTypeCloudFunction, extractCloudFunction)
}

// cfSecretRef is a bounded Cloud Functions secret reference (from
// secretEnvironmentVariables or secretVolumes). Only the secret id and its
// project are decoded — never the value, key, or version — so no secret material
// is surfaced.
type cfSecretRef struct {
	Secret    string `json:"secret"`
	ProjectID string `json:"projectId"`
}

// cloudFunctionData is the bounded view of a CAI
// cloudfunctions.googleapis.com/Function resource.data blob. It unions the gen1
// (v1, top-level runtime/serviceAccountEmail/vpcConnector/sourceArchiveUrl) and
// gen2 (v2, buildConfig/serviceConfig) shapes; a resolver prefers the gen2 nested
// fields and falls back to the gen1 top-level fields. Only redaction-safe
// control-plane metadata and resource references are decoded — never env values,
// secret values, source object paths, or the https trigger URL.
type cloudFunctionData struct {
	Environment string `json:"environment"`
	State       string `json:"state"`
	Status      string `json:"status"`
	UpdateTime  string `json:"updateTime"`

	// gen1 top-level fields.
	Runtime             string        `json:"runtime"`
	ServiceAccountEmail string        `json:"serviceAccountEmail"`
	VPCConnector        string        `json:"vpcConnector"`
	VPCConnectorEgress  string        `json:"vpcConnectorEgressSettings"`
	IngressSettings     string        `json:"ingressSettings"`
	SourceArchiveURL    string        `json:"sourceArchiveUrl"`
	SecretEnvVars       []cfSecretRef `json:"secretEnvironmentVariables"`
	SecretVolumes       []cfSecretRef `json:"secretVolumes"`

	// gen2 nested config.
	BuildConfig *struct {
		Runtime string `json:"runtime"`
		Source  *struct {
			StorageSource *struct {
				Bucket string `json:"bucket"`
			} `json:"storageSource"`
		} `json:"source"`
	} `json:"buildConfig"`
	ServiceConfig *struct {
		ServiceAccountEmail string        `json:"serviceAccountEmail"`
		VPCConnector        string        `json:"vpcConnector"`
		VPCConnectorEgress  string        `json:"vpcConnectorEgressSettings"`
		IngressSettings     string        `json:"ingressSettings"`
		SecretEnvVars       []cfSecretRef `json:"secretEnvironmentVariables"`
		SecretVolumes       []cfSecretRef `json:"secretVolumes"`
	} `json:"serviceConfig"`
	EventTrigger *struct {
		EventType           string `json:"eventType"`
		PubsubTopic         string `json:"pubsubTopic"`
		Resource            string `json:"resource"`
		ServiceAccountEmail string `json:"serviceAccountEmail"`
	} `json:"eventTrigger"`
}

// extractCloudFunction extracts bounded, redaction-safe typed depth for one CAI
// Cloud Functions asset. It returns the Terraform/drift/monitoring attribute set
// (environment, state, runtime, ingress, VPC egress, event type, secret mount
// count, update time, and the fingerprinted runtime service account), the source
// bucket, VPC connector, mounted secrets, trigger topic, and fingerprinted
// runtime and trigger service-account emails as correlation anchors, and the
// typed function_source_bucket, function_uses_vpc_connector,
// function_mounts_secret, and function_triggered_by_topic edges. Service accounts
// are joined via their fingerprinted-email digests (the IAM/trust layer owns
// those inbound edges); raw emails, secret values, source object paths, and the
// https trigger URL are never read or persisted.
func extractCloudFunction(ctx ExtractContext) (AttributeExtraction, error) {
	var data cloudFunctionData
	if err := json.Unmarshal(ctx.Data, &data); err != nil {
		return AttributeExtraction{}, fmt.Errorf("decode cloud function data: %w", err)
	}

	runtimeSADigest := secretsiam.GCPServiceAccountEmailDigest(cloudFunctionServiceAccountEmail(data))
	attrs := cloudFunctionAttributes(ctx.ProjectID, data, runtimeSADigest)

	var anchors []string
	var rels []RelationshipObservation
	if runtimeSADigest != "" {
		anchors = append(anchors, runtimeSADigest)
	}
	if data.EventTrigger != nil {
		if d := secretsiam.GCPServiceAccountEmailDigest(data.EventTrigger.ServiceAccountEmail); d != "" {
			anchors = append(anchors, d)
		}
	}
	if bucket := cloudFunctionSourceBucketFullName(data); bucket != "" {
		anchors = append(anchors, bucket)
		rels = append(rels, cloudFunctionEdge(ctx, relationshipTypeFunctionSourceBucket, bucket, assetTypeStorageBucket))
	}
	if connector := cloudFunctionConnectorFullName(ctx.FullResourceName, cloudFunctionVPCConnector(data)); connector != "" {
		anchors = append(anchors, connector)
		rels = append(rels, cloudFunctionEdge(ctx, relationshipTypeFunctionUsesVPCConnector, connector, assetTypeVPCAccessConnector))
	}
	for _, secret := range cloudFunctionSecretFullNames(ctx.ProjectID, data) {
		anchors = append(anchors, secret)
		rels = append(rels, cloudFunctionEdge(ctx, relationshipTypeFunctionMountsSecret, secret, secretManagerSecretAssetType))
	}
	if topic := cloudFunctionTriggerTopicFullName(data); topic != "" {
		anchors = append(anchors, topic)
		rels = append(rels, cloudFunctionEdge(ctx, relationshipTypeFunctionTriggeredByTopic, topic, assetTypePubSubTopic))
	}

	return AttributeExtraction{
		Attributes:         attrs,
		CorrelationAnchors: dedupeNonEmpty(anchors),
		Relationships:      rels,
	}, nil
}

// cloudFunctionAttributes assembles the bounded attribute map, preferring gen2
// nested config and falling back to gen1 top-level fields. Absent fields are
// omitted rather than written as zero values.
func cloudFunctionAttributes(projectID string, data cloudFunctionData, runtimeSADigest string) map[string]any {
	attrs := map[string]any{}
	if v := strings.TrimSpace(data.Environment); v != "" {
		attrs["environment"] = v
	}
	if v := firstNonEmpty(data.State, data.Status); v != "" {
		attrs["state"] = v
	}
	if v := strings.TrimSpace(cloudFunctionRuntime(data)); v != "" {
		attrs["runtime"] = v
	}
	if v := strings.TrimSpace(cloudFunctionIngressSettings(data)); v != "" {
		attrs["ingress_settings"] = v
	}
	if v := strings.TrimSpace(cloudFunctionVPCEgress(data)); v != "" {
		attrs["vpc_egress"] = v
	}
	if data.EventTrigger != nil {
		if v := strings.TrimSpace(data.EventTrigger.EventType); v != "" {
			attrs["event_type"] = v
		}
	}
	if n := cloudFunctionDistinctSecretCount(projectID, data); n > 0 {
		attrs["secret_mount_count"] = n
	}
	if v, ok := normalizeRFC3339(data.UpdateTime); ok {
		attrs["update_time"] = v
	}
	if runtimeSADigest != "" {
		attrs["service_account_fingerprint"] = runtimeSADigest
	}
	return attrs
}

// cloudFunctionServiceAccountEmail returns the runtime service-account email,
// preferring the gen2 serviceConfig over the gen1 top-level field.
func cloudFunctionServiceAccountEmail(data cloudFunctionData) string {
	if sc := data.ServiceConfig; sc != nil && strings.TrimSpace(sc.ServiceAccountEmail) != "" {
		return sc.ServiceAccountEmail
	}
	return data.ServiceAccountEmail
}

func cloudFunctionRuntime(data cloudFunctionData) string {
	if bc := data.BuildConfig; bc != nil && strings.TrimSpace(bc.Runtime) != "" {
		return bc.Runtime
	}
	return data.Runtime
}

func cloudFunctionIngressSettings(data cloudFunctionData) string {
	if sc := data.ServiceConfig; sc != nil && strings.TrimSpace(sc.IngressSettings) != "" {
		return sc.IngressSettings
	}
	return data.IngressSettings
}

// cloudFunctionConnectorFullName resolves the function's VPC connector to its CAI
// full resource name. The gen2 serviceConfig and gen1 vpcConnector fields may be
// a fully qualified URI, a relative projects/.../connectors/... name, or (gen1
// only) a bare connector name; a bare name is qualified with the function's own
// project and location before the shared connector builder validates and prefixes
// it, so a short name never produces a bogus //vpcaccess.googleapis.com/<name>
// target the reducer would silently drop.
func cloudFunctionConnectorFullName(functionFullName, connector string) string {
	trimmed := strings.TrimSpace(connector)
	if trimmed == "" {
		return ""
	}
	if !strings.HasPrefix(trimmed, "//") && !strings.Contains(trimmed, "/") {
		project, location := cloudFunctionProjectLocation(functionFullName)
		if project == "" || location == "" {
			return ""
		}
		trimmed = "projects/" + project + "/locations/" + location + "/connectors/" + trimmed
	}
	return runServiceConnectorFullName(trimmed)
}

// cloudFunctionProjectLocation extracts the project id and location from a Cloud
// Functions CAI full resource name
// (//cloudfunctions.googleapis.com/projects/<project>/locations/<location>/functions/<name>).
// It returns empty strings when either segment is absent.
func cloudFunctionProjectLocation(functionFullName string) (project, location string) {
	segments := strings.Split(strings.TrimSpace(functionFullName), "/")
	for i := 0; i+1 < len(segments); i++ {
		switch segments[i] {
		case "projects":
			project = segments[i+1]
		case "locations":
			location = segments[i+1]
		}
	}
	return project, location
}

func cloudFunctionVPCConnector(data cloudFunctionData) string {
	if sc := data.ServiceConfig; sc != nil && strings.TrimSpace(sc.VPCConnector) != "" {
		return sc.VPCConnector
	}
	return data.VPCConnector
}

func cloudFunctionVPCEgress(data cloudFunctionData) string {
	if sc := data.ServiceConfig; sc != nil && strings.TrimSpace(sc.VPCConnectorEgress) != "" {
		return sc.VPCConnectorEgress
	}
	return data.VPCConnectorEgress
}

// cloudFunctionSecretRefs returns every secret reference the function mounts,
// preferring the gen2 serviceConfig lists and falling back to the gen1 top-level
// lists, in observation order.
func cloudFunctionSecretRefs(data cloudFunctionData) []cfSecretRef {
	if sc := data.ServiceConfig; sc != nil && (len(sc.SecretEnvVars) > 0 || len(sc.SecretVolumes) > 0) {
		return append(append([]cfSecretRef{}, sc.SecretEnvVars...), sc.SecretVolumes...)
	}
	return append(append([]cfSecretRef{}, data.SecretEnvVars...), data.SecretVolumes...)
}

// cloudFunctionSecretFullName resolves one secret reference to its CAI full
// resource name, using the reference's own projectId when set and otherwise the
// function's project. It reuses the Cloud Run Service secret builder so the same
// bare-id/relative/absolute handling and domain validation apply.
func cloudFunctionSecretFullName(fallbackProject string, ref cfSecretRef) string {
	project := strings.TrimSpace(ref.ProjectID)
	if project == "" {
		project = strings.TrimSpace(fallbackProject)
	}
	return runServiceSecretFullName(project, ref.Secret)
}

// cloudFunctionSecretFullNames returns the deduplicated mounted-secret full
// resource names.
func cloudFunctionSecretFullNames(fallbackProject string, data cloudFunctionData) []string {
	refs := cloudFunctionSecretRefs(data)
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

// cloudFunctionDistinctSecretCount counts distinct mounted secrets, deduplicating
// by resolved full resource name so the same secret referenced twice counts once;
// an unresolved absolute (wrong-domain) reference is not a secret and is skipped,
// while a bare id with no project falls back to its raw reference.
func cloudFunctionDistinctSecretCount(fallbackProject string, data cloudFunctionData) int {
	seen := map[string]struct{}{}
	for _, ref := range cloudFunctionSecretRefs(data) {
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

// cloudFunctionSourceBucketFullName returns the CAI Storage Bucket full resource
// name of the function's source archive: the gen2 storageSource bucket or the
// bucket parsed from the gen1 gs:// sourceArchiveUrl. The object path is always
// dropped.
func cloudFunctionSourceBucketFullName(data cloudFunctionData) string {
	bucket := ""
	if bc := data.BuildConfig; bc != nil && bc.Source != nil && bc.Source.StorageSource != nil {
		bucket = strings.TrimSpace(bc.Source.StorageSource.Bucket)
	}
	if bucket == "" {
		bucket = cloudFunctionArchiveBucket(data.SourceArchiveURL)
	}
	if bucket == "" {
		return ""
	}
	return storageBucketResourceNamePrefixFmt + bucket
}

// cloudFunctionArchiveBucket extracts the bucket name from a gs://bucket/object
// URL, dropping the object path (which can carry sensitive artifact paths). It
// returns "" for a non-gs URL or a blank value.
func cloudFunctionArchiveBucket(sourceArchiveURL string) string {
	trimmed := strings.TrimSpace(sourceArchiveURL)
	const scheme = "gs://"
	if !strings.HasPrefix(trimmed, scheme) {
		return ""
	}
	rest := strings.TrimPrefix(trimmed, scheme)
	if idx := strings.Index(rest, "/"); idx >= 0 {
		rest = rest[:idx]
	}
	return strings.TrimSpace(rest)
}

// cloudFunctionTriggerTopicFullName returns the Pub/Sub Topic full resource name
// the function's event trigger subscribes to, from the gen2 pubsubTopic field or
// a gen1 event-trigger resource that names a topic. It reuses the shared topic
// builder, which returns "" for a non-topic reference.
func cloudFunctionTriggerTopicFullName(data cloudFunctionData) string {
	if data.EventTrigger == nil {
		return ""
	}
	if name := secretManagerTopicFullName(data.EventTrigger.PubsubTopic); name != "" {
		return name
	}
	return secretManagerTopicFullName(data.EventTrigger.Resource)
}

func cloudFunctionEdge(ctx ExtractContext, relationshipType, targetName, targetType string) RelationshipObservation {
	return RelationshipObservation{
		SourceFullResourceName: ctx.FullResourceName,
		SourceAssetType:        ctx.AssetType,
		RelationshipType:       relationshipType,
		TargetFullResourceName: targetName,
		TargetAssetType:        targetType,
		SupportState:           RelationshipSupportSupported,
	}
}
