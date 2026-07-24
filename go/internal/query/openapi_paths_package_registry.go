// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

const openAPIPathsPackageRegistry = `
    "/api/v0/package-registry/packages": {
      "get": {
        "tags": ["package-registry"],
        "summary": "List package registry packages",
        "description": "Lists package identity nodes materialized from package-registry facts. Source repository hints remain provenance-only and are not returned as ownership truth. Scoped tokens receive visibility='public' rows for an ecosystem browse (source_path redacted), and any package rows a bounded correlation grant proves the caller's repositories own, consume, or publish; a private/unknown package the caller has no grant for returns the same empty result as a nonexistent package.",
        "operationId": "listPackageRegistryPackages",
        "x-scoped-token-support": true,
        "parameters": [
          {"name": "package_id", "in": "query", "schema": {"type": "string"}, "description": "Exact Package.uid lookup."},
          {"name": "ecosystem", "in": "query", "schema": {"type": "string"}, "description": "Package ecosystem scope such as npm, maven, pypi, go, cargo, hex, or nuget."},
          {"name": "name", "in": "query", "schema": {"type": "string"}, "description": "Normalized package name. Requires ecosystem when package_id is absent."},
          {"name": "limit", "in": "query", "required": true, "schema": {"type": "integer", "minimum": 1, "maximum": 200}}
        ],
        "responses": {
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
          "200": {
            "description": "Package registry package identities",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "packages": {
                      "type": "array",
                      "items": {
                        "type": "object",
                        "properties": {
                          "package_id": {"type": "string"},
                          "ecosystem": {"type": "string"},
                          "registry": {"type": "string"},
                          "namespace": {"type": "string"},
                          "normalized_name": {"type": "string"},
                          "purl": {"type": "string"},
                          "bom_ref": {"type": "string"},
                          "package_manager": {"type": "string"},
                          "source_path": {"type": "string"},
                          "source_specific_id": {"type": "string"},
                          "visibility": {"type": "string"},
                          "source_confidence": {"type": "string"},
                          "version_count": {"type": "integer"}
                        },
                        "required": ["package_id", "version_count"]
                      }
                    },
                    "identity_issues": {
                      "type": "array",
                      "description": "Package-registry graph rows that could not be returned as valid package identities because required identity evidence was missing.",
                      "items": {
                        "type": "object",
                        "properties": {
                          "reason": {"type": "string", "enum": ["package_id_missing"]},
                          "missing_evidence": {"type": "array", "items": {"type": "string", "enum": ["package_id"]}},
                          "ecosystem": {"type": "string"},
                          "registry": {"type": "string"},
                          "namespace": {"type": "string"},
                          "normalized_name": {"type": "string"},
                          "purl": {"type": "string"},
                          "bom_ref": {"type": "string"},
                          "package_manager": {"type": "string"},
                          "source_path": {"type": "string"},
                          "source_specific_id": {"type": "string"},
                          "visibility": {"type": "string"},
                          "source_confidence": {"type": "string"},
                          "version_count": {"type": "integer"}
                        },
                        "required": ["reason", "missing_evidence", "version_count"]
                      }
                    },
                    "count": {"type": "integer"},
                    "limit": {"type": "integer"},
                    "truncated": {"type": "boolean"},
                    "collector_readiness": {"$ref": "#/components/schemas/CollectorReadinessEnvelope"}
                  },
                  "required": ["packages", "identity_issues", "count", "limit", "truncated"]
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
    "/api/v0/package-registry/versions": {
      "get": {
        "tags": ["package-registry"],
        "summary": "List package registry versions",
        "description": "Lists package-version identity nodes for one Package.uid. This endpoint reports registry identity and version metadata, not repository ownership. Scoped tokens receive results only when the anchor package is visibility='public' or a bounded correlation grant proves the caller's repositories own, consume, or publish it; otherwise the response is the same empty result as a nonexistent package_id.",
        "operationId": "listPackageRegistryVersions",
        "x-scoped-token-support": true,
        "parameters": [
          {"name": "package_id", "in": "query", "required": true, "schema": {"type": "string"}, "description": "Package.uid to anchor the version lookup."},
          {"name": "limit", "in": "query", "required": true, "schema": {"type": "integer", "minimum": 1, "maximum": 200}}
        ],
        "responses": {
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
          "200": {
            "description": "Package registry package-version identities",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "versions": {
                      "type": "array",
                      "items": {
                        "type": "object",
                        "properties": {
                          "version_id": {"type": "string"},
                          "package_id": {"type": "string"},
                          "version": {"type": "string"},
                          "purl": {"type": "string"},
                          "bom_ref": {"type": "string"},
                          "package_manager": {"type": "string"},
                          "published_at": {"type": "string", "format": "date-time"},
                          "is_yanked": {"type": "boolean"},
                          "is_unlisted": {"type": "boolean"},
                          "is_deprecated": {"type": "boolean"},
                          "is_retracted": {"type": "boolean"}
                        },
                        "required": ["version_id", "package_id", "is_yanked", "is_unlisted", "is_deprecated", "is_retracted"]
                      }
                    },
                    "count": {"type": "integer"},
                    "limit": {"type": "integer"},
                    "truncated": {"type": "boolean"},
                    "collector_readiness": {"$ref": "#/components/schemas/CollectorReadinessEnvelope"}
                  },
                  "required": ["versions", "count", "limit", "truncated"]
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
    "/api/v0/package-registry/dependencies": {
      "get": {
        "tags": ["package-registry"],
        "summary": "List package registry dependencies",
        "description": "Lists package-native dependency edges materialized from package-registry facts. This endpoint reports declared package dependencies, not repository ownership or runtime consumption. Scoped tokens receive results only when the source package (resolved from package_id, or from version_id's owning package) is visibility='public' or a bounded correlation grant proves the caller's repositories own, consume, or publish it; otherwise the response is the same empty result as a nonexistent anchor.",
        "operationId": "listPackageRegistryDependencies",
        "x-scoped-token-support": true,
        "parameters": [
          {"name": "package_id", "in": "query", "schema": {"type": "string"}, "description": "Package.uid to anchor dependency lookup when version_id is absent."},
          {"name": "version_id", "in": "query", "schema": {"type": "string"}, "description": "PackageVersion.uid for an exact version-scoped dependency lookup."},
          {"name": "after_version_id", "in": "query", "schema": {"type": "string"}, "description": "Source PackageVersion.uid from next_cursor when continuing a truncated dependency page."},
          {"name": "after_dependency_id", "in": "query", "schema": {"type": "string"}, "description": "PackageDependency.uid from next_cursor when continuing a truncated dependency page."},
          {"name": "limit", "in": "query", "required": true, "schema": {"type": "integer", "minimum": 1, "maximum": 200}}
        ],
        "responses": {
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
          "200": {
            "description": "Package registry dependency edges",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "dependencies": {
                      "type": "array",
                      "items": {
                        "type": "object",
                        "properties": {
                          "dependency_id": {"type": "string"},
                          "source_package_id": {"type": "string"},
                          "source_version_id": {"type": "string"},
                          "version": {"type": "string"},
                          "dependency_package_id": {"type": "string"},
                          "dependency_ecosystem": {"type": "string"},
                          "dependency_registry": {"type": "string"},
                          "dependency_namespace": {"type": "string"},
                          "dependency_normalized": {"type": "string"},
                          "dependency_purl": {"type": "string"},
                          "dependency_bom_ref": {"type": "string"},
                          "dependency_manager": {"type": "string"},
                          "dependency_range": {"type": "string"},
                          "dependency_type": {"type": "string"},
                          "target_framework": {"type": "string"},
                          "marker": {"type": "string"},
                          "optional": {"type": "boolean"},
                          "excluded": {"type": "boolean"},
                          "source_confidence": {"type": "string"},
                          "collector_kind": {"type": "string"},
                          "collector_instance_id": {"type": "string"},
                          "correlation_anchors": {"type": "array", "items": {"type": "string"}}
                        },
                        "required": ["dependency_id", "source_package_id", "source_version_id", "dependency_package_id", "optional", "excluded"]
                      }
                    },
                    "count": {"type": "integer"},
                    "limit": {"type": "integer"},
                    "truncated": {"type": "boolean"},
                    "next_cursor": {
                      "type": "object",
                      "properties": {
                        "after_version_id": {"type": "string"},
                        "after_dependency_id": {"type": "string"}
                      },
                      "required": ["after_version_id", "after_dependency_id"]
                    },
                    "collector_readiness": {"$ref": "#/components/schemas/CollectorReadinessEnvelope"}
                  },
                  "required": ["dependencies", "count", "limit", "truncated"]
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
    "/api/v0/package-registry/correlations": {
      "get": {
        "tags": ["package-registry"],
        "summary": "List package registry correlations",
        "description": "Lists reducer-owned package ownership candidates, publication evidence, and manifest-backed consumption correlations. Requests must be anchored by package_id or repository_id and return provenance_only so source hints are not mistaken for ownership truth.",
        "operationId": "listPackageRegistryCorrelations",
        "x-scoped-token-support": true,
        "parameters": [
          {"name": "package_id", "in": "query", "schema": {"type": "string"}, "description": "Package.uid to anchor package ownership, publication, or consumption correlation lookup."},
          {"name": "repository_id", "in": "query", "schema": {"type": "string"}, "description": "Canonical repository id or human source repository selector (name, repo slug, indexed path, local path, or remote URL) to anchor package ownership, publication, or consumption correlation lookup. Unknown or ambiguous selectors return a selector error instead of an empty page."},
          {"name": "relationship_kind", "in": "query", "schema": {"type": "string", "enum": ["ownership", "publication", "consumption"]}, "description": "Optional relationship kind filter."},
          {"name": "after_correlation_id", "in": "query", "schema": {"type": "string"}, "description": "Correlation ID from next_cursor when continuing a truncated page."},
          {"name": "limit", "in": "query", "required": true, "schema": {"type": "integer", "minimum": 1, "maximum": 200}}
        ],
        "responses": {
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
          "200": {
            "description": "Package registry ownership, publication, and consumption correlations",
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
                          "relationship_kind": {"type": "string"},
                          "package_id": {"type": "string"},
                          "version_id": {"type": "string"},
                          "version": {"type": "string"},
                          "published_at": {"type": "string", "format": "date-time"},
                          "ecosystem": {"type": "string"},
                          "package_name": {"type": "string"},
                          "repository_id": {"type": "string"},
                          "repository_name": {"type": "string"},
                          "source_url": {"type": "string"},
                          "candidate_repository_ids": {"type": "array", "items": {"type": "string"}},
                          "relative_path": {"type": "string"},
                          "manifest_section": {"type": "string"},
                          "dependency_range": {"type": "string"},
                          "outcome": {"type": "string"},
                          "reason": {"type": "string"},
                          "provenance_only": {"type": "boolean"},
                          "canonical_writes": {"type": "integer"},
                          "evidence_fact_ids": {"type": "array", "items": {"type": "string"}}
                        },
                        "required": ["correlation_id", "relationship_kind", "package_id", "outcome", "provenance_only", "canonical_writes"]
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
                    },
                    "collector_readiness": {"$ref": "#/components/schemas/CollectorReadinessEnvelope"}
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
    "/api/v0/package-registry/dependency-chains": {
      "get": {
        "tags": ["package-registry"],
        "summary": "List package-evidenced repo-to-repo dependency chains",
        "description": "Resolves consumer-repo -> package -> publisher-repo dependency chains for one repository entirely on the read side, by joining canonical manifest-backed consumption correlations with provenance-only publication/ownership correlations. Publisher legs are inferred provenance-only links (provenance_only=true), never asserted Repository dependency edges; multiple candidate publishers mark the chain ambiguous and are never collapsed to a single publisher.",
        "operationId": "listPackageRegistryDependencyChains",
        "x-scoped-token-support": true,
        "parameters": [
          {"name": "repository_id", "in": "query", "required": true, "schema": {"type": "string"}, "description": "Canonical repository id or human source repository selector (name, repo slug, indexed path, local path, or remote URL) for the consumer repository. Unknown or ambiguous selectors return a selector error instead of an empty page."},
          {"name": "limit", "in": "query", "required": true, "schema": {"type": "integer", "minimum": 1, "maximum": 200}}
        ],
        "responses": {
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
          "200": {
            "description": "Package-evidenced repo-to-repo dependency chains for the repository",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "chains": {
                      "type": "array",
                      "items": {
                        "type": "object",
                        "properties": {
                          "consumer_repository_id": {"type": "string"},
                          "consumer_repository_name": {"type": "string"},
                          "package_id": {"type": "string"},
                          "package_name": {"type": "string"},
                          "ecosystem": {"type": "string"},
                          "dependency_range": {"type": "string"},
                          "consumption_correlation_id": {"type": "string"},
                          "consumption_provenance_only": {"type": "boolean"},
                          "consumption_canonical_writes": {"type": "integer"},
                          "ambiguous": {"type": "boolean"},
                          "publishers": {
                            "type": "array",
                            "items": {
                              "type": "object",
                              "properties": {
                                "correlation_id": {"type": "string"},
                                "relationship_kind": {"type": "string"},
                                "repository_id": {"type": "string"},
                                "repository_name": {"type": "string"},
                                "source_url": {"type": "string"},
                                "outcome": {"type": "string"},
                                "reason": {"type": "string"},
                                "provenance_only": {"type": "boolean"},
                                "canonical_writes": {"type": "integer"}
                              },
                              "required": ["correlation_id", "relationship_kind", "repository_id", "provenance_only", "canonical_writes"]
                            }
                          }
                        },
                        "required": ["consumer_repository_id", "package_id", "consumption_correlation_id", "consumption_provenance_only", "consumption_canonical_writes", "ambiguous", "publishers"]
                      }
                    },
                    "repository_id": {"type": "string"},
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
                  "required": ["chains", "count", "limit", "truncated"]
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
