// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cloud

// Inventory documents the canonical multi-cloud resource
// inventory readback route. It surfaces reducer-owned
// reducer_cloud_resource_identity rows (one per cloud_resource_uid) with
// provider, normalized identity, management_origin, evidence-layer flags,
// provider-neutral source state, optional keyed tag fingerprints, and optional
// bounded identity-policy and resource-change freshness evidence. The
// route is read-only, bounded, paginated, and truth-labeled; it never returns
// raw provider locators, raw actors, raw identities, tag values, assignment
// scopes, or credentials.
const Inventory = `
    "/api/v0/cloud/inventory": {
      "get": {
        "summary": "List canonical multi-cloud resource identities (bounded, filterable, paginated, truth-labeled)",
        "operationId": "listCloudResourceInventory",
        "x-scoped-token-support": true,
        "description": "Reads the reducer-owned canonical CloudResource identity rows (reducer_cloud_resource_identity). Filterable by provider (aws/gcp/azure), canonical scope or provider account, and management_origin. local_lightweight returns unsupported_capability. Scoped tokens receive rows intersected with the caller's granted repositories/ingestion scopes (fact_records.scope_id); a scoped caller with no grants receives an empty page without a query. account_id/project_id/subscription_id are provider-SPECIFIC aliases (aws/gcp/azure respectively) that all resolve against one shared canonical payload key with no per-provider disambiguation in the predicate itself, so each REQUIRES its matching provider: account_id requires provider=aws, project_id requires provider=gcp, subscription_id requires provider=azure. Omitting provider, or supplying a mismatched provider (e.g. provider=gcp&account_id=...), is rejected as invalid_argument rather than risking a cross-provider numeric collision (e.g. an AWS account id and a GCP project number can be the identical decimal string). A 503 with Retry-After means the canonical identity read hit a stale or timed-out PostgreSQL reader and is safe to retry; it names capability cloud_inventory.readback.list in its error envelope. Any other read failure answers 500 internal_error with a fixed message. No 5xx body carries backend error text.",
        "parameters": [
          {"name": "provider", "in": "query", "description": "Filter by cloud provider: aws, gcp, or azure. REQUIRED alongside account_id/project_id/subscription_id, and must match the alias used (account_id->aws, project_id->gcp, subscription_id->azure).", "schema": {"type": "string", "enum": ["aws", "gcp", "azure"]}},
          {"name": "scope_id", "in": "query", "description": "Filter by the exact canonical ingestion scope id. A canonical scope is a single collector partition (for AWS, one account+region+service claim), not the whole provider account -- one account can span many scope ids. Takes precedence over account_id/project_id/subscription_id when both are given.", "schema": {"type": "string"}},
          {"name": "account_id", "in": "query", "description": "Filter by the raw AWS account number. Requires provider=aws exactly (rejected with any other provider value). Unlike scope_id, this matches every canonical resource whose admitting aws_resource source fact carried this account_id (potentially spanning many scope ids, one per region/service partition).", "schema": {"type": "string"}},
          {"name": "project_id", "in": "query", "description": "Filter by the raw GCP project id. Requires provider=gcp exactly (rejected with any other provider value). Matches every canonical resource whose admitting gcp_cloud_resource source fact carried this project_id.", "schema": {"type": "string"}},
          {"name": "subscription_id", "in": "query", "description": "Filter by the raw Azure subscription id. Requires provider=azure exactly (rejected with any other provider value). Matches every canonical resource whose admitting azure_cloud_resource source fact carried this subscription_id.", "schema": {"type": "string"}},
          {"name": "management_origin", "in": "query", "description": "Filter by strongest contributing evidence layer: declared, applied, or observed.", "schema": {"type": "string", "enum": ["declared", "applied", "observed"]}},
          {"name": "limit", "in": "query", "description": "Page size.", "schema": {"type": "integer", "minimum": 1, "maximum": 200, "default": 50}},
          {"name": "cursor", "in": "query", "description": "Continuation cursor: non-negative integer offset returned in next_cursor of the previous page.", "schema": {"type": "string"}}
        ],
        "responses": {
          "200": {
            "description": "Bounded canonical cloud inventory list envelope ordered by cloud_resource_uid",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "resources": {
                      "type": "array",
                      "description": "Page of canonical cloud resource identity views, emitted by query.cloudInventoryResourceView.",
                      "items": {
                        "type": "object",
                        "description": "One canonical cloud resource identity view. Owner: query.cloudInventoryResourceView.",
                        "properties": {
                          "cloud_resource_uid": {"type": "string", "description": "Canonical cross-provider resource identity, emitted by query.cloudInventoryResourceView from the reducer payload."},
                          "provider": {"type": "string", "description": "Cloud provider of the canonical identity, emitted by query.cloudInventoryResourceView from the reducer payload."},
                          "resource_type": {"type": "string", "description": "Canonical resource type, emitted by query.cloudInventoryResourceView from the reducer payload."},
                          "management_origin": {"type": "string", "enum": ["declared", "applied", "observed"], "description": "Strongest contributing evidence layer, emitted by query.cloudInventoryResourceView from the reducer payload."},
                          "scope_id": {"type": "string", "description": "Canonical ingestion scope of the identity, emitted by query.cloudInventoryScopeID preferring the fact-record column."},
                          "generation_id": {"type": "string", "description": "Reducer generation that admitted the identity, emitted by query.cloudInventoryResourceView from the envelope."},
                          "source_state": {"type": "string", "description": "Provider-neutral source-state taxonomy value derived from management_origin."},
                          "tag_value_fingerprints": {
                            "type": "object",
                            "description": "Optional keyed, non-reversible tag value fingerprints attached by reducer evidence; raw tag values are never returned.",
                            "additionalProperties": {"type": "string"}
                          },
                          "attributes": {
                            "type": "object",
                            "description": "Optional bounded provider-specific attributes. GCP surfaces its typed-depth payload (e.g. table_type, schema_field_count, kms_key_name, clustering_fields) as a bounded redaction-safe passthrough. AWS surfaces a CLOSED image/version allowlist only: task_definition_arn, image_uri, resolved_image_uri, code_sha256, package_type (the bounded Lambda packaging discriminator Zip/Image), version, EKS platform_version/release_version/addon_version, EC2 ami_id, Lambda runtime, and a containers array reduced per element to {image, image_digest}. For resource_type aws_ec2_ami only, the route additionally surfaces the AMI name: a JSON string of 3-128 runes inside the exact AWS CreateImage charset, admitted only after structural validation at the loader (printable, no controls, hygiene scan as drop-trigger; surrounding whitespace trimmed before validation; nonconforming values are dropped, never truncated). The AMI name is untrusted creator-controlled text and is NOT proven secret-free: a patternless bare token fits the charset and will pass. The exact test sentinel 'must-not-surface' is additionally dropped as a name value even though it fits the charset. AMI description is never surfaced for any resource. Azure's allowlist is wired but currently empty (its resource fact carries no image/version key yet). The surfaced image/version references (image_uri, resolved_image_uri, containers[].image, task_definition_arn) are intentional deployed-code evidence and name the image/registry/repository for the account-scoped caller's own resources; no credential, secret, or non-image infrastructure locator (cluster_arn, role_arn, kms_key_arn, arm_resource_id, network_interfaces, container name/runtime_id, ...) is ever present.",
                            "additionalProperties": true
                          },
                          "code_sha256_correlation": {
                            "type": "object",
                            "description": "Present only for a zip-packaged Lambda function (attributes.package_type=Zip) that carries a code_sha256. It states, programmatically, that the code hash is NOT correlated to any CI/package hash: a Lambda code_sha256 is base64(SHA256(the deployment .zip)) and no collected hash covers those bytes (the GitHub Actions artifact_digest hashes GitHub's own re-zip, package-registry hashes are of tarballs/wheels, OCI digests are image manifests), so the code_sha256 is display-only evidence and the limitation is surfaced rather than left silent (issue #5454). Absent for an image-packaged Lambda, whose deployment code is the container image correlated via image_uri/resolved_image_uri.",
                            "properties": {
                              "status": {"type": "string", "enum": ["uncorrelated"], "description": "Correlation verdict emitted by query.cloudInventoryCodeCorrelationLabel (always uncorrelated for a zip Lambda code_sha256)."},
                              "truth_basis": {"type": "string", "enum": ["display_only_evidence"], "description": "Evidence standing of the code hash, emitted by query.cloudInventoryCodeCorrelationLabel (display-only, never joinable)."},
                              "unsupported_reason": {"type": "string", "enum": ["zip_code_sha256_no_ci_counterpart"], "description": "Machine-readable reason no counterpart exists, emitted by query.cloudInventoryCodeCorrelationLabel."}
                            }
                          },
                          "identity_policy_evidence": {
                            "type": "array",
                            "description": "Optional bounded identity-policy evidence rows containing only safe enum/text classes and keyed fingerprints; raw identities, assignment scopes, and principal GUIDs are never returned.",
                            "items": {
                              "type": "object",
                            "description": "One identity-policy evidence row. Owner: query.cloudInventoryIdentityPolicyEvidence.",
                              "properties": {
                                "evidence_key": {"type": "string", "description": "Evidence row key emitted by query.cloudInventoryIdentityPolicyEvidence (closed safe-field projection)."},
                                "identity_type": {"type": "string", "description": "Bounded identity class emitted by query.cloudInventoryIdentityPolicyEvidence; raw identities never cross the wire."},
                                "role_class": {"type": "string", "description": "Bounded role class emitted by query.cloudInventoryIdentityPolicyEvidence."},
                                "principal_fingerprint": {"type": "string", "description": "Keyed principal fingerprint emitted by query.cloudInventoryIdentityPolicyEvidence; raw principal GUIDs are omitted."},
                                "client_fingerprint": {"type": "string", "description": "Keyed client fingerprint emitted by query.cloudInventoryIdentityPolicyEvidence."},
                                "object_fingerprint": {"type": "string", "description": "Keyed object fingerprint emitted by query.cloudInventoryIdentityPolicyEvidence."},
                                "tenant_fingerprint": {"type": "string", "description": "Keyed tenant fingerprint emitted by query.cloudInventoryIdentityPolicyEvidence."}
                              }
                            }
                          },
                          "identity_policy_evidence_truncated": {"type": "boolean", "description": "Present and true when reducer evidence was capped for this resource."},
                          "resource_change_freshness": {
                            "type": "array",
                            "description": "Optional sanitized Azure Resource Graph change evidence attached to an already-admitted canonical resource. Delete rows are tombstone candidates only.",
                            "items": {
                              "type": "object",
                            "description": "One change evidence row. Owner: query.cloudInventoryResourceChangeFreshness.",
                              "properties": {
                                "evidence_key": {"type": "string", "description": "Change row key emitted by query.cloudInventoryResourceChangeFreshness (reducer allowlist)."},
                                "change_type": {"type": "string", "enum": ["created", "updated", "deleted"], "description": "Azure Resource Graph change type emitted by query.cloudInventoryResourceChangeFreshness."},
                                "change_time": {"type": "string", "format": "date-time", "description": "Provider change timestamp emitted by query.cloudInventoryResourceChangeFreshness."},
                                "operation": {"type": "string", "description": "Provider operation name emitted by query.cloudInventoryResourceChangeFreshness."},
                                "client_type": {"type": "string", "description": "Provider client class emitted by query.cloudInventoryResourceChangeFreshness."},
                                "actor_class": {"type": "string", "description": "Bounded actor class emitted by query.cloudInventoryResourceChangeFreshness; raw actor ids are dropped."},
                                "actor_fingerprint": {"type": "string", "description": "Keyed actor fingerprint emitted by query.cloudInventoryResourceChangeFreshness."},
                                "changed_property_paths": {"type": "array", "items": {"type": "string"}, "description": "Changed property paths emitted by query.cloudInventoryResourceChangeFreshness; before/after values are dropped."},
                                "changed_property_truncated": {"type": "boolean", "description": "True when changed_property_paths was capped, emitted by query.cloudInventoryResourceChangeFreshness."},
                                "tombstone_candidate": {"type": "boolean", "description": "Whether this delete row is a tombstone candidate only, emitted by query.cloudInventoryResourceChangeFreshness."}
                              }
                            }
                          },
                          "resource_change_freshness_truncated": {"type": "boolean", "description": "Present and true when freshness evidence was capped for this resource, emitted by query.cloudInventoryResourceView."},
                          "evidence": {
                            "type": "object",
                            "description": "Per-layer evidence flags that contributed to the canonical identity.",
                            "properties": {
                              "declared": {"type": "boolean", "description": "Whether declared-layer evidence contributed, emitted by query.cloudInventoryResourceView from has_declared_evidence."},
                              "applied": {"type": "boolean", "description": "Whether applied-layer evidence contributed, emitted by query.cloudInventoryResourceView from has_applied_evidence."},
                              "observed": {"type": "boolean", "description": "Whether observed-layer evidence contributed, emitted by query.cloudInventoryResourceView from has_observed_evidence."}
                            }
                          }
                        }
                      }
                    },
                    "count": {"type": "integer", "description": "Number of identities in this page, emitted by query.cloudInventoryResponse as len(resources)."},
                    "limit": {"type": "integer", "description": "Echo of the caller's page size, emitted by query.cloudInventoryResponse from cloudInventoryFilter.Limit."},
                    "truncated": {"type": "boolean", "description": "True when more identities exist past this page, emitted by query.cloudInventoryResponse (nextCursor non-empty)."},
                    "next_cursor": {"type": "string", "description": "Present only when truncated is true. Pass back as cursor to fetch the next page. Opaque offset cursor emitted by query.cloudInventoryResponse from the read model."},
                    "scope": {"type": "object", "additionalProperties": {"type": "string"}, "description": "Bounded non-sensitive filter scope applied to the readback, emitted by query.cloudInventoryScope: provider, scope_id (or the account alias key the caller used: account_id, project_id, subscription_id), and management_origin. Keys vary per request (filter echo); values are the caller-supplied filter strings."},
                    "warning_flags": {
                      "type": "array",
                      "items": {"type": "string", "enum": ["account_alias_rollout_gap", "account_alias_rollout_gap_check_failed"]},
                      "description": "Present only for an account_id/project_id/subscription_id-filtered request that returned zero resources. account_alias_rollout_gap means a canonical row exists in the same provider/access scope whose payload predates the #5238 account_id rollout (admitted before the reducer started writing account_id; it will resolve once that scope's next collector sync re-admits it) -- so this zero-row response does NOT prove the account does not exist. account_alias_rollout_gap_check_failed means that disambiguation check itself could not run; absent means the check ran and found no such gap, so the zero rows are a genuine no-such-account/no-such-scope result. Never present for scope_id-filtered or unfiltered requests, or for any request that returned at least one resource, since the second query behind this signal only fires for the zero-result account-alias case."
                    }
                  }
                }
              }
            }
          },
          "400": {"description": "Invalid provider, management_origin, limit, or cursor; or account_id/project_id/subscription_id supplied without provider, or with a mismatched provider (account_id requires provider=aws, project_id requires provider=gcp, subscription_id requires provider=azure)"},
          "403": {"$ref": "#/components/responses/Forbidden"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "501": {"description": "Capability unsupported by the active query profile, or canonical identity read model unavailable"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
`
