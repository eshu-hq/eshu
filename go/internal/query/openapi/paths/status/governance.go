// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status

// Governance is the OpenAPI path fragment documenting the
// `/api/v0/status/governance` route. openapi.Spec concatenates it into the
// published document; keep it in lockstep with the handlers and
// docs/public/reference/http-api.md.
const Governance = `
    "/api/v0/status/governance": {
      "get": {
        "tags": ["status"],
        "summary": "Get hosted governance status",
        "description": "Returns redacted hosted governance policy mode, state, readiness, and aggregate reason-code readbacks without raw policy bodies, subjects, tenants, source identifiers, credentials, provider endpoints, prompts, or provider responses. A 503 with Retry-After means a transient condition that is safe to retry: the status snapshot read or the governance audit summary read hit a stale or timed-out PostgreSQL reader; this route has no configuration 503, so every 503 it answers carries Retry-After. No 5xx body carries backend error text. A reader-verdict 503 names capability hosted_governance.status in its error envelope.",
        "operationId": "getHostedGovernanceStatus",
        "x-scoped-token-support": true,
        "responses": {
          "403": {"$ref": "#/components/responses/Forbidden"},
          "200": {
            "description": "Hosted governance status",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "mode": {"type": "string", "enum": ["local_no_policy", "hosted_single_tenant", "hosted_multi_tenant", "unrecognized"], "description": "Configured governance posture. \"unrecognized\" is a readback state, not a settable value: ESHU_GOVERNANCE_MODE held something that is not one of supported_modes, and the deployment is fail-closed for all-scope callers on grant-bound routes."},
                    "state": {"type": "string", "enum": ["disabled", "partial", "enforcing", "stale", "invalid"], "description": "Normalized governance state from buildGovernanceStatus; anything but enforcing carries reason codes."},
                    "source_kind": {"type": "string", "enum": ["environment", "kubernetes_secret", "config_map", "postgres_revision", "unknown"], "description": "Where the governance config was loaded from, as normalized by buildGovernanceStatus."},
                    "policy_revision_hash": {"type": "string", "description": "Safe hash of the policy revision; never the policy body."},
                    "readiness": {"type": "object", "description": "Per-pillar readiness booleans derived from the normalized config and the semantic extraction status by governanceReadiness.", "properties": {
                      "identity": {"type": "boolean", "description": "True when an auth mode other than none is configured."},
                      "tenant": {"type": "boolean", "description": "True when tenant and workspace modes are both configured."},
                      "egress": {"type": "boolean", "description": "True when egress mode is configured and not broad."},
                      "semantic": {"type": "boolean", "description": "True when the semantic provider is configured and available."},
                      "extensions": {"type": "boolean", "description": "True when the extension mode is configured."},
                      "redaction": {"type": "boolean", "description": "True when the redaction state is configured."},
                      "retention": {"type": "boolean", "description": "True when the retention mode is configured."},
                      "audit": {"type": "boolean", "description": "True when the audit state is configured or the extraction audit carries values."}
                    }},
                    "identity": {"type": "object", "description": "Normalized auth posture from governanceIdentity; no credentials.", "properties": {
                      "auth_mode": {"type": "string", "description": "Normalized auth mode: none, shared_token, scoped_token, or workload_identity."},
                      "configured": {"type": "boolean", "description": "True when auth mode is not none."},
                      "shared_token_limitation": {"type": "boolean", "description": "True when auth mode is shared_token, flagging its all-scope limits."}
                    }},
                    "tenancy": {"type": "object", "description": "Normalized tenancy modes from governanceTenancy.", "properties": {
                      "tenant_mode": {"type": "string", "description": "Normalized tenant mode: local_dev, single_tenant, multi_tenant, or not_configured."},
                      "workspace_mode": {"type": "string", "description": "Normalized workspace mode: single_workspace_dev, single_workspace, multi_workspace, or not_configured."}
                    }},
                    "egress": {"type": "object", "description": "Egress posture from buildGovernanceStatus.", "properties": {
                      "mode": {"type": "string", "description": "Normalized egress mode: restricted, broad, or not_configured."}
                    }},
                    "semantic": {"type": "object", "description": "Semantic extraction readback projected by governanceSemantic.", "properties": {
                      "state": {"type": "string", "description": "Semantic extraction state readback."},
                      "reason": {"type": "string", "description": "Reason for the extraction state."},
                      "provider_configured": {"type": "boolean", "description": "Whether a semantic provider is configured."},
                      "provider_profile_count": {"type": "integer", "description": "Number of provider profiles in the extraction view."},
                      "policy_denied_count": {"type": "integer", "description": "Policy-denied queue depth from the extraction status."},
                      "audit_actor_class_count": {"type": "integer", "description": "Distinct actor classes in the extraction audit."}
                    }},
                    "extensions": {"type": "object", "description": "Extension posture from buildGovernanceStatus.", "properties": {
                      "mode": {"type": "string", "description": "Normalized extension mode: strict, allowlist, disabled, not_configured, stale, or untrusted."}
                    }},
                    "redaction": {"type": "object", "description": "Redaction posture from buildGovernanceStatus.", "properties": {
                      "state": {"type": "string", "description": "Normalized redaction state: configured, disabled, not_configured, stale, or invalid."}
                    }},
                    "retention": {"type": "object", "description": "Retention posture from buildGovernanceStatus.", "properties": {
                      "mode": {"type": "string", "description": "Normalized retention mode: metadata_only, configured, disabled, not_configured, stale, or invalid."}
                    }},
                    "audit": {
                      "type": "object",
                      "description": "Governance audit summary counts from governanceAudit; bounded class tokens only.",
                      "properties": {
                        "state": {"type": "string", "description": "Normalized audit state: observed, configured, disabled, not_configured, stale, or unavailable."},
                        "event_count": {"type": "integer", "minimum": 0, "description": "Total audit events in the summary."},
                        "denied_decision_count": {"type": "integer", "minimum": 0, "description": "Denied decisions in the summary."},
                        "unavailable_decision_count": {"type": "integer", "minimum": 0, "description": "Unavailable decisions in the summary."},
                        "event_type_count": {"type": "integer", "minimum": 0, "description": "Distinct event types in the summary."},
                        "actor_class_count": {"type": "integer", "minimum": 0, "description": "Distinct actor classes across the summary and the extraction audit."},
                        "scope_class_count": {"type": "integer", "minimum": 0, "description": "Distinct scope classes in the summary."},
                        "reason_count": {"type": "integer", "minimum": 0, "description": "Distinct reason codes in the summary."},
                        "acl_state_count": {"type": "integer", "minimum": 0, "description": "Distinct ACL states in the extraction audit."}
                      }
                    },
                    "aggregates": {"type": "object", "description": "Policy and denial aggregates from governanceAggregates.", "properties": {
                      "policy_section_count": {"type": "integer", "description": "Configured policy sections."},
                      "denied_decision_count": {"type": "integer", "description": "Denied decisions, falling back through the audit summary to the extraction queue."},
                      "stale_section_count": {"type": "integer", "description": "Stale policy sections."}
                    }},
                    "reasons": {"type": "array", "items": {"type": "string"}, "description": "Machine-readable reason codes explaining a non-enforcing posture from governanceReasons."},
                    "all_scope_route_policy": {
                      "type": "object",
                      "description": "Whether a tenant-bound all-scope credential is admitted on routes whose handlers filter reads by the caller's repository or scope grant. Derived from the same mode mapping the auth middleware applies.",
                      "properties": {
                        "grant_bound_routes": {"type": "string", "enum": ["admitted", "refused"], "description": "Admission verdict for tenant-bound all-scope credentials on grant-bound routes."}
                      }
                    },
                    "supported_modes": {"type": "array", "items": {"type": "string"}, "description": "Modes the deployment recognizes; anything else reads back as unrecognized."},
                    "supported_states": {"type": "array", "items": {"type": "string"}, "description": "States the status readback can report."}
                  }
                }
              }
            }
          },
          "500": {"$ref": "#/components/responses/InternalError"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
`
