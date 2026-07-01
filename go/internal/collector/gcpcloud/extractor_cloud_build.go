// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/secretsiam"
)

// Asset types and full-resource-name prefixes for the Cloud Build Build endpoints.
// The Storage Bucket endpoint reuses storageBucketResourceNamePrefixFmt and the
// project/location parser is shared with the Cloud Functions extractor.
const (
	assetTypeCloudBuild          = "cloudbuild.googleapis.com/Build"
	assetTypeCloudBuildTrigger   = "cloudbuild.googleapis.com/BuildTrigger"
	assetTypeSourceRepo          = "sourcerepo.googleapis.com/Repository"
	cloudBuildResourceNamePrefix = "//cloudbuild.googleapis.com/"
	sourceRepoResourceNamePrefix = "//sourcerepo.googleapis.com/"
)

// Bounded provider relationship types for Cloud Build Build edges.
const (
	relationshipTypeBuildTriggeredBy  = "build_triggered_by"
	relationshipTypeBuildSourceBucket = "build_source_bucket"
	relationshipTypeBuildSourceRepo   = "build_source_repo"
)

func init() {
	RegisterAssetExtractor(assetTypeCloudBuild, extractCloudBuild)
}

// cloudBuildData is the bounded view of a CAI cloudbuild.googleapis.com/Build
// resource.data blob. Only redaction-safe control-plane metadata and resource
// references are decoded. Build substitutions (which can carry secrets), build
// logs, the log/source object paths, and build-step definitions are never
// decoded, so no build secret or log content can be surfaced.
type cloudBuildData struct {
	Status         string   `json:"status"`
	CreateTime     string   `json:"createTime"`
	FinishTime     string   `json:"finishTime"`
	ServiceAccount string   `json:"serviceAccount"`
	BuildTriggerID string   `json:"buildTriggerId"`
	LogURL         string   `json:"logUrl"`
	Images         []string `json:"images"`
	Source         struct {
		StorageSource *struct {
			Bucket string `json:"bucket"`
		} `json:"storageSource"`
		RepoSource *struct {
			ProjectID string `json:"projectId"`
			RepoName  string `json:"repoName"`
		} `json:"repoSource"`
	} `json:"source"`
	Results *struct {
		Images []struct {
			Name   string `json:"name"`
			Digest string `json:"digest"`
		} `json:"images"`
	} `json:"results"`
}

// extractCloudBuild extracts bounded, redaction-safe typed depth for one CAI Cloud
// Build Build. It returns the Terraform/drift/monitoring attribute set (status,
// create/finish time, log URL host, source type, output image count, and the
// fingerprinted build service account), the build trigger, source repo/bucket,
// output image digests, and fingerprinted service-account email as correlation
// anchors, and the typed build_triggered_by, build_source_repo, and
// build_source_bucket edges. Output image digests feed container-image identity
// correlation and are anchors rather than direct edges. The build service account
// is joined via its fingerprinted-email digest; build substitutions, logs, and
// source/log object paths are never read.
func extractCloudBuild(ctx ExtractContext) (AttributeExtraction, error) {
	var data cloudBuildData
	if err := json.Unmarshal(ctx.Data, &data); err != nil {
		return AttributeExtraction{}, fmt.Errorf("decode cloud build data: %w", err)
	}

	saDigest := secretsiam.GCPServiceAccountEmailDigest(cloudBuildServiceAccountEmail(data.ServiceAccount))
	digests := cloudBuildImageDigests(data)
	attrs := cloudBuildAttributes(data, saDigest, digests)

	var anchors []string
	var rels []RelationshipObservation
	if saDigest != "" {
		anchors = append(anchors, saDigest)
	}
	anchors = append(anchors, digests...)
	if trigger := cloudBuildTriggerFullName(ctx.FullResourceName, data.BuildTriggerID); trigger != "" {
		anchors = append(anchors, trigger)
		rels = append(rels, cloudBuildEdge(ctx, relationshipTypeBuildTriggeredBy, trigger, assetTypeCloudBuildTrigger))
	}
	if data.Source.RepoSource != nil {
		// repoSource.projectId is optional and defaults to the build's own
		// project; fall back to it so a same-project repo edge is not dropped.
		repoProject := strings.TrimSpace(data.Source.RepoSource.ProjectID)
		if repoProject == "" {
			repoProject, _ = cloudFunctionProjectLocation(ctx.FullResourceName)
		}
		if repo := cloudBuildSourceRepoFullName(repoProject, data.Source.RepoSource.RepoName); repo != "" {
			anchors = append(anchors, repo)
			rels = append(rels, cloudBuildEdge(ctx, relationshipTypeBuildSourceRepo, repo, assetTypeSourceRepo))
		}
	}
	if data.Source.StorageSource != nil {
		if bucket := strings.TrimSpace(data.Source.StorageSource.Bucket); bucket != "" {
			full := storageBucketResourceNamePrefixFmt + bucket
			anchors = append(anchors, full)
			rels = append(rels, cloudBuildEdge(ctx, relationshipTypeBuildSourceBucket, full, assetTypeStorageBucket))
		}
	}

	return AttributeExtraction{
		Attributes:         attrs,
		CorrelationAnchors: dedupeNonEmpty(anchors),
		Relationships:      rels,
	}, nil
}

