// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package catalog

// ComponentExtensions is the OpenAPI path fragment documenting the
// `/api/v0/component-extensions`,
// `/api/v0/component-extensions/{component_id}/diagnostics` routes.
// openapi.Spec concatenates it into the published document; keep it in
// lockstep with the handlers and docs/public/reference/http-api.md.
const ComponentExtensions = `
    "/api/v0/component-extensions": {
      "get": {
        "tags": ["status"],
        "summary": "List component extensions",
        "description": "Returns a bounded local component package inventory and policy diagnostics from the configured component registry without exposing server-local manifest or activation config paths.",
        "operationId": "listComponentExtensions",
        "x-scoped-token-support": true,
        "parameters": [
          {
            "name": "limit",
            "in": "query",
            "schema": {"type": "integer", "minimum": 1, "maximum": 500, "default": 100},
            "description": "Maximum number of component rows to return."
          }
        ],
        "responses": {
          "403": {"$ref": "#/components/responses/Forbidden"},
          "200": {
            "description": "Component extension inventory",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "schema_version": {"type": "string", "description": "Inventory schema version emitted by listComponentExtensions."},
                    "status": {"type": "string", "enum": ["available"], "description": "Inventory status emitted by listComponentExtensions."},
                    "component_home_configured": {"type": "boolean", "description": "Whether a component home is configured, emitted by listComponentExtensions."},
                    "components": {
                      "type": "array",
                      "description": "Component rows emitted by listComponentExtensions (sanitizedComponentExtensions).",
                      "items": {
                        "type": "object",
                        "description": "One component extension row. Owner: listComponentExtensions (sanitizedComponentExtensions).",
                        "properties": {
                          "id": {"type": "string", "description": "Component package id emitted by listComponentExtensions (ComponentExtensionComponent.ID)."},
                          "name": {"type": "string", "description": "Component name emitted by listComponentExtensions (ComponentExtensionComponent.Name)."},
                          "publisher": {"type": "string", "description": "Component publisher emitted by listComponentExtensions (ComponentExtensionComponent.Publisher)."},
                          "version": {"type": "string", "description": "Component version emitted by listComponentExtensions (ComponentExtensionComponent.Version)."},
                          "manifest_digest": {"type": "string", "description": "Component manifest digest emitted by listComponentExtensions (ComponentExtensionComponent.ManifestDigest)."},
                          "verified": {"type": "boolean", "description": "Whether the component verified, emitted by listComponentExtensions (ComponentExtensionComponent.Verified)."},
                          "trust_mode": {"type": "string", "description": "Trust mode emitted by listComponentExtensions (ComponentExtensionComponent.TrustMode)."},
                          "installed_at": {"type": "string", "format": "date-time", "description": "Installation time emitted by listComponentExtensions (ComponentExtensionComponent.InstalledAt)."},
                          "states": {"type": "array", "items": {"type": "string"}, "description": "Component states emitted by listComponentExtensions (ComponentExtensionComponent.States)."},
                          "activations": {
                            "type": "array",
                            "description": "Component activations emitted by listComponentExtensions (ComponentExtensionActivation).",
                            "items": {
                              "type": "object",
                              "description": "One component activation. Owner: listComponentExtensions (ComponentExtensionActivation).",
                              "properties": {
                                "instance_id": {"type": "string", "description": "Activation instance id emitted by listComponentExtensions (ComponentExtensionActivation.InstanceID)."},
                                "mode": {"type": "string", "description": "Activation mode emitted by listComponentExtensions (ComponentExtensionActivation.Mode)."},
                                "claims_enabled": {"type": "boolean", "description": "Whether claims are enabled, emitted by listComponentExtensions (ComponentExtensionActivation.ClaimsEnabled)."},
                                "config_handle": {"type": "string", "description": "Config handle emitted by listComponentExtensions (ComponentExtensionActivation.ConfigHandle)."},
                                "enabled_at": {"type": "string", "format": "date-time", "description": "Activation time emitted by listComponentExtensions (ComponentExtensionActivation.EnabledAt)."}
                              }
                            }
                          },
                          "diagnostics": {"type": "object", "description": "Local policy re-verification diagnostics emitted by listComponentExtensions (ComponentExtensionPolicyDiagnostics).", "properties": {
                            "policy_configured": {"type": "boolean", "description": "Whether a policy is configured, emitted by listComponentExtensions (ComponentExtensionPolicyDiagnostics.PolicyConfigured)."},
                            "policy_allowed": {"type": "boolean", "description": "Whether the policy allows the component, emitted by listComponentExtensions (ComponentExtensionPolicyDiagnostics.PolicyAllowed)."},
                            "policy_mode": {"type": "string", "description": "Policy mode emitted by listComponentExtensions (ComponentExtensionPolicyDiagnostics.PolicyMode)."},
                            "policy_code": {"type": "string", "description": "Policy code emitted by listComponentExtensions (ComponentExtensionPolicyDiagnostics.PolicyCode)."},
                            "policy_reason": {"type": "string", "description": "Policy reason emitted by listComponentExtensions (ComponentExtensionPolicyDiagnostics.PolicyReason)."}
                          }},
                          "trust_decision": {"type": "object", "description": "Installed-package trust classification emitted by listComponentExtensions (ComponentExtensionTrustDecision).", "properties": {
                            "decision": {"type": "string", "description": "Trust decision emitted by listComponentExtensions (ComponentExtensionTrustDecision.Decision)."},
                            "code": {"type": "string", "description": "Trust decision code emitted by listComponentExtensions (ComponentExtensionTrustDecision.Code)."},
                            "reason": {"type": "string", "description": "Trust decision reason emitted by listComponentExtensions (ComponentExtensionTrustDecision.Reason)."}
                          }},
                          "policy_gate": {"type": "object", "description": "Stable blocker class emitted by listComponentExtensions (ComponentExtensionPolicyGate).", "properties": {
                            "state": {"type": "string", "description": "Gate state emitted by listComponentExtensions (ComponentExtensionPolicyGate.State)."},
                            "mode": {"type": "string", "description": "Gate mode emitted by listComponentExtensions (ComponentExtensionPolicyGate.Mode)."},
                            "code": {"type": "string", "description": "Gate code emitted by listComponentExtensions (ComponentExtensionPolicyGate.Code)."}
                          }},
                          "last_conformance_proof": {"type": "object", "description": "Last known proof state emitted by listComponentExtensions (ComponentExtensionConformanceProof).", "properties": {
                            "status": {"type": "string", "description": "Proof status emitted by listComponentExtensions (ComponentExtensionConformanceProof.Status)."},
                            "reason": {"type": "string", "description": "Proof reason emitted by listComponentExtensions (ComponentExtensionConformanceProof.Reason)."}
                          }},
                          "scheduler_state": {"type": "object", "description": "Hosted scheduling claim state emitted by listComponentExtensions (ComponentExtensionSchedulerState).", "properties": {
                            "state": {"type": "string", "description": "Scheduler state emitted by listComponentExtensions (ComponentExtensionSchedulerState.State)."},
                            "reason": {"type": "string", "description": "Scheduler reason emitted by listComponentExtensions (ComponentExtensionSchedulerState.Reason)."}
                          }},
                          "read_model_availability": {"type": "object", "description": "Readback availability emitted by listComponentExtensions (ComponentExtensionReadModelAvailability).", "properties": {
                            "state": {"type": "string", "description": "Availability state emitted by listComponentExtensions (ComponentExtensionReadModelAvailability.State)."},
                            "unavailable_reason": {"type": "string", "description": "Unavailable reason emitted by listComponentExtensions (ComponentExtensionReadModelAvailability.UnavailableReason)."}
                          }},
                          "error": {"type": "object", "description": "Stable sanitized component error emitted by listComponentExtensions (ComponentExtensionError).", "properties": {
                            "code": {"type": "string", "description": "Error code emitted by listComponentExtensions (ComponentExtensionError.Code)."},
                            "message": {"type": "string", "description": "Error message emitted by listComponentExtensions (ComponentExtensionError.Message)."}
                          }}
                        }
                      }
                    },
                    "count": {"type": "integer", "description": "Rows on this page, emitted by listComponentExtensions."},
                    "total_count": {"type": "integer", "description": "Total components before paging, emitted by listComponentExtensions."},
                    "limit": {"type": "integer", "description": "Requested page size echoed by listComponentExtensions."},
                    "truncated": {"type": "boolean", "description": "Whether more rows exist past this page, emitted by listComponentExtensions."},
                    "policy": {"type": "object", "description": "Trust policy knob summary emitted by listComponentExtensions (ComponentExtensionPolicyStatus); no filesystem paths are exposed.", "properties": {
                      "mode": {"type": "string", "description": "Policy mode emitted by listComponentExtensions (ComponentExtensionPolicyStatus.Mode)."},
                      "allow_ids_configured": {"type": "boolean", "description": "Whether allow ids are configured, emitted by listComponentExtensions (ComponentExtensionPolicyStatus.AllowIDsConfigured)."},
                      "allow_publishers_configured": {"type": "boolean", "description": "Whether allow publishers are configured, emitted by listComponentExtensions (ComponentExtensionPolicyStatus.AllowPublishersConfigured)."},
                      "revoke_ids_configured": {"type": "boolean", "description": "Whether revoke ids are configured, emitted by listComponentExtensions (ComponentExtensionPolicyStatus.RevokeIDsConfigured)."},
                      "revoke_publishers_configured": {"type": "boolean", "description": "Whether revoke publishers are configured, emitted by listComponentExtensions (ComponentExtensionPolicyStatus.RevokePublishersConfigured)."},
                      "core_version_configured": {"type": "boolean", "description": "Whether a core version is configured, emitted by listComponentExtensions (ComponentExtensionPolicyStatus.CoreVersionConfigured)."}
                    }}
                  }
                }
              }
            }
          },
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
    "/api/v0/component-extensions/{component_id}/diagnostics": {
      "get": {
        "tags": ["status"],
        "summary": "Get component extension diagnostics",
        "description": "Returns one component extension's lifecycle and policy diagnostics from local registry readback. Local manifest paths and activation config paths are redacted.",
        "operationId": "getComponentExtensionDiagnostics",
        "x-scoped-token-support": true,
        "parameters": [
          {
            "name": "component_id",
            "in": "path",
            "required": true,
            "schema": {"type": "string"},
            "description": "Component package ID"
          }
        ],
        "responses": {
          "403": {"$ref": "#/components/responses/Forbidden"},
          "200": {
            "description": "Component extension diagnostics",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "schema_version": {"type": "string", "description": "Diagnostics schema version emitted by getComponentExtensionDiagnostics."},
                    "status": {"type": "string", "enum": ["available"], "description": "Diagnostics status emitted by getComponentExtensionDiagnostics."},
                    "component_home_configured": {"type": "boolean", "description": "Whether a component home is configured, emitted by getComponentExtensionDiagnostics."},
                    "component": {"type": "object", "description": "One component extension with lifecycle and policy diagnostics, emitted by getComponentExtensionDiagnostics (ComponentExtensionComponent with the same keys as the list route components items).", "properties": {
                      "id": {"type": "string", "description": "Component package id emitted by getComponentExtensionDiagnostics (ComponentExtensionComponent.ID)."},
                      "name": {"type": "string", "description": "Component name emitted by getComponentExtensionDiagnostics (ComponentExtensionComponent.Name)."},
                      "publisher": {"type": "string", "description": "Component publisher emitted by getComponentExtensionDiagnostics (ComponentExtensionComponent.Publisher)."},
                      "version": {"type": "string", "description": "Component version emitted by getComponentExtensionDiagnostics (ComponentExtensionComponent.Version)."},
                      "manifest_digest": {"type": "string", "description": "Component manifest digest emitted by getComponentExtensionDiagnostics (ComponentExtensionComponent.ManifestDigest)."},
                      "verified": {"type": "boolean", "description": "Whether the component verified, emitted by getComponentExtensionDiagnostics (ComponentExtensionComponent.Verified)."},
                      "trust_mode": {"type": "string", "description": "Trust mode emitted by getComponentExtensionDiagnostics (ComponentExtensionComponent.TrustMode)."},
                      "installed_at": {"type": "string", "format": "date-time", "description": "Installation time emitted by getComponentExtensionDiagnostics (ComponentExtensionComponent.InstalledAt)."},
                      "states": {"type": "array", "items": {"type": "string"}, "description": "Component states emitted by getComponentExtensionDiagnostics (ComponentExtensionComponent.States)."},
                      "activations": {"type": "array", "description": "Component activations emitted by getComponentExtensionDiagnostics (ComponentExtensionActivation).", "items": {
                        "type": "object",
                        "description": "One component activation. Owner: getComponentExtensionDiagnostics (ComponentExtensionActivation).",
                        "properties": {
                          "instance_id": {"type": "string", "description": "Activation instance id emitted by getComponentExtensionDiagnostics (ComponentExtensionActivation.InstanceID)."},
                          "mode": {"type": "string", "description": "Activation mode emitted by getComponentExtensionDiagnostics (ComponentExtensionActivation.Mode)."},
                          "claims_enabled": {"type": "boolean", "description": "Whether claims are enabled, emitted by getComponentExtensionDiagnostics (ComponentExtensionActivation.ClaimsEnabled)."},
                          "config_handle": {"type": "string", "description": "Config handle emitted by getComponentExtensionDiagnostics (ComponentExtensionActivation.ConfigHandle)."},
                          "enabled_at": {"type": "string", "format": "date-time", "description": "Activation time emitted by getComponentExtensionDiagnostics (ComponentExtensionActivation.EnabledAt)."}
                        }
                      }},
                      "diagnostics": {"type": "object", "description": "Local policy re-verification diagnostics emitted by getComponentExtensionDiagnostics (ComponentExtensionPolicyDiagnostics).", "properties": {
                        "policy_configured": {"type": "boolean", "description": "Whether a policy is configured, emitted by getComponentExtensionDiagnostics (ComponentExtensionPolicyDiagnostics.PolicyConfigured)."},
                        "policy_allowed": {"type": "boolean", "description": "Whether the policy allows the component, emitted by getComponentExtensionDiagnostics (ComponentExtensionPolicyDiagnostics.PolicyAllowed)."},
                        "policy_mode": {"type": "string", "description": "Policy mode emitted by getComponentExtensionDiagnostics (ComponentExtensionPolicyDiagnostics.PolicyMode)."},
                        "policy_code": {"type": "string", "description": "Policy code emitted by getComponentExtensionDiagnostics (ComponentExtensionPolicyDiagnostics.PolicyCode)."},
                        "policy_reason": {"type": "string", "description": "Policy reason emitted by getComponentExtensionDiagnostics (ComponentExtensionPolicyDiagnostics.PolicyReason)."}
                      }},
                      "trust_decision": {"type": "object", "description": "Installed-package trust classification emitted by getComponentExtensionDiagnostics (ComponentExtensionTrustDecision).", "properties": {
                        "decision": {"type": "string", "description": "Trust decision emitted by getComponentExtensionDiagnostics (ComponentExtensionTrustDecision.Decision)."},
                        "code": {"type": "string", "description": "Trust decision code emitted by getComponentExtensionDiagnostics (ComponentExtensionTrustDecision.Code)."},
                        "reason": {"type": "string", "description": "Trust decision reason emitted by getComponentExtensionDiagnostics (ComponentExtensionTrustDecision.Reason)."}
                      }},
                      "policy_gate": {"type": "object", "description": "Stable blocker class emitted by getComponentExtensionDiagnostics (ComponentExtensionPolicyGate).", "properties": {
                        "state": {"type": "string", "description": "Gate state emitted by getComponentExtensionDiagnostics (ComponentExtensionPolicyGate.State)."},
                        "mode": {"type": "string", "description": "Gate mode emitted by getComponentExtensionDiagnostics (ComponentExtensionPolicyGate.Mode)."},
                        "code": {"type": "string", "description": "Gate code emitted by getComponentExtensionDiagnostics (ComponentExtensionPolicyGate.Code)."}
                      }},
                      "last_conformance_proof": {"type": "object", "description": "Last known proof state emitted by getComponentExtensionDiagnostics (ComponentExtensionConformanceProof).", "properties": {
                        "status": {"type": "string", "description": "Proof status emitted by getComponentExtensionDiagnostics (ComponentExtensionConformanceProof.Status)."},
                        "reason": {"type": "string", "description": "Proof reason emitted by getComponentExtensionDiagnostics (ComponentExtensionConformanceProof.Reason)."}
                      }},
                      "scheduler_state": {"type": "object", "description": "Hosted scheduling claim state emitted by getComponentExtensionDiagnostics (ComponentExtensionSchedulerState).", "properties": {
                        "state": {"type": "string", "description": "Scheduler state emitted by getComponentExtensionDiagnostics (ComponentExtensionSchedulerState.State)."},
                        "reason": {"type": "string", "description": "Scheduler reason emitted by getComponentExtensionDiagnostics (ComponentExtensionSchedulerState.Reason)."}
                      }},
                      "read_model_availability": {"type": "object", "description": "Readback availability emitted by getComponentExtensionDiagnostics (ComponentExtensionReadModelAvailability).", "properties": {
                        "state": {"type": "string", "description": "Availability state emitted by getComponentExtensionDiagnostics (ComponentExtensionReadModelAvailability.State)."},
                        "unavailable_reason": {"type": "string", "description": "Unavailable reason emitted by getComponentExtensionDiagnostics (ComponentExtensionReadModelAvailability.UnavailableReason)."}
                      }},
                      "error": {"type": "object", "description": "Stable sanitized component error emitted by getComponentExtensionDiagnostics (ComponentExtensionError).", "properties": {
                        "code": {"type": "string", "description": "Error code emitted by getComponentExtensionDiagnostics (ComponentExtensionError.Code)."},
                        "message": {"type": "string", "description": "Error message emitted by getComponentExtensionDiagnostics (ComponentExtensionError.Message)."}
                      }}
                    }},
                    "policy": {"type": "object", "description": "Trust policy knob summary emitted by getComponentExtensionDiagnostics (ComponentExtensionPolicyStatus).", "properties": {
                      "mode": {"type": "string", "description": "Policy mode emitted by getComponentExtensionDiagnostics (ComponentExtensionPolicyStatus.Mode)."},
                      "allow_ids_configured": {"type": "boolean", "description": "Whether allow ids are configured, emitted by getComponentExtensionDiagnostics (ComponentExtensionPolicyStatus.AllowIDsConfigured)."},
                      "allow_publishers_configured": {"type": "boolean", "description": "Whether allow publishers are configured, emitted by getComponentExtensionDiagnostics (ComponentExtensionPolicyStatus.AllowPublishersConfigured)."},
                      "revoke_ids_configured": {"type": "boolean", "description": "Whether revoke ids are configured, emitted by getComponentExtensionDiagnostics (ComponentExtensionPolicyStatus.RevokeIDsConfigured)."},
                      "revoke_publishers_configured": {"type": "boolean", "description": "Whether revoke publishers are configured, emitted by getComponentExtensionDiagnostics (ComponentExtensionPolicyStatus.RevokePublishersConfigured)."},
                      "core_version_configured": {"type": "boolean", "description": "Whether a core version is configured, emitted by getComponentExtensionDiagnostics (ComponentExtensionPolicyStatus.CoreVersionConfigured)."}
                    }}
                  }
                }
              }
            }
          },
          "404": {"$ref": "#/components/responses/NotFound"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
`
