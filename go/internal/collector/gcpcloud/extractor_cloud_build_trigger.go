// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/secretsiam"
)

// Bounded provider relationship type for Cloud Build Trigger edges. The
// assetTypeCloudBuildTrigger and assetTypeSourceRepo asset types are declared
// and owned by the Cloud Build Build extractor (extractor_cloud_build.go) and
// are reused here, never redeclared.
const relationshipTypeTriggerSourceRepo = "trigger_source_repo"

func init() {
	RegisterAssetExtractor(assetTypeCloudBuildTrigger, extractCloudBuildTrigger)
}

// cloudBuildTriggerData is the bounded view of a CAI
// cloudbuild.googleapis.com/BuildTrigger resource.data blob. Only
// redaction-safe control-plane metadata and resource references are decoded.
// `tags` is a free-form user-assigned annotation array (unlike the shared
// `labels` map, it is never fingerprinted by the collector's label path), so
// only its bounded count is kept here, never the tag strings themselves. The
// trigger's `build` template, `substitutions`, `filter` (a free-text CEL
// expression), `webhookConfig.secret` (a Secret Manager version reference used
// only to validate inbound webhook signatures), GitHub/GitLab/Bitbucket push
// and pull-request branch/tag regex detail, and `gitFileSource` are never
// decoded, so no build secret, CEL filter detail, or webhook secret reference
// can be surfaced.
type cloudBuildTriggerData struct {
	Name             string   `json:"name"`
	CreateTime       string   `json:"createTime"`
	Disabled         *bool    `json:"disabled"`
	Filename         string   `json:"filename"`
	EventType        string   `json:"eventType"`
	IncludeBuildLogs string   `json:"includeBuildLogs"`
	ServiceAccount   string   `json:"serviceAccount"`
	IncludedFiles    []string `json:"includedFiles"`
	IgnoredFiles     []string `json:"ignoredFiles"`
	Tags             []string `json:"tags"`
	TriggerTemplate  *struct {
		ProjectID string `json:"projectId"`
		RepoName  string `json:"repoName"`
	} `json:"triggerTemplate"`
	GitHub *struct {
		Owner string `json:"owner"`
		Name  string `json:"name"`
	} `json:"github"`
	SourceToBuild *struct {
		Repository string `json:"repository"`
	} `json:"sourceToBuild"`
	PubsubConfig *struct {
		Topic string `json:"topic"`
	} `json:"pubsubConfig"`
	WebhookConfig         *struct{} `json:"webhookConfig"`
	RepositoryEventConfig *struct{} `json:"repositoryEventConfig"`
	ApprovalConfig        *struct {
		ApprovalRequired *bool `json:"approvalRequired"`
	} `json:"approvalConfig"`
}

// extractCloudBuildTrigger extracts bounded, redaction-safe typed depth for
// one CAI Cloud Build Trigger. It returns the Terraform/drift/monitoring
// attribute set (the user-assigned trigger name, disabled posture, creation
// time, build-config filename, event type, derived source type,
// include-build-logs posture, approval posture, bounded included/ignored
// file and tag counts, and the fingerprinted trigger service account), the
// source repo and fingerprinted service-account email as correlation
// anchors, and the typed trigger_source_repo edge for a Cloud Source
// Repositories `triggerTemplate`.
// A GitHub, GitLab Enterprise, Bitbucket Server, Pub/Sub, webhook, Repo-API,
// or manual source has no CAI-resolvable target asset type in this graph, so
// no edge is emitted for those; only the bounded `source_type` enum records
// which kind of source the trigger uses. The service account is joined via
// its fingerprinted-email digest; substitutions, the CEL `filter`, and the
// webhook secret reference are never read.
func extractCloudBuildTrigger(ctx ExtractContext) (AttributeExtraction, error) {
	var data cloudBuildTriggerData
	if err := json.Unmarshal(ctx.Data, &data); err != nil {
		return AttributeExtraction{}, fmt.Errorf("decode cloud build trigger data: %w", err)
	}

	saDigest := secretsiam.GCPServiceAccountEmailDigest(cloudBuildServiceAccountEmail(data.ServiceAccount))
	attrs := cloudBuildTriggerAttributes(data, saDigest)

	var anchors []string
	var rels []RelationshipObservation
	if saDigest != "" {
		anchors = append(anchors, saDigest)
	}
	if data.TriggerTemplate != nil {
		// triggerTemplate.projectId is optional and defaults to the trigger's
		// own project; fall back to it so a same-project repo edge is not
		// dropped.
		repoProject := strings.TrimSpace(data.TriggerTemplate.ProjectID)
		if repoProject == "" {
			repoProject, _ = eventarcProjectLocation(ctx.FullResourceName)
		}
		if repo := cloudBuildSourceRepoFullName(repoProject, data.TriggerTemplate.RepoName); repo != "" {
			anchors = append(anchors, repo)
			rels = append(rels, cloudBuildEdge(ctx, relationshipTypeTriggerSourceRepo, repo, assetTypeSourceRepo))
		}
	}

	return AttributeExtraction{
		Attributes:         attrs,
		CorrelationAnchors: dedupeNonEmpty(anchors),
		Relationships:      rels,
	}, nil
}

