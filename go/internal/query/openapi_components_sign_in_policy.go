// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

// openAPIComponentsSignInPolicy documents the tenant sign-in policy schemas
// (#4968, epic #4962), included into openAPIComponents' "schemas" object as
// a separate constant fragment to keep openapi_components.go under the
// repository's file-size limit — same pattern as
// openAPIComponentsProviderConfigs (openapi_components_provider_configs.go).
const openAPIComponentsSignInPolicy = `      "AdminSignInPolicy": {
        "type": "object",
        "description": "The tenant sign-in policy. sso_admin_verified_at/sso_admin_verified_provider_config_id are only present once at least one admin has completed one SSO sign-in for this tenant; neither is ever a secret.",
        "properties": {
          "tenant_id": {"type": "string"},
          "require_sso": {"type": "boolean", "description": "Hides the local password form on the login page and rejects non-admin local login. Break-glass local admin sign-in stays reachable regardless."},
          "allow_local_user_creation": {"type": "boolean", "description": "false rejects new local-identity invitation creation. Default true."},
          "require_mfa_for_all_users": {"type": "boolean", "description": "Admins always require MFA regardless of this flag; true extends the requirement to every local-identity invitation acceptance."},
          "idle_timeout_seconds": {"type": "integer", "description": "0 means \"use the process default\" (DefaultBrowserSessionIdleTimeout)."},
          "absolute_timeout_seconds": {"type": "integer", "description": "0 means \"use the process default\" (DefaultBrowserSessionAbsoluteTimeout)."},
          "sso_admin_verified_at": {"type": "string", "format": "date-time"},
          "sso_admin_verified_provider_config_id": {"type": "string"},
          "policy_revision_hash": {"type": "string"},
          "updated_at": {"type": "string", "format": "date-time"}
        }
      },
      "AdminSignInPolicyUpdateRequest": {
        "type": "object",
        "description": "Partial update. Every field is optional; an absent field leaves the current value unchanged.",
        "properties": {
          "require_sso": {"type": "boolean"},
          "allow_local_user_creation": {"type": "boolean"},
          "require_mfa_for_all_users": {"type": "boolean"},
          "idle_timeout_seconds": {"type": "integer", "minimum": 0, "description": "0 means \"use the process default\"; any other value must be at least 60 seconds."},
          "absolute_timeout_seconds": {"type": "integer", "minimum": 0, "description": "0 means \"use the process default\"; any other value must be at least 60 seconds and not less than idle_timeout_seconds when both are set."}
        }
      },
`
