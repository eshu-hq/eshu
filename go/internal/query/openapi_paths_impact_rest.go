package query

const openAPIPathsImpactRest = `
    "/api/v0/impact/pre-change": {
      "post": {
        "tags": ["impact"],
        "summary": "Analyze pre-change impact",
        "description": "Maps a changed-file list or base/head diff descriptor onto the bounded change-surface evidence graph, returning affected symbols, graph impacts, truth labels, missing evidence, truncation, and recommended next calls without requiring provider credentials.",
        "operationId": "analyzePreChangeImpact",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "description": "Provide changed_paths or changes with repo_id, optionally including base/head refs as provenance for that caller-derived diff, or provide an explicit graph target/topic.",
                "anyOf": [
                  {"required": ["changed_paths", "repo_id"]},
                  {"required": ["changes", "repo_id"]},
                  {"required": ["target"]},
                  {"required": ["service_name"]},
                  {"required": ["topic"]}
                ],
                "properties": {
                  "repo_id": {"type": "string", "description": "Repository selector for changed-path lookup"},
                  "base_ref": {"type": "string", "description": "Git base ref used by the caller to derive the supplied changed_paths or changes"},
                  "head_ref": {"type": "string", "description": "Git head ref used by the caller to derive the supplied changed_paths or changes"},
                  "changed_paths": {"type": "array", "description": "Repo-relative changed paths treated as modified files", "items": {"type": "string"}},
                  "changes": {
                    "type": "array",
                    "description": "Changed files with status and optional rename source.",
                    "items": {
                      "type": "object",
                      "properties": {
                        "path": {"type": "string", "description": "Current repo-relative path"},
                        "old_path": {"type": "string", "description": "Previous repo-relative path for renamed or copied files"},
                        "status": {"type": "string", "enum": ["added", "modified", "deleted", "renamed", "copied"]}
                      }
                    }
                  },
                  "target": {"type": "string"},
                  "target_type": {"type": "string", "enum": ["service", "workload", "workload_instance", "repository", "resource", "cloud_resource", "terraform_module", "module"]},
                  "service_name": {"type": "string"},
                  "workload_id": {"type": "string"},
                  "resource_id": {"type": "string"},
                  "module_id": {"type": "string"},
                  "topic": {"type": "string"},
                  "query": {"type": "string"},
                  "environment": {"type": "string"},
                  "max_depth": {"type": "integer", "default": 4, "minimum": 1, "maximum": 8},
                  "limit": {"type": "integer", "default": 25, "minimum": 1, "maximum": 100},
                  "offset": {"type": "integer", "default": 0, "minimum": 0, "maximum": 10000}
                }
              }
            }
          }
        },
        "responses": {
          "200": {
            "description": "Pre-change impact packet",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "workflow": {"type": "string", "enum": ["pre_change_impact"]},
                    "change_set": {"type": "object"},
                    "changed_files": {"type": "array", "items": {"type": "object"}},
                    "changed_file_count": {"type": "integer"},
                    "code_surface": {"type": "object"},
                    "direct_impact": {"type": "array", "items": {"type": "object"}},
                    "transitive_impact": {"type": "array", "items": {"type": "object"}},
                    "impact_summary": {"type": "object"},
                    "missing_evidence": {"type": "array", "items": {"type": "object"}},
                    "coverage": {"type": "object"},
                    "recommended_next_calls": {"type": "array", "items": {"type": "object"}},
                    "truncated": {"type": "boolean"},
                    "answer_metadata": {"type": "object", "description": "Normalized additive answer metadata with evidence handles, missing evidence, limitations, truncation, coverage, partial reasons, and next calls."},
                    "answer_packet": {"type": "object", "description": "AnswerPacket-shaped pre-change response for agent workflows."}
                  }
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "501": {"$ref": "#/components/responses/NotImplemented"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
    "/api/v0/impact/entity-map": {
      "post": {
        "tags": ["impact"],
        "summary": "Map a bounded entity neighborhood",
        "description": "Resolves one supported entity handle with typed label/property probes, then returns a bounded code/cloud neighborhood through relationship-family traversal grouped into defined_by, deployed_by, runs_as, depends_on, consumed_by, and evidence sections.",
        "operationId": "entityMap",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["from"],
                "properties": {
                  "from": {"type": "string", "description": "Entity handle such as terraform/aws_lb.main, workload:checkout, a repo id, or a typed graph id"},
                  "from_type": {"type": "string", "enum": ["service", "workload", "workload_instance", "repository", "repo", "resource", "cloud_resource", "terraform_resource", "terraform_datasource", "k8s_resource", "terraform_module", "module", "file"]},
                  "repo_id": {"type": "string"},
                  "environment": {"type": "string"},
                  "relationship": {"type": "string", "description": "Optional exact uppercase relationship type filter"},
                  "depth": {"type": "integer", "default": 1, "minimum": 1, "maximum": 4},
                  "limit": {"type": "integer", "default": 25, "minimum": 1, "maximum": 100}
                }
              }
            }
          }
        },
        "responses": {
          "200": {
            "description": "Entity map, ambiguity packet, or no-match packet",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "status": {"type": "string", "enum": ["mapped", "ambiguous", "no_match"]},
                    "command": {"type": "string"},
                    "from": {"type": "string"},
                    "scope": {"type": "object"},
                    "resolution": {"type": "object"},
                    "sections": {
                      "type": "object",
                      "properties": {
                        "defined_by": {"type": "array", "items": {"type": "object"}},
                        "deployed_by": {"type": "array", "items": {"type": "object"}},
                        "runs_as": {"type": "array", "items": {"type": "object"}},
                        "depends_on": {"type": "array", "items": {"type": "object"}},
                        "consumed_by": {"type": "array", "items": {"type": "object"}}
                      }
                    },
                    "evidence": {"type": "object"},
                    "coverage": {"type": "object"},
                    "warnings": {"type": "array", "items": {"type": "string"}}
                  }
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "501": {"$ref": "#/components/responses/NotImplemented"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
    "/api/v0/impact/resource-investigation": {
      "post": {
        "tags": ["impact"],
        "summary": "Investigate resource",
        "description": "Resolves a queue, database, cloud resource, Terraform resource, or Kubernetes object into a bounded investigation packet with ambiguity metadata, workload users, repository provenance paths, source handles, limitations, and recommended next calls.",
        "operationId": "investigateResource",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "description": "Provide query or resource_id.",
                "anyOf": [
                  {"required": ["query"]},
                  {"required": ["resource_id"]}
                ],
                "properties": {
                  "query": {"type": "string", "description": "Resource name, kind, queue, database, or cloud identifier to resolve"},
                  "resource_id": {"type": "string", "description": "Canonical graph resource id, provider resource id, or cloud ARN when already known"},
                  "resource_type": {"type": "string", "enum": ["queue", "database", "cloud_resource", "k8s_resource", "terraform_resource", "terraform_module"]},
                  "environment": {"type": "string"},
                  "max_depth": {"type": "integer", "default": 4, "minimum": 1, "maximum": 8},
                  "limit": {"type": "integer", "default": 25, "minimum": 1, "maximum": 100}
                }
              }
            }
          }
        },
        "responses": {
          "200": {
            "description": "Resource investigation packet",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "scope": {"type": "object"},
                    "target_resolution": {"type": "object"},
                    "resource": {"type": "object"},
                    "story": {"type": "string"},
                    "workloads": {"type": "array", "items": {"type": "object"}},
                    "workload_count": {"type": "integer"},
                    "provisioning_paths": {"type": "array", "items": {"type": "object"}},
                    "source_handles": {"type": "array", "items": {"type": "object"}},
                    "recommended_next_calls": {"type": "array", "items": {"type": "object"}},
                    "missing_evidence": {"type": "array", "items": {"type": "string"}},
                    "limitations": {"type": "array", "items": {"type": "string"}},
                    "coverage": {"type": "object"},
                    "limit": {"type": "integer"},
                    "max_depth": {"type": "integer"},
                    "truncated": {"type": "boolean"},
                    "environment": {"type": "string"},
                    "source_backend": {"type": "string"}
                  }
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "501": {"$ref": "#/components/responses/NotImplemented"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
    "/api/v0/impact/trace-resource-to-code": {
      "post": {
        "tags": ["impact"],
        "summary": "Trace resource to code",
        "description": "Traces a resource back to its source code repositories.",
        "operationId": "traceResourceToCode",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["start"],
                "properties": {
                  "start": {"type": "string", "description": "Starting entity ID"},
                  "environment": {"type": "string"},
                  "max_depth": {"type": "integer", "default": 8, "minimum": 1, "maximum": 20},
                  "limit": {"type": "integer", "default": 50, "minimum": 1, "maximum": 200}
                }
              }
            }
          }
        },
        "responses": {
          "200": {
            "description": "Trace paths",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "start": {"type": "object"},
                    "paths": {"type": "array", "items": {"type": "object"}},
                    "count": {"type": "integer"},
                    "limit": {"type": "integer"},
                    "truncated": {"type": "boolean"},
                    "environment": {"type": "string"}
                  }
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
    "/api/v0/impact/explain-dependency-path": {
      "post": {
        "tags": ["impact"],
        "summary": "Explain dependency path",
        "description": "Finds and explains the shortest path between two entities.",
        "operationId": "explainDependencyPath",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["source", "target"],
                "properties": {
                  "source": {"type": "string", "description": "Source entity ID"},
                  "target": {"type": "string", "description": "Target entity ID"},
                  "environment": {"type": "string"}
                }
              }
            }
          }
        },
        "responses": {
          "200": {
            "description": "Dependency path",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "source": {"type": "object"},
                    "target": {"type": "object"},
                    "path": {"type": "object"},
                    "confidence": {"type": "number"},
                    "reason": {"type": "string"},
                    "environment": {"type": "string"}
                  }
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "404": {"$ref": "#/components/responses/NotFound"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
`
