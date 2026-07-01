// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"fmt"
	"net"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/secretsiam"
)

// assetTypeCloudTasksQueue is the CAI asset type this extractor registers for.
const assetTypeCloudTasksQueue = "cloudtasks.googleapis.com/Queue"

func init() {
	RegisterAssetExtractor(assetTypeCloudTasksQueue, extractCloudTasksQueue)
}

// cloudTasksQueueData is the bounded view of a CAI cloudtasks.googleapis.com/Queue
// resource.data blob. The HTTP target URI override path/query, OIDC audience, and
// header overrides are never decoded (only the host is fingerprinted), so no
// task-dispatch secret can be surfaced.
type cloudTasksQueueData struct {
	State      string `json:"state"`
	PurgeTime  string `json:"purgeTime"`
	RateLimits *struct {
		MaxDispatchesPerSecond  *float64 `json:"maxDispatchesPerSecond"`
		MaxConcurrentDispatches *int     `json:"maxConcurrentDispatches"`
		MaxBurstSize            *int     `json:"maxBurstSize"`
	} `json:"rateLimits"`
	RetryConfig *struct {
		MaxAttempts *int `json:"maxAttempts"`
	} `json:"retryConfig"`
	AppEngineRoutingOverride *struct {
		Service string `json:"service"`
	} `json:"appEngineRoutingOverride"`
	HTTPTarget *struct {
		URIOverride *struct {
			Host string `json:"host"`
		} `json:"uriOverride"`
		OAuthToken *struct {
			ServiceAccountEmail string `json:"serviceAccountEmail"`
		} `json:"oauthToken"`
		OIDCToken *struct {
			ServiceAccountEmail string `json:"serviceAccountEmail"`
		} `json:"oidcToken"`
	} `json:"httpTarget"`
}

// extractCloudTasksQueue extracts bounded, redaction-safe typed depth for one CAI
// Cloud Tasks Queue. It returns the Terraform/drift/monitoring attribute set
// (state, rate limits, retry max attempts, App Engine routing service, purge
// time, HTTP target host fingerprint, and the fingerprinted HTTP-target service
// account) and the HTTP host and service-account fingerprints as correlation
// anchors. It emits no typed edge: the queue's App Engine routing override names
// a service by short name, but a CAI Queue full resource name carries the numeric
// project number, which does not match the App Engine application id used in the
// App Engine Service full resource name, so any constructed edge would never
// resolve. The routing service is kept as a bounded attribute instead. The HTTP
// override path/query, OIDC audience, and headers are never read.
func extractCloudTasksQueue(ctx ExtractContext) (AttributeExtraction, error) {
	var data cloudTasksQueueData
	if err := json.Unmarshal(ctx.Data, &data); err != nil {
		return AttributeExtraction{}, fmt.Errorf("decode cloud tasks queue data: %w", err)
	}

	saDigest := secretsiam.GCPServiceAccountEmailDigest(cloudTasksQueueServiceAccountEmail(data))
	hostFP := cloudTasksQueueHostFingerprint(data)
	attrs := cloudTasksQueueAttributes(data, saDigest, hostFP)

	var anchors []string
	if saDigest != "" {
		anchors = append(anchors, saDigest)
	}
	if hostFP != "" {
		anchors = append(anchors, hostFP)
	}

	return AttributeExtraction{
		Attributes:         attrs,
		CorrelationAnchors: dedupeNonEmpty(anchors),
	}, nil
}

// cloudTasksQueueAttributes assembles the bounded attribute map. Absent fields are
// omitted rather than written as zero values.
func cloudTasksQueueAttributes(data cloudTasksQueueData, saDigest, hostFP string) map[string]any {
	attrs := map[string]any{}
	if v := strings.TrimSpace(data.State); v != "" {
		attrs["state"] = v
	}
	if r := data.RateLimits; r != nil {
		if r.MaxDispatchesPerSecond != nil {
			attrs["max_dispatches_per_second"] = *r.MaxDispatchesPerSecond
		}
		if r.MaxConcurrentDispatches != nil {
			attrs["max_concurrent_dispatches"] = *r.MaxConcurrentDispatches
		}
		if r.MaxBurstSize != nil {
			attrs["max_burst_size"] = *r.MaxBurstSize
		}
	}
	if rc := data.RetryConfig; rc != nil && rc.MaxAttempts != nil {
		attrs["retry_max_attempts"] = *rc.MaxAttempts
	}
	if a := data.AppEngineRoutingOverride; a != nil {
		if v := strings.TrimSpace(a.Service); v != "" {
			attrs["app_engine_routing_service"] = v
		}
	}
	if v, ok := normalizeRFC3339(data.PurgeTime); ok {
		attrs["purge_time"] = v
	}
	if hostFP != "" {
		attrs["http_target_host_fingerprint"] = hostFP
	}
	if saDigest != "" {
		attrs["service_account_fingerprint"] = saDigest
	}
	return attrs
}

// cloudTasksQueueServiceAccountEmail returns the HTTP target's token
// service-account email, preferring the OIDC token over the OAuth token.
func cloudTasksQueueServiceAccountEmail(data cloudTasksQueueData) string {
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

// cloudTasksQueueHostFingerprint reduces the HTTP target URI-override host to the
// shared external-host fingerprint so a queue dispatching to the same endpoint
// correlates across extractors without persisting the raw host. Any port suffix
// is stripped first so a "host:port" reference fingerprints identically to the
// bare hostname other extractors (e.g. Pub/Sub push) already reduce via
// url.Hostname(). It returns "" when there is no HTTP target host.
func cloudTasksQueueHostFingerprint(data cloudTasksQueueData) string {
	if data.HTTPTarget == nil || data.HTTPTarget.URIOverride == nil {
		return ""
	}
	host := strings.TrimSpace(data.HTTPTarget.URIOverride.Host)
	if host == "" {
		return ""
	}
	if bare, _, err := net.SplitHostPort(host); err == nil && bare != "" {
		host = bare
	}
	return pubSubPushEndpointHostFingerprint(host)
}
