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
`
