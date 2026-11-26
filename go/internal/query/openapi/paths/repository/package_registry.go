// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repository

// PackageRegistry is the OpenAPI path fragment documenting the
// `/api/v0/package-registry/*` routes. openapi.Spec concatenates it into the
// published document; keep it in lockstep with the handlers and
// docs/public/reference/http-api.md.
const PackageRegistry = `
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
          "403": {"$ref": "#/components/responses/Forbidden"},
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
                      "description": "Package identity rows, emitted by the registry handler from the package read model.",
                      "items": {
                        "type": "object",
                        "description": "One package identity row. Owner: registry handler package read model.",
                        "properties": {
                          "package_id": {"type": "string", "description": "Package identity emitted by the registry package row."},
                          "ecosystem": {"type": "string", "description": "Package ecosystem emitted by the registry package row."},
                          "registry": {"type": "string", "description": "Source registry emitted by the registry package row."},
                          "namespace": {"type": "string", "description": "Package namespace emitted by the registry package row."},
                          "normalized_name": {"type": "string", "description": "Normalized package name emitted by the registry package row."},
                          "purl": {"type": "string", "description": "Package URL emitted by the registry package row."},
                          "bom_ref": {"type": "string", "description": "BOM reference emitted by the registry package row."},
                          "package_manager": {"type": "string", "description": "Package manager emitted by the registry package row."},
                          "source_path": {"type": "string", "description": "Source path emitted by the registry package row (redacted for scoped ecosystem browse)."},
                          "source_specific_id": {"type": "string", "description": "Source-specific identity emitted by the registry package row."},
                          "visibility": {"type": "string", "description": "Package visibility emitted by the registry package row (forced public for scoped callers)."},
                          "source_confidence": {"type": "string", "description": "Source confidence emitted by the registry package row."},
                          "version_count": {"type": "integer", "description": "Version count emitted by the registry package row."}
                        },
                        "required": ["package_id", "version_count"]
                      }
                    },
                    "identity_issues": {
                      "type": "array",
                      "description": "Package-registry graph rows that could not be returned as valid package identities because required identity evidence was missing. Emitted by the registry handler from the identity-issue projection sharing the package identity fields.",
                      "items": {
                        "type": "object",
                        "description": "One identity-issue row. Owner: registry handler package read model.",
                        "properties": {
                          "reason": {"type": "string", "enum": ["package_id_missing"], "description": "Issue reason emitted by the registry identity-issue projection."},
                          "missing_evidence": {"type": "array", "items": {"type": "string", "enum": ["package_id"]}, "description": "Missing evidence classes emitted by the registry identity-issue projection."},
                          "ecosystem": {"type": "string", "description": "Package ecosystem emitted by the registry identity-issue projection."},
                          "registry": {"type": "string", "description": "Source registry emitted by the registry identity-issue projection."},
                          "namespace": {"type": "string", "description": "Package namespace emitted by the registry identity-issue projection."},
                          "normalized_name": {"type": "string", "description": "Normalized package name emitted by the registry identity-issue projection."},
                          "purl": {"type": "string", "description": "Package URL emitted by the registry identity-issue projection."},
                          "bom_ref": {"type": "string", "description": "BOM reference emitted by the registry identity-issue projection."},
                          "package_manager": {"type": "string", "description": "Package manager emitted by the registry identity-issue projection."},
                          "source_path": {"type": "string", "description": "Source path emitted by the registry identity-issue projection."},
                          "source_specific_id": {"type": "string", "description": "Source-specific identity emitted by the registry identity-issue projection."},
                          "visibility": {"type": "string", "description": "Package visibility emitted by the registry identity-issue projection."},
                          "source_confidence": {"type": "string", "description": "Source confidence emitted by the registry identity-issue projection."},
                          "version_count": {"type": "integer", "description": "Version count emitted by the registry identity-issue projection."}
                        },
                        "required": ["reason", "missing_evidence", "version_count"]
                      }
                    },
                    "count": {"type": "integer", "description": "Rows returned in this page, emitted by the registry handler as len(results)."},
                    "limit": {"type": "integer", "description": "Echo of the caller's page size, emitted by the registry handler from the normalized limit."},
                    "truncated": {"type": "boolean", "description": "True when more rows exist past this page, emitted by the registry handler from the lookahead row."},
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
          "403": {"$ref": "#/components/responses/Forbidden"},
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
                      "description": "Package-version identity rows for the anchored package, emitted by the registry handler from the version read model.",
                      "items": {
                        "type": "object",
                        "description": "One package-version row. Owner: registry handler version read model.",
                        "properties": {
                          "version_id": {"type": "string", "description": "Version identity emitted by the registry version row."},
                          "package_id": {"type": "string", "description": "Owning package identity emitted by the registry version row."},
                          "version": {"type": "string", "description": "Version string emitted by the registry version row."},
                          "purl": {"type": "string", "description": "Package URL emitted by the registry version row."},
                          "bom_ref": {"type": "string", "description": "BOM reference emitted by the registry version row."},
                          "package_manager": {"type": "string", "description": "Package manager emitted by the registry version row."},
                          "published_at": {"type": "string", "format": "date-time", "description": "Publication timestamp emitted by the registry version row."},
                          "is_yanked": {"type": "boolean", "description": "Yank flag emitted by the registry version row."},
                          "is_unlisted": {"type": "boolean", "description": "Unlisted flag emitted by the registry version row."},
                          "is_deprecated": {"type": "boolean", "description": "Deprecated flag emitted by the registry version row."},
                          "is_retracted": {"type": "boolean", "description": "Retracted flag emitted by the registry version row."}
                        },
                        "required": ["version_id", "package_id", "is_yanked", "is_unlisted", "is_deprecated", "is_retracted"]
                      }
                    },
                    "count": {"type": "integer", "description": "Rows returned in this page, emitted by the registry handler as len(results)."},
                    "limit": {"type": "integer", "description": "Echo of the caller's page size, emitted by the registry handler from the normalized limit."},
                    "truncated": {"type": "boolean", "description": "True when more rows exist past this page, emitted by the registry handler from the lookahead row."},
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
          "403": {"$ref": "#/components/responses/Forbidden"},
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
                      "description": "Package-native dependency edges for the anchored package, emitted by the registry handler from the dependency read model.",
                      "items": {
                        "type": "object",
                        "description": "One package dependency edge. Owner: registry handler dependency read model.",
                        "properties": {
                          "dependency_id": {"type": "string", "description": "Edge identity emitted by the registry dependency row."},
                          "source_package_id": {"type": "string", "description": "Declaring package identity emitted by the registry dependency row."},
                          "source_version_id": {"type": "string", "description": "Declaring version identity emitted by the registry dependency row."},
                          "version": {"type": "string", "description": "Declaring version string emitted by the registry dependency row."},
                          "dependency_package_id": {"type": "string", "description": "Dependency package identity emitted by the registry dependency row."},
                          "dependency_ecosystem": {"type": "string", "description": "Dependency ecosystem emitted by the registry dependency row."},
                          "dependency_registry": {"type": "string", "description": "Dependency registry emitted by the registry dependency row."},
                          "dependency_namespace": {"type": "string", "description": "Dependency namespace emitted by the registry dependency row."},
                          "dependency_normalized": {"type": "string", "description": "Normalized dependency name emitted by the registry dependency row."},
                          "dependency_purl": {"type": "string", "description": "Dependency package URL emitted by the registry dependency row."},
                          "dependency_bom_ref": {"type": "string", "description": "Dependency BOM reference emitted by the registry dependency row."},
                          "dependency_manager": {"type": "string", "description": "Dependency package manager emitted by the registry dependency row."},
                          "dependency_range": {"type": "string", "description": "Declared dependency range emitted by the registry dependency row."},
                          "dependency_type": {"type": "string", "description": "Dependency type emitted by the registry dependency row."},
                          "target_framework": {"type": "string", "description": "Target framework emitted by the registry dependency row."},
                          "marker": {"type": "string", "description": "Environment marker emitted by the registry dependency row."},
                          "optional": {"type": "boolean", "description": "Whether the edge is optional, emitted by the registry dependency row."},
                          "excluded": {"type": "boolean", "description": "Whether the edge is excluded, emitted by the registry dependency row."},
                          "source_confidence": {"type": "string", "description": "Source confidence emitted by the registry dependency row."},
                          "collector_kind": {"type": "string", "description": "Collector kind emitted by the registry dependency row."},
                          "collector_instance_id": {"type": "string", "description": "Collector instance emitted by the registry dependency row."},
                          "correlation_anchors": {"type": "array", "items": {"type": "string"}, "description": "Correlation anchors emitted by the registry dependency row."}
                        },
                        "required": ["dependency_id", "source_package_id", "source_version_id", "dependency_package_id", "optional", "excluded"]
                      }
                    },
                    "count": {"type": "integer", "description": "Rows returned in this page, emitted by the registry handler as len(results)."},
                    "limit": {"type": "integer", "description": "Echo of the caller's page size, emitted by the registry handler from the normalized limit."},
                    "truncated": {"type": "boolean", "description": "True when more rows exist past this page, emitted by the registry handler from the lookahead row."},
                    "next_cursor": {
                      "type": "object",
                      "description": "Keyset cursor for the next page (present only when truncated), emitted by the registry handler from the last row.",
                      "properties": {
                        "after_version_id": {"type": "string", "description": "Source version id of the last row, emitted by the registry handler from the last dependency row."},
                        "after_dependency_id": {"type": "string", "description": "Edge id of the last row, emitted by the registry handler from the last dependency row."}
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
          "403": {"$ref": "#/components/responses/Forbidden"},
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
                      "description": "Reducer-owned ownership, publication, and consumption correlations, emitted by the registry handler as []registry.CorrelationResult (provenance-only).",
                      "items": {
                        "type": "object",
                        "description": "One package registry correlation. Owner: registry handler (registry.CorrelationResult).",
                        "properties": {
                          "correlation_id": {"type": "string", "description": "Correlation identity emitted by registry.CorrelationResult.CorrelationID."},
                          "relationship_kind": {"type": "string", "description": "Relationship kind (ownership, publication, consumption) emitted by registry.CorrelationResult.RelationshipKind."},
                          "package_id": {"type": "string", "description": "Package identity emitted by registry.CorrelationResult.PackageID."},
                          "version_id": {"type": "string", "description": "Version identity emitted by registry.CorrelationResult.VersionID."},
                          "version": {"type": "string", "description": "Version string emitted by registry.CorrelationResult.Version."},
                          "published_at": {"type": "string", "format": "date-time", "description": "Publication timestamp emitted by registry.CorrelationResult.PublishedAt."},
                          "ecosystem": {"type": "string", "description": "Package ecosystem emitted by registry.CorrelationResult.Ecosystem."},
                          "package_name": {"type": "string", "description": "Package name emitted by registry.CorrelationResult.PackageName."},
                          "repository_id": {"type": "string", "description": "Repository identity emitted by registry.CorrelationResult.RepositoryID."},
                          "repository_name": {"type": "string", "description": "Repository name emitted by registry.CorrelationResult.RepositoryName."},
                          "source_url": {"type": "string", "description": "Source URL emitted by registry.CorrelationResult.SourceURL."},
                          "candidate_repository_ids": {"type": "array", "items": {"type": "string"}, "description": "Candidate repository identities emitted by registry.CorrelationResult.CandidateRepositoryIDs."},
                          "relative_path": {"type": "string", "description": "Manifest-relative path emitted by registry.CorrelationResult.RelativePath."},
                          "manifest_section": {"type": "string", "description": "Manifest section emitted by registry.CorrelationResult.ManifestSection."},
                          "dependency_range": {"type": "string", "description": "Declared dependency range emitted by registry.CorrelationResult.DependencyRange."},
                          "outcome": {"type": "string", "description": "Correlation outcome emitted by registry.CorrelationResult.Outcome."},
                          "reason": {"type": "string", "description": "Machine-readable reason emitted by registry.CorrelationResult.Reason."},
                          "provenance_only": {"type": "boolean", "description": "Whether the row is provenance-only, emitted by registry.CorrelationResult.ProvenanceOnly."},
                          "canonical_writes": {"type": "integer", "description": "Canonical write count emitted by registry.CorrelationResult.CanonicalWrites."},
                          "evidence_fact_ids": {"type": "array", "items": {"type": "string"}, "description": "Backing evidence fact ids emitted by registry.CorrelationResult.EvidenceFactIDs."}
                        },
                        "required": ["correlation_id", "relationship_kind", "package_id", "outcome", "provenance_only", "canonical_writes"]
                      }
                    },
                    "count": {"type": "integer", "description": "Rows returned in this page, emitted by the registry handler as len(results)."},
                    "limit": {"type": "integer", "description": "Echo of the caller's page size, emitted by the registry handler from the normalized limit."},
                    "truncated": {"type": "boolean", "description": "True when more rows exist past this page, emitted by the registry handler from the lookahead row."},
                    "next_cursor": {
                      "type": "object",
                      "description": "Continuation cursor for the next page (present only when truncated), emitted by the registry handler from the last row.",
                      "properties": {
                        "after_correlation_id": {"type": "string", "description": "Correlation id of the last row, emitted by the registry handler from the last CorrelationResult.CorrelationID."}
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
          "403": {"$ref": "#/components/responses/Forbidden"},
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
                      "description": "Consumer-repo to package to publisher-repo chains resolved on the read side, emitted by the registry handler as []registry.PackageDependencyChainResult.",
                      "items": {
                        "type": "object",
                        "description": "One package dependency chain. Owner: registry handler (registry.PackageDependencyChainResult).",
                        "properties": {
                          "consumer_repository_id": {"type": "string", "description": "Consumer repository identity emitted by registry.PackageDependencyChainResult.ConsumerRepositoryID."},
                          "consumer_repository_name": {"type": "string", "description": "Consumer repository name emitted by registry.PackageDependencyChainResult.ConsumerRepositoryName."},
                          "package_id": {"type": "string", "description": "Package identity emitted by registry.PackageDependencyChainResult.PackageID."},
                          "package_name": {"type": "string", "description": "Package name emitted by registry.PackageDependencyChainResult.PackageName."},
                          "ecosystem": {"type": "string", "description": "Package ecosystem emitted by registry.PackageDependencyChainResult.Ecosystem."},
                          "dependency_range": {"type": "string", "description": "Declared dependency range emitted by registry.PackageDependencyChainResult.DependencyRange."},
                          "consumption_correlation_id": {"type": "string", "description": "Consumption correlation identity emitted by registry.PackageDependencyChainResult.ConsumptionCorrelationID."},
                          "consumption_provenance_only": {"type": "boolean", "description": "Whether the consumption leg is provenance-only, emitted by registry.PackageDependencyChainResult.ConsumptionProvenanceOnly."},
                          "consumption_canonical_writes": {"type": "integer", "description": "Consumption canonical write count emitted by registry.PackageDependencyChainResult.ConsumptionCanonicalWrite."},
                          "ambiguous": {"type": "boolean", "description": "Whether multiple publishers were inferred, emitted by registry.PackageDependencyChainResult.Ambiguous."},
                          "publishers": {
                            "type": "array",
                            "description": "Inferred provenance-only publisher legs, emitted by registry.PackageDependencyChainResult.Publishers as []registry.PackageDependencyChainPublisherResult.",
                            "items": {
                              "type": "object",
                              "description": "One inferred publisher leg. Owner: registry handler (registry.PackageDependencyChainResult.Publishers).",
                              "properties": {
                                "correlation_id": {"type": "string", "description": "Correlation identity emitted by registry.PackageDependencyChainPublisherResult.CorrelationID."},
                                "relationship_kind": {"type": "string", "description": "Relationship kind emitted by registry.PackageDependencyChainPublisherResult.RelationshipKind."},
                                "repository_id": {"type": "string", "description": "Publisher repository identity emitted by registry.PackageDependencyChainPublisherResult.RepositoryID."},
                                "repository_name": {"type": "string", "description": "Publisher repository name emitted by registry.PackageDependencyChainPublisherResult.RepositoryName."},
                                "source_url": {"type": "string", "description": "Publisher source URL emitted by registry.PackageDependencyChainPublisherResult.SourceURL."},
                                "outcome": {"type": "string", "description": "Publisher outcome emitted by registry.PackageDependencyChainPublisherResult.Outcome."},
                                "reason": {"type": "string", "description": "Machine-readable reason emitted by registry.PackageDependencyChainPublisherResult.Reason."},
                                "provenance_only": {"type": "boolean", "description": "Whether the publisher leg is provenance-only (always true), emitted by registry.PackageDependencyChainPublisherResult.ProvenanceOnly."},
                                "canonical_writes": {"type": "integer", "description": "Publisher canonical write count emitted by registry.PackageDependencyChainPublisherResult.CanonicalWrites."}
                              },
                              "required": ["correlation_id", "relationship_kind", "repository_id", "provenance_only", "canonical_writes"]
                            }
                          }
                        },
                        "required": ["consumer_repository_id", "package_id", "consumption_correlation_id", "consumption_provenance_only", "consumption_canonical_writes", "ambiguous", "publishers"]
                      }
                    },
                    "repository_id": {"type": "string", "description": "Echo of the caller's consumer repository selector, emitted by the registry handler from the normalized selector."},
                    "count": {"type": "integer", "description": "Chains returned in this page, emitted by the registry handler as len(results)."},
                    "limit": {"type": "integer", "description": "Echo of the caller's page size, emitted by the registry handler from the normalized limit."},
                    "truncated": {"type": "boolean", "description": "True when more consumption correlations exist beyond this page for the requested repository. Emitted by the registry handler from the phase-1 consumption page."},
                    "publishers_truncated": {"type": "boolean", "description": "True when the batched phase-2 publication/ownership read hit its own bounded limit for the distinct package set this page consumes, so some publisher legs are missing from one or more chains. Independent of truncated, which describes only the phase-1 consumption page. Emitted by the registry handler from the phase-2 read."},
                    "next_cursor": {
                      "type": "object",
                      "description": "Continuation cursor for the next page (present only when truncated), emitted by the registry handler from the phase-1 page.",
                      "properties": {
                        "after_correlation_id": {"type": "string", "description": "Consumption correlation id of the last chain, emitted by the registry handler from the phase-1 page cursor."}
                      },
                      "required": ["after_correlation_id"]
                    }
                  },
                  "required": ["chains", "count", "limit", "truncated", "publishers_truncated"]
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
