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
      "LocalIdentitySessionResponse": {
        "type": "object",
        "properties": {
          "status": {"type": "string", "enum": ["authenticated", "mfa_required", "locked", "disabled", "invalid", "break_glass_authenticated"]},
          "auth": {"$ref": "#/components/schemas/BrowserSessionAuth"},
          "csrf_token": {"type": "string"},
          "idle_expires_at": {"type": "string", "format": "date-time"},
          "absolute_expires_at": {"type": "string", "format": "date-time"},
          "locked_until": {"type": "string", "format": "date-time"}
        }
      },
`