// cloudBuildAttributes assembles the bounded attribute map. Absent fields are
// omitted rather than written as zero values.
func cloudBuildAttributes(data cloudBuildData, saDigest string, digests []string) map[string]any {
	attrs := map[string]any{}
	if v := strings.TrimSpace(data.Status); v != "" {
		attrs["status"] = v
	}
	if v, ok := normalizeRFC3339(data.CreateTime); ok {
		attrs["creation_time"] = v
	}
	if v, ok := normalizeRFC3339(data.FinishTime); ok {
		attrs["finish_time"] = v
	}
	if v := cloudBuildLogURLHost(data.LogURL); v != "" {
		attrs["log_url_host"] = v
	}
	switch {
	case data.Source.RepoSource != nil:
		attrs["source_type"] = "repo"
	case data.Source.StorageSource != nil:
		attrs["source_type"] = "storage"
	}
	if n := len(digests); n > 0 {
		attrs["image_count"] = n
	}
	if saDigest != "" {
		attrs["service_account_fingerprint"] = saDigest
	}
	return attrs
}

// cloudBuildImageDigests returns the deduplicated sha256 digests of the build's
// output images, from results.images digests and any digest-pinned requested
// image references. These are the cross-source join keys for container image
// identity.
func cloudBuildImageDigests(data cloudBuildData) []string {
	var digests []string
	if data.Results != nil {
		for _, img := range data.Results.Images {
			if d := strings.TrimSpace(img.Digest); strings.HasPrefix(d, "sha256:") {
				digests = append(digests, d)
				continue
			}
			if d := imageDigestFromReference(img.Name); d != "" {
				digests = append(digests, d)
			}
		}
	}
	for _, ref := range data.Images {
		if d := imageDigestFromReference(ref); d != "" {
			digests = append(digests, d)
		}
	}
	return dedupeNonEmpty(digests)
}

// cloudBuildServiceAccountEmail returns the build service-account email, stripping
// the projects/.../serviceAccounts/ prefix when the value is a relative resource
// name rather than a bare email.
func cloudBuildServiceAccountEmail(serviceAccount string) string {
	trimmed := strings.TrimSpace(serviceAccount)
	const marker = "/serviceAccounts/"
	if i := strings.LastIndex(trimmed, marker); i >= 0 {
		return trimmed[i+len(marker):]
	}
	return trimmed
}

// cloudBuildLogURLHost returns the lower-cased host of the build log URL,
// dropping the path and query (which can carry project and build identifiers),
// any userinfo (which could embed credentials), and the port. It returns "" for a
// non-http value.
func cloudBuildLogURLHost(logURL string) string {
	trimmed := strings.TrimSpace(logURL)
	for _, scheme := range []string{"https://", "http://"} {
		if !strings.HasPrefix(strings.ToLower(trimmed), scheme) {
			continue
		}
		rest := trimmed[len(scheme):]
		if i := strings.IndexAny(rest, "/?#"); i >= 0 {
			rest = rest[:i]
		}
		// Drop userinfo (user:pass@host) and the port (host:443).
		if i := strings.LastIndex(rest, "@"); i >= 0 {
			rest = rest[i+1:]
		}
		if i := strings.LastIndex(rest, ":"); i >= 0 {
			rest = rest[:i]
		}
		return strings.ToLower(strings.TrimSpace(rest))
	}
	return ""
}

// cloudBuildTriggerFullName builds the CAI BuildTrigger full resource name from
// the build's trigger id, deriving the project and location from the build's own
// full resource name. It returns the regional form when the build carries a
// location and the global form otherwise; "" when there is no trigger id or
// project to ground the endpoint.
func cloudBuildTriggerFullName(buildFullName, triggerID string) string {
	id := strings.TrimSpace(triggerID)
	if id == "" {
		return ""
	}
	project, location := cloudFunctionProjectLocation(buildFullName)
	if project == "" {
		return ""
	}
	if location != "" {
		return cloudBuildResourceNamePrefix + "projects/" + project + "/locations/" + location + "/triggers/" + id
	}
	return cloudBuildResourceNamePrefix + "projects/" + project + "/triggers/" + id
}

// cloudBuildSourceRepoFullName builds the CAI Cloud Source Repositories repository
// full resource name from a repoSource's project and repo name. It returns "" when
// either is absent.
func cloudBuildSourceRepoFullName(projectID, repoName string) string {
	project := strings.TrimSpace(projectID)
	repo := strings.TrimSpace(repoName)
	if project == "" || repo == "" {
		return ""
	}
	return sourceRepoResourceNamePrefix + "projects/" + project + "/repos/" + repo
}

func cloudBuildEdge(ctx ExtractContext, relationshipType, targetName, targetType string) RelationshipObservation {
	return RelationshipObservation{
		SourceFullResourceName: ctx.FullResourceName,
		SourceAssetType:        ctx.AssetType,
		RelationshipType:       relationshipType,
		TargetFullResourceName: targetName,
		TargetAssetType:        targetType,
		SupportState:           RelationshipSupportSupported,
	}
}
