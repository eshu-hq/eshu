package query

const openAPIPathsPackageRegistry = `
    "/api/v0/package-registry/packages": {
      "get": {
        "tags": ["package-registry"],
        "summary": "List package registry packages",
        "description": "Lists package identity nodes materialized from package-registry facts. Source repository hints remain provenance-only and are not returned as ownership truth.",
        "operationId": "listPackageRegistryPackages",
        "parameters": [
          {"name": "package_id", "in": "query", "schema": {"type": "string"}, "description": "Exact Package.uid lookup."},
          {"name": "ecosystem", "in": "query", "schema": {"type": "string"}, "description": "Package ecosystem scope such as npm, maven, pypi, go, cargo, or nuget."},
          {"name": "name", "in": "query", "schema": {"type": "string"}, "description": "Normalized package name. Requires ecosystem when package_id is absent."},
          {"name": "limit", "in": "query", "required": true, "schema": {"type": "integer", "minimum": 1, "maximum": 200}}
        ],
        "responses": {
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
                          "visibility": {"type": "string"},
                          "source_confidence": {"type": "string"},
                          "version_count": {"type": "integer"}
                        },
                        "required": ["package_id", "version_count"]
                      }
                    },
                    "count": {"type": "integer"},
                    "limit": {"type": "integer"},
                    "truncated": {"type": "boolean"}
                  },
                  "required": ["packages", "count", "limit", "truncated"]
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
        "description": "Lists package-version identity nodes for one Package.uid. This endpoint reports registry identity and version metadata, not repository ownership.",
        "operationId": "listPackageRegistryVersions",
        "parameters": [
          {"name": "package_id", "in": "query", "required": true, "schema": {"type": "string"}, "description": "Package.uid to anchor the version lookup."},
          {"name": "limit", "in": "query", "required": true, "schema": {"type": "integer", "minimum": 1, "maximum": 200}}
        ],
        "responses": {
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
                    "truncated": {"type": "boolean"}
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
        "description": "Lists package-native dependency edges materialized from package-registry facts. This endpoint reports declared package dependencies, not repository ownership or runtime consumption.",
        "operationId": "listPackageRegistryDependencies",
        "parameters": [
          {"name": "package_id", "in": "query", "schema": {"type": "string"}, "description": "Package.uid to anchor dependency lookup when version_id is absent."},
          {"name": "version_id", "in": "query", "schema": {"type": "string"}, "description": "PackageVersion.uid for an exact version-scoped dependency lookup."},
          {"name": "after_version_id", "in": "query", "schema": {"type": "string"}, "description": "Source PackageVersion.uid from next_cursor when continuing a truncated dependency page."},
          {"name": "after_dependency_id", "in": "query", "schema": {"type": "string"}, "description": "PackageDependency.uid from next_cursor when continuing a truncated dependency page."},
          {"name": "limit", "in": "query", "required": true, "schema": {"type": "integer", "minimum": 1, "maximum": 200}}
        ],
        "responses": {
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
                    }
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
        "description": "Lists reducer-owned package ownership candidates and manifest-backed consumption correlations. Requests must be anchored by package_id or repository_id and return provenance_only so source hints are not mistaken for ownership truth.",
        "operationId": "listPackageRegistryCorrelations",
        "parameters": [
          {"name": "package_id", "in": "query", "schema": {"type": "string"}, "description": "Package.uid to anchor package ownership or consumption correlation lookup."},
          {"name": "repository_id", "in": "query", "schema": {"type": "string"}, "description": "Repository.id to anchor package ownership or consumption correlation lookup."},
          {"name": "relationship_kind", "in": "query", "schema": {"type": "string", "enum": ["ownership", "consumption"]}, "description": "Optional relationship kind filter."},
          {"name": "after_correlation_id", "in": "query", "schema": {"type": "string"}, "description": "Correlation ID from next_cursor when continuing a truncated page."},
          {"name": "limit", "in": "query", "required": true, "schema": {"type": "integer", "minimum": 1, "maximum": 200}}
        ],
        "responses": {
          "200": {
            "description": "Package registry ownership and consumption correlations",
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
                          "ecosystem": {"type": "string"},
                          "package_name": {"type": "string"},
                          "repository_id": {"type": "string"},
                          "repository_name": {"type": "string"},
                          "source_url": {"type": "string"},
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
