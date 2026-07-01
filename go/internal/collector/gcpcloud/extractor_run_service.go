// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/secretsiam"
)

// Asset type and full-resource-name prefix for the Serverless VPC Access
// connector a Cloud Run Service egresses through. The Secret Manager Secret
// endpoint reuses secretManagerSecretAssetType already declared in
// gcp_secrets_iam.go.
const (
	assetTypeVPCAccessConnector     = "vpcaccess.googleapis.com/Connector"
	vpcAccessResourceNamePrefix     = "//vpcaccess.googleapis.com/"
	secretManagerResourceNamePrefix = "//secretmanager.googleapis.com/"
)

// Bounded provider relationship types for Cloud Run Service edges. Each is a
// stable, bounded string carried on a gcp_cloud_relationship fact; the reducer
// materializes an edge only when both endpoints resolve exactly.
const (
	relationshipTypeRunServiceUsesVPCConnector = "run_service_uses_vpc_connector"
	relationshipTypeRunServiceMountsSecret     = "run_service_mounts_secret"
)

func init() {
	RegisterAssetExtractor(assetTypeRunService, extractRunService)
}

// runServiceData is the bounded view of a CAI run.googleapis.com/Service
// resource.data blob (Cloud Run Admin API v2 shape). Only redaction-safe
// control-plane metadata and resource references are decoded. Container env
// values are never decoded — only env keys and secret references are read — so a
// runtime value cannot be surfaced. Container images are handled by the shared
// image-reference path (parseImageReferences) and are not re-extracted here.
type runServiceData struct {
	Ingress             string `json:"ingress"`
	LatestReadyRevision string `json:"latestReadyRevision"`
	CreateTime          string `json:"createTime"`
	Template            struct {
		ServiceAccount       string `json:"serviceAccount"`
		ExecutionEnvironment string `json:"executionEnvironment"`
		Scaling              *struct {
			MinInstanceCount *int `json:"minInstanceCount"`
			MaxInstanceCount *int `json:"maxInstanceCount"`
		} `json:"scaling"`
		VPCAccess *struct {
			Connector string `json:"connector"`
			Egress    string `json:"egress"`
		} `json:"vpcAccess"`
		Containers []struct {
			Env []struct {
				Name        string `json:"name"`
				ValueSource *struct {
					SecretKeyRef *struct {
						Secret string `json:"secret"`
					} `json:"secretKeyRef"`
				} `json:"valueSource"`
			} `json:"env"`
		} `json:"containers"`
		Volumes []struct {
			Secret *struct {
				Secret string `json:"secret"`
			} `json:"secret"`
		} `json:"volumes"`
	} `json:"template"`
}

// extractRunService extracts bounded, redaction-safe typed depth for one Cloud
// Run Service CAI asset. It returns the Terraform/drift/monitoring attribute set
// (ingress, execution environment, VPC egress posture, scaling bounds, latest
// ready revision, creation time, and bounded container/env/secret counts plus
// the env keys), the fingerprinted runtime service-account email, the VPC
// connector, and mounted secrets as cross-source correlation anchors, and the
// typed run_service_uses_vpc_connector and run_service_mounts_secret edges. The
// runtime service account is joined via its fingerprinted-email digest (the
// IAM/trust layer owns that inbound edge); its raw email is never persisted.
func extractRunService(ctx ExtractContext) (AttributeExtraction, error) {
	var data runServiceData
	if err := json.Unmarshal(ctx.Data, &data); err != nil {
		return AttributeExtraction{}, fmt.Errorf("decode run service data: %w", err)
	}

	saDigest := secretsiam.GCPServiceAccountEmailDigest(data.Template.ServiceAccount)
	attrs := runServiceAttributes(data, saDigest)

	var anchors []string
	var rels []RelationshipObservation
	if saDigest != "" {
		anchors = append(anchors, saDigest)
	}
	if data.Template.VPCAccess != nil {
		if connector := runServiceConnectorFullName(data.Template.VPCAccess.Connector); connector != "" {
			anchors = append(anchors, connector)
			rels = append(rels, runServiceEdge(ctx, relationshipTypeRunServiceUsesVPCConnector, connector, assetTypeVPCAccessConnector))
		}
	}
	for _, secret := range runServiceSecretFullNames(ctx.ProjectID, data) {
		anchors = append(anchors, secret)
		rels = append(rels, runServiceEdge(ctx, relationshipTypeRunServiceMountsSecret, secret, secretManagerSecretAssetType))
	}

	return AttributeExtraction{
		Attributes:         attrs,
		CorrelationAnchors: dedupeNonEmpty(anchors),
		Relationships:      rels,
	}, nil
}

