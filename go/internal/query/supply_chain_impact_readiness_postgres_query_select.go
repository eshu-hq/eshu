// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

const listSupplyChainImpactReadinessQuerySelect = `
SELECT family, fact_count, latest_observed_at, target_incomplete, incomplete_reasons, source_snapshots_json, source_states_json, unsupported_targets_json FROM vulnerability_advisory
UNION ALL SELECT family, fact_count, latest_observed_at, target_incomplete, incomplete_reasons, source_snapshots_json, source_states_json, unsupported_targets_json FROM vulnerability_exploitability
UNION ALL SELECT family, fact_count, latest_observed_at, target_incomplete, incomplete_reasons, source_snapshots_json, source_states_json, unsupported_targets_json FROM package_consumption_correlation
UNION ALL SELECT family, fact_count, latest_observed_at, target_incomplete, incomplete_reasons, source_snapshots_json, source_states_json, unsupported_targets_json FROM package_manifest_dependency
UNION ALL SELECT family, fact_count, latest_observed_at, target_incomplete, incomplete_reasons, source_snapshots_json, source_states_json, unsupported_targets_json FROM package_registry
UNION ALL SELECT family, fact_count, latest_observed_at, target_incomplete, incomplete_reasons, source_snapshots_json, source_states_json, unsupported_targets_json FROM sbom_component
UNION ALL SELECT family, fact_count, latest_observed_at, target_incomplete, incomplete_reasons, source_snapshots_json, source_states_json, unsupported_targets_json FROM sbom_attestation
UNION ALL SELECT family, fact_count, latest_observed_at, target_incomplete, incomplete_reasons, source_snapshots_json, source_states_json, unsupported_targets_json FROM container_image_identity
UNION ALL SELECT family, fact_count, latest_observed_at, target_incomplete, incomplete_reasons, source_snapshots_json, source_states_json, unsupported_targets_json FROM vulnerability_source_snapshot
UNION ALL SELECT family, fact_count, latest_observed_at, target_incomplete, incomplete_reasons, source_snapshots_json, source_states_json, unsupported_targets_json FROM vulnerability_source_state
UNION ALL SELECT family, fact_count, latest_observed_at, target_incomplete, incomplete_reasons, source_snapshots_json, source_states_json, unsupported_targets_json FROM unsupported_target
UNION ALL SELECT family, fact_count, latest_observed_at, target_incomplete, incomplete_reasons, source_snapshots_json, source_states_json, unsupported_targets_json FROM scanner_worker_analysis
UNION ALL SELECT family, fact_count, latest_observed_at, target_incomplete, incomplete_reasons, source_snapshots_json, source_states_json, unsupported_targets_json FROM vulnerability_os_package
`
