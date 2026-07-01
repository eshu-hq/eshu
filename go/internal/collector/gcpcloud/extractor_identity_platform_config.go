// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// identityConfigAssetType is the Cloud Asset Inventory asset type for a GCP
// Identity Platform (Identity Toolkit) project Config.
const identityConfigAssetType = "identitytoolkit.googleapis.com/Config"

func init() {
	RegisterAssetExtractor(identityConfigAssetType, extractIdentityPlatformConfig)
}

// identityConfigData is the bounded view of a CAI
// identitytoolkit.googleapis.com/Config resource.data blob. Only the
// authentication posture is decoded: which sign-in methods are enabled, the MFA
// state, the multi-tenant toggle, and the count of authorized domains. OAuth/IdP
// client secrets, API keys, blocking-function URIs, and the authorized-domain
// values themselves are never fields, so none can be surfaced.
type identityConfigData struct {
	SignIn *struct {
		Email *struct {
			Enabled bool `json:"enabled"`
		} `json:"email"`
		PhoneNumber *struct {
			Enabled bool `json:"enabled"`
		} `json:"phoneNumber"`
		Anonymous *struct {
			Enabled bool `json:"enabled"`
		} `json:"anonymous"`
	} `json:"signIn"`
	MFA *struct {
		State string `json:"state"`
	} `json:"mfa"`
	MultiTenant *struct {
		AllowTenants bool `json:"allowTenants"`
	} `json:"multiTenant"`
	AuthorizedDomains []string `json:"authorizedDomains"`
}

// extractIdentityPlatformConfig extracts bounded, redaction-safe typed depth for
// one CAI Identity Platform Config asset. It surfaces the enabled sign-in methods
// (email / phone / anonymous), the MFA state, the multi-tenant toggle, and the
// count of authorized domains. Authorized-domain values (which can name internal
// hosts) are only counted, and OAuth/IdP client secrets, API keys, and
// blocking-function URIs are never read.
//
// The config's graph value — its owning project, its IdP configs, and its
// authorized domains — is ancestry already carried on the base observation, or
// child IdP sub-resources and domain strings that are not resolvable CAI
// resources, so the extractor derives no outbound relationships or anchors.
func extractIdentityPlatformConfig(ctx ExtractContext) (AttributeExtraction, error) {
	var data identityConfigData
	if err := json.Unmarshal(ctx.Data, &data); err != nil {
		return AttributeExtraction{}, fmt.Errorf("decode identity platform config data: %w", err)
	}

	attrs := map[string]any{}
	if methods := identityConfigSignInMethods(data); len(methods) > 0 {
		attrs["sign_in_methods_enabled"] = methods
	}
	if data.MFA != nil {
		if v := strings.TrimSpace(data.MFA.State); v != "" {
			attrs["mfa_state"] = v
		}
	}
	if data.MultiTenant != nil {
		attrs["multi_tenant_allow_tenants"] = data.MultiTenant.AllowTenants
	}
	if n := len(data.AuthorizedDomains); n > 0 {
		attrs["authorized_domain_count"] = n
	}

	return AttributeExtraction{Attributes: attrs}, nil
}

// identityConfigSignInMethods returns the sorted set of enabled sign-in method
// names. Method names are a bounded control-plane enum, never credentials.
func identityConfigSignInMethods(data identityConfigData) []string {
	if data.SignIn == nil {
		return nil
	}
	var methods []string
	if data.SignIn.Email != nil && data.SignIn.Email.Enabled {
		methods = append(methods, "email")
	}
	if data.SignIn.PhoneNumber != nil && data.SignIn.PhoneNumber.Enabled {
		methods = append(methods, "phone")
	}
	if data.SignIn.Anonymous != nil && data.SignIn.Anonymous.Enabled {
		methods = append(methods, "anonymous")
	}
	sort.Strings(methods)
	return methods
}
