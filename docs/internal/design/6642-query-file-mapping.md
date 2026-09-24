# Query target tree: the file mapping (#6642)

**This plan is four pages.** [The tree](6642-query-target-tree.md) ·
[What the moves cost](6642-query-move-cost.md) ·
[The file mapping](6642-query-file-mapping.md) ·
[Sequencing and open questions](6642-query-move-sequence.md)

## The mapping

All 277 root non-test files, grouped by destination. The test column names the
root test file whose stem prefix-matches, and counts the others that do.

Coverage adds up exactly: **250 rows below + 5 in [What stays at
root](6642-query-move-cost.md#what-stays-at-root) + 21 in [The alias ledger](6642-query-move-cost.md#the-alias-ledger) +
`semantic_evidence.go`, which is split rather than moved, = 277.** No file
appears twice and none is unaccounted for.

#### `query/ask/` — 6 non-test, 4 test files

| current | new | test files carried |
| --- | --- | --- |
| `answer_packet.go` | `ask/answer_packet.go` | `answer_packet_test.go` |
| `answer_packet_metadata.go` | `ask/answer_packet_metadata.go` | — |
| `answer_packet_routes.go` | `ask/answer_packet_routes.go` | — |
| `ask_guardrails.go` | `ask/guardrails.go` | — |
| `ask_handler.go` | `ask/handler.go` | `ask_handler_test.go` |
| `ask_sse.go` | `ask/sse.go` | `ask_sse_stream_test.go` +1 more |

#### `query/auth/` — 8 non-test, 39 test files

| current | new | test files carried |
| --- | --- | --- |
| `auth.go` | `auth/handler.go` | `auth_all_scope_bearer_two_tenant_test.go` +34 more |
| `auth_allowed_read_audit_wiring.go` | `auth/allowed_read_audit_wiring.go` | — |
| `auth_audit.go` | `auth/audit.go` | — |
| `auth_constructors.go` | `auth/constructors.go` | — |
| `auth_oauth_challenge_context.go` | `auth/oauth_challenge_context.go` | — |
| `auth_oauth_discovery.go` | `auth/oauth_discovery.go` | `auth_oauth_discovery_serve_test.go` +1 more |
| `auth_posture.go` | `auth/posture.go` | `auth_posture_test.go` |
| `auth_providers_handler.go` | `auth/providers_handler.go` | `auth_providers_handler_test.go` |

#### `query/auth/acl/` — 2 non-test, 2 test files

| current | new | test files carried |
| --- | --- | --- |
| `source_acl_disclosure.go` | `auth/acl/disclosure.go` | `source_acl_disclosure_test.go` |
| `source_acl_state.go` | `auth/acl/state.go` | `source_acl_state_test.go` |

#### `query/auth/route/` — 24 non-test, 26 test files

| current | new | test files carried |
| --- | --- | --- |
| `auth_browser_session_route_policy.go` | `auth/route/browser_session_policy.go` | — |
| `auth_public_routes.go` | `auth/route/public.go` | `auth_public_routes_test.go` |
| `auth_scoped_routes.go` | `auth/route/scoped.go` | `auth_scoped_routes_admin_completeness_test.go` +15 more |
| `auth_scoped_routes_auth_admin.go` | `auth/route/scoped_admin.go` | — |
| `auth_scoped_routes_capabilities.go` | `auth/route/scoped_capabilities.go` | — |
| `auth_scoped_routes_cloud.go` | `auth/route/scoped_cloud.go` | — |
| `auth_scoped_routes_code_flow.go` | `auth/route/scoped_code_flow.go` | — |
| `auth_scoped_routes_collector_extraction_readiness.go` | `auth/route/scoped_collector_extraction_readiness.go` | `auth_scoped_routes_collector_extraction_readiness_test.go` |
| `auth_scoped_routes_completeness.go` | `auth/route/scoped_completeness.go` | `auth_scoped_routes_completeness_helpers_test.go` +1 more |
| `auth_scoped_routes_context.go` | `auth/route/scoped_context.go` | — |
| `auth_scoped_routes_dead_letters.go` | `auth/route/scoped_dead_letters.go` | — |
| `auth_scoped_routes_documentation.go` | `auth/route/scoped_documentation.go` | — |
| `auth_scoped_routes_iac.go` | `auth/route/scoped_iac.go` | — |
| `auth_scoped_routes_impact.go` | `auth/route/scoped_impact.go` | `auth_scoped_routes_impact_change_surface_test.go` +4 more |
| `auth_scoped_routes_input_invalid_facts.go` | `auth/route/scoped_input_invalid_facts.go` | — |
| `auth_scoped_routes_mcp_export.go` | `auth/route/scoped_mcp_export.go` | `auth_scoped_routes_mcp_export_test.go` |
| `auth_scoped_routes_mcp_transport.go` | `auth/route/scoped_mcp_transport.go` | — |
| `auth_scoped_routes_pending_row_filtering.go` | `auth/route/scoped_pending_row_filtering.go` | — |
| `auth_scoped_routes_repository.go` | `auth/route/scoped_repository.go` | — |
| `auth_scoped_routes_secrets_iam.go` | `auth/route/scoped_secrets_iam.go` | — |
| `auth_scoped_routes_shared_key_only.go` | `auth/route/scoped_shared_key_only.go` | — |
| `auth_scoped_routes_status.go` | `auth/route/scoped_status.go` | — |
| `auth_scoped_routes_status_semantic.go` | `auth/route/scoped_status_semantic.go` | — |
| `auth_scoped_routes_supply_chain_infra.go` | `auth/route/scoped_supply_chain_infra.go` | — |

#### `query/auth/session/` — 4 non-test, 5 test files

| current | new | test files carried |
| --- | --- | --- |
| `browser_session_cookie_secure.go` | `auth/session/cookie.go` | `browser_session_cookie_secure_test.go` |
| `browser_session_handler.go` | `auth/session/handler.go` | `browser_session_handler_test.go` +1 more |
| `browser_session_list.go` | `auth/session/list.go` | `browser_session_list_handler_test.go` |
| `session_timeout_policy.go` | `auth/session/timeout_policy.go` | `session_timeout_policy_test.go` |

#### `query/auth/setup/` — 5 non-test, 2 test files

| current | new | test files carried |
| --- | --- | --- |
| `setup_handler.go` | `auth/setup/handler.go` | `setup_handler_test.go` |
| `setup_handler_helpers.go` | `auth/setup/helpers.go` | — |
| `setup_mfa_handler.go` | `auth/setup/mfa_handler.go` | `setup_mfa_handler_test.go` |
| `setup_requests.go` | `auth/setup/requests.go` | — |
| `setup_types.go` | `auth/setup/types.go` | — |

#### `query/auth/signin/` — 9 non-test, 9 test files

| current | new | test files carried |
| --- | --- | --- |
| `github_login_handler.go` | `auth/signin/github.go` | `github_login_handler_test.go` |
| `oidc_login_handler.go` | `auth/signin/oidc.go` | — |
| `oidc_rate_limiter.go` | `auth/signin/oidc_rate_limiter.go` | `oidc_rate_limiter_test.go` |
| `saml_handler.go` | `auth/signin/saml.go` | `saml_handler_audit_test.go` +3 more |
| `saml_verifier.go` | `auth/signin/saml_verifier.go` | `saml_verifier_test.go` |
| `sign_in_policy_mutations.go` | `auth/signin/policy_mutations.go` | `sign_in_policy_mutations_test.go` |
| `sign_in_policy_reads.go` | `auth/signin/policy_reads.go` | `sign_in_policy_reads_test.go` |
| `sign_in_policy_types.go` | `auth/signin/policy_types.go` | — |
| `sso_login_audit.go` | `auth/signin/audit.go` | — |

#### `query/capability/` — 2 non-test files

| current | new | test files carried |
| --- | --- | --- |
| `capabilities.go` | `capability/handler.go` | `capabilities_test.go` |
| `capability_keys.go` | **stays at root** | — |
| `capability_registry.go` | `capability/lookup.go` | — |
| `permission_catalog.go` | **deleted** | — |

`permission_catalog.go` does not move; it **dissolves**. Every symbol in it
was a forwarder onto `queryauth` or `querycontract`, so once root's call
sites name those directly the file is a header and a package clause. An
extraction is a good way to find these: a forwarder is invisible while it
shares a package with its callers, because it is used. Move the callers
across the boundary and its usage drops to zero, where `unused` sees it.

`capabilities_test.go` stays at root too — it drives the handler through
root's `APIRouter`, so it cannot follow without an import cycle.

`capability_keys.go` does **not** move, though its name says it should. It
holds the five capability-id constants root's own handlers name, and its doc
comment records why they are there: the registrations moved to `contract/` in
Part C and "these keys stayed because the routes did". A trial move plus
`go test -c -gcflags=-e` strands all five; a plain `go build` sees only
three, because two are named just by root test files. This is another entry for
[Where the prefix lies](6642-query-target-tree.md#where-the-prefix-lies).

#### `query/cicd/` — 5 non-test, 6 test files (2 stay)

| current | new | test files carried |
| --- | --- | --- |
| `ci_cd.go` | `cicd/handler.go` | — (`ci_cd_authz_test.go` stays: auth + scoped-grant proofs via the alias) |
| `ci_cd_evidence_summary.go` | `cicd/evidence_summary.go` | `ci_cd_evidence_summary_artifact_test.go` |
| `ci_cd_run_correlation_aggregates.go` | `cicd/run_correlation_aggregates.go` | `ci_cd_run_correlation_aggregates_count_coverage_test.go` + aggregates test |
| `ci_cd_run_correlation_aggregates_handler.go` | `cicd/run_correlation_aggregates_handler.go` | — |
| `ci_cd_run_correlations.go` | `cicd/run_correlations.go` | `ci_cd_run_correlations_environment_evidence_test.go` + correlations test |

Only the SQL test leaves `ci_cd_authz_test.go` (`cicd/queries_test.go`); the story tests stay, and doubles live in both places (root: `cicd_read_model_doubles_test.go`).

#### `query/cloud/` — 5 non-test, 7 test files

| current | new | test files carried |
| --- | --- | --- |
| `cloud_inventory_code_correlation.go` | `cloud/inventory_code_correlation.go` | `cloud_inventory_code_correlation_test.go` |
| `cloud_inventory_readback.go` | `cloud/inventory_readback.go` | `cloud_inventory_readback_test.go` |
| `cloud_resource_forwarders.go` | `cloud/resource_forwarders.go` | — |
| `cloud_resource_list_store.go` | `cloud/resource_list_store.go` | `cloud_resource_list_store_live_test.go` +1 more |
| `cloud_resource_owner_backfill.go` | `cloud/resource_owner_backfill.go` | `cloud_resource_owner_backfill_nornicdb_live_test.go` +2 more |

#### `query/cloud/drift/` — 6 non-test, 5 test files

| current | new | test files carried |
| --- | --- | --- |
| `cloud_runtime_drift.go` | `cloud/drift/handler.go` | `cloud_runtime_drift_test.go` +1 more |
| `cloud_runtime_drift_aggregate.go` | `cloud/drift/runtime_aggregate.go` | `cloud_runtime_drift_aggregate_test.go` |
| `cloud_runtime_drift_store.go` | `cloud/drift/runtime_store.go` | — |
| `cloud_runtime_drift_view.go` | `cloud/drift/runtime_view.go` | — |
| `drifted_attributes.go` | `cloud/drift/attributes.go` | `drifted_attributes_test.go` |
| `investigation_packet_api_drift.go` | `cloud/drift/investigation_packet.go` | `investigation_packet_api_drift_scope_test.go` |

#### `query/contract/code/` and `query/code/owners/` — the former `code/seam`, 2 files

| current | new | test files carried |
| --- | --- | --- |
| `code_seam.go` | `contract/code/seam.go` | — |
| `family_codeowners_shim.go` | `code/owners/shim.go`, deleted with its root caller | — |

#### `query/collector/` — 8 non-test, 10 test files

| current | new | test files carried |
| --- | --- | --- |
| `collector_extraction_readiness.go` | `collector/extraction_readiness.go` | `collector_extraction_readiness_test.go` |
| `collector_list_readiness.go` | `collector/list_readiness.go` | `collector_list_readiness_handler_test.go` +2 more |
| `collector_list_readiness_postgres.go` | `collector/list_readiness_postgres.go` | `collector_list_readiness_postgres_test.go` |
| `component_extensions.go` | `collector/component_extensions.go` | `component_extensions_test.go` |
| `component_extensions_status.go` | `collector/component_extensions_status.go` | `component_extensions_status_test.go` |
| `fact_schema_version.go` | `collector/fact_schema_version.go` | `fact_schema_version_test.go` |
| `profile_handler.go` | `collector/profile_handler.go` | `profile_handler_test.go` |
| `surface_inventory.go` | `collector/surface_inventory.go` | `surface_inventory_test.go` |

#### `query/compare/` — 4 non-test, 4 test files

| current | new | test files carried |
| --- | --- | --- |
| `compare.go` | `compare/handler.go` | `compare_golden_fixture_test.go` +1 more |
| `compare_evidence.go` | `compare/evidence.go` | — |
| `compare_story.go` | `compare/story.go` | `compare_story_test.go` |
| `context_story_limits.go` | stays in root: only `querycontract` forwarders no compare file calls; leaves with the alias sweep | `context_story_limits_test.go` |

#### `query/content/read/` — 40 non-test, 30 test files

| current | new | test files carried |
| --- | --- | --- |
| `cloud_inventory_read_model.go` | `content/read/cloud_inventory_model.go` | `cloud_inventory_read_model_test.go` |
| `cloud_inventory_rollout_signal.go` | `content/read/cloud_inventory_rollout_signal.go` | `cloud_inventory_rollout_signal_live_test.go` +1 more |
| `content_entity_access_batch.go` | `content/read/entity_access_batch.go` | — |
| `content_reader.go` | `content/read/reader.go` | `content_reader_code_divergence_test.go` +4 more |
| `content_reader_by_type.go` | `content/read/by_type.go` | `content_reader_by_type_test.go` |
| `content_reader_code_topic.go` | `content/read/code_topic.go` | — |
| `content_reader_coverage.go` | `content/read/coverage.go` | — |
| `content_reader_dead_code.go` | `content/read/dead_code.go` | `content_reader_dead_code_provenance_test.go` +1 more |
| `content_reader_dead_code_candidates.go` | `content/read/dead_code_candidates.go` | — |
| `content_reader_dead_code_cross_repo.go` | `content/read/dead_code_cross_repo.go` | — |
| `content_reader_entities_by_ids.go` | `content/read/entities_by_ids.go` | — |
| `content_reader_entities_by_paths.go` | `content/read/entities_by_paths.go` | — |
| `content_reader_entity.go` | `content/read/entity.go` | `content_reader_entity_test.go` +1 more |
| `content_reader_entity_names.go` | `content/read/entity_names.go` | — |
| `content_reader_entity_search.go` | `content/read/entity_search.go` | `content_reader_entity_search_zero_match_test.go` |
| `content_reader_entity_search_page.go` | `content/read/entity_search_page.go` | — |
| `content_reader_evidence_citation.go` | `content/read/evidence_citation.go` | — |
| `content_reader_framework_routes.go` | `content/read/framework_routes.go` | `content_reader_framework_routes_php_test.go` +4 more |
| `content_reader_index_readiness.go` | `content/read/index_readiness.go` | — |
| `content_reader_k8s_select_candidates.go` | `content/read/k8s_select_candidates.go` | `content_reader_k8s_select_candidates_test.go` |
| `content_reader_language.go` | `content/read/language.go` | `content_reader_language_pushdown_test.go` |
| `content_reader_language_inventory.go` | `content/read/language_inventory.go` | — |
| `content_reader_names.go` | `content/read/names.go` | — |
| `content_reader_repository_catalog.go` | `content/read/repository_catalog.go` | `content_reader_repository_catalog_test.go` |
| `content_reader_repository_refs.go` | `content/read/repository_refs.go` | — |
| `content_reader_search_page.go` | `content/read/search_page.go` | — |
| `content_reader_security_secrets.go` | `content/read/security_secrets.go` | `content_reader_security_secrets_test.go` |
| `content_reader_structural_inventory.go` | `content/read/structural_inventory.go` | — |
| `content_reader_symbol_search.go` | `content/read/symbol_search.go` | — |
| `documentation_packet_read_model.go` | `content/read/documentation_packet_model.go` | — |
| `documentation_read_model.go` | `content/read/documentation_model.go` | — |
| `documentation_source_only.go` | `content/read/documentation_source_only.go` | `documentation_source_only_test.go` |
| `documentation_target_read_model.go` | `content/read/documentation_target_model.go` | — |
| `evidence_read_model.go` | `content/read/evidence_model.go` | — |
| `repository_deployment_evidence_read_model.go` | `content/read/repository_deployment_evidence_model.go` | — |
| `repository_entry_points.go` | `content/read/repository_entry_points.go` | `repository_entry_points_test.go` |
| `repository_read_model_summary.go` | `content/read/repository_model_summary.go` | — |
| `repository_relationship_read_model.go` | `content/read/repository_relationship_model.go` | `repository_relationship_read_model_test.go` |
| `service_story_target_support.go` | `content/read/service_story_target_support.go` | `service_story_target_support_live_test.go` +2 more |
| `service_story_target_support_source_only.go` | `content/read/service_story_target_support_source_only.go` | `service_story_target_support_source_only_test.go` |

#### `query/content/relationship/` — 9 non-test, 16 test files

| current | new | test files carried |
| --- | --- | --- |
| `content_relationship_builder.go` | `content/relationship/index_builder.go` | — |
| `content_relationships.go` | `content/relationship/builder.go` | `content_relationships_dockerfile_test.go` +3 more |
| `content_relationships_argocd.go` | `content/relationship/argocd.go` | `content_relationships_argocd_test.go` |
| `content_relationships_cloudformation.go` | `content/relationship/cloudformation.go` | — |
| `content_relationships_docker.go` | `content/relationship/docker.go` | `content_relationships_docker_compose_test.go` |
| `content_relationships_github_actions.go` | `content/relationship/github_actions.go` | `content_relationships_github_actions_dedup_test.go` +4 more |
| `content_relationships_k8s.go` | `content/relationship/k8s.go` | `content_relationships_k8s_match_test.go` +3 more |
| `content_relationships_read_surface_backing.go` | `content/relationship/read_surface_backing.go` | — |
| `content_relationships_rust.go` | `content/relationship/rust.go` | `content_relationships_rust_test.go` |

#### `query/decode/` — 1 non-test, 0 test files

| current | new | test files carried |
| --- | --- | --- |
| `factschema_decode_shared.go` | `decode/factschema_shared.go` | — |

#### `query/dependency/` — 2 non-test, 1 test files

| current | new | test files carried |
| --- | --- | --- |
| `dependencies.go` | `dependency/handler.go` | `dependencies_test.go` |
| `dependencies_cypher.go` | `dependency/cypher.go` | — |

#### `query/documentation/` — 8 non-test, 23 test files

| current | new | test files carried |
| --- | --- | --- |
| `documentation.go` | `documentation/handler.go` | `documentation_archive_readback_test.go` +17 more |
| `documentation_authz.go` | `documentation/authz.go` | `documentation_authz_test.go` |
| `documentation_facts.go` | `documentation/facts.go` | `documentation_facts_test.go` |
| `documentation_finding_aggregate_authz.go` | `documentation/finding_aggregate_authz.go` | `documentation_finding_aggregate_authz_test.go` |
| `documentation_finding_aggregates.go` | `documentation/finding_aggregates.go` | `documentation_finding_aggregates_test.go` |
| `documentation_finding_aggregates_handler.go` | `documentation/finding_aggregates_handler.go` | — |
| `documentation_packet_authz.go` | `documentation/packet_authz.go` | `documentation_packet_authz_test.go` |
| `documentation_payload_helpers.go` | `documentation/payload_helpers.go` | — |

#### `query/evidence/` — 10 non-test, 9 test files

| current | new | test files carried |
| --- | --- | --- |
| `admission_decision_store.go` | `evidence/admission_decision_store.go` | — |
| `admission_decision_types.go` | `evidence/admission_decision_types.go` | — |
| `admission_decisions.go` | `evidence/admission_decisions.go` | `admission_decisions_bounds_test.go` +1 more |
| `evidence.go` | `evidence/handler.go` | `evidence_scoped_test.go` +1 more |
| `evidence_boundaries.go` | `evidence/boundaries.go` | `evidence_boundaries_test.go` |
| `evidence_bundle_live.go` | `evidence/bundle_live.go` | `evidence_bundle_live_test.go` |
| `evidence_citation.go` | `evidence/citation.go` | `evidence_citation_authz_test.go` +1 more |
| `evidence_citation_public.go` | `evidence/citation_public.go` | — |
| `evidence_citation_unified.go` | `evidence/citation_unified.go` | `evidence_citation_unified_test.go` |
| `investigation_packet_api_deployable.go` | `evidence/deployable_unit_packet.go` | — |

#### `query/graph/entity/` — 2 non-test, 3 test files

| current | new | test files carried |
| --- | --- | --- |
| `graph_entity_inventory.go` | `graph/entity/inventory.go` | `graph_entity_inventory_slo_live_test.go` +1 more |
| `graph_entity_inventory_counts.go` | `graph/entity/inventory_counts.go` | `graph_entity_inventory_counts_test.go` |

#### `query/graph/read/` — 3 non-test, 5 test files

| current | new | test files carried |
| --- | --- | --- |
| `graph_read_http_error.go` | `graph/read/http_error.go` | `graph_read_http_error_test.go` |
| `neo4j.go` | `graph/read/reader.go` | `neo4j_test.go` |
| `neo4j_read_policy.go` | `graph/read/policy.go` | `neo4j_read_policy_bench_test.go` +2 more |

#### `query/image/` — 6 non-test, 6 test files

| current | new | test files carried |
| --- | --- | --- |
| `container_image_candidate_explanation.go` | `image/candidate_explanation.go` | — |
| `container_image_identities.go` | `image/identities.go` | `container_image_identities_source_bridge_test.go` +1 more |
| `container_image_identity_aggregates.go` | `image/identity_aggregates.go` | `container_image_identity_aggregates_count_coverage_test.go` +1 more |
| `container_image_identity_v3_sql.go` | `image/identity_v3_sql.go` | — |
| `images.go` | `image/handler.go` | `images_test.go` |
| `images_telemetry.go` | `image/telemetry.go` | `images_telemetry_test.go` |

#### `query/image/tag/` — 2 non-test, 9 test files

| current | new | test files carried |
| --- | --- | --- |
| `tag_history.go` | `image/tag/handler.go` | `tag_history_cursor_seal_test.go` +7 more |
| `tag_history_telemetry.go` | `image/tag/telemetry.go` | `tag_history_telemetry_test.go` |

#### `query/impact/trace/` — the former `impact/seam`, 5 files

Four of these five have no consumer outside the leaf, so they were never a seam;
they are impact's own backends.

| current | new | test files carried |
| --- | --- | --- |
| `deployment_trace_support_helpers.go` | `impact/trace/support_helpers.go` | `deployment_trace_support_helpers_test.go` |
| `family_impact_change_surface_code.go` | `impact/trace/change_surface_code.go` | — |
| `family_impact_path_probe_adapter.go` | `impact/trace/path_probe_adapter.go` | — |
| `family_impact_shim.go` | `impact/trace/ports.go` | — |
| `family_impact_trace_deployment.go` | `impact/trace/deployment.go` | — |

#### `query/infra/` — 10 non-test, 20 test files

| current | new | test files carried |
| --- | --- | --- |
| `cloud_resources.go` | `infra/cloud_resources.go` | `cloud_resources_paging_test.go` +1 more |
| `cloud_resources_metrics.go` | `infra/cloud_resources_metrics.go` | — |
| `cloud_resources_params.go` | `infra/cloud_resources_params.go` | — |
| `infra.go` | `infra/handler.go` | `infra_capability_test.go` +9 more |
| `infra_argocd_search.go` | `infra/argocd_search.go` | `infra_argocd_search_test.go` |
| `infra_ecosystem_overview.go` | `infra/ecosystem_overview.go` | `infra_ecosystem_overview_test.go` |
| `infra_scope.go` | `infra/scope.go` | — |
| `infra_scope_grant.go` | `infra/scope_grant.go` | `infra_scope_grant_live_test.go` +2 more |
| `infra_scope_grant_capped.go` | `infra/scope_grant_capped.go` | `infra_scope_grant_capped_bench_test.go` +2 more |
| `infra_search_predicates.go` | `infra/search_predicates.go` | — |

#### `query/infra/aggregate/` — 5 non-test, 10 test files

| current | new | test files carried |
| --- | --- | --- |
| `infra_resource_aggregates.go` | `infra/aggregate/store.go` | `infra_resource_aggregates_category_test.go` +5 more |
| `infra_resource_aggregates_cypher.go` | `infra/aggregate/cypher.go` | `infra_resource_aggregates_cypher_test.go` |
| `infra_resource_aggregates_handler.go` | `infra/aggregate/handler.go` | — |
| `infra_resource_aggregates_read_model.go` | `infra/aggregate/read_model.go` | `infra_resource_aggregates_read_model_readiness_test.go` +1 more |
| `infra_resource_aggregates_scope.go` | `infra/aggregate/scope.go` | `infra_resource_aggregates_scope_test.go` |

#### `query/infra/relationship/` — 4 non-test, 14 test files

| current | new | test files carried |
| --- | --- | --- |
| `infra_relationship_filter.go` | `infra/relationship/filter.go` | `infra_relationship_filter_lambda_image_test.go` +1 more |
| `relationship_confidence_basis.go` | `infra/relationship/confidence_basis.go` | `relationship_confidence_basis_test.go` |
| `relationships_catalog.go` | `infra/relationship/catalog.go` | `relationships_catalog_atlantis_test.go` +10 more |
| `relationships_catalog_cypher.go` | `infra/relationship/catalog_cypher.go` | — |

#### `query/infra/summary/` — 2 non-test, 2 test files

| current | new | test files carried |
| --- | --- | --- |
| `infra_graph_summary_packet.go` | `infra/summary/packet.go` | `infra_graph_summary_packet_nornic_test.go` +1 more |
| `infra_graph_summary_packet_cypher.go` | `infra/summary/packet_cypher.go` | — |

#### `query/investigation/packet/` — 8 non-test, 5 test files

| current | new | test files carried |
| --- | --- | --- |
| `investigation_packet.go` | `investigation/packet/core.go` | `investigation_packet_bounds_test.go` +1 more |
| `investigation_packet_api.go` | `investigation/packet/api.go` | `investigation_packet_api_test.go` |
| `investigation_packet_build.go` | `investigation/packet/build.go` | — |
| `investigation_packet_deployable_unit.go` | `investigation/packet/deployable_unit.go` | `investigation_packet_deployable_unit_test.go` |
| `investigation_packet_drift.go` | `investigation/packet/drift.go` | `investigation_packet_drift_test.go` |
| `investigation_packet_identity.go` | `investigation/packet/identity.go` | — |
| `investigation_packet_render.go` | `investigation/packet/render.go` | — |
| `investigation_packet_validate.go` | `investigation/packet/validate.go` | — |

#### `query/investigation/workflow/` — 3 non-test, 4 test files

| current | new | test files carried |
| --- | --- | --- |
| `investigation_workflow.go` | `investigation/workflow/model.go` | `investigation_workflow_deployable_test.go` +2 more |
| `investigation_workflow_catalog.go` | `investigation/workflow/catalog.go` | — |
| `investigation_workflow_handler.go` | `investigation/workflow/handler.go` | `investigation_workflow_handler_test.go` |

#### `query/kubernetes/` — 3 non-test, 5 test files

| current | new | test files carried |
| --- | --- | --- |
| `kubernetes.go` | `kubernetes/handler.go` | — |
| `kubernetes_correlations.go` | `kubernetes/correlations.go` | `kubernetes_correlations_test.go` |
| `kubernetes_runtime_workload_store.go` | `kubernetes/runtime_workload_store.go` | `kubernetes_runtime_workload_store_bench_test.go` +3 more |

#### `query/metrics/` — 3 non-test, 3 test files

| current | new | test files carried |
| --- | --- | --- |
| `metrics.go` | `metrics/handler.go` | `metrics_test.go` |
| `metrics_prometheus.go` | `metrics/prometheus.go` | `metrics_prometheus_test.go` |
| `request_metrics.go` | `metrics/request.go` | `request_metrics_test.go` |

#### `query/observability/coverage/` — 2 non-test, 1 test files

| current | new | test files carried |
| --- | --- | --- |
| `observability_coverage.go` | `observability/coverage/handler.go` | — |
| `observability_coverage_correlations.go` | `observability/coverage/correlations.go` | `observability_coverage_correlations_test.go` |

#### `query/contract/` and `query/cicd/` — the former `repository/seam`, 3 files

| current | new | test files carried |
| --- | --- | --- |
| `repository_authz.go` | `contract/repository_access.go` (18 consuming destinations) | — |
| `repository_compat.go` | `contract/repository_helpers.go` | — |
| `repository_selector.go` | DELETED — the cicd family calls `selector.ResolveForRequestWithAccess` directly like every other leaf, leaving zero callers (a callerless forwarder trips `unused` and the capability sweep) | — |

#### `query/semantic/` — 2 non-test, 0 test files

| current | new | test files carried |
| --- | --- | --- |
| `elixir_semantic_types.go` | `semantic/elixir_types.go` | — |
| `typescript_semantics.go` | `semantic/typescript.go` | — |

#### `query/semantic/evidence/` — 1 non-test, 0 test files

| current | new | test files carried |
| --- | --- | --- |
| `semantic_evidence_read_model.go` | `semantic/evidence/read_model.go` | — |

#### `query/status/` — 14 non-test, 17 test files

| current | new | test files carried |
| --- | --- | --- |
| `operator_control_plane.go` | `status/operator_control_plane.go` | `operator_control_plane_test.go` |
| `status.go` | `status/handler.go` | `status_handler_test.go` +4 more |
| `status_answer_narration.go` | `status/answer_narration.go` | `status_answer_narration_test.go` |
| `status_collector_readiness.go` | `status/collector_readiness.go` | `status_collector_readiness_test.go` |
| `status_collectors.go` | `status/collectors.go` | `status_collectors_handler_test.go` |
| `status_freshness_causality.go` | `status/freshness_causality.go` | `status_freshness_causality_test.go` |
| `status_governance.go` | `status/governance.go` | `status_governance_audit_test.go` +2 more |
| `status_hosted_readiness.go` | `status/hosted_readiness.go` | `status_hosted_readiness_test.go` |
| `status_mappers.go` | `status/mappers.go` | — |
| `status_operations.go` | `status/operations.go` | `status_operations_envelope_test.go` +2 more |
| `status_projection.go` | `status/projection.go` | — |
| `status_scoped.go` | `status/scoped.go` | — |
| `status_semantic_extraction.go` | `status/semantic_extraction.go` | — |
| `status_tfstate.go` | `status/tfstate.go` | — |

#### `query/supply/chain/` — 3 non-test, 2 test files

| current | new | test files carried |
| --- | --- | --- |
| `compat_supply_chain.go` | `supply/chain/compat.go` | — |
| `factschema_decode_supplychain.go` | `supply/chain/factschema_decode.go` | `factschema_decode_supplychain_test.go` |
| `investigation_packet_supply_chain.go` | `supply/chain/investigation_packet.go` | `investigation_packet_supply_chain_test.go` |

#### `query/supply/chain/sbom/` — 4 non-test, 5 test files

| current | new | test files carried |
| --- | --- | --- |
| `sbom_attestation_attachment_aggregates.go` | `supply/chain/sbom/attachment_aggregates.go` | `sbom_attestation_attachment_aggregates_count_coverage_test.go` +2 more |
| `sbom_attestation_attachment_evidence_rows.go` | `supply/chain/sbom/attachment_evidence_rows.go` | `sbom_attestation_attachment_evidence_rows_test.go` |
| `sbom_attestation_attachment_rows.go` | `supply/chain/sbom/attachment_rows.go` | — |
| `sbom_attestation_attachments.go` | `supply/chain/sbom/attachments.go` | `sbom_attestation_attachments_test.go` |

#### `query/terraform/drift/` — 2 non-test, 3 test files

| current | new | test files carried |
| --- | --- | --- |
| `terraform_config_state_drift.go` | `terraform/drift/handler.go` | `terraform_config_state_drift_access_test.go` +2 more |
| `terraform_config_state_drift_evidence_access.go` | `terraform/drift/config_state_evidence_access.go` | — |

#### `query/workload/` — 1 non-test, 0 test files

| current | new | test files carried |
| --- | --- | --- |
| `aws_materialization_status.go` | `workload/materialization_status.go` | — |
