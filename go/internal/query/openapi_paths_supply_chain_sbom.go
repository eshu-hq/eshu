package query

const openAPIPathsSupplyChainSBOMAttestations = `
    "/api/v0/supply-chain/sbom-attestations/attachments": {
      "get": {
        "summary": "List SBOM and attestation attachments",
        "operationId": "listSBOMAttestationAttachments",
        "parameters": [
          {"name": "subject_digest", "in": "query", "schema": {"type": "string"}},
          {"name": "digest", "in": "query", "description": "Alias for subject_digest when the caller has an image digest.", "schema": {"type": "string"}},
          {"name": "document_id", "in": "query", "schema": {"type": "string"}},
          {"name": "document_digest", "in": "query", "schema": {"type": "string"}},
          {"name": "repository_id", "in": "query", "description": "Canonical source repository id or human repository selector (name, repo slug, indexed path, local path, or remote URL). Unknown or ambiguous selectors return a selector error before the bounded attachment read.", "schema": {"type": "string"}},
          {"name": "workload_id", "in": "query", "description": "Reducer-admitted workload anchor. Missing workload-to-image evidence remains explicit.", "schema": {"type": "string"}},
          {"name": "service_id", "in": "query", "description": "Reducer-admitted service anchor. Missing service-to-image evidence remains explicit.", "schema": {"type": "string"}},
          {"name": "attachment_status", "in": "query", "schema": {"type": "string", "enum": ["attached_verified", "attached_unverified", "attached_parse_only", "subject_mismatch", "ambiguous_subject", "unknown_subject", "unparseable"]}},
          {"name": "artifact_kind", "in": "query", "schema": {"type": "string", "enum": ["sbom", "attestation"]}},
          {"name": "after_attachment_id", "in": "query", "schema": {"type": "string"}},
          {"name": "limit", "in": "query", "required": true, "schema": {"type": "integer", "minimum": 1, "maximum": 200}}
        ],
        "responses": {
          "200": {
            "description": "SBOM and attestation attachment page",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "attachments": {
                      "type": "array",
                      "items": {
                        "type": "object",
                        "properties": {
                          "attachment_id": {"type": "string"},
                          "subject_digest": {"type": "string"},
                          "document_id": {"type": "string"},
                          "document_digest": {"type": "string"},
                          "repository_ids": {"type": "array", "items": {"type": "string"}},
                          "workload_ids": {"type": "array", "items": {"type": "string"}},
                          "service_ids": {"type": "array", "items": {"type": "string"}},
                          "attachment_status": {"type": "string"},
                          "parse_status": {"type": "string"},
                          "verification_status": {"type": "string"},
                          "verification_policy": {"type": "string"},
                          "reason": {"type": "string"},
                          "attachment_scope": {"type": "string", "enum": ["image_subject", "parse_only_unanchored", "subject_only_unanchored", "unanchored"]},
                          "missing_evidence": {"type": "array", "items": {"type": "string"}},
                          "canonical_writes": {"type": "integer"},
                          "component_count": {"type": "integer"},
                          "warning_summaries": {
                            "type": "array",
                            "maxItems": 10,
                            "description": "Bounded duplicate-collapsed preview of SBOM warning summaries.",
                            "items": {"type": "string"}
                          },
                          "warning_summary_count": {
                            "type": "integer",
                            "description": "Total SBOM warning occurrences represented by the attachment payload before preview bounding."
                          },
                          "warning_summaries_truncated": {
                            "type": "boolean",
                            "description": "True when warning_summaries omits duplicate, aggregate, or overflow entries from the recorded warning summaries."
                          }
                        }
                      }
                    },
                    "count": {"type": "integer"},
                    "missing_evidence": {"type": "array", "items": {"type": "string"}},
                    "limit": {"type": "integer"},
                    "truncated": {"type": "boolean"},
                    "next_cursor": {"type": "object"},
                    "collector_readiness": {"$ref": "#/components/schemas/CollectorReadinessEnvelope"}
                  }
                }
              }
            }
          }
        }
      }
    },
`
