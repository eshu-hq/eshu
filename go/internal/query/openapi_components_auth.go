// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

const openAPIComponentsLocalIdentity = `
      "LocalIdentityBootstrapRequest": {
        "type": "object",
        "required": ["tenant_id", "workspace_id", "login_id", "password", "recovery_codes"],
        "properties": {
          "tenant_id": {"type": "string"},
          "workspace_id": {"type": "string"},
          "login_id": {"type": "string"},
          "profile_handle": {"type": "string"},
          "password": {"type": "string", "format": "password"},
          "mfa_factor_kind": {"type": "string", "default": "recovery_code"},
          "mfa_credential_handle": {"type": "string"},
          "recovery_codes": {"type": "array", "items": {"type": "string"}}
        }
      },
      "LocalIdentityLoginRequest": {
        "type": "object",
        "required": ["login_id", "password"],
        "properties": {
          "login_id": {"type": "string"},
          "password": {"type": "string", "format": "password"},
          "recovery_code": {"type": "string", "format": "password"}
        }
      },
      "LocalIdentityInvitationRequest": {
        "type": "object",
        "properties": {
          "tenant_id": {"type": "string"},
          "workspace_id": {"type": "string"},
          "invite_code": {"type": "string", "format": "password"},
          "invitee_handle": {"type": "string"},
          "role_id": {"type": "string", "default": "developer"},
          "expires_at": {"type": "string", "format": "date-time"}
        }
      },
      "LocalIdentityInvitationAcceptanceRequest": {
        "type": "object",
        "required": ["invite_code", "login_id", "password"],
        "properties": {
          "invite_code": {"type": "string", "format": "password"},
          "login_id": {"type": "string"},
          "profile_handle": {"type": "string"},
          "password": {"type": "string", "format": "password"},
          "mfa_factor_kind": {"type": "string", "default": "recovery_code"},
          "mfa_credential_handle": {"type": "string"},
          "recovery_codes": {"type": "array", "items": {"type": "string"}}
        }
      },
      "LocalIdentityPasswordRotationRequest": {
        "type": "object",
        "required": ["login_id", "current_password", "new_password"],
        "properties": {
          "login_id": {"type": "string"},
          "current_password": {"type": "string", "format": "password"},
          "new_password": {"type": "string", "format": "password"},
          "recovery_code": {"type": "string", "format": "password", "description": "Required only when the account has an active MFA factor."}
        }
      },
      "LocalIdentityBreakGlassRequest": {
        "type": "object",
        "required": ["tenant_id", "workspace_id", "subject_id"],
        "properties": {
          "tenant_id": {"type": "string"},
          "workspace_id": {"type": "string"},
          "subject_id": {"type": "string"},
          "break_glass_code": {"type": "string", "format": "password"},
          "reason_code": {"type": "string", "default": "operator_recovery"},
          "expires_at": {"type": "string", "format": "date-time"}
        }
      },
      "LocalIdentityAPITokenCreateRequest": {
        "type": "object",
        "required": ["token_class"],
        "properties": {
          "token_class": {"type": "string", "enum": ["personal", "service_principal"]},
          "tenant_id": {"type": "string"},
          "workspace_id": {"type": "string"},
          "user_id": {"type": "string"},
          "service_principal_id": {"type": "string"},
          "display_label": {"type": "string", "description": "Operator-facing label hashed before storage."},
          "expires_at": {"type": "string", "format": "date-time"}
        }
      },
      "LocalIdentityAPITokenResponse": {
        "type": "object",
        "required": ["status", "token_id", "api_token", "issued_at"],
        "properties": {
          "status": {"type": "string", "enum": ["created", "rotated"]},
          "token_id": {"type": "string"},
          "token_class": {"type": "string", "enum": ["personal", "service_principal"]},
          "api_token": {"type": "string", "format": "password", "description": "One-time bearer value. The server persists only its hash."},
          "issued_at": {"type": "string", "format": "date-time"},
          "expires_at": {"type": "string", "format": "date-time"}
        }
      },
      "LocalIdentitySessionResponse": {
        "type": "object",
        "properties": {
          "status": {"type": "string", "enum": ["authenticated", "mfa_required", "must_change_password", "locked", "disabled", "invalid", "break_glass_authenticated"]},
          "auth": {"$ref": "#/components/schemas/BrowserSessionAuth"},
          "csrf_token": {"type": "string"},
          "idle_expires_at": {"type": "string", "format": "date-time"},
          "absolute_expires_at": {"type": "string", "format": "date-time"},
          "locked_until": {"type": "string", "format": "date-time"}
        }
      },
      "AdminAuditCountList": {
        "type": "array",
        "description": "Low-cardinality aggregate counts. Each entry is a name and an integer count; never an individual event identifier.",
        "items": {
          "type": "object",
          "properties": {
            "name": {"type": "string"},
            "count": {"type": "integer"}
          }
        }
      },
`
