// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

// #nosec G101 -- OpenAPI spec JSON whose const name contains "TOTP"; the value is a static API schema definition, not a credential literal
const openAPIPathsAuthTOTP = `
    "/api/v0/auth/local/mfa/totp/begin": {
      "post": {
        "tags": ["auth"],
        "summary": "Begin authenticator-app (TOTP) MFA enrollment",
        "description": "Self-service route for the authenticated caller's own local identity (any authenticated session, not admin-only): generates a fresh TOTP shared secret, seals it, and persists a PENDING MFA factor. The response returns the plaintext secret exactly once, as an otpauth:// provisioning URI (for QR rendering) and a base32 manual-entry string; neither is ever returned again by any other route. The factor cannot satisfy an MFA login challenge until POST .../mfa/totp/confirm verifies a first submitted code.",
        "operationId": "beginLocalIdentityTOTPEnrollment",
        "requestBody": {
          "required": false,
          "content": {"application/json": {"schema": {"type": "object", "properties": {"account_label": {"type": "string", "description": "Cosmetic label shown in the authenticator app entry. The server never has the caller's original login identifier (sessions carry only a one-way subject hash), so the console supplies a label it already knows from its own session state."}}}}}
        },
        "responses": {
          "201": {
            "description": "Pending TOTP factor created. secret and otpauth_uri are returned once only.",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "factor_id": {"type": "string"},
                    "otpauth_uri": {"type": "string"},
                    "secret": {"type": "string", "description": "Base32-encoded shared secret for manual entry."},
                    "issuer": {"type": "string"},
                    "digits": {"type": "integer"},
                    "period_seconds": {"type": "integer"}
                  }
                }
              }
            }
          },
          "401": {"$ref": "#/components/responses/Unauthorized"},
          "404": {"$ref": "#/components/responses/NotFound"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
    "/api/v0/auth/local/mfa/totp/confirm": {
      "post": {
        "tags": ["auth"],
        "summary": "Confirm authenticator-app (TOTP) MFA enrollment",
        "description": "Self-service route for the authenticated caller's own local identity: verifies the first submitted authenticator-app code against the pending factor identified by factor_id and, on match, activates it so it can satisfy MFA login. A wrong code leaves the factor pending so the caller may retry.",
        "operationId": "confirmLocalIdentityTOTPEnrollment",
        "requestBody": {
          "required": true,
          "content": {"application/json": {"schema": {"type": "object", "required": ["factor_id", "code"], "properties": {"factor_id": {"type": "string"}, "code": {"type": "string", "description": "6-digit authenticator-app code."}}}}}
        },
        "responses": {
          "204": {"description": "TOTP factor activated."},
          "400": {"$ref": "#/components/responses/BadRequest"},
          "401": {"$ref": "#/components/responses/Unauthorized"},
          "404": {"$ref": "#/components/responses/NotFound"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
`
