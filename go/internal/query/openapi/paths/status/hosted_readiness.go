// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status

// HostedReadiness is the OpenAPI path fragment documenting the
// `/api/v0/status/hosted-readiness` route. openapi.Spec concatenates it into
// the published document; keep it in lockstep with the handlers and
// docs/public/reference/http-api.md.
const HostedReadiness = `
    "/api/v0/status/hosted-readiness": {
      "get": {
        "tags": ["status"],
        "summary": "Get hosted operator readiness",
        "description": "Returns a fail-closed hosted readiness report across status snapshot loading, queue drain, collector completion, shared projection backlog, API/MCP query readback, and leftover name-only Workload nodes: the check workload_identity_leftovers fails with failure class legacy_workload_nodes while pre-repository-scoped Workload nodes (id workload:<name>) with a scoped twin remain, fails with failure class legacy_workload_detection_unavailable when its detection read errors, and is unknown (no failure class) on a graph backend other than Neo4j, where the detection is not validated and does not run, or when its examined cap was reached with none found. A 503 with Retry-After means a transient condition that is safe to retry: the status snapshot read hit a stale or timed-out PostgreSQL reader, including a snapshot read that outran its bounded budget; a 503 without Retry-After means a configuration state: the status reader is not configured on this API instance. No 5xx body carries backend error text. A reader-verdict 503 error envelope names capability operations.hosted_readiness_status. A 500 answers the shared failure envelope with code internal_error under the same capability and writes one ERROR log line with the wrapped error and the request id.",
        "operationId": "getHostedReadiness",
        "x-scoped-token-support": true,
        "parameters": [
          {
            "name": "format",
            "in": "query",
            "required": false,
            "schema": {"type": "string", "enum": ["json", "text"], "default": "json"},
            "description": "Return a plain-text human summary when set to text. The value is not validated: only text selects the text/plain rendering, and any other value serves the JSON 200. Owner: query.statusHostedReadiness via QueryParam format."
          }
        ],
        "responses": {
          "403": {"$ref": "#/components/responses/Forbidden"},
          "200": {
            "description": "Hosted readiness report",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "description": "Fail-closed hosted readiness report. Owner: query.hostedReadinessReport built by buildHostedReadinessReport.",
                  "properties": {
                    "version": {"type": "string", "description": "API binary version. Owner: query.buildHostedReadinessReport via buildinfo."},
                    "state": {"type": "string", "enum": ["ready", "not_ready"], "description": "Aggregate readiness state. Owner: query.buildHostedReadinessReport."},
                    "ready": {"type": "boolean", "description": "True when the state is ready. Owner: query.buildHostedReadinessReport."},
                    "summary": {"type": "string", "description": "Human readiness summary. Owner: query.hostedReadinessSummary."},
                    "generated_at": {"type": "string", "format": "date-time", "description": "Snapshot time in UTC. Owner: query.buildHostedReadinessReport via report.AsOf."},
                    "failure_classes": {"type": "array", "items": {"type": "string"}, "description": "Deduplicated failure classes behind a not_ready state; empty when ready. Owner: query.hostedReadinessBuilder."},
                    "repository_count": {"type": "integer", "description": "Repositories read back from the graph. Owner: query.readHostedRepositoryCount."},
                    "queue": ` + queueSnapshotSchema + `,
                    "active_work_source": {"$ref": "#/components/schemas/ActiveWorkSource"},
                    "coordinator_source": {"$ref": "#/components/schemas/CoordinatorSource"},
                    "coordinator": ` + coordinatorSchema + `,
                    "checks": {
                      "type": "array",
                      "description": "Per-check readiness verdicts. Owner: query.hostedReadinessBuilder.",
                      "items": {
                        "type": "object",
                        "description": "One readiness check verdict. Owner: query.hostedReadinessCheck.",
                        "properties": {
                          "name": {"type": "string", "description": "Check name. Owner: query.hostedReadinessCheck Name."},
                          "state": {"type": "string", "enum": ["pass", "fail", "unknown"], "description": "Check verdict. Owner: query.hostedReadinessCheck State."},
                          "failure_class": {"type": "string", "description": "Machine failure class; present only on a fail. Owner: query.hostedReadinessCheck FailureClass."},
                          "detail": {"type": "string", "description": "Human detail for the verdict. Owner: query.hostedReadinessCheck Detail."},
                          "next_diagnostic": {"type": "string", "description": "Follow-up read for the operator; present only on a fail. Owner: query.hostedReadinessCheck NextDiagnostic."}
                        }
                      }
                    },
                    "diagnostic_paths": {
                      "type": "array",
                      "description": "Fixed follow-up reads for the operator: admin_status, index_status, collector_status, and repository_coverage.",
                      "items": ` + diagnosticPathItemSchema + `
                    }
                  }
                }
              },
              "text/plain": {
                "schema": {"type": "string"}
              }
            }
          },
          "500": {"$ref": "#/components/responses/InternalError"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
`
