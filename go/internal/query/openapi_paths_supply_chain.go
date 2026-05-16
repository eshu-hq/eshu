package query

const openAPIPathsSupplyChain = `
    "/api/v0/supply-chain/impact/findings": {
      "get": {
        "summary": "List supply-chain impact findings",
        "operationId": "listSupplyChainImpactFindings",
        "parameters": [
          {"name": "cve_id", "in": "query", "schema": {"type": "string"}},
          {"name": "package_id", "in": "query", "schema": {"type": "string"}},
          {"name": "repository_id", "in": "query", "schema": {"type": "string"}},
          {"name": "subject_digest", "in": "query", "schema": {"type": "string"}},
          {"name": "impact_status", "in": "query", "schema": {"type": "string", "enum": ["affected_exact", "affected_derived", "possibly_affected", "not_affected_known_fixed", "unknown_impact"]}},
          {"name": "after_finding_id", "in": "query", "schema": {"type": "string"}},
          {"name": "limit", "in": "query", "required": true, "schema": {"type": "integer", "minimum": 1, "maximum": 200}}
        ],
        "responses": {
          "200": {
            "description": "Supply-chain impact finding page",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "findings": {
                      "type": "array",
                      "items": {
                        "type": "object",
                        "properties": {
                          "finding_id": {"type": "string"},
                          "cve_id": {"type": "string"},
                          "package_id": {"type": "string"},
                          "impact_status": {"type": "string"},
                          "confidence": {"type": "string"},
                          "runtime_reachability": {"type": "string"},
                          "missing_evidence": {"type": "array", "items": {"type": "string"}}
                        }
                      }
                    },
                    "count": {"type": "integer"},
                    "limit": {"type": "integer"},
                    "truncated": {"type": "boolean"},
                    "next_cursor": {"type": "object"}
                  }
                }
              }
            }
          }
        }
      }
    },
    "/api/v0/supply-chain/sbom-attestations/attachments": {
      "get": {
        "summary": "List SBOM and attestation attachments",
        "operationId": "listSBOMAttestationAttachments",
        "parameters": [
          {"name": "subject_digest", "in": "query", "schema": {"type": "string"}},
          {"name": "document_id", "in": "query", "schema": {"type": "string"}},
          {"name": "document_digest", "in": "query", "schema": {"type": "string"}},
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
                          "attachment_status": {"type": "string"},
                          "parse_status": {"type": "string"},
                          "verification_status": {"type": "string"},
                          "verification_policy": {"type": "string"},
                          "component_count": {"type": "integer"}
                        }
                      }
                    },
                    "count": {"type": "integer"},
                    "limit": {"type": "integer"},
                    "truncated": {"type": "boolean"},
                    "next_cursor": {"type": "object"}
                  }
                }
              }
            }
          }
        }
      }
    },
`
