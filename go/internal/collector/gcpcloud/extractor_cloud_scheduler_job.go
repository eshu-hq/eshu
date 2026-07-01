// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/secretsiam"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// assetTypeCloudSchedulerJob is the CAI asset type for a Cloud Scheduler job. Its
// Pub/Sub target edge reuses assetTypePubSubTopic and the shared topic builder.
const assetTypeCloudSchedulerJob = "cloudscheduler.googleapis.com/Job"

// relationshipTypeSchedulerJobTargetsTopic is the bounded provider relationship
// type for the Cloud Scheduler Pub/Sub-target edge. HTTP and App Engine targets
// resolve no CAI-asset endpoint (an external host / an App Engine service), so
// they are carried as posture attributes and a host fingerprint rather than
// edges.
const relationshipTypeSchedulerJobTargetsTopic = "scheduler_job_targets_topic"

func init() {
	RegisterAssetExtractor(assetTypeCloudSchedulerJob, extractCloudSchedulerJob)
}

// cloudSchedulerJobData is the bounded view of a CAI cloudscheduler.googleapis.com/Job
// resource.data blob. The Pub/Sub message payload/attributes, the HTTP target URI
// (reduced to a host fingerprint), OIDC audience, and request headers are never
// decoded, so no scheduled-request secret can be surfaced.
type cloudSchedulerJobData struct {
	Schedule        string `json:"schedule"`
	TimeZone        string `json:"timeZone"`
	State           string `json:"state"`
	LastAttemptTime string `json:"lastAttemptTime"`
	PubsubTarget    *struct {
		TopicName string `json:"topicName"`
	} `json:"pubsubTarget"`
	HTTPTarget *struct {
		URI        string `json:"uri"`
		HTTPMethod string `json:"httpMethod"`
		OAuthToken *struct {
			ServiceAccountEmail string `json:"serviceAccountEmail"`
		} `json:"oauthToken"`
		OIDCToken *struct {
			ServiceAccountEmail string `json:"serviceAccountEmail"`
		} `json:"oidcToken"`
	} `json:"httpTarget"`
	AppEngineHTTPTarget *struct {
		HTTPMethod string `json:"httpMethod"`
	} `json:"appEngineHttpTarget"`
	RetryConfig *struct {
		RetryCount *int `json:"retryCount"`
	} `json:"retryConfig"`
}

// extractCloudSchedulerJob extracts bounded, redaction-safe typed depth for one
// CAI Cloud Scheduler Job. It returns the Terraform/drift/monitoring attribute
// set (schedule, time zone, state, target type, HTTP method, retry count, last
// attempt time, HTTP target host fingerprint, and the fingerprinted target
// service account), the Pub/Sub topic and fingerprinted anchors, and the typed
// scheduler_job_targets_topic edge. HTTP/App Engine targets resolve no edge; the
// HTTP URI, OIDC audience, request headers, and Pub/Sub payload are never read.
func extractCloudSchedulerJob(ctx ExtractContext) (AttributeExtraction, error) {
	var data cloudSchedulerJobData
	if err := json.Unmarshal(ctx.Data, &data); err != nil {
		return AttributeExtraction{}, fmt.Errorf("decode cloud scheduler job data: %w", err)
	}

	saDigest := secretsiam.GCPServiceAccountEmailDigest(cloudSchedulerJobServiceAccountEmail(data))
	hostFP := cloudSchedulerHostFingerprint(data)
	attrs := cloudSchedulerJobAttributes(data, saDigest, hostFP)

	var anchors []string
	var rels []RelationshipObservation
	if saDigest != "" {
		anchors = append(anchors, saDigest)
	}
	if hostFP != "" {
		anchors = append(anchors, hostFP)
	}
	if data.PubsubTarget != nil {
		if topic := pubSubTopicRefFullName(data.PubsubTarget.TopicName); topic != "" {
			anchors = append(anchors, topic)
			rels = append(rels, cloudSchedulerJobEdge(ctx, relationshipTypeSchedulerJobTargetsTopic, topic, assetTypePubSubTopic))
		}
	}

	return AttributeExtraction{
		Attributes:         attrs,
		CorrelationAnchors: dedupeNonEmpty(anchors),
		Relationships:      rels,
	}, nil
}

