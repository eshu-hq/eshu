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
const (
	relationshipTypeTriggerSourceRepo           = "trigger_source_repo"
	relationshipTypeTriggerSourceRepositoryLink = "trigger_source_repository_link"
)

// assetTypeDeveloperConnectGitRepositoryLink is the CAI asset type for a
// Developer Connect connected repository, per the live Cloud Asset Inventory
// supported-asset-types reference. A `sourceToBuild.repository` value is
// always shaped as this asset type's resource name
// (`projects/*/locations/*/connections/*/repositories/*`) regardless of the
// underlying `repoType` (Cloud Source Repositories, GitHub, GitLab, or
// Bitbucket connected through Developer Connect) — Cloud Build's
// `GitRepoSource.repository` field never carries a `sourcerepo.googleapis.com`
// resource name, so this is a distinct target asset type from
// `assetTypeSourceRepo` (the `triggerTemplate` legacy Cloud Source
// Repositories reference), never the same edge.
const (
	assetTypeDeveloperConnectGitRepositoryLink    = "developerconnect.googleapis.com/GitRepositoryLink"
	developerConnectResourceNamePrefix            = "//developerconnect.googleapis.com/"
	developerConnectGitRepositoryLinkNameSegments = 8 // projects/<p>/locations/<l>/connections/<c>/repositories/<r>
)

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
// can be surfaced. `sourceToBuild.uri` (a raw repo URL),
// `sourceToBuild.githubEnterpriseConfig`, and `sourceToBuild.bitbucketServerConfig`
// are never decoded either — only `sourceToBuild.repository`, the Developer
// Connect connected-repository resource name, is read, and only to derive an
// edge.
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
// source repo/repository-link and fingerprinted service-account email as
// correlation anchors, and the typed `trigger_source_repo` edge for a Cloud
// Source Repositories `triggerTemplate` plus the typed
// `trigger_source_repository_link` edge for a Developer Connect
// `sourceToBuild.repository`. The two edges are independent: `sourceToBuild`
// names the build-source reference for a trigger whose actual firing
// mechanism is Pub/Sub, webhook, manual, or cron (per the live Cloud Build v1
// discovery document), so it is resolved unconditionally alongside whichever
// event-mechanism field fires the build, never as a mutually-exclusive
// alternative. A GitHub, GitLab Enterprise, or Bitbucket Server source named
// directly by `github` (not through Developer Connect) has no CAI-resolvable
// target asset type in this graph, so no edge is emitted for it; only the
// bounded `source_type` enum records which mechanism fires the trigger. The
// service account is joined via its fingerprinted-email digest;
// substitutions, the CEL `filter`, the webhook secret reference, and
// `sourceToBuild.uri` are never read.
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
	// sourceToBuild is the build-source reference for a trigger that does not
	// respond to SCM push/PR events (Pub/Sub, webhook, manual, or cron); it is
	// independent of, and can coexist with, whichever event-mechanism field
	// above fires the build, so it is resolved unconditionally rather than as
	// a mutually-exclusive alternative to triggerTemplate/github/etc.
	if data.SourceToBuild != nil {
		if link := developerConnectGitRepositoryLinkFullName(data.SourceToBuild.Repository); link != "" {
			anchors = append(anchors, link)
			rels = append(rels, cloudBuildEdge(ctx, relationshipTypeTriggerSourceRepositoryLink, link, assetTypeDeveloperConnectGitRepositoryLink))
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
// event mechanism fires the trigger's build. It checks the SCM-event
// discriminators first (`triggerTemplate`, `github`, `repositoryEventConfig`),
// then the invocation-mechanism discriminators (`pubsubConfig`,
// `webhookConfig`) — these are the trigger's true classification per the
// live Cloud Build v1 discovery document, which documents `sourceToBuild` as
// "used only by Webhook, Pub/Sub, Manual, and Cron triggers": it names the
// build-source reference for one of those triggers, not a distinct event
// mechanism, so its presence must never shadow the actual firing mechanism
// (a Pub/Sub or webhook trigger commonly carries both `pubsubConfig`/
// `webhookConfig` AND `sourceToBuild` at once). `sourceToBuild`'s own
// `source_to_build` classification and the `eventType`-derived `manual`
// fallback apply only once none of the mechanism fields above are set.
func cloudBuildTriggerSourceType(data cloudBuildTriggerData) string {
	switch {
	case data.TriggerTemplate != nil:
		return "repo"
	case data.GitHub != nil:
		return "github"
	case data.RepositoryEventConfig != nil:
		return "repository_event"
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
	}
	if data.SourceToBuild != nil {
		return "source_to_build"
	}
	return ""
}

// developerConnectGitRepositoryLinkFullName validates and returns the CAI
// full resource name for a Developer Connect connected repository referenced
// by `sourceToBuild.repository`. Cloud Build's `GitRepoSource.repository`
// field is documented as `projects/*/locations/*/connections/*/repositories/*`
// regardless of the underlying `repoType` (Cloud Source Repositories, GitHub,
// GitLab, or Bitbucket Server/Cloud connected through Developer Connect), so
// it is never a `sourcerepo.googleapis.com` resource name and must never be
// routed to `assetTypeSourceRepo`/`cloudBuildSourceRepoFullName`. The
// derivation fails closed: an already-absolute value is trusted only when it
// carries the exact Developer Connect CAI service prefix, and a relative
// value is qualified only when it matches the documented
// `projects/<p>/locations/<l>/connections/<c>/repositories/<r>` eight-segment
// shape; any other value (including a `uri`-only source with no `repository`
// field) yields "" and mints no edge or anchor.
func developerConnectGitRepositoryLinkFullName(repository string) string {
	trimmed := strings.TrimSpace(repository)
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(trimmed, "//") {
		if !strings.HasPrefix(trimmed, developerConnectResourceNamePrefix) {
			return ""
		}
		relative := strings.TrimPrefix(trimmed, developerConnectResourceNamePrefix)
		if !developerConnectGitRepositoryLinkShapeValid(relative) {
			return ""
		}
		return trimmed
	}
	relative := strings.TrimPrefix(trimmed, "/")
	if !developerConnectGitRepositoryLinkShapeValid(relative) {
		return ""
	}
	return developerConnectResourceNamePrefix + relative
}

// developerConnectGitRepositoryLinkShapeValid reports whether a relative
// resource-name path matches the documented
// `projects/<p>/locations/<l>/connections/<c>/repositories/<r>` shape (odd
// segments are the literal path keywords, even segments are their values; all
// must be non-blank).
func developerConnectGitRepositoryLinkShapeValid(relative string) bool {
	segments := strings.Split(relative, "/")
	if len(segments) != developerConnectGitRepositoryLinkNameSegments {
		return false
	}
	wantKeywords := []string{"projects", "", "locations", "", "connections", "", "repositories", ""}
	for i, want := range wantKeywords {
		if want == "" {
			if strings.TrimSpace(segments[i]) == "" {
				return false
			}
			continue
		}
		if segments[i] != want {
			return false
		}
	}
	return true
}
