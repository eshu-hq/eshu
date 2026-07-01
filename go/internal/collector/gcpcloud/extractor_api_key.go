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

// apiKeyAssetType is the Cloud Asset Inventory asset type for a GCP API Key.
const apiKeyAssetType = "apikeys.googleapis.com/Key"

func init() {
	RegisterAssetExtractor(apiKeyAssetType, extractAPIKey)
}

// apiKeyData is the bounded view of a CAI apikeys.googleapis.com/Key
// resource.data blob. The secret key material (`keyString`) is never a field so
// it cannot be surfaced. Each restriction block is decoded as raw presence only —
// its contents (allowed IPs, referrer URLs, Android app fingerprints, iOS bundle
// ids) are never extracted, so no address, URL, or app identifier leaves the
// parser; only which restriction type is configured is reported. The API-target
// service names are bounded control-plane API identifiers and are kept.
type apiKeyData struct {
	DisplayName string `json:"displayName"`
	CreateTime  string `json:"createTime"`
	// ServiceAccountEmail is set on an authorization key, which authenticates as
	// that service account. It is reduced to a redaction-safe digest (never the
	// raw email) so reducers keep the IAM/trust join without persisting the address.
	ServiceAccountEmail string `json:"serviceAccountEmail"`
	Restrictions        *struct {
		BrowserKeyRestrictions *json.RawMessage `json:"browserKeyRestrictions"`
		ServerKeyRestrictions  *json.RawMessage `json:"serverKeyRestrictions"`
		AndroidKeyRestrictions *json.RawMessage `json:"androidKeyRestrictions"`
		IOSKeyRestrictions     *json.RawMessage `json:"iosKeyRestrictions"`
		APITargets             []struct {
			Service string `json:"service"`
		} `json:"apiTargets"`
	} `json:"restrictions"`
}

// extractAPIKey extracts bounded, redaction-safe typed depth for one CAI API Key
// asset. It surfaces the display name, creation time, the configured key
// restriction type (browser / server / android / ios), and the restricted
// API-target services (count plus the bounded, sorted service-name list). The
// secret key string and every restriction value (allowed IPs, referrers, Android
// app fingerprints, iOS bundle ids) are never read.
//
// For an authorization key it also surfaces the fingerprinted service-account
// email it authenticates as, as the cross-source IAM/trust join anchor. The key's
// owning project is ancestry already carried on the base observation and its
// restricted API targets are GCP service identifiers that are not resolvable CAI
// resources, so the extractor emits no outbound relationships.
func extractAPIKey(ctx ExtractContext) (AttributeExtraction, error) {
	var data apiKeyData
	if err := json.Unmarshal(ctx.Data, &data); err != nil {
		return AttributeExtraction{}, fmt.Errorf("decode api key data: %w", err)
	}

	attrs := map[string]any{}
	if v := strings.TrimSpace(data.DisplayName); v != "" {
		attrs["display_name"] = v
	}
	if v, ok := normalizeRFC3339(data.CreateTime); ok {
		attrs["creation_time"] = v
	}
	if r := data.Restrictions; r != nil {
		if t := apiKeyRestrictionType(r.BrowserKeyRestrictions, r.ServerKeyRestrictions, r.AndroidKeyRestrictions, r.IOSKeyRestrictions); t != "" {
			attrs["restriction_type"] = t
		}
		if services := apiKeyTargetServices(r.APITargets); len(services) > 0 {
			// Count the deduplicated/non-empty services so the count and the list
			// never disagree.
			attrs["api_target_count"] = len(services)
			attrs["api_target_services"] = services
		}
	}

	// An authorization key authenticates as a service account; carry the
	// fingerprinted email as the cross-source IAM/trust join anchor (never the raw
	// address). No outbound edge is emitted because the email is not a resolvable
	// CAI full resource name — the IAM/trust layer joins on the digest.
	var anchors []string
	if digest := secretsiam.GCPServiceAccountEmailDigest(data.ServiceAccountEmail); digest != "" {
		attrs["authorized_service_account_email_fingerprint"] = digest
		anchors = append(anchors, digest)
	}

	return AttributeExtraction{Attributes: attrs, CorrelationAnchors: anchors}, nil
}

// apiKeyRestrictionType reports the configured key restriction type without
// decoding any restriction value. A key carries at most one restriction type;
// the checks are ordered but mutually exclusive in practice.
func apiKeyRestrictionType(browser, server, android, ios *json.RawMessage) string {
	switch {
	case browser != nil:
		return "browser"
	case server != nil:
		return "server"
	case android != nil:
		return "android"
	case ios != nil:
		return "ios"
	default:
		return ""
	}
}

// apiKeyTargetServices returns the deduplicated, sorted set of restricted
// API-target service names. Service names are bounded control-plane API
// identifiers (for example translate.googleapis.com), not resource instances.
func apiKeyTargetServices(targets []struct {
	Service string `json:"service"`
},
) []string {
	if len(targets) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	services := make([]string, 0, len(targets))
	for _, target := range targets {
		service := strings.TrimSpace(target.Service)
		if service == "" {
			continue
		}
		if _, ok := seen[service]; ok {
			continue
		}
		seen[service] = struct{}{}
		services = append(services, service)
	}
	sort.Strings(services)
	return services
}
