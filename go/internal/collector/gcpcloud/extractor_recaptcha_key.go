// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"fmt"
	"strings"
)

// recaptchaKeyAssetType is the Cloud Asset Inventory asset type for a GCP
// reCAPTCHA Enterprise Key.
const recaptchaKeyAssetType = "recaptchaenterprise.googleapis.com/Key"

func init() {
	RegisterAssetExtractor(recaptchaKeyAssetType, extractRecaptchaKey)
}

// recaptchaKeyData is the bounded view of a CAI
// recaptchaenterprise.googleapis.com/Key resource.data blob. The platform
// allow-lists (web domains, Android package names, iOS bundle ids) can name
// internal domains and applications, so their entries are only counted — never
// surfaced — while the allow-all posture and bounded enums (integration type, WAF
// service/feature) are kept.
type recaptchaKeyData struct {
	DisplayName    string `json:"displayName"`
	CreateTime     string `json:"createTime"`
	WebKeySettings *struct {
		IntegrationType string   `json:"integrationType"`
		AllowAllDomains bool     `json:"allowAllDomains"`
		AllowedDomains  []string `json:"allowedDomains"`
	} `json:"webKeySettings"`
	AndroidKeySettings *struct {
		AllowAllPackageNames bool     `json:"allowAllPackageNames"`
		AllowedPackageNames  []string `json:"allowedPackageNames"`
	} `json:"androidKeySettings"`
	IOSKeySettings *struct {
		AllowAllBundleIds bool     `json:"allowAllBundleIds"`
		AllowedBundleIds  []string `json:"allowedBundleIds"`
	} `json:"iosKeySettings"`
	ExpressKeySettings *json.RawMessage `json:"expressKeySettings"`
	WafSettings        *struct {
		WafService string `json:"wafService"`
		WafFeature string `json:"wafFeature"`
	} `json:"wafSettings"`
}

// extractRecaptchaKey extracts bounded, redaction-safe typed depth for one CAI
// reCAPTCHA Enterprise Key asset. It surfaces the display name, creation time,
// platform type (web / android / ios / express), the web integration type, the
// per-platform allow-all posture and allow-list counts, and the bounded WAF
// service/feature enums. The allow-list entries themselves (domains, package
// names, bundle ids) are never surfaced.
//
// The key's graph value — its owning project and its platform settings — is
// ancestry already carried on the base observation or bounded posture on the
// resource itself, so the extractor derives no outbound relationships or anchors.
func extractRecaptchaKey(ctx ExtractContext) (AttributeExtraction, error) {
	var data recaptchaKeyData
	if err := json.Unmarshal(ctx.Data, &data); err != nil {
		return AttributeExtraction{}, fmt.Errorf("decode recaptcha key data: %w", err)
	}

	attrs := map[string]any{}
	if v := strings.TrimSpace(data.DisplayName); v != "" {
		attrs["display_name"] = v
	}
	if v, ok := normalizeRFC3339(data.CreateTime); ok {
		attrs["creation_time"] = v
	}

	switch {
	case data.WebKeySettings != nil:
		attrs["platform_type"] = "web"
		if v := strings.TrimSpace(data.WebKeySettings.IntegrationType); v != "" {
			attrs["integration_type"] = v
		}
		if data.WebKeySettings.AllowAllDomains {
			attrs["allow_all_domains"] = true
		}
		if n := len(data.WebKeySettings.AllowedDomains); n > 0 {
			attrs["allowed_domain_count"] = n
		}
	case data.AndroidKeySettings != nil:
		attrs["platform_type"] = "android"
		if data.AndroidKeySettings.AllowAllPackageNames {
			attrs["allow_all_package_names"] = true
		}
		if n := len(data.AndroidKeySettings.AllowedPackageNames); n > 0 {
			attrs["allowed_package_name_count"] = n
		}
	case data.IOSKeySettings != nil:
		attrs["platform_type"] = "ios"
		if data.IOSKeySettings.AllowAllBundleIds {
			attrs["allow_all_bundle_ids"] = true
		}
		if n := len(data.IOSKeySettings.AllowedBundleIds); n > 0 {
			attrs["allowed_bundle_id_count"] = n
		}
	case data.ExpressKeySettings != nil:
		attrs["platform_type"] = "express"
	}

	if w := data.WafSettings; w != nil {
		if v := strings.TrimSpace(w.WafService); v != "" {
			attrs["waf_service"] = v
		}
		if v := strings.TrimSpace(w.WafFeature); v != "" {
			attrs["waf_feature"] = v
		}
	}

	return AttributeExtraction{Attributes: attrs}, nil
}
