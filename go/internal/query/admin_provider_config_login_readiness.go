// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "strings"

// providerConfigLoginRequiredFields lists, per login-capable provider kind,
// the non-secret configuration fields that
// githublogin/oidclogin/samlauth.ResolveSealedProviderConfig requires to
// resolve a DB-backed provider for login, but that buildProviderConfigWrite
// (create/update) and the connection tester both leave optional. A kind
// absent from this map is not currently login-capable through this admin
// surface (only external_oidc, external_saml, and external_github exist
// today — see buildProviderConfigWrite) and is never subject to this guard;
// a future bearer-only or non-browser-login kind added later would need its
// own entry here (or none, if it never resolves for login) to opt in.
//
// This is issue #5604's enable-time guard: an admin could previously create
// a provider, watch it pass test-connection (which never checks these
// fields — see admin_provider_config_connection_tester.go), enable it, and
// only discover the gap when every login attempt 503s. See each field's
// resolver-package doc comment (githublogin.ResolveSealedProviderConfig,
// oidclogin.ResolveSealedProviderConfig, samlauth.ResolveSealedProviderConfig)
// for why it is optional-at-write but required-at-login.
var providerConfigLoginRequiredFields = map[string][]string{
	"external_oidc":   {"redirect_url"},
	"external_github": {"redirect_url"},
	"external_saml": {
		"service_provider_entity_id",
		"service_provider_acs_url",
		// samlauth.ResolveSealedProviderConfig accepts only inline
		// metadata_xml at login resolution (no fetch step), even though
		// buildProviderConfigWrite accepts metadata_url as an alternative at
		// write time — so a metadata_url-only SAML provider must still be
		// flagged here.
		"metadata_xml",
	},
}

// providerConfigMissingLoginField returns the first field name (checked in
// providerConfigLoginRequiredFields' declared order, for a deterministic
// result) that providerKind's stored configuration is missing but its login
// resolver requires. It returns "" when providerKind is not a known
// login-capable kind, or when every required field is present and non-blank.
func providerConfigMissingLoginField(providerKind string, configuration map[string]any) string {
	requiredFields, loginCapable := providerConfigLoginRequiredFields[providerKind]
	if !loginCapable {
		return ""
	}
	for _, field := range requiredFields {
		if !providerConfigHasNonEmptyString(configuration, field) {
			return field
		}
	}
	return ""
}

// providerConfigHasNonEmptyString reports whether configuration[field] is a
// JSON string with non-whitespace content. json.Unmarshal into
// map[string]any always decodes a JSON string field as a Go string, so any
// other dynamic type (or a missing key, or a nil map) is treated as absent.
func providerConfigHasNonEmptyString(configuration map[string]any, field string) bool {
	value, ok := configuration[field]
	if !ok {
		return false
	}
	s, ok := value.(string)
	if !ok {
		return false
	}
	return strings.TrimSpace(s) != ""
}
