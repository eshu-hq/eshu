// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package payloadusage

// defaultRawPayloadExemptions is the reviewed raw-payload exemption list.
// It lives in this file (not rawpayload.go) to keep both under the
// 500-line file cap; every entry carries its justification inline.
func defaultRawPayloadExemptions() []RawPayloadExemption {
	return []RawPayloadExemption{
		{Path: "go/internal/relationships/argocd_generator_config.go", Accessor: rawPayloadIndexAccessor, Key: "parsed_file_data"},
		{Path: "go/internal/relationships/evidence.go", Accessor: rawPayloadIndexAccessor, Key: "artifact_type"},
		{Path: "go/internal/relationships/evidence.go", Accessor: rawPayloadIndexAccessor, Key: "parsed_file_data"},
		{Path: "go/internal/relationships/evidence.go", Accessor: rawPayloadIndexAccessor, Key: "repo_id"},
		{Path: "go/internal/relationships/evidence_content_index.go", Accessor: rawPayloadIndexAccessor, Key: "content"},
		{Path: "go/internal/relationships/evidence_content_index.go", Accessor: rawPayloadIndexAccessor, Key: "content_body"},
		{Path: "go/internal/relationships/evidence_content_index.go", Accessor: rawPayloadIndexAccessor, Key: "content_path"},
		{Path: "go/internal/relationships/evidence_content_index.go", Accessor: rawPayloadIndexAccessor, Key: "relative_path"},
		{Path: "go/internal/relationships/evidence_byte_citation.go", Accessor: rawPayloadIndexAccessor, Key: "commit_sha"},
		{Path: "go/internal/storage/postgres/incident_routing_evidence_loader.go", Accessor: "incidentRoutingPayloadMap", Key: "service"},
		// crossplaneRedriveXRDFields (issue #5476's cross-scope SATISFIED_BY
		// redrive sweep) reads a CrossplaneXRD content_entity fact's
		// entity_metadata to extract the XRD's (group, claim_kind) join key.
		// content_entity is parser output: every entity_type's extra fields
		// (Variable, Function, K8sResource, CrossplaneXRD, ...) live under
		// entity_metadata as an untyped bag, and content_entity has no typed
		// factschema struct -- sdk/go/factschema models only collector-emitted
		// families (AWS, IAM, incident, ...), never parsed code/content
		// entities, so there is no seam to migrate to. The reducer's own
		// pre-existing SATISFIED_BY correlation
		// (internal/reducer/crossplane/satisfied_by_edge_rows.go's
		// crossplaneEntityMetadataString) reads this exact field the exact
		// same raw way; this exemption keeps the sweep's read consistent with
		// that already-established pattern rather than inventing a one-off
		// typed struct ahead of a content_entity family migration this change
		// does not own.
		{Path: "go/internal/storage/postgres/crossplane/sweep.go", Accessor: rawPayloadIndexAccessor, Key: "entity_metadata"},
		// The repository-catalog payload parser moved from the Postgres ingestion
		// path to relationships.RepositoryCatalogEntry so Ifá can derive the same
		// catalog offline (#4394 T2). The reads are unchanged (still pre-typed
		// repository payload keys); the exemption follows the code to its new home.
		{Path: "go/internal/relationships/catalog.go", Accessor: "catalogPayloadString", Key: "graph_id"},
		{Path: "go/internal/relationships/catalog.go", Accessor: "catalogPayloadString", Key: "name"},
		{Path: "go/internal/relationships/catalog.go", Accessor: "catalogPayloadString", Key: "repo_id"},
		{Path: "go/internal/relationships/catalog.go", Accessor: "catalogPayloadString", Key: "repo_name"},
		{Path: "go/internal/relationships/catalog.go", Accessor: "catalogPayloadString", Key: "repo_slug"},
		// remote_url feeds CatalogEntry.RemoteURL, the strict cross-repo URL
		// resolution primitive discoverStructuredFluxEvidence uses (issue #5483
		// C2). Same untyped repository-payload pattern as the sibling reads
		// above; the repository fact kind has no typed struct yet.
		{Path: "go/internal/relationships/catalog.go", Accessor: "catalogPayloadString", Key: "remote_url"},
		{Path: "go/internal/replay/offlinetier/delta.go", Accessor: rawPayloadIndexAccessor, Key: "path"},
		{Path: "go/internal/replay/offlinetier/materialization.go", Accessor: "optionalString", Key: "needs"},
		{Path: "go/internal/replay/offlinetier/materialization.go", Accessor: "requireInt", Key: "depth"},
		{Path: "go/internal/replay/offlinetier/materialization.go", Accessor: "requireString", Key: "dir_path"},
		{Path: "go/internal/replay/offlinetier/materialization.go", Accessor: "requireString", Key: "file_path"},
		{Path: "go/internal/replay/offlinetier/materialization.go", Accessor: "requireString", Key: "language"},
		{Path: "go/internal/replay/offlinetier/materialization.go", Accessor: "requireString", Key: "name"},
		{Path: "go/internal/replay/offlinetier/materialization.go", Accessor: "requireString", Key: "parent_path"},
		{Path: "go/internal/replay/offlinetier/materialization.go", Accessor: "requireString", Key: "path"},
		{Path: "go/internal/replay/offlinetier/materialization.go", Accessor: "requireString", Key: "relative_path"},
		{Path: "go/internal/replay/offlinetier/materialization.go", Accessor: "requireString", Key: "repo_id"},
		{Path: "go/internal/replay/offlinetier/materialization.go", Accessor: "requireString", Key: "uid"},
		// The relationship-reference side table records the source repo_id for
		// candidate content/file/cloud-relationship facts so the deferred
		// backfill can keep exact self-exclusion semantics while avoiding
		// per-candidate regex scans.
		{Path: "go/internal/storage/postgres/relationship_reference_keys.go", Accessor: rawPayloadIndexAccessor, Key: "repo_id"},
		// content_entity manifest and package registry facts have no typed
		// factschema seam. Keep each readiness sidecar input key explicit.
		{Path: "go/internal/storage/postgres/package_manifest_consumption_keys.go", Accessor: "packageManifestMetadataString", Key: "package_manager"},
		{Path: "go/internal/storage/postgres/package_manifest_consumption_keys.go", Accessor: "packageManifestPayloadString", Key: "repo_id"},
		{Path: "go/internal/storage/postgres/package_manifest_consumption_keys.go", Accessor: "packageManifestPayloadString", Key: "entity_type"},
		{Path: "go/internal/storage/postgres/package_manifest_consumption_keys.go", Accessor: "packageManifestMetadataString", Key: "config_kind"},
		{Path: "go/internal/storage/postgres/package_manifest_consumption_keys.go", Accessor: "packageManifestPayloadString", Key: "entity_name"},
		{Path: "go/internal/storage/postgres/package_manifest_consumption_keys.go", Accessor: "packageManifestMetadataString", Key: "lockfile_unsupported_feature"},
		{Path: "go/internal/storage/postgres/package_manifest_consumption_keys.go", Accessor: "packageManifestMetadataBool", Key: "source_ambiguous"},
		// #8824: a central version pin (NuGet PackageVersion) is never package
		// consumption, so the sidecar predicate drops it by this flag.
		{Path: "go/internal/storage/postgres/package_manifest_consumption_keys.go", Accessor: "centralFlagText", Key: "central_version_declaration"},
		// #8961: an sbt row joins the Maven artifact it publishes, which only
		// these two keys name; the sbt parser writes both (go/internal/parser/sbt).
		{Path: "go/internal/storage/postgres/package_manifest_consumption_keys.go", Accessor: "packageManifestMetadataString", Key: "sbt_cross_version"},
		{Path: "go/internal/storage/postgres/package_manifest_consumption_keys.go", Accessor: "packageManifestMetadataString", Key: "sbt_binary_artifact"},
		{Path: "go/internal/storage/postgres/package_manifest_consumption_keys.go", Accessor: "packageManifestMetadataString", Key: "namespace"},
		{Path: "go/internal/storage/postgres/package_manifest_consumption_keys.go", Accessor: rawPayloadIndexAccessor, Key: "entity_metadata"},
		{Path: "go/internal/storage/postgres/package_registry_identity_keys.go", Accessor: "packageManifestPayloadString", Key: "package_id"},
		{Path: "go/internal/storage/postgres/package_registry_identity_keys.go", Accessor: "packageManifestPayloadString", Key: "ecosystem"},
		{Path: "go/internal/storage/postgres/package_registry_identity_keys.go", Accessor: "packageManifestPayloadString", Key: "raw_name"},
		{Path: "go/internal/storage/postgres/package_registry_identity_keys.go", Accessor: "packageManifestPayloadString", Key: "normalized_name"},
		{Path: "go/internal/storage/postgres/package_registry_identity_keys.go", Accessor: "packageManifestPayloadString", Key: "namespace"},
		{Path: "go/internal/storage/postgres/service_vulnerability_advisory_loader.go", Accessor: "payloadString", Key: "advisory_id"},
		{Path: "go/internal/storage/postgres/service_vulnerability_advisory_loader.go", Accessor: "payloadString", Key: "confidence"},
		{Path: "go/internal/storage/postgres/service_vulnerability_advisory_loader.go", Accessor: "payloadString", Key: "cve_id"},
		{Path: "go/internal/storage/postgres/service_vulnerability_advisory_loader.go", Accessor: "payloadString", Key: "ecosystem"},
		{Path: "go/internal/storage/postgres/service_vulnerability_advisory_loader.go", Accessor: "payloadString", Key: "epss_probability"},
		{Path: "go/internal/storage/postgres/service_vulnerability_advisory_loader.go", Accessor: "payloadString", Key: "package_name"},
		{Path: "go/internal/storage/postgres/service_vulnerability_advisory_loader.go", Accessor: "payloadString", Key: "repository_id"},
		{Path: "go/internal/storage/postgres/service_vulnerability_advisory_loader.go", Accessor: "payloadString", Key: "selected_severity_label"},
		{Path: "go/internal/storage/postgres/service_vulnerability_advisory_loader.go", Accessor: "payloadBool", Key: "known_exploited"},
		{Path: "go/internal/storage/postgres/service_vulnerability_advisory_loader.go", Accessor: rawPayloadIndexAccessor, Key: "provenance"},
		{Path: "go/internal/storage/postgres/shared_intents.go", Accessor: "sharedIntentPayloadString", Key: "acceptance_unit_id"},
		{Path: "go/internal/storage/postgres/shared_intents.go", Accessor: "sharedIntentPayloadString", Key: "scope_id"},
		{Path: "go/internal/storage/postgres/shared_intents_history.go", Accessor: "sharedIntentPayloadString", Key: "intent_type"},
		{Path: "go/internal/storage/postgres/shared_intents_history.go", Accessor: "sharedIntentPayloadString", Key: "repo_id"},
		{Path: "go/internal/storage/postgres/shared_intents_history.go", Accessor: "sharedIntentPayloadStringSlice", Key: "delta_file_paths"},
		// #10204: union-leg narrowing reads file relative_path +
		// parsed_file_data definitions; no SDK seam covers definition
		// package_id. Filter-only reads; unparseable payloads keep rows.
		{Path: "go/internal/storage/postgres/active_code_call_symbol_legs.go", Accessor: rawPayloadIndexAccessor, Key: "relative_path"},
		{Path: "go/internal/storage/postgres/active_code_call_symbol_legs.go", Accessor: rawPayloadIndexAccessor, Key: "parsed_file_data"},
		// #10875: the relationship lookup-key writer derives its v1 family /
		// marker columns from the fact's own envelope at save time (#10861
		// D2). Each read mirrors one arm of the legacy text-search predicate
		// the keyed reader replaces, so the keyed set equals the legacy set;
		// the content/file/gcp fact kinds have no typed factschema seam for
		// these predicate fields.
		{Path: "go/internal/storage/postgres/relationship_reference_keys.go", Accessor: "relationshipReferencePayloadText", Key: "artifact_type"},
		{Path: "go/internal/storage/postgres/relationship_reference_keys.go", Accessor: rawPayloadIndexAccessor, Key: "parsed_file_data"},
		// #10875: the security-alert key writer explodes the six legacy
		// filter arms (repository, package, cve scalar+array, advisory
		// scalar+array) into key rows in the same save transaction. Values
		// are stored verbatim so the keyed EXISTS set equals the legacy
		// six-arm set; the security-alert kinds have no typed seam for
		// these id fields.
		{Path: "go/internal/storage/postgres/alert_reconciliation_keys.go", Accessor: rawPayloadIndexAccessor, Key: "repository_id"},
		{Path: "go/internal/storage/postgres/alert_reconciliation_keys.go", Accessor: rawPayloadIndexAccessor, Key: "package_id"},
		{Path: "go/internal/storage/postgres/alert_reconciliation_keys.go", Accessor: rawPayloadIndexAccessor, Key: "cve_ids"},
		{Path: "go/internal/storage/postgres/alert_reconciliation_keys.go", Accessor: rawPayloadIndexAccessor, Key: "cve_id"},
		{Path: "go/internal/storage/postgres/alert_reconciliation_keys.go", Accessor: rawPayloadIndexAccessor, Key: "ghsa_ids"},
		{Path: "go/internal/storage/postgres/alert_reconciliation_keys.go", Accessor: rawPayloadIndexAccessor, Key: "advisory_id"},
	}
}
