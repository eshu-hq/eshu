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
`
