// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package catalog

// Capabilities documents the capability maturity catalog route.
const Capabilities = `
    "/api/v0/capabilities": {
      "get": {
        "tags": ["capabilities"],
        "summary": "List the capability maturity catalog",
        "description": "Returns the reconciled capability catalog from the embedded, generated artifact: per-capability maturity, public surfaces, owner package, known gaps, linked issues, and per-capability authorization metadata. The read is static, bounded, and exact in every profile, and backs the MCP get_capability_catalog tool and the console capability matrix. Supports optional maturity and owner_package filters with deterministic limit/offset paging. The default (compact) response omits per-capability profiles/proof_signals and the top-level authorization catalog to keep a default page small for MCP clients (#6795); pass view=full and/or include_authorization=true to opt into that detail.",
        "operationId": "listCapabilities",
        "x-scoped-token-support": true,
        "parameters": [
          {"name": "maturity", "in": "query", "required": false, "schema": {"type": "string", "enum": ["general_availability", "experimental", "preview", "gated", "degraded", "not_implemented"]}, "description": "Optional maturity filter."},
          {"name": "owner", "in": "query", "required": false, "schema": {"type": "string"}, "description": "Optional owner_package filter (exact match)."},
          {"name": "limit", "in": "query", "required": false, "schema": {"type": "integer", "minimum": 1, "maximum": 500, "default": 12}, "description": "Maximum number of capabilities to return."},
          {"name": "offset", "in": "query", "required": false, "schema": {"type": "integer", "minimum": 0, "default": 0}, "description": "Number of capabilities to skip for paging."},
          {"name": "view", "in": "query", "required": false, "schema": {"type": "string", "enum": ["compact", "full"], "default": "compact"}, "description": "compact (default) omits per-entry profiles and proof_signals; full returns the complete entry shape."},
          {"name": "include_authorization", "in": "query", "required": false, "schema": {"type": "boolean", "default": false}, "description": "When true, includes the full top-level role/grant/data-class authorization catalog. Default false: the field is present but empty."}
        ],
        "responses": {
          "200": {
            "description": "Capability catalog page",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "version": {"type": "string", "description": "Catalog version emitted by capability.Handler.list."},
                    "authorization": {
                      "type": "object",
                      "description": "Built-in authorization catalog emitted by capability.Handler.list (capabilitycatalog.AuthorizationCatalog); present but empty unless include_authorization is true: roles, role grants, data classes, permission families, bootstrap-owner posture, and custom-policy posture.",
                      "properties": {
                        "version": {"type": "string", "description": "Authorization catalog version emitted by capability.Handler.list (capabilitycatalog.AuthorizationCatalog.Version)."},
                        "roles": {
                          "type": "array",
                          "description": "Product roles emitted by capability.Handler.list (capabilitycatalog.BuiltInRole).",
                          "items": {
                            "type": "object",
                            "description": "One product role. Owner: capability.Handler.list (capabilitycatalog.BuiltInRole).",
                            "properties": {
                              "role": {"type": "string", "description": "Role id emitted by capability.Handler.list (capabilitycatalog.BuiltInRole.Role)."},
                              "display_name": {"type": "string", "description": "Role display name emitted by capability.Handler.list (capabilitycatalog.BuiltInRole.DisplayName)."},
                              "description": {"type": "string", "description": "Role description emitted by capability.Handler.list (capabilitycatalog.BuiltInRole.Description)."},
                              "bootstrap_default": {"type": "boolean", "description": "Whether the role is a bootstrap default, emitted by capability.Handler.list (capabilitycatalog.BuiltInRole.BootstrapDefault)."},
                              "grants": {
                                "type": "array",
                                "description": "Role grants emitted by capability.Handler.list (capabilitycatalog.RoleGrant).",
                                "items": {
                                  "type": "object",
                                  "description": "One role grant. Owner: capability.Handler.list (capabilitycatalog.RoleGrant).",
                                  "properties": {
                                    "action": {"type": "string", "description": "Granted action emitted by capability.Handler.list (capabilitycatalog.RoleGrant.Action)."},
                                    "data_classes": {"type": "array", "items": {"type": "string"}, "description": "Grant data classes emitted by capability.Handler.list (capabilitycatalog.RoleGrant.DataClasses)."},
                                    "scope_levels": {"type": "array", "items": {"type": "string"}, "description": "Grant scope levels emitted by capability.Handler.list (capabilitycatalog.RoleGrant.ScopeLevels)."}
                                  }
                                }
                              }
                            }
                          }
                        },
                        "data_classes": {
                          "type": "array",
                          "description": "Data classes emitted by capability.Handler.list (capabilitycatalog.DataClass).",
                          "items": {
                            "type": "object",
                            "description": "One data class. Owner: capability.Handler.list (capabilitycatalog.DataClass).",
                            "properties": {
                              "data_class": {"type": "string", "description": "Data class id emitted by capability.Handler.list (capabilitycatalog.DataClass.DataClass)."},
                              "sensitivity": {"type": "string", "enum": ["restricted", "sensitive"], "description": "Data class sensitivity emitted by capability.Handler.list (capabilitycatalog.DataClass.Sensitivity)."},
                              "description": {"type": "string", "description": "Data class description emitted by capability.Handler.list (capabilitycatalog.DataClass.Description)."}
                            }
                          }
                        },
                        "permission_families": {
                          "type": "array",
                          "description": "Permission families emitted by capability.Handler.list (capabilitycatalog.PermissionFamily).",
                          "items": {
                            "type": "object",
                            "description": "One permission family. Owner: capability.Handler.list (capabilitycatalog.PermissionFamily).",
                            "properties": {
                              "family": {"type": "string", "description": "Family id emitted by capability.Handler.list (capabilitycatalog.PermissionFamily.Family)."},
                              "description": {"type": "string", "description": "Family description emitted by capability.Handler.list (capabilitycatalog.PermissionFamily.Description)."},
                              "planned": {"type": "boolean", "description": "Whether the family is planned, emitted by capability.Handler.list (capabilitycatalog.PermissionFamily.Planned)."},
                              "capability_prefixes": {"type": "array", "items": {"type": "string"}, "description": "Capability id prefixes emitted by capability.Handler.list (capabilitycatalog.PermissionFamily.CapabilityPrefixes)."},
                              "action": {"type": "string", "description": "Family action emitted by capability.Handler.list (capabilitycatalog.PermissionFamily.Action)."},
                              "data_classes": {"type": "array", "items": {"type": "string"}, "description": "Family data classes emitted by capability.Handler.list (capabilitycatalog.PermissionFamily.DataClasses)."},
                              "scope_levels": {"type": "array", "items": {"type": "string"}, "description": "Family scope levels emitted by capability.Handler.list (capabilitycatalog.PermissionFamily.ScopeLevels)."},
                              "default_roles": {"type": "array", "items": {"type": "string"}, "description": "Family default roles emitted by capability.Handler.list (capabilitycatalog.PermissionFamily.DefaultRoles)."}
                            }
                          }
                        },
                        "bootstrap_owner": {
                          "type": "object",
                          "description": "First-owner grant posture emitted by capability.Handler.list (capabilitycatalog.BootstrapOwnerPolicy).",
                          "properties": {
                            "role": {"type": "string", "description": "Bootstrap owner role emitted by capability.Handler.list (capabilitycatalog.BootstrapOwnerPolicy.Role)."},
                            "starts_with_admin": {"type": "boolean", "description": "Whether the owner starts with admin, emitted by capability.Handler.list (capabilitycatalog.BootstrapOwnerPolicy.StartsWithAdmin)."},
                            "starts_with_sensitive_data_grants": {"type": "boolean", "description": "Whether the owner starts with sensitive data grants, emitted by capability.Handler.list (capabilitycatalog.BootstrapOwnerPolicy.StartsWithSensitiveDataGrants)."},
                            "delegable_roles": {"type": "array", "items": {"type": "string"}, "description": "Delegable roles emitted by capability.Handler.list (capabilitycatalog.BootstrapOwnerPolicy.DelegableRoles)."}
                          }
                        },
                        "custom_policy": {
                          "type": "object",
                          "description": "Custom-policy posture emitted by capability.Handler.list (capabilitycatalog.CustomPolicyPosture); v1 defers custom policy logic.",
                          "properties": {
                            "status": {"type": "string", "description": "Custom policy status emitted by capability.Handler.list (capabilitycatalog.CustomPolicyPosture.Status)."},
                            "note": {"type": "string", "description": "Custom policy note emitted by capability.Handler.list (capabilitycatalog.CustomPolicyPosture.Note)."}
                          }
                        }
                      }
                    },
                    "total": {"type": "integer", "description": "Capabilities matching the filters, emitted by capability.Handler.list."},
                    "limit": {"type": "integer", "description": "Requested page size echoed by capability.Handler.list."},
                    "offset": {"type": "integer", "description": "Requested page offset echoed by capability.Handler.list."},
                    "truncated": {"type": "boolean", "description": "Whether more entries exist past this page, emitted by capability.Handler.list."},
                    "next_offset": {"type": "integer", "nullable": true, "description": "offset to fetch the next page, or null when truncated is false, emitted by capability.Handler.list."},
                    "capabilities": {
                      "type": "array",
                      "description": "Capability entries emitted by capability.Handler.list (capabilitycatalog.Entry in full view, CompactEntry in compact view).",
                      "items": {
                        "type": "object",
                        "description": "One capability entry. Owner: capability.Handler.list (capabilitycatalog.Entry).",
                        "properties": {
                          "capability": {"type": "string", "description": "Capability id emitted by capability.Handler.list (capabilitycatalog.Entry.Capability)."},
                          "answering_stores": {"type": "array", "items": {"type": "string"}, "description": "Store or stores that answer the capability's reads, emitted by capability.Handler.list (capabilitycatalog.Entry.AnsweringStores): graph, fact_records_reducer, fact_records_collector, read_table:<table>, content_search, or status_fixed. Full view only."},
                          "display_name": {"type": "string", "description": "Display name emitted by capability.Handler.list (capabilitycatalog.Entry.DisplayName)."},
                          "owner_package": {"type": "string", "description": "Owning package import path emitted by capability.Handler.list (capabilitycatalog.Entry.OwnerPackage)."},
                          "maturity": {"type": "string", "description": "Effective maturity emitted by capability.Handler.list (capabilitycatalog.Entry.Maturity)."},
                          "derived_maturity": {"type": "string", "description": "Matrix-derived maturity emitted by capability.Handler.list (capabilitycatalog.Entry.DerivedMaturity)."},
                          "maturity_reason": {"type": "string", "description": "Maturity override reason emitted by capability.Handler.list (capabilitycatalog.Entry.MaturityReason)."},
                          "profiles": {
                            "type": "object",
                            "description": "Per runtime-profile support, truth ceiling, required runtime, p95 latency budget, and max-scope claim, emitted by capability.Handler.list (capabilitycatalog.EntryProfile). Full view only.",
                            "additionalProperties": {
                              "type": "object",
                              "properties": {
                                "status": {"type": "string", "enum": ["supported", "unsupported", "experimental"], "description": "Profile support status emitted by capability.Handler.list (capabilitycatalog.EntryProfile.Status)."},
                                "max_truth_level": {"type": "string", "description": "Highest truth level allowed in the profile, emitted by capability.Handler.list (capabilitycatalog.EntryProfile.MaxTruthLevel)."},
                                "required_runtime": {"type": "string", "description": "Required runtime shape emitted by capability.Handler.list (capabilitycatalog.EntryProfile.RequiredRuntime)."},
                                "p95_latency_ms": {"type": "integer", "nullable": true, "description": "Declared p95 latency budget in milliseconds, emitted by capability.Handler.list (capabilitycatalog.EntryProfile.P95LatencyMS)."},
                                "max_scope_size": {"type": "string", "description": "Declared maximum supported scope emitted by capability.Handler.list (capabilitycatalog.EntryProfile.MaxScopeSize)."}
                              }
                            }
                          },
                          "surfaces": {
                            "type": "array",
                            "description": "Classified public surfaces emitted by capability.Handler.list (capabilitycatalog.Surface).",
                            "items": {
                              "type": "object",
                              "description": "One classified public surface. Owner: capability.Handler.list (capabilitycatalog.Surface).",
                              "properties": {
                                "tool": {"type": "string", "description": "Surface/tool name emitted by capability.Handler.list (capabilitycatalog.Surface.Tool)."},
                                "kind": {"type": "string", "description": "Surface kind emitted by capability.Handler.list (capabilitycatalog.Surface.Kind)."}
                              }
                            }
                          },
                          "proof_signals": {
                            "type": "array",
                            "description": "Deduplicated verification signals emitted by capability.Handler.list (capabilitycatalog.ProofSignal). Full view only.",
                            "items": {
                              "type": "object",
                              "description": "One verification signal. Owner: capability.Handler.list (capabilitycatalog.ProofSignal).",
                              "properties": {
                                "kind": {"type": "string", "description": "Proof kind emitted by capability.Handler.list (capabilitycatalog.ProofSignal.Kind)."},
                                "ref": {"type": "string", "description": "Proof reference emitted by capability.Handler.list (capabilitycatalog.ProofSignal.Ref)."}
                              }
                            }
                          },
                          "known_gaps": {"type": "array", "items": {"type": "string"}, "description": "Tracked gaps emitted by capability.Handler.list (capabilitycatalog.Entry.KnownGaps)."},
                          "linked_issues": {"type": "array", "items": {"type": "integer"}, "description": "Linked issue numbers emitted by capability.Handler.list (capabilitycatalog.Entry.LinkedIssues)."},
                          "docs": {"type": "array", "items": {"type": "string"}, "description": "Doc paths emitted by capability.Handler.list (capabilitycatalog.Entry.Docs)."},
                          "console": {"type": "boolean", "description": "Whether the capability is surfaced in the console matrix, emitted by capability.Handler.list (capabilitycatalog.Entry.Console)."},
                          "authorization": {
                            "type": "object",
                            "description": "Matched permission family and grant metadata for this capability, emitted by capability.Handler.list (capabilitycatalog.CapabilityAuthorization).",
                            "properties": {
                              "family": {"type": "string", "description": "Permission family emitted by capability.Handler.list (capabilitycatalog.CapabilityAuthorization.Family)."},
                              "action": {"type": "string", "description": "Granted action emitted by capability.Handler.list (capabilitycatalog.CapabilityAuthorization.Action)."},
                              "data_classes": {"type": "array", "items": {"type": "string"}, "description": "Grant data classes emitted by capability.Handler.list (capabilitycatalog.CapabilityAuthorization.DataClasses)."},
                              "scope_levels": {"type": "array", "items": {"type": "string"}, "description": "Grant scope levels emitted by capability.Handler.list (capabilitycatalog.CapabilityAuthorization.ScopeLevels)."},
                              "default_roles": {"type": "array", "items": {"type": "string"}, "description": "Default roles emitted by capability.Handler.list (capabilitycatalog.CapabilityAuthorization.DefaultRoles)."},
                              "sensitive_data": {"type": "boolean", "description": "Whether the grant touches sensitive data, emitted by capability.Handler.list (capabilitycatalog.CapabilityAuthorization.SensitiveData)."}
                            }
                          }
                        }
                      }
                    }
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
`
