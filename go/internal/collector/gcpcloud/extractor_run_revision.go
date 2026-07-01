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

// assetTypeRunRevision is the CAI asset type for a Cloud Run Revision. The VPC
// connector and Secret Manager endpoints reuse the asset-type constants and
// full-resource-name builders declared by the Cloud Run Service extractor in
// this package; the parent Service endpoint reuses assetTypeRunService.
const assetTypeRunRevision = "run.googleapis.com/Revision"

const revisionsPathSegment = "/revisions/"

// Bounded provider relationship types for Cloud Run Revision edges. Each is a
// stable, bounded string carried on a gcp_cloud_relationship fact; the reducer
// materializes an edge only when both endpoints resolve exactly.
const (
	relationshipTypeRevisionOfService = "revision_of_service"
	// #nosec G101 -- bounded gcp_cloud_relationship type label, not a credential
	relationshipTypeRevisionMountsSecret     = "revision_mounts_secret"
	relationshipTypeRevisionUsesVPCConnector = "revision_uses_vpc_connector"
)

func init() {
	RegisterAssetExtractor(assetTypeRunRevision, extractRunRevision)
}

// runRevisionData is the bounded view of a CAI run.googleapis.com/Revision
// resource.data blob (Cloud Run Admin API v2 shape). A Revision is the immutable
// deployed template, so the container, service-account, scaling, VPC, and secret
// fields sit at the top level rather than under a template block. Only
// redaction-safe control-plane metadata and resource references are decoded;
// container env values are never decoded — only env keys and secret references
// are read — so a runtime value cannot be surfaced.
type runRevisionData struct {
	ServiceAccount       string `json:"serviceAccount"`
	ExecutionEnvironment string `json:"executionEnvironment"`
	CreateTime           string `json:"createTime"`
	Scaling              *struct {
		MinInstanceCount *int `json:"minInstanceCount"`
		MaxInstanceCount *int `json:"maxInstanceCount"`
	} `json:"scaling"`
	VPCAccess *struct {
		Connector string `json:"connector"`
		Egress    string `json:"egress"`
	} `json:"vpcAccess"`
	Containers []struct {
		Image string `json:"image"`
		Env   []struct {
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
	Conditions []struct {
		Type  string `json:"type"`
		State string `json:"state"`
	} `json:"conditions"`
}

// extractRunRevision extracts bounded, redaction-safe typed depth for one Cloud
// Run Revision CAI asset. It returns the Terraform/drift/monitoring attribute set
// (execution environment, VPC egress posture, scaling bounds, primary container
// image and digest, creation time, Ready condition state, and bounded
// container/env/secret counts plus the env keys), the parent Service, VPC
// connector, mounted secrets, and image digests as cross-source correlation
// anchors, and the typed revision_of_service, revision_uses_vpc_connector, and
// revision_mounts_secret edges. Unlike the Service extractor, the Revision owns
// its container images (the shared image-reference path covers only Service and
// Job assets). The runtime service account is joined via its fingerprinted-email
// digest (the IAM/trust layer owns that inbound edge); its raw email is never
// persisted.
func extractRunRevision(ctx ExtractContext) (AttributeExtraction, error) {
	var data runRevisionData
	if err := json.Unmarshal(ctx.Data, &data); err != nil {
		return AttributeExtraction{}, fmt.Errorf("decode run revision data: %w", err)
	}

	saDigest := secretsiam.GCPServiceAccountEmailDigest(data.ServiceAccount)
	image, digest := runRevisionPrimaryImage(data)
	attrs := runRevisionAttributes(ctx.ProjectID, data, saDigest, image, digest)

	var anchors []string
	var rels []RelationshipObservation
	if parent := runRevisionParentServiceFullName(ctx.FullResourceName); parent != "" {
		anchors = append(anchors, parent)
		rels = append(rels, runRevisionEdge(ctx, relationshipTypeRevisionOfService, parent, assetTypeRunService))
	}
	if saDigest != "" {
		anchors = append(anchors, saDigest)
	}
	// Anchor every container's image digest, not just the primary, so a
	// sidecar image still participates in container-image-identity correlation.
	anchors = append(anchors, runRevisionImageDigests(data)...)
	if data.VPCAccess != nil {
		if connector := runServiceConnectorFullName(data.VPCAccess.Connector); connector != "" {
			anchors = append(anchors, connector)
			rels = append(rels, runRevisionEdge(ctx, relationshipTypeRevisionUsesVPCConnector, connector, assetTypeVPCAccessConnector))
		}
	}
	for _, secret := range runRevisionSecretFullNames(ctx.ProjectID, data) {
		anchors = append(anchors, secret)
		rels = append(rels, runRevisionEdge(ctx, relationshipTypeRevisionMountsSecret, secret, secretManagerSecretAssetType))
	}

	return AttributeExtraction{
		Attributes:         attrs,
		CorrelationAnchors: dedupeNonEmpty(anchors),
		Relationships:      rels,
	}, nil
}

// runRevisionAttributes assembles the bounded attribute map. Empty or absent
// fields are omitted rather than written as zero values so a partial CAI page
// does not fabricate a posture. Scaling counts are pointer-decoded so a genuine
// minInstanceCount of 0 (scale-to-zero posture) is distinguishable from an absent
// scaling block.
func runRevisionAttributes(projectID string, data runRevisionData, saDigest, image, digest string) map[string]any {
	attrs := map[string]any{}
	if v := strings.TrimSpace(data.ExecutionEnvironment); v != "" {
		attrs["execution_environment"] = v
	}
	if va := data.VPCAccess; va != nil {
		if v := strings.TrimSpace(va.Egress); v != "" {
			attrs["vpc_egress"] = v
		}
	}
	if s := data.Scaling; s != nil {
		if s.MinInstanceCount != nil {
			attrs["scaling_min_instance_count"] = *s.MinInstanceCount
		}
		if s.MaxInstanceCount != nil {
			attrs["scaling_max_instance_count"] = *s.MaxInstanceCount
		}
	}
	if v, ok := normalizeRFC3339(data.CreateTime); ok {
		attrs["creation_time"] = v
	}
	if n := len(data.Containers); n > 0 {
		attrs["container_count"] = n
	}
	if image != "" {
		attrs["container_image"] = image
	}
	if digest != "" {
		attrs["container_image_digest"] = digest
	}
	if keys := runRevisionEnvKeys(data); len(keys) > 0 {
		attrs["env_keys"] = keys
		attrs["env_key_count"] = len(keys)
	}
	if n := runRevisionDistinctSecretCount(projectID, data); n > 0 {
		attrs["secret_mount_count"] = n
	}
	if v := runRevisionReadyState(data); v != "" {
		attrs["ready_condition_state"] = v
	}
	if saDigest != "" {
		attrs["service_account_fingerprint"] = saDigest
	}
	return attrs
}

// runRevisionPrimaryImage returns the first container that declares an image (its
// reference) and that reference's sha256 digest (when it is digest-pinned). It
// skips containers with an empty image so a redacted or partial leading container
// does not blank the scalar image attributes when a later container has one. The
// digest is the cross-source join key for container image identity; a tag-only
// reference yields an empty digest. Every container's digest is anchored
// separately by runRevisionImageDigests, so a sidecar still correlates.
func runRevisionPrimaryImage(data runRevisionData) (image, digest string) {
	for _, container := range data.Containers {
		if ref := strings.TrimSpace(container.Image); ref != "" {
			return ref, imageDigestFromReference(ref)
		}
	}
	return "", ""
}

// runRevisionImageDigests returns the deduplicated sha256 digests of every
// container image reference that is digest-pinned. These are the cross-source
// join keys for container image identity; tag-only references contribute none.
func runRevisionImageDigests(data runRevisionData) []string {
	var digests []string
	for _, container := range data.Containers {
		if d := imageDigestFromReference(container.Image); d != "" {
			digests = append(digests, d)
		}
	}
	return dedupeNonEmpty(digests)
}

// runRevisionReadyState returns the state of the Ready condition when present. It
// is a bounded control-plane enum used for operator monitoring of revision
// health.
func runRevisionReadyState(data runRevisionData) string {
	for _, cond := range data.Conditions {
		if strings.EqualFold(strings.TrimSpace(cond.Type), "Ready") {
			return strings.TrimSpace(cond.State)
		}
	}
	return ""
}

// runRevisionEnvKeys returns the sorted, deduplicated set of container
// environment variable names across all containers. Only keys are read; env
// values (literal or secret-sourced) are never decoded, so no runtime value can
// leak.
func runRevisionEnvKeys(data runRevisionData) []string {
	seen := map[string]struct{}{}
	var keys []string
	for _, container := range data.Containers {
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

// runRevisionSecretRefs returns every raw secret reference the revision mounts,
// from both env secretKeyRef sources and secret volumes, in observation order.
func runRevisionSecretRefs(data runRevisionData) []string {
	var refs []string
	for _, container := range data.Containers {
		for _, env := range container.Env {
			if env.ValueSource != nil && env.ValueSource.SecretKeyRef != nil {
				refs = append(refs, env.ValueSource.SecretKeyRef.Secret)
			}
		}
	}
	for _, volume := range data.Volumes {
		if volume.Secret != nil {
			refs = append(refs, volume.Secret.Secret)
		}
	}
	return refs
}

// runRevisionSecretFullNames returns the deduplicated Secret Manager Secret full
// resource names the revision mounts, reusing the Cloud Run Service builder so
// the same bare-id/relative/absolute handling and domain validation apply.
func runRevisionSecretFullNames(projectID string, data runRevisionData) []string {
	refs := runRevisionSecretRefs(data)
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

// runRevisionDistinctSecretCount counts distinct mounted secrets, deduplicating
// by resolved full resource name so the same secret referenced in two forms
// (a bare id in one mount and its projects/.../secrets/... form in another) is
// counted once. A bare id that cannot be resolved without a project falls back
// to its raw reference as the dedup key, so the posture count is still stable.
func runRevisionDistinctSecretCount(projectID string, data runRevisionData) int {
	seen := map[string]struct{}{}
	for _, ref := range runRevisionSecretRefs(data) {
		trimmed := strings.TrimSpace(ref)
		if trimmed == "" {
			continue
		}
		key := runServiceSecretFullName(projectID, trimmed)
		if key == "" {
			// An absolute //... name that did not resolve carries a non-secret
			// domain prefix and is not a secret mount, so it must not inflate the
			// count. Only a bare id with no project to resolve against is a
			// legitimate-but-unresolvable mount; dedup it by its raw reference.
			if strings.HasPrefix(trimmed, "//") {
				continue
			}
			key = trimmed
		}
		seen[key] = struct{}{}
	}
	return len(seen)
}

// runRevisionParentServiceFullName derives the parent Service CAI full resource
// name from a revision full resource name
// (//run.googleapis.com/.../services/<svc>/revisions/<rev>) by truncating at the
// /revisions/ segment. It returns "" when the name carries no /revisions/
// segment, so no parent edge is fabricated.
func runRevisionParentServiceFullName(revisionFullName string) string {
	trimmed := strings.TrimSpace(revisionFullName)
	idx := strings.Index(trimmed, revisionsPathSegment)
	if idx <= 0 {
		return ""
	}
	return trimmed[:idx]
}

func runRevisionEdge(ctx ExtractContext, relationshipType, targetName, targetType string) RelationshipObservation {
	return RelationshipObservation{
		SourceFullResourceName: ctx.FullResourceName,
		SourceAssetType:        ctx.AssetType,
		RelationshipType:       relationshipType,
		TargetFullResourceName: targetName,
		TargetAssetType:        targetType,
		SupportState:           RelationshipSupportSupported,
	}
}
