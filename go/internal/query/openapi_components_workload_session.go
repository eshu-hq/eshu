// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

// openAPIComponentsWorkloadSession documents the WorkloadContext and browser
// -session schemas, included into openAPIComponents' "schemas" object as a
// separate constant fragment to keep openapi_components.go comfortably under
// the repository's file-size limit — same pattern as
// openAPIComponentsReplatforming (openapi_components_replatforming.go) and
// openAPIComponentsProviderConfigs (openapi_components_provider_configs.go).
const openAPIComponentsWorkloadSession = `      "WorkloadContext": {
        "type": "object",
        "properties": {
          "id": {"type": "string"},
          "name": {"type": "string"},
          "kind": {"type": "string"},
          "repo_id": {"type": "string"},
          "repo_name": {"type": "string"},
          "hostnames": {"type": "array", "items": {"type": "object"}},
          "entrypoint_candidates": {"type": "array", "description": "Hostname-shaped candidates kept as non-entrypoint supporting evidence with classification and reason.", "items": {"type": "object"}},
          "entrypoints": {"type": "array", "items": {"type": "object"}},
          "network_paths": {"type": "array", "items": {"type": "object"}},
          "ingress_posture": {"type": "object", "description": "WAF coverage and TLS termination posture for the service's internet-facing edge resources, derived strictly from the materialized AWS_wafv2_web_acl_protects_resource and AWS_acm_certificate_used_by_resource edges. waf_coverage and tls_termination are three-valued (protected/unprotected/unproven and terminated/not_terminated/unproven). unproven covers both no edge resource materialized and collector-absent, so absence of collector is never misreported as absence of protection."},
          "observed_config_environments": {"type": "array", "items": {"type": "string"}},
          "api_surface": {"type": "object"},
          "deployment_overview": {"type": "object"},
          "deployment_evidence": {"type": "object"},
          "story_sections": {"type": "array", "items": {"type": "object"}},
          "documentation_overview": {"type": "object"},
          "support_overview": {"type": "object"},
          "dependents": {"type": "array", "items": {"type": "object"}},
          "consumer_repositories": {"type": "array", "items": {"type": "object"}},
          "provisioning_source_chains": {"type": "array", "items": {"type": "object"}},
          "language_breakdown": {"type": "object", "description": "Per-language file counts for the service's primary repository, derived from indexed File nodes. Present only when language data is available. Keys are language names; values are integer file counts.", "additionalProperties": {"type": "integer"}},
          "source_tool_breakdown": {"type": "object", "description": "Per-source_tool outgoing relationship-edge counts for the service's primary repository. Present only when edges carry a source_tool property. Keys are canonical source_tool tokens; values are integer edge counts.", "additionalProperties": {"type": "integer"}},
          "result_limits": {"type": "object", "description": "Additive drilldown block: bounded limit, deterministic ordering, fan-out counts, truncation flag, and the next prompt tool plus context path.", "additionalProperties": true},
          "partial_reasons": {"type": "array", "description": "Explicit limitations or unsupported-evidence reasons for the context read; always present so the envelope shape is stable across complete and partial reads.", "items": {"type": "string"}},
          "instances": {
            "type": "array",
            "items": {
              "type": "object",
              "properties": {
                "instance_id": {"type": "string"},
                "platform_name": {"type": "string"},
                "platform_kind": {"type": "string"},
                "platforms": {
                  "type": "array",
                  "items": {
                    "type": "object",
                    "properties": {
                      "platform_id": {"type": "string", "description": "Canonical graph Platform identity; never derived from the display name."},
                      "platform_name": {"type": "string"},
                      "platform_kind": {"type": "string"},
                      "platform_confidence": {"type": "number"},
                      "platform_reason": {"type": "string"},
                      "topology_basis": {"type": "string", "enum": ["direct_runtime"], "description": "The platform is supported by an exact WorkloadInstance RUNS_ON Platform relationship."},
                      "topology_edges": {
                        "type": "array",
                        "items": {
                          "type": "object",
                          "required": ["relationship_type", "source_id", "target_id"],
                          "properties": {
                            "relationship_type": {"type": "string", "enum": ["RUNS_ON"]},
                            "source_id": {"type": "string"},
                            "source_name": {"type": "string"},
                            "target_id": {"type": "string"},
                            "target_name": {"type": "string"},
                            "confidence": {"type": "number"},
                            "reason": {"type": "string"},
                            "evidence_source": {"type": "string"},
                            "source_tool": {"type": "string"},
                            "properties": {"type": "object", "additionalProperties": true}
                          }
                        }
                      }
                    }
                  }
                },
                "environment": {"type": "string"}
              }
            }
          },
          "topology_edges": {
            "type": "array",
            "description": "Exact graph-observed Repository DEFINES Workload and WorkloadInstance INSTANCE_OF Workload edges.",
            "items": {
              "type": "object",
              "required": ["relationship_type", "source_id", "target_id", "properties"],
              "properties": {
                "relationship_type": {"type": "string", "enum": ["DEFINES", "INSTANCE_OF"]},
                "source_id": {"type": "string"},
                "target_id": {"type": "string"},
                "properties": {"type": "object", "additionalProperties": true}
              }
            }
          },
          "provisioned_platforms": {
            "type": "array",
            "description": "Repository-level provisioning evidence kept separate from runtime instance placement.",
            "items": {"type": "object"}
          },
          "runtime_topology_limits": {
            "type": "object",
            "description": "Completeness metadata for bounded instance, RUNS_ON, and provisioning reads.",
            "additionalProperties": true
          }
        }
      },
      "BrowserSessionAuth": {
        "type": "object",
        "description": "Authorization context attached to a server-managed dashboard browser session. Subject and policy identifiers are hashes or stable opaque ids; raw credentials are never returned.",
        "properties": {
          "mode": {"type": "string", "enum": ["browser_session"]},
          "tenant_id": {"type": "string"},
          "workspace_id": {"type": "string"},
          "subject_class": {"type": "string"},
          "subject_id_hash": {"type": "string"},
          "policy_revision_hash": {"type": "string"},
          "role_ids": {"type": "array", "items": {"type": "string"}},
          "all_scopes": {"type": "boolean"},
          "allowed_scope_ids": {"type": "array", "items": {"type": "string"}},
          "allowed_repository_ids": {"type": "array", "items": {"type": "string"}},
          "permission_catalog_enforced": {"type": "boolean"},
          "allowed_permission_features": {"type": "array", "items": {"type": "string"}}
        }
      },
      "BrowserSessionResponse": {
        "type": "object",
        "description": "Dashboard browser session response. csrf_token appears only when creating a session; the raw session secret is never returned in JSON and is sent only via the HttpOnly session cookie.",
        "properties": {
          "auth": {"$ref": "#/components/schemas/BrowserSessionAuth"},
          "csrf_token": {"type": "string", "description": "CSRF secret for X-Eshu-CSRF on unsafe cookie-authenticated requests. It is bound to the server-side session hash."},
          "idle_expires_at": {"type": "string", "format": "date-time"},
          "absolute_expires_at": {"type": "string", "format": "date-time"}
        }
      },
`
