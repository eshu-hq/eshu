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
          "dependents_truncated": {"type": "boolean", "description": "Present and true when the graph-derived dependent-repository read hit its bound. That read bounds rows, and one repository can supply several rows, so this reports either that the dependents list is not exhaustive or that the relationship_types and relationship_reasons on a returned entry were clipped. See the Context and stories HTTP API reference for the conditions under which the flag is true although no repository was dropped."},
          "consumer_repositories": {"type": "array", "items": {"type": "object"}},
          "consumer_repositories_truncated": {"type": "boolean", "description": "Present and true when any read underneath the consumer-repository list hit its bound: the graph-derived candidate read, the service evidence file read the observed hostnames are extracted from, any of the three narrowings applied to those hostnames before the cross-repository searches (the affinity filter, the four-hostname cap, and the cut against the caller's own limit), a per-search content row cap, or the final merge cap. The returned consumer_repositories list may not be exhaustive even though it is well under any displayed row limit. See the Context and stories HTTP API reference for the full enumeration and the conditions under which the flag is true although nothing was dropped."},
          "provisioning_source_chains": {"type": "array", "items": {"type": "object"}},
          "provisioning_source_chains_truncated": {"type": "boolean", "description": "Present and true when the provisioning-source-chain read hit its bound. That read bounds rows, and one repository can supply several rows, so this reports either that the provisioning_source_chains list is not exhaustive or that the relationship_types and relationship_reasons on a returned entry were clipped. See the Context and stories HTTP API reference for the conditions under which the flag is true although no repository was dropped."},
          "language_breakdown": {"type": "object", "description": "Per-language file counts for the service's primary repository, derived from indexed File nodes. Present only when language data is available. Keys are language names; values are integer file counts.", "additionalProperties": {"type": "integer"}},
          "source_tool_breakdown": {"type": "object", "description": "Per-source_tool outgoing relationship-edge counts for the service's primary repository. Present only when edges carry a source_tool property. Keys are canonical source_tool tokens; values are integer edge counts.", "additionalProperties": {"type": "integer"}},
          "dependencies": {"type": "array", "description": "Repository-level dependency rows for the workload's resolved repository. Present only once a repository has been resolved.", "items": {"type": "object"}},
          "infrastructure": {"type": "array", "description": "Repository-level infrastructure entities (Kubernetes, Terraform, Terragrunt, ArgoCD, Helm, Kustomize, Crossplane, CloudFormation) for the workload's resolved repository. Present only once a repository has been resolved; see limitations for degraded or truncated reads.", "items": {"type": "object"}},
          "materialization_status": {"type": "string", "description": "Present only on the repository-read-model fallback path (no materialized graph Workload node yet). \"identity_only\" reports that this response was assembled from repository identity facts rather than a materialized Workload."},
          "query_basis": {"type": "string", "description": "Present only on the repository-read-model fallback path. \"repository_read_model\" reports the response was derived from repository content evidence, not a graph Workload read."},
          "result_limits": {"type": "object", "description": "Additive drilldown block: bounded limit, deterministic ordering, fan-out counts, truncation flag, and the next prompt tool plus context path. downstream_read_limit reports the tighter 25-row bound underneath the fan-out lists (versus limit, the 50-row rendering cap); evidence_file_read_limit reports the separate 5,000-file bound on the service repository's own indexed-file read that consumer_repositories_truncated also folds in. When truncated is true because of dependents_truncated or provisioning_source_chains_truncated, downstream_read_limit was exceeded; when it is true only because of consumer_repositories_truncated, evidence_file_read_limit or one of the other bounds documented on that flag may be the one that fired instead; truncated is also true when the infrastructure panel's own read landed past its bound (see limitations' infrastructure_truncated).", "additionalProperties": true},
          "limitations": {"type": "array", "description": "Raw limitation reasons attached directly to the context payload before promotion into partial_reasons. Populated on the primary graph-materialized path (not only the repository read-model fallback) when an auxiliary infrastructure read degrades or lands past its bound: infrastructure_read_degraded (the graph read failed and infrastructure answered empty rather than propagating), infrastructure_truncated (a healthy read landed past its LIMIT bound; more rows may exist), or workload_identity_not_materialized (repository read-model fallback with no materialized Workload node).", "items": {"type": "string", "enum": ["infrastructure_read_degraded", "infrastructure_truncated", "workload_identity_not_materialized"]}},
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
                    "required": ["topology_basis"],
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
            "items": {
              "type": "object",
              "required": ["topology_basis"],
              "properties": {
                "platform_id": {"type": "string"},
                "platform_name": {"type": "string"},
                "platform_kind": {"type": "string"},
                "platform_provider": {"type": "string"},
                "platform_region": {"type": "string"},
                "platform_locator": {"type": "string"},
                "platform_confidence": {"type": "number"},
                "platform_reason": {"type": "string"},
                "topology_basis": {"type": "string", "enum": ["provisioning_fallback"], "description": "The platform is supported by exact repository provisioning relationships, not a WorkloadInstance RUNS_ON relationship."},
                "topology_edges": {
                  "type": "array",
                  "description": "Exact canonical provisioning relationships supporting this fallback platform.",
                  "items": {
                    "type": "object",
                    "required": ["relationship_type", "source_id", "target_id", "properties"],
                    "properties": {
                      "relationship_type": {"type": "string", "enum": ["PROVISIONS_DEPENDENCY_FOR", "PROVISIONS_PLATFORM"]},
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
          "runtime_topology_limits": ` + openAPIImpactRuntimeTopologyLimits + `
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
