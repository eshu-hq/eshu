package query

const openAPIPathsObservabilityCoverage = `
    "/api/v0/observability/coverage/correlations": {
      "get": {
        "tags": ["observability"],
        "summary": "List observability coverage correlations",
        "description": "Lists reducer-owned observability coverage correlation rows: which monitored cloud resources or services have alarm, dashboard, log, or trace coverage versus which coverage gaps remain. Coverage is structural correlation over observability object and resource identity facts, not a health assertion derived from telemetry values.",
        "operationId": "listObservabilityCoverageCorrelations",
        "parameters": [
          {"name": "scope_id", "in": "query", "schema": {"type": "string"}, "description": "Reducer scope ID to anchor lookup."},
          {"name": "provider", "in": "query", "schema": {"type": "string"}, "description": "Observability provider such as aws."},
          {"name": "coverage_signal", "in": "query", "schema": {"type": "string"}, "description": "Coverage signal class such as alarm, dashboard, log_group, or trace_sampling."},
          {"name": "observability_object_ref", "in": "query", "schema": {"type": "string"}, "description": "Provider-native observability object reference such as a CloudWatch alarm ARN."},
          {"name": "target_uid", "in": "query", "schema": {"type": "string"}, "description": "Monitored cloud resource UID (ARN or bare resource id) to anchor lookup."},
          {"name": "target_service_ref", "in": "query", "schema": {"type": "string"}, "description": "Target service reference such as an X-Ray service name to anchor lookup."},
          {"name": "outcome", "in": "query", "schema": {"type": "string", "enum": ["exact", "derived", "ambiguous", "unresolved", "stale", "rejected"]}},
          {"name": "coverage_status", "in": "query", "schema": {"type": "string"}, "description": "Optional coverage status filter such as covered or gap."},
          {"name": "after_correlation_id", "in": "query", "schema": {"type": "string"}, "description": "Correlation ID from next_cursor when continuing a truncated page."},
          {"name": "limit", "in": "query", "required": true, "schema": {"type": "integer", "minimum": 1, "maximum": 200}}
        ],
        "responses": {
          "200": {
            "description": "Observability coverage correlation rows",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "correlations": {
                      "type": "array",
                      "items": {
                        "type": "object",
                        "properties": {
                          "correlation_id": {"type": "string"},
                          "provider": {"type": "string"},
                          "coverage_signal": {"type": "string"},
                          "observability_object_ref": {"type": "string"},
                          "observability_resource_uid": {"type": "string"},
                          "target_uid": {"type": "string"},
                          "target_service_ref": {"type": "string"},
                          "outcome": {"type": "string"},
                          "reason": {"type": "string"},
                          "coverage_status": {"type": "string"},
                          "provenance_only": {"type": "boolean"},
                          "resolution_mode": {"type": "string"},
                          "candidate_target_uids": {"type": "array", "items": {"type": "string"}},
                          "evidence_fact_ids": {"type": "array", "items": {"type": "string"}}
                        },
                        "required": ["correlation_id", "outcome", "provenance_only"]
                      }
                    },
                    "count": {"type": "integer"},
                    "limit": {"type": "integer"},
                    "truncated": {"type": "boolean"},
                    "next_cursor": {
                      "type": "object",
                      "properties": {
                        "after_correlation_id": {"type": "string"}
                      },
                      "required": ["after_correlation_id"]
                    }
                  },
                  "required": ["correlations", "count", "limit", "truncated"]
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "501": {"$ref": "#/components/responses/NotImplemented"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
`
