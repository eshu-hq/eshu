// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

// openAPIComponentsProviderConfigs documents the DB-backed identity
// provider-config CRUD schemas (#4966, epic #4962), included into
// openAPIComponents' "schemas" object as a separate constant fragment to
// keep openapi_components.go under the repository's file-size limit — same
// pattern as openAPIComponentsReplatforming
// (openapi_components_replatforming.go).
const openAPIComponentsProviderConfigs = `      "AdminProviderConfig": {
        "type": "object",
        "description": "Metadata-only admin view of one identity provider config. Never carries a secret — has_secret, secret_fingerprint (a non-reversible short hash of the sealed envelope's ciphertext), and key_id are the only secret-adjacent fields.",
        "properties": {
          "provider_config_id": {"type": "string"},
          "provider_kind": {"type": "string", "enum": ["external_oidc", "external_saml", "external_github"]},
          "status": {"type": "string", "enum": ["draft", "active"]},
          "active_revision_id": {"type": "string"},
          "configuration": {"type": "object", "description": "Non-secret settings: issuer/client_id/scopes/group_claim (oidc); metadata_url/entity_id/group_attribute/service_provider_entity_id/service_provider_acs_url (saml); client_id/base_url/api_base_url/scopes/allowed_orgs (github, issue #5166)."},
          "has_secret": {"type": "boolean"},
          "secret_fingerprint": {"type": "string"},
          "key_id": {"type": "string"},
          "shadowed_by_environment": {"type": "boolean", "description": "True when an env/file-registered provider already occupies this provider_config_id; the env config is authoritative and this row's sealed secret is never consulted for login."},
          "managed_by": {"type": "string", "enum": ["database", "environment"], "description": "\"database\" for a normal DB-backed row; \"environment\" for a row shadowed by (or synthesized from) an env/file-registered provider."},
          "created_at": {"type": "string", "format": "date-time"},
          "updated_at": {"type": "string", "format": "date-time"}
        }
      },
      "AdminProviderConfigWriteRequest": {
        "type": "object",
        "description": "Create/update request. Exactly one of the oidc, saml, or github field groups applies, selected by provider_kind. Secret fields (client_secret, sp_private_key, sp_certificate) are write-only and are never echoed back. The github group (issue #5166) reuses client_id/client_secret/scopes and adds base_url/api_base_url/allowed_orgs.",
        "required": ["provider_kind"],
        "properties": {
          "provider_kind": {"type": "string", "enum": ["oidc", "saml", "github"]},
          "provider_config_id": {"type": "string", "description": "Optional on create; when supplied, used verbatim (this is how a DB row is made to share an id with, and thereby be detected as shadowed by, an env-registered provider). Ignored on update."},
          "issuer": {"type": "string"},
          "client_id": {"type": "string"},
          "client_secret": {"type": "string", "description": "Write-only. Required on every create and update."},
          "scopes": {"type": "array", "items": {"type": "string"}},
          "group_claim": {"type": "string"},
          "redirect_url": {"type": "string"},
          "metadata_url": {"type": "string"},
          "metadata_xml": {"type": "string"},
          "entity_id": {"type": "string", "description": "Expected identity provider entity id, validated against the parsed IdP metadata."},
          "group_attribute": {"type": "string"},
          "service_provider_entity_id": {"type": "string", "description": "Eshu's own SP entity id advertised for this provider. Optional at create/test-connection time; required for the provider to resolve for login."},
          "service_provider_acs_url": {"type": "string", "description": "Eshu's own SP Assertion Consumer Service URL for this provider. Optional at create/test-connection time; required for the provider to resolve for login."},
          "sp_private_key": {"type": "string", "description": "Write-only PEM. Required on every create and update for saml."},
          "sp_certificate": {"type": "string", "description": "Write-only PEM. Required on every create and update for saml."},
          "base_url": {"type": "string", "description": "GitHub only (issue #5166). GitHub Enterprise Server host, e.g. https://github.example.com. Optional — defaults to https://github.com."},
          "api_base_url": {"type": "string", "description": "GitHub only (issue #5166). GitHub Enterprise Server REST API host, e.g. https://github.example.com/api/v3. Optional — defaults to https://api.github.com."},
          "allowed_orgs": {"type": "array", "items": {"type": "string"}, "description": "GitHub only (issue #5166). Required and non-empty: the GitHub organizations a user must have an active membership in to sign in. A GitHub OAuth App can authenticate any GitHub account, so this org allow-list is the connector's only tenant boundary."}
        }
      },
      "AdminProviderConfigWriteResult": {
        "type": "object",
        "properties": {
          "provider_config_id": {"type": "string"},
          "revision_id": {"type": "string"},
          "status": {"type": "string"},
          "changed": {"type": "boolean"}
        }
      },
`
