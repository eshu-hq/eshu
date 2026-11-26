// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package openapi

const componentsLocalIdentity = `
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
          "recovery_code": {"type": "string", "format": "password"},
          "totp_code": {"type": "string", "description": "Authenticator-app code, checked before recovery_code when both are submitted (issue #4986). Required only when the account has an active TOTP factor."}
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
          "recovery_code": {"type": "string", "format": "password", "description": "Required only when the account has an active MFA factor."},
          "totp_code": {"type": "string", "description": "Authenticator-app code re-proved at rotation time (issue #4986), checked before recovery_code when both are submitted."}
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
        "properties": {
          "token_class": {"type": "string", "enum": ["personal", "service_principal"], "default": "personal", "description": "Token class. Optional: defaults to personal when absent, per the token create handler."},
          "tenant_id": {"type": "string"},
          "workspace_id": {"type": "string"},
          "user_id": {"type": "string", "description": "Required for token_class=personal unless omitted, in which case a browser session resolves it to the caller's OWN internal user_id (self-service create). Explicitly naming a different user_id remains the existing all-scope admin flow."},
          "service_principal_id": {"type": "string"},
          "display_label": {"type": "string", "description": "Operator-facing display label. Persisted as plaintext (non-secret) so list endpoints can render it; a separate SHA-256 handle hash of the same input is also stored."},
          "expires_at": {"type": "string", "format": "date-time"}
        }
      },
      "LocalIdentityAPITokenResponse": {
        "type": "object",
        "required": ["status", "token_id", "api_token", "issued_at"],
        "properties": {
          "status": {"type": "string", "enum": ["created", "rotated"], "description": "Issue outcome emitted by the local.IdentityHandler token routes: created for a new token, rotated for a replacement."},
          "token_id": {"type": "string", "description": "Server-side token identifier emitted by the local.IdentityHandler token routes."},
          "token_class": {"type": "string", "enum": ["personal", "service_principal"], "description": "Token class echoed from the create request by the local.IdentityHandler token routes."},
          "api_token": {"type": "string", "format": "password", "description": "One-time bearer value. The server persists only its hash."},
          "issued_at": {"type": "string", "format": "date-time", "description": "Issuance timestamp emitted by the local.IdentityHandler token routes."},
          "expires_at": {"type": "string", "format": "date-time", "description": "Expiry timestamp emitted by the local.IdentityHandler token routes; absent when the token does not expire."}
        }
      },
      "LocalIdentitySessionResponse": {
        "type": "object",
        "properties": {
          "status": {"type": "string", "enum": ["authenticated", "mfa_required", "must_change_password", "locked", "disabled", "invalid", "break_glass_authenticated"], "description": "Login outcome emitted by the local identity session writer (local.IdentitySessionResponse): the terminal state of the sign-in attempt."},
          "auth": {"$ref": "#/components/schemas/BrowserSessionAuth", "description": "Authenticated session identity emitted by the local identity session writer; present only on an authenticated status."},
          "csrf_token": {"type": "string", "description": "CSRF token issued with the session by the local identity session writer; present only when a session was issued."},
          "idle_expires_at": {"type": "string", "format": "date-time", "description": "Idle expiry of the issued session, emitted by the local identity session writer; honors the tenant sign-in policy override."},
          "absolute_expires_at": {"type": "string", "format": "date-time", "description": "Absolute expiry of the issued session, emitted by the local identity session writer; honors the tenant sign-in policy override."},
          "locked_until": {"type": "string", "format": "date-time", "description": "Lockout expiry emitted by the local identity session writer; present only on a locked status."}
        }
      },
      "AdminAuditCountList": {
        "type": "array",
        "description": "Low-cardinality aggregate counts. Each entry is a name and an integer count; never an individual event identifier.",
        "items": {
          "type": "object",
          "description": "One aggregate bucket emitted by query.normalizeGovernanceAuditCounts from a governanceaudit.Count row.",
          "properties": {
            "name": {"type": "string", "description": "Aggregate bucket name emitted by query.normalizeGovernanceAuditCounts: one low-cardinality event type, decision, reason, actor class, or scope class."},
            "count": {"type": "integer", "description": "Aggregate count for the bucket, emitted by query.normalizeGovernanceAuditCounts; always positive."}
          }
        }
      },
`