// cloudSchedulerJobAttributes assembles the bounded attribute map. Absent fields
// are omitted rather than written as zero values.
func cloudSchedulerJobAttributes(data cloudSchedulerJobData, saDigest, hostFP string) map[string]any {
	attrs := map[string]any{}
	if v := strings.TrimSpace(data.Schedule); v != "" {
		attrs["schedule"] = v
	}
	if v := strings.TrimSpace(data.TimeZone); v != "" {
		attrs["time_zone"] = v
	}
	if v := strings.TrimSpace(data.State); v != "" {
		attrs["state"] = v
	}
	if v := cloudSchedulerJobTargetType(data); v != "" {
		attrs["target_type"] = v
	}
	if v := cloudSchedulerJobHTTPMethod(data); v != "" {
		attrs["http_method"] = v
	}
	if hostFP != "" {
		attrs["http_target_host_fingerprint"] = hostFP
	}
	if data.RetryConfig != nil && data.RetryConfig.RetryCount != nil {
		attrs["retry_count"] = *data.RetryConfig.RetryCount
	}
	if v, ok := normalizeRFC3339(data.LastAttemptTime); ok {
		attrs["last_attempt_time"] = v
	}
	if saDigest != "" {
		attrs["service_account_fingerprint"] = saDigest
	}
	return attrs
}

// cloudSchedulerJobHTTPMethod returns the request method for an HTTP or App
// Engine HTTP target (both carry httpMethod), or "".
func cloudSchedulerJobHTTPMethod(data cloudSchedulerJobData) string {
	if h := data.HTTPTarget; h != nil {
		if v := strings.TrimSpace(h.HTTPMethod); v != "" {
			return v
		}
	}
	if a := data.AppEngineHTTPTarget; a != nil {
		if v := strings.TrimSpace(a.HTTPMethod); v != "" {
			return v
		}
	}
	return ""
}

// cloudSchedulerJobTargetType returns the bounded target kind.
func cloudSchedulerJobTargetType(data cloudSchedulerJobData) string {
	switch {
	case data.PubsubTarget != nil:
		return "pubsub"
	case data.HTTPTarget != nil:
		return "http"
	case data.AppEngineHTTPTarget != nil:
		return "app_engine"
	default:
		return ""
	}
}

// cloudSchedulerJobServiceAccountEmail returns the HTTP target's token
// service-account email, preferring the OIDC token over the OAuth token.
func cloudSchedulerJobServiceAccountEmail(data cloudSchedulerJobData) string {
	if h := data.HTTPTarget; h != nil {
		if h.OIDCToken != nil && strings.TrimSpace(h.OIDCToken.ServiceAccountEmail) != "" {
			return h.OIDCToken.ServiceAccountEmail
		}
		if h.OAuthToken != nil {
			return h.OAuthToken.ServiceAccountEmail
		}
	}
	return ""
}

// cloudSchedulerHostFingerprint reduces the HTTP target URI to a deterministic
// fingerprint of its host so jobs hitting the same endpoint can be correlated
// without persisting the raw URI (which can carry tokens/query secrets) or the
// host (a DNS name the collector contract fingerprints). It returns "" when there
// is no HTTP target or host.
func cloudSchedulerHostFingerprint(data cloudSchedulerJobData) string {
	if data.HTTPTarget == nil {
		return ""
	}
	host := gitRemoteHost(data.HTTPTarget.URI)
	if host == "" {
		return ""
	}
	return "sha256:" + facts.StableID("GCPCloudSchedulerHTTPTargetHost", map[string]any{"host": host})
}

func cloudSchedulerJobEdge(ctx ExtractContext, relationshipType, targetName, targetType string) RelationshipObservation {
	return RelationshipObservation{
		SourceFullResourceName: ctx.FullResourceName,
		SourceAssetType:        ctx.AssetType,
		RelationshipType:       relationshipType,
		TargetFullResourceName: targetName,
		TargetAssetType:        targetType,
		SupportState:           RelationshipSupportSupported,
	}
}