// runServiceAttributes assembles the bounded attribute map. Empty or absent
// fields are omitted rather than written as zero values so a partial CAI page
// does not fabricate a posture. Scaling counts are pointer-decoded so a genuine
// minInstanceCount of 0 (scale-to-zero posture) is distinguishable from an absent
// scaling block.
func runServiceAttributes(data runServiceData, saDigest string) map[string]any {
	attrs := map[string]any{}
	if v := strings.TrimSpace(data.Ingress); v != "" {
		attrs["ingress"] = v
	}
	if v := strings.TrimSpace(data.Template.ExecutionEnvironment); v != "" {
		attrs["execution_environment"] = v
	}
	if va := data.Template.VPCAccess; va != nil {
		if v := strings.TrimSpace(va.Egress); v != "" {
			attrs["vpc_egress"] = v
		}
	}
	if s := data.Template.Scaling; s != nil {
		if s.MinInstanceCount != nil {
			attrs["scaling_min_instance_count"] = *s.MinInstanceCount
		}
		if s.MaxInstanceCount != nil {
			attrs["scaling_max_instance_count"] = *s.MaxInstanceCount
		}
	}
	if v := strings.TrimSpace(data.LatestReadyRevision); v != "" {
		attrs["latest_ready_revision"] = v
	}
	if v, ok := normalizeRFC3339(data.CreateTime); ok {
		attrs["creation_time"] = v
	}
	if n := len(data.Template.Containers); n > 0 {
		attrs["container_count"] = n
	}
	if keys := runServiceEnvKeys(data); len(keys) > 0 {
		attrs["env_keys"] = keys
		attrs["env_key_count"] = len(keys)
	}
	// Count distinct mounted secrets by their raw reference, independent of
	// project resolution, so the posture count is stable even when a bare secret
	// id cannot be expanded to a full resource name for an edge.
	if n := runServiceDistinctSecretCount(data); n > 0 {
		attrs["secret_mount_count"] = n
	}
	if saDigest != "" {
		attrs["service_account_fingerprint"] = saDigest
	}
	return attrs
}

// runServiceEnvKeys returns the sorted, deduplicated set of container environment
// variable names across all containers. Only keys are read; env values (literal
// or secret-sourced) are never decoded, so no runtime value can leak.
func runServiceEnvKeys(data runServiceData) []string {
	seen := map[string]struct{}{}
	var keys []string
	for _, container := range data.Template.Containers {
		for _, env := range container.Env {
			name := strings.TrimSpace(env.Name)
			if name == "" {
				continue
			}
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			keys = append(keys, name)
		}
	}
	sort.Strings(keys)
	return keys
}

// runServiceSecretRefs returns every raw secret reference the service mounts,
// from both env secretKeyRef sources and secret volumes, in observation order.
func runServiceSecretRefs(data runServiceData) []string {
	var refs []string
	for _, container := range data.Template.Containers {
		for _, env := range container.Env {
			if env.ValueSource != nil && env.ValueSource.SecretKeyRef != nil {
				refs = append(refs, env.ValueSource.SecretKeyRef.Secret)
			}
		}
	}
	for _, volume := range data.Template.Volumes {
		if volume.Secret != nil {
			refs = append(refs, volume.Secret.Secret)
		}
	}
	return refs
}

// runServiceSecretFullNames returns the deduplicated Secret Manager Secret full
// resource names the service mounts. A bare secret id is expanded with projectID;
// when projectID is empty a bare id yields no full name (and thus no edge).
func runServiceSecretFullNames(projectID string, data runServiceData) []string {
	refs := runServiceSecretRefs(data)
	if len(refs) == 0 {
		return nil
	}
	names := make([]string, 0, len(refs))
	for _, ref := range refs {
		if name := runServiceSecretFullName(projectID, ref); name != "" {
			names = append(names, name)
		}
	}
	return dedupeNonEmpty(names)
}

// runServiceDistinctSecretCount counts distinct mounted secrets by their raw
// reference, so the posture count does not depend on project resolution.
func runServiceDistinctSecretCount(data runServiceData) int {
	seen := map[string]struct{}{}
	for _, ref := range runServiceSecretRefs(data) {
		trimmed := strings.TrimSpace(ref)
		if trimmed == "" {
			continue
		}
		seen[trimmed] = struct{}{}
	}
	return len(seen)
}

// runServiceConnectorFullName builds the CAI Serverless VPC Access connector full
// resource name from a connector reference (projects/.../connectors/...). An
// already-normalized CAI full resource name (//vpcaccess.googleapis.com/...) is
// returned unchanged so the prefix is never doubled. It returns "" for a blank
// reference so the caller emits no connector edge.
func runServiceConnectorFullName(connector string) string {
	trimmed := strings.TrimSpace(connector)
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(trimmed, "//") {
		return trimmed
	}
	return vpcAccessResourceNamePrefix + strings.TrimPrefix(trimmed, "/")
}

// runServiceSecretFullName builds the CAI Secret Manager Secret full resource
// name from a secret reference. A bare secret id (no "projects/" prefix) is
// expanded with projectID; an already-relative name (projects/.../secrets/...) is
// prefixed; an already-normalized CAI full resource name is returned unchanged. It
// returns "" for a blank reference, or for a bare id with no project to resolve
// against, so the caller emits no secret edge it cannot ground.
func runServiceSecretFullName(projectID, secretRef string) string {
	trimmed := strings.TrimSpace(secretRef)
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(trimmed, "//") {
		return trimmed
	}
	if strings.HasPrefix(trimmed, "projects/") {
		return secretManagerResourceNamePrefix + trimmed
	}
	if strings.HasPrefix(trimmed, "/projects/") {
		return secretManagerResourceNamePrefix + strings.TrimPrefix(trimmed, "/")
	}
	project := strings.TrimSpace(projectID)
	if project == "" {
		return ""
	}
	return secretManagerResourceNamePrefix + "projects/" + project + "/secrets/" + trimmed
}

func runServiceEdge(ctx ExtractContext, relationshipType, targetName, targetType string) RelationshipObservation {
	return RelationshipObservation{
		SourceFullResourceName: ctx.FullResourceName,
		SourceAssetType:        ctx.AssetType,
		RelationshipType:       relationshipType,
		TargetFullResourceName: targetName,
		TargetAssetType:        targetType,
		SupportState:           RelationshipSupportSupported,
	}
}