// cloudBuildTriggerAttributes assembles the bounded attribute map. Absent
// fields are omitted rather than written as zero values, so disabled and
// approval_required are present only when the API returned them.
func cloudBuildTriggerAttributes(data cloudBuildTriggerData, saDigest string) map[string]any {
	attrs := map[string]any{}
	if v := strings.TrimSpace(data.Name); v != "" {
		attrs["name"] = v
	}
	if data.Disabled != nil {
		attrs["disabled"] = *data.Disabled
	}
	if v, ok := normalizeRFC3339(data.CreateTime); ok {
		attrs["creation_time"] = v
	}
	if v := strings.TrimSpace(data.Filename); v != "" {
		attrs["filename"] = v
	}
	if v := strings.TrimSpace(data.EventType); v != "" {
		attrs["event_type"] = v
	}
	if v := cloudBuildTriggerSourceType(data); v != "" {
		attrs["source_type"] = v
	}
	if strings.TrimSpace(data.IncludeBuildLogs) == "INCLUDE_BUILD_LOGS_WITH_STATUS" {
		attrs["include_build_logs"] = true
	}
	if data.ApprovalConfig != nil && data.ApprovalConfig.ApprovalRequired != nil {
		attrs["approval_required"] = *data.ApprovalConfig.ApprovalRequired
	}
	if n := len(data.IncludedFiles); n > 0 {
		attrs["included_files_count"] = n
	}
	if n := len(data.IgnoredFiles); n > 0 {
		attrs["ignored_files_count"] = n
	}
	if n := len(data.Tags); n > 0 {
		attrs["tags_count"] = n
	}
	if saDigest != "" {
		attrs["service_account_fingerprint"] = saDigest
	}
	return attrs
}

// cloudBuildTriggerSourceType returns the bounded enum describing which
// mutually-exclusive source configuration the trigger carries. It checks the
// shape-specific fields first (authoritative regardless of eventType), then
// falls back to the API's own explicit `eventType` enum for a webhook or
// pubsub trigger whose config sub-message CAI did not populate, and finally a
// manual trigger, which carries no source config at all; "" when nothing is
// set.
func cloudBuildTriggerSourceType(data cloudBuildTriggerData) string {
	switch {
	case data.TriggerTemplate != nil:
		return "repo"
	case data.GitHub != nil:
		return "github"
	case data.RepositoryEventConfig != nil:
		return "repository_event"
	case data.SourceToBuild != nil:
		return "source_to_build"
	case data.PubsubConfig != nil:
		return "pubsub"
	case data.WebhookConfig != nil:
		return "webhook"
	}
	switch strings.ToUpper(strings.TrimSpace(data.EventType)) {
	case "WEBHOOK":
		return "webhook"
	case "PUBSUB":
		return "pubsub"
	case "MANUAL":
		return "manual"
	default:
		return ""
	}
}
