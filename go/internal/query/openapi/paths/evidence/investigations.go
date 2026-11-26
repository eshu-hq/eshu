// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package evidence

// Investigations is the OpenAPI path fragment documenting the
// `/api/v0/investigations/*` routes. openapi.Spec concatenates it into the
// published document; keep it in lockstep with the handlers and
// docs/public/reference/http-api.md.
const Investigations = `
    "/api/v0/investigations/supply-chain/impact/packet": {
      "get": {
        "tags": ["investigations"],
        "summary": "Export supply-chain impact investigation packet",
        "description": "Returns an investigation_evidence_packet.v2 artifact for one bounded supply-chain impact investigation, using the same composer as the CLI export surface.",
        "operationId": "getSupplyChainImpactPacket",
        "x-scoped-token-support": true,
        "parameters": [
          {"name": "finding_id", "in": "query", "required": false, "schema": {"type": "string"}, "description": "Exact reducer-owned finding id. Preferred when known."},
          {"name": "advisory_id", "in": "query", "required": false, "schema": {"type": "string"}, "description": "Advisory identifier such as GHSA, OSV, GLAD, vendor advisory, or CVE id."},
          {"name": "cve_id", "in": "query", "required": false, "schema": {"type": "string"}, "description": "CVE identifier when advisory_id is not the canonical CVE field."},
          {"name": "package_id", "in": "query", "required": false, "schema": {"type": "string"}, "description": "Normalized package identity."},
          {"name": "repository_id", "in": "query", "required": false, "schema": {"type": "string"}, "description": "Repository identifier or selector from package consumption evidence."},
          {"name": "subject_digest", "in": "query", "required": false, "schema": {"type": "string"}, "description": "Image or artifact digest from SBOM/runtime evidence."},
          {"name": "image_ref", "in": "query", "required": false, "schema": {"type": "string"}, "description": "Exact image reference stored on reducer-owned impact findings."},
          {"name": "workload_id", "in": "query", "required": false, "schema": {"type": "string"}, "description": "Reducer-admitted workload anchor."},
          {"name": "service_id", "in": "query", "required": false, "schema": {"type": "string"}, "description": "Reducer-admitted service anchor."},
          {"name": "max_source_facts", "in": "query", "required": false, "schema": {"type": "integer", "minimum": 1}, "description": "Optional lower cap for the packet source_facts layer."}
        ],
        "responses": {
          "403": {"$ref": "#/components/responses/Forbidden"},
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
          "200": {"description": "Investigation evidence packet", "content": {"application/json": {"schema": {"$ref": "#/components/schemas/InvestigationEvidencePacket"}}}},
          "400": {"$ref": "#/components/responses/BadRequest"},
          "404": {"$ref": "#/components/responses/NotFound"},
          "409": {"$ref": "#/components/responses/Conflict"},
          "501": {"$ref": "#/components/responses/NotImplemented"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
    "/api/v0/investigations/deployable-unit/packet": {
      "get": {
        "tags": ["investigations"],
        "summary": "Export deployable-unit investigation packet",
        "description": "Returns an investigation_evidence_packet.v2 artifact for deployable-unit admission truth, using reducer-owned admission-decision readback. A 503 with Retry-After means a transient condition that is safe to retry: the admission decision read hit a stale or timed-out PostgreSQL reader; a 503 without Retry-After means a configuration state: the Postgres admission decision read model is not configured on this API instance. No 5xx body carries backend error text. A reader-verdict 503 names capability admission_decisions.list in its error envelope.",
        "operationId": "getDeployableUnitPacket",
        "x-scoped-token-support": true,
        "parameters": [
          {"name": "scope_id", "in": "query", "required": true, "schema": {"type": "string"}, "description": "Ingestion scope id that bounds the admission decision read."},
          {"name": "generation_id", "in": "query", "required": true, "schema": {"type": "string"}, "description": "Scope generation id that bounds the admission decision read."},
          {"name": "repository_id", "in": "query", "required": false, "schema": {"type": "string"}, "description": "Optional repository anchor used to narrow deployable-unit decisions."},
          {"name": "repo_id", "in": "query", "required": false, "schema": {"type": "string"}, "description": "Alias for repository_id."},
          {"name": "max_source_facts", "in": "query", "required": false, "schema": {"type": "integer", "minimum": 1}, "description": "Optional lower cap for the packet source_facts layer."}
        ],
        "responses": {
          "200": {"description": "Investigation evidence packet", "content": {"application/json": {"schema": {"$ref": "#/components/schemas/InvestigationEvidencePacket"}}}},
          "400": {"$ref": "#/components/responses/BadRequest"},
          "403": {"$ref": "#/components/responses/Forbidden"},
          "501": {"$ref": "#/components/responses/NotImplemented"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
    "/api/v0/investigations/drift/packet": {
      "get": {
        "tags": ["investigations"],
        "summary": "Export runtime-drift investigation packet",
        "description": "Returns an investigation_evidence_packet.v2 artifact for bounded provider-neutral runtime drift findings. Scoped tokens must carry an exact AllowedScopeIDs grant for the requested cloud ingestion scope_id -- drift findings have no repository dimension, so there is no repository-to-cloud-scope map on this path -- and receive a scope_not_found refusal packet instead of findings for an ungranted or empty-grant scope_id. A 503 with Retry-After means a transient condition that is safe to retry: the drift finding read hit a stale or timed-out PostgreSQL reader; this route has no configuration 503, so every 503 it answers carries Retry-After. No 5xx body carries backend error text. A reader-verdict 503 names capability cloud_runtime_drift.readback.list in its error envelope.",
        "operationId": "getDriftPacket",
        "x-scoped-token-support": true,
        "parameters": [
          {"name": "scope_id", "in": "query", "required": false, "schema": {"type": "string"}, "description": "Canonical ingestion scope id. Required unless an account/project/subscription alias is set."},
          {"name": "account_id", "in": "query", "required": false, "schema": {"type": "string"}, "description": "Alias for scope_id for AWS account scope."},
          {"name": "project_id", "in": "query", "required": false, "schema": {"type": "string"}, "description": "Alias for scope_id for GCP project scope."},
          {"name": "subscription_id", "in": "query", "required": false, "schema": {"type": "string"}, "description": "Alias for scope_id for Azure subscription scope."},
          {"name": "provider", "in": "query", "required": false, "schema": {"type": "string", "enum": ["aws", "gcp", "azure"]}, "description": "Cloud provider filter."},
          {"name": "cloud_resource_uid", "in": "query", "required": false, "schema": {"type": "string"}, "description": "Optional exact canonical resource uid to inspect."},
          {"name": "max_source_facts", "in": "query", "required": false, "schema": {"type": "integer", "minimum": 1}, "description": "Optional lower cap for the packet source_facts layer."}
        ],
        "responses": {
          "200": {"description": "Investigation evidence packet", "content": {"application/json": {"schema": {"$ref": "#/components/schemas/InvestigationEvidencePacket"}}}},
          "400": {"$ref": "#/components/responses/BadRequest"},
          "403": {"$ref": "#/components/responses/Forbidden"},
          "501": {"$ref": "#/components/responses/NotImplemented"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
    "/api/v0/investigations/services/{service_name}": {
      "get": {
        "tags": ["entities"],
        "summary": "Investigate service",
        "description": "Returns a bounded service investigation packet with repositories considered, evidence coverage, findings, and recommended follow-up calls. An ambiguous selector answers 409 with error.code ambiguous; each candidate in error.details.candidates carries next_selector, the argument (service_id) and value to pass next. A candidate with legacy_id true is a leftover name-only workload from the identity change, not the live workload; choose a scoped candidate.",
        "operationId": "investigateService",
        "x-scoped-token-support": true,
        "parameters": [
          {"$ref": "#/components/parameters/ServiceName"},
          {
            "name": "service_id",
            "in": "query",
            "required": false,
            "schema": {"type": "string"},
            "description": "Optional canonical workload id used to disambiguate the service selector"
          },
          {
            "name": "repo",
            "in": "query",
            "required": false,
            "schema": {"type": "string"},
            "description": "Optional repository selector. It separates only services in different repositories; two same-named services in one repository stay ambiguous and need service_id"
          },
          {
            "name": "repository_id",
            "in": "query",
            "required": false,
            "schema": {"type": "string"},
            "description": "Optional repository selector alias"
          },
          {
            "name": "repo_id",
            "in": "query",
            "required": false,
            "schema": {"type": "string"},
            "description": "Optional repository selector alias"
          },
          {
            "name": "environment",
            "in": "query",
            "required": false,
            "schema": {"type": "string"},
            "description": "Optional environment context"
          },
          {
            "name": "intent",
            "in": "query",
            "required": false,
            "schema": {"type": "string"},
            "description": "Optional investigation intent such as runbook, onboarding, or incident"
          },
          {
            "name": "question",
            "in": "query",
            "required": false,
            "schema": {"type": "string"},
            "description": "Optional user question to preserve in the investigation packet"
          }
        ],
        "responses": {
          "403": {"$ref": "#/components/responses/Forbidden"},
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
          "200": {
            "description": "Service investigation packet",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "service_name": {"type": "string", "description": "Canonical service name the packet was built for, from BuildServiceInvestigationPacket in service/investigation_packet.go."},
                    "environment": {"type": "string", "description": "Optional environment selector carried by InvestigationOptions in service/investigation_packet.go; present only when requested."},
                    "intent": {"type": "string", "description": "Optional investigation intent (for example runbook or onboarding) in InvestigationOptions; present only when requested."},
                    "question": {"type": "string", "description": "Optional user question preserved in InvestigationOptions; present only when requested."},
                    "repositories_considered": {"type": "array", "items": {"type": "object", "description": "One considered repository (or the __truncated__ marker row past the cap). Owner: service/investigation_packet.go serviceInvestigationRepositories.", "properties": {
                      "repo_id": {"type": "string", "description": "Repository id merged by serviceInvestigationRepositories in service/investigation_packet.go; __truncated__ on the marker row that closes a cut list."},
                      "repo_name": {"type": "string", "description": "Repository display name; empty on the __truncated__ marker row."},
                      "roles": {"type": "array", "items": {"type": "string"}, "description": "Roles the repository plays in the investigation (service_owner, deployment_source, deployment_target, graph_dependent, content_consumer, provisioning_source)."},
                      "evidence_families": {"type": "array", "items": {"type": "string"}, "description": "Evidence families the repository contributes to, sorted."},
                      "evidence_family_count": {"type": "integer", "description": "Number of evidence families on this row, from addInvestigationRepo."},
                      "note": {"type": "string", "description": "Present only on the __truncated__ marker row that closes a list cut at the 50-row cap."}
                    }}, "description": "Repositories merged from the workload context by serviceInvestigationRepositories in service/investigation_packet.go, sorted by repo_id and capped at 50 rows."},
                    "repositories_with_evidence": {"type": "array", "items": {"type": "object", "description": "One repository with evidence. Owner: service/investigation_packet.go buildServiceInvestigationPacketWithContext.", "properties": {
                      "repo_id": {"type": "string", "description": "Repository id from repositoriesWithEvidence in service/investigation_packet.go (never the marker row)."},
                      "repo_name": {"type": "string", "description": "Repository display name."},
                      "roles": {"type": "array", "items": {"type": "string"}, "description": "Roles the repository plays in the investigation."},
                      "evidence_families": {"type": "array", "items": {"type": "string"}, "description": "Non-empty evidence families the repository contributes to, sorted."},
                      "evidence_family_count": {"type": "integer", "description": "Number of evidence families on this row."},
                      "note": {"type": "string", "description": "Never set on this list; documented for shape parity with repositories_considered."}
                    }}, "description": "Subset of repositories_considered with a non-empty evidence_families list, from repositoriesWithEvidence."},
                    "evidence_families_found": {"type": "array", "items": {"type": "string"}, "description": "Sorted evidence families present in the workload context, from serviceInvestigationEvidenceFamiliesWithContext in service/investigation_packet.go (api_surface, deployment_lanes, documentation, upstream_dependencies, downstream_consumers, support)."},
                    "coverage_summary": {"type": "object", "description": "Coverage state and reason plus repository/evidence counts. repository_count is the true number of repositories considered, taken before the result_limit cap; it never counts the __truncated__ marker row that closes a cut repositories_considered list. repositories_truncated is true when that list was cut at result_limit, so a caller reads it instead of scanning rows for the marker; repository_count above result_limit always comes with repositories_truncated true, and repositories_with_evidence_count counts only the rows returned. The packet also carries result_limit (the 50-row repository rendering cap), downstream_read_limit (the tighter bound that actually fires on the cross-repository fan-out, 25 by default), evidence_file_read_limit (the separate 5,000-file bound on the service repository's own indexed-file read), and truncated. truncated is true when the repository list was capped OR when the bounded provisioning-candidate, hostname, or consumer-evidence reads behind dependents, consumer_repositories, and provisioning_source_chains hit their own bounds; when only the consumer_repositories read is why, evidence_file_read_limit rather than downstream_read_limit may be the bound that fired.", "properties": {
                      "state": {"type": "string", "description": "Coverage state from serviceInvestigationCoverage in service/investigation_packet.go (partial once any evidence family is found)."},
                      "reason": {"type": "string", "description": "Why the state holds, naming each truncated family with the bound that fired (#10477)."},
                      "repository_count": {"type": "integer", "description": "True number of repositories considered before the result_limit cap; never counts the marker row (#8802)."},
                      "repositories_truncated": {"type": "boolean", "description": "True when repositories_considered was cut at result_limit."},
                      "repositories_with_evidence_count": {"type": "integer", "description": "Rows in repositories_with_evidence."},
                      "evidence_family_count": {"type": "integer", "description": "Evidence families found."},
                      "result_limit": {"type": "integer", "description": "Repository rendering cap (50, serviceStoryItemLimit)."},
                      "downstream_read_limit": {"type": "integer", "description": "Indirect-evidence row bound the enrichment ran under (25 by default)."},
                      "evidence_file_read_limit": {"type": "integer", "description": "Indexed-file bound on the service repository's own file read (5000, serviceEvidenceFileLimit)."},
                      "truncated": {"type": "boolean", "description": "True when the repository list was capped or a bounded dependent/consumer/chain read hit its bound."}
                    }, "required": ["state", "reason", "repository_count", "repositories_truncated", "repositories_with_evidence_count", "evidence_family_count", "result_limit", "downstream_read_limit", "evidence_file_read_limit", "truncated"]},
                    "investigation_findings": {"type": "array", "items": {"type": "object", "description": "One evidence-family finding. Owner: service/investigation_packet.go serviceInvestigationFindingsWithContext.", "properties": {
                      "family": {"type": "string", "description": "Evidence family from serviceInvestigationFindingsWithContext in service/investigation_packet.go."},
                      "summary": {"type": "string", "description": "Human-readable family summary; carries a (bounded) suffix when the counts behind it hit a bound (#5720)."},
                      "evidence_path": {"type": "string", "description": "Workload-context path backing the finding, from serviceInvestigationEvidencePath."}
                    }, "required": ["family", "summary", "evidence_path"]}, "description": "One finding per evidence family, from serviceInvestigationFindingsWithContext."},
                    "recommended_next_calls": {"type": "array", "items": {"type": "object", "description": "One recommended follow-up call. Owner: service/investigation_packet.go serviceInvestigationNextCalls.", "properties": {
                      "tool": {"type": "string", "description": "MCP tool to call next, from serviceInvestigationNextCalls in service/investigation_packet.go."},
                      "reason": {"type": "string", "description": "Why the follow-up call is recommended."},
                      "arguments": {"type": "object", "description": "Tool arguments from serviceInvestigationNextCalls: workload_id for dossier and context calls, service_name plus include_related_module_usage for chain walks, or resolved_id for relationship dereference.", "additionalProperties": {}}
                    }, "required": ["tool", "reason", "arguments"]}, "description": "Dossier, context, chain-walk, and relationship-dereference follow-ups from serviceInvestigationNextCalls."},
                    "service_story_path": {"type": "string", "description": "One-call dossier route for this service, built by BuildServiceInvestigationPacket (/api/v0/services/<service_name>/story)."},
                    "service_context_path": {"type": "string", "description": "Raw service context route for this service (/api/v0/services/<service_name>/context)."}
                  }
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "404": {"$ref": "#/components/responses/NotFound"},
          "409": {"$ref": "#/components/responses/Conflict"},
          "501": {"$ref": "#/components/responses/NotImplemented"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
`
