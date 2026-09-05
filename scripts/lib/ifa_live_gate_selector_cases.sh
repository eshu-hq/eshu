#!/usr/bin/env bash
# shellcheck shell=bash disable=SC2034
# Concrete trigger|path fixtures for the real IFA live-gate registry matcher.
# Patterns appear once per representative path so wildcard drift cannot hide
# behind a string-only workflow/registry check.
#
# The negative controls live in the sibling file sourced immediately below:
# ifa_live_gate_negative_seams (paths that must select neither live gate) and
# ifa_live_gate_negative_gate_seams (path|required|forbidden, for a path that
# must keep selecting one gate and must not have been widened onto another --
# ifa-dead-letter-matrix is the case that needed it). They were split out when
# this file reached 488 of the blocking 500-line cap; both halves are covered
# by the scripts/lib/ifa_live_gate_*.sh trigger both live gates now carry
# (#6200), so unlike every earlier split in this directory the new half is
# not dark.
#
# Sourced by variable path, which internal/cigates/scripttrigger.go's
# sourced-to-triggered drift walk cannot resolve -- the rg pin below is what
# stops the source line from being quietly deleted, matching the pin
# test-ifa-determinism-registry-lockstep-cases.sh puts on this file.
ifa_live_gate_negative_cases_lib="${BASH_SOURCE[0]%/*}/ifa_live_gate_negative_cases.sh"
rg --quiet --fixed-strings --line-regexp -- 'source "${ifa_live_gate_negative_cases_lib}"' "${BASH_SOURCE[0]}" ||
	{
		printf 'ifa_live_gate_selector_cases: negative controls must be sourced from scripts/lib/ifa_live_gate_negative_cases.sh\n' >&2
		exit 1
	}
# shellcheck source=scripts/lib/ifa_live_gate_negative_cases.sh
source "${ifa_live_gate_negative_cases_lib}"

# The determinism-only seams took the same split at 491 lines. Same variable-path
# source and the same rg pin, for the same reason: the drift walk cannot resolve
# the path, so nothing else would notice the source line being deleted -- and a
# deleted source leaves the consuming loop iterating an unset array, which under
# `set -u` is a hard error and under anything laxer is silent zero coverage.
ifa_live_gate_determinism_only_cases_lib="${BASH_SOURCE[0]%/*}/ifa_live_gate_determinism_only_cases.sh"
rg --quiet --fixed-strings --line-regexp -- 'source "${ifa_live_gate_determinism_only_cases_lib}"' "${BASH_SOURCE[0]}" ||
	{
		printf 'ifa_live_gate_selector_cases: determinism-only seams must be sourced from scripts/lib/ifa_live_gate_determinism_only_cases.sh\n' >&2
		exit 1
	}
# shellcheck source=scripts/lib/ifa_live_gate_determinism_only_cases.sh
source "${ifa_live_gate_determinism_only_cases_lib}"

ifa_live_gate_common_seams=(
	'scripts/lib/ifa_live_gate_*.sh|scripts/lib/ifa_live_gate_selector_cases.sh'
	# The half this file was split into at 488 of the 500-line cap. Pinned
	# through the real matcher rather than read off the glob: this is the
	# first split in scripts/lib/ that does NOT go dark, and the assertion
	# that it does not is worth more than the sentence saying so.
	'scripts/lib/ifa_live_gate_*.sh|scripts/lib/ifa_live_gate_negative_cases.sh'
	'scripts/lib/ifa_live_gate_*.sh|scripts/lib/ifa_live_gate_determinism_only_cases.sh'
	# Wildcard fixtures. The literals above cannot exercise a glob, and this
	# file's own contract is one representative PATH per pattern -- a
	# string-only registry/workflow comparison agrees just as happily on a
	# broken glob as on a working one. Concrete paths under each new wildcard
	# so a narrowed pattern (e.g. '**' quietly becoming '*', which does not
	# cross '/') fails here instead of silently selecting no gate for the most
	# common edit in the tree.
	'scripts/lib/ifa_family_registry/**|scripts/lib/ifa_family_registry/rows/01_sql_relationships.sh'
	'go/internal/storage/postgres/migrations/**|go/internal/storage/postgres/migrations/001_ingestion_scopes.sql'
	'go/internal/storage/postgres/migrations/**|go/internal/storage/postgres/migrations/096_provenance_edge_identity_upgrade_seed.sql'
	'go/internal/graphschemacompat/**|go/internal/graphschemacompat/compatibility.go'
	'go/internal/graphschemacompat/**|go/internal/graphschemacompat/write_fence.go'
	'go/internal/projector/retry.go|go/internal/projector/retry.go'
	'go/internal/projector/schema_version_admission.go|go/internal/projector/schema_version_admission.go'
	'go/internal/storage/postgres/failure_metadata.go|go/internal/storage/postgres/failure_metadata.go'
	'go/internal/storage/postgres/retry_backoff.go|go/internal/storage/postgres/retry_backoff.go'
	'go/internal/storage/cypher/writer.go|go/internal/storage/cypher/writer.go'
	'go/internal/storage/cypher/phase_group_metadata.go|go/internal/storage/cypher/phase_group_metadata.go'
	'go/internal/storage/cypher/phase_group_sanitize.go|go/internal/storage/cypher/phase_group_sanitize.go'
	'go/internal/storage/cypher/statement_chunk.go|go/internal/storage/cypher/statement_chunk.go'
	'go/internal/storage/cypher/bounded_retract_drain.go|go/internal/storage/cypher/bounded_retract_drain.go'
	'go/internal/storage/cypher/reconciliation_drift_metrics.go|go/internal/storage/cypher/reconciliation_drift_metrics.go'
	'go/internal/storage/cypher/canonical_node_writer_entities_singleton.go|go/internal/storage/cypher/canonical_node_writer_entities_singleton.go'
	'go/internal/storage/postgres/deferred_maintenance_lock.go|go/internal/storage/postgres/deferred_maintenance_lock.go'
	'go/internal/storage/postgres/relationship_reference_keys.go|go/internal/storage/postgres/relationship_reference_keys.go'
	'go/internal/storage/postgres/relationship_store*.go|go/internal/storage/postgres/relationship_store.go'
	'go/internal/storage/postgres/relationship_store*.go|go/internal/storage/postgres/relationship_store_resolved.go'
	'go/internal/storage/postgres/ingestion_backfill*.go|go/internal/storage/postgres/ingestion_backfill.go'
	'go/internal/storage/postgres/ingestion_backfill*.go|go/internal/storage/postgres/ingestion_backfill_generation_guard.go'
	'go/internal/storage/postgres/ingestion_backfill*.go|go/internal/storage/postgres/ingestion_backfill_deferred_facts.go'
	'go/internal/storage/postgres/ingestion_flux_cross_repo_telemetry.go|go/internal/storage/postgres/ingestion_flux_cross_repo_telemetry.go'
	'go/internal/relationships/**|go/internal/relationships/resolver.go'
	'go/internal/relationships/**|go/internal/relationships/evidence.go'
	'go/internal/relationships/**|go/internal/relationships/tfstatebackend/resolver.go'
	'go/internal/runtime/data_stores.go|go/internal/runtime/data_stores.go'
	'go/internal/runtime/retry_policy.go|go/internal/runtime/retry_policy.go'
	'go/internal/storage/postgres/projector_queue_crossplane_redrive_hook.go|go/internal/storage/postgres/projector_queue_crossplane_redrive_hook.go'
	'go/internal/storage/postgres/projector_queue_config_state_drift_trigger_hook.go|go/internal/storage/postgres/projector_queue_config_state_drift_trigger_hook.go'
	'go/go.mod|go/go.mod'
	'go/go.sum|go/go.sum'
	'sdk/go/factschema/go.mod|sdk/go/factschema/go.mod'
	# sdk/go/factschema/*.go replaced decode.go, fact_kinds.go,
	# decode_codegraph.go and #6198's *submodule*.go (#6200). Same evidence
	# shape as the reducer block above: formerly-dark decode machinery first,
	# then the filenames the old list did name.
	'sdk/go/factschema/*.go|sdk/go/factschema/decode_map.go'
	'sdk/go/factschema/*.go|sdk/go/factschema/decode_map_coerce.go'
	'sdk/go/factschema/*.go|sdk/go/factschema/decode_map_numbers.go'
	'sdk/go/factschema/*.go|sdk/go/factschema/fields.go'
	'sdk/go/factschema/*.go|sdk/go/factschema/encode_direct.go'
	'sdk/go/factschema/*.go|sdk/go/factschema/decode_codeowners.go'
	'sdk/go/factschema/*.go|sdk/go/factschema/fact_kinds_codeowners.go'
	'sdk/go/factschema/*.go|sdk/go/factschema/decode_documentation.go'
	'sdk/go/factschema/*.go|sdk/go/factschema/decode_gcp.go'
	'sdk/go/factschema/*.go|sdk/go/factschema/decode_parsed_file_data_gitops.go'
	'sdk/go/factschema/*.go|sdk/go/factschema/decode.go'
	'sdk/go/factschema/*.go|sdk/go/factschema/fact_kinds.go'
	'sdk/go/factschema/*.go|sdk/go/factschema/decode_codegraph.go'
	'sdk/go/factschema/*.go|sdk/go/factschema/decode_submodule.go'
	'sdk/go/factschema/*.go|sdk/go/factschema/fact_kinds_submodule.go'
	# The two v1 packages the driven cassettes reach. A '*' here would not
	# cross '/', so these paths are what catches a narrowed pattern.
	'sdk/go/factschema/codegraph/v1/**|sdk/go/factschema/codegraph/v1/parsed_file_data_gitops.go'
	'sdk/go/factschema/codegraph/v1/**|sdk/go/factschema/codegraph/v1/file.go'
	'sdk/go/factschema/codegraph/v1/**|sdk/go/factschema/codegraph/v1/repository.go'
	'sdk/go/factschema/gcp/v1/**|sdk/go/factschema/gcp/v1/resource.go'
	'sdk/go/factschema/gcp/v1/**|sdk/go/factschema/gcp/v1/relationship.go'
	'.github/workflows/ifa-determinism-gate.yml|.github/workflows/ifa-determinism-gate.yml'
	'specs/ci-gates.v1.yaml|specs/ci-gates.v1.yaml'
	'specs/ifa-materialized-edge-coverage.v1.yaml|specs/ifa-materialized-edge-coverage.v1.yaml'
	# The direct-materialization half of the same ledger (#6181). Both
	# halves are loaded by one gate run, so a change to either has to
	# retrigger both live matrices; the trigger is a literal, and this row
	# is what proves the real matcher SELECTS on it rather than proving
	# only that the string appears in the registry.
	'specs/ifa-materialized-edge-coverage-direct.v1.yaml|specs/ifa-materialized-edge-coverage-direct.v1.yaml'
	'go/internal/ifa/graphdump/**|go/internal/ifa/graphdump/canonical.go'
	'go/internal/ifa/graphdump/**|go/internal/ifa/graphdump/reader.go'
	'go/cmd/ifa/**|go/cmd/ifa/main.go'
	'go/cmd/ifa/**|go/cmd/ifa/assert_edges.go'
	'go/internal/synth/gcp/**|go/internal/synth/gcp/generator.go'
	'go/internal/synth/gcp/**|go/internal/synth/gcp/multiscope.go'
	# go/internal/reducer/** replaced ~40 hand-picked filenames on both live
	# gates (#6200). One pattern, so one seam entry would satisfy the string
	# checks -- and that is exactly the shape this file exists to distrust.
	# The paths below are the concrete evidence: the first block is files
	# that were DARK before the glob (no live gate re-ran when they changed),
	# the second is files the old literal list did name, kept so a glob
	# narrowed to something that no longer reaches them fails here instead of
	# quietly selecting nothing.
	'go/internal/reducer/**|go/internal/reducer/admission_decisions.go'
	'go/internal/reducer/**|go/internal/reducer/projection_helpers.go'
	'go/internal/reducer/**|go/internal/reducer/candidate_loader.go'
	'go/internal/reducer/**|go/internal/reducer/graph_projection_phase_publish.go'
	'go/internal/reducer/**|go/internal/reducer/graph_projection_phase_repair_runner.go'
	# The five files that split off sql_relationships, the one reducer family
	# that had been pinned to two literal filenames. They decide which edges
	# `ifa assert-edges -domain sql_relationships` sees. #6061 moved them into
	# the sqlrelationship subpackage -- deeper, still under the same glob.
	'go/internal/reducer/**|go/internal/reducer/sqlrelationship/sql_relationship_delta_scope.go'
	'go/internal/reducer/**|go/internal/reducer/sqlrelationship/sql_relationship_intents.go'
	'go/internal/reducer/**|go/internal/reducer/sqlrelationship/sql_relationship_metadata.go'
	'go/internal/reducer/**|go/internal/reducer/sqlrelationship/sql_relationship_names.go'
	'go/internal/reducer/**|go/internal/reducer/sqlrelationship/sql_relationship_table_targets.go'
	# Previously-listed literals, now covered by the glob.
	'go/internal/reducer/**|go/internal/reducer/intent.go'
	'go/internal/reducer/**|go/internal/reducer/shared_projection.go'
	'go/internal/reducer/**|go/internal/reducer/graph_projection_phase.go'
	'go/internal/reducer/**|go/internal/reducer/schemadecode/factschema_decode_submodule.go'
	# go/internal/reducer/contract/ is a subpackage, and it arrived on main as
	# its own literal trigger (#6222) while this branch was open. The glob
	# subsumes it, so the literal is gone -- this seam is what proves the
	# subsumption, since "**" crossing a "/" is the one property the whole
	# replacement rests on.
	'go/internal/reducer/**|go/internal/reducer/contract/intent.go'
	# Previously fault-ONLY, and no longer: the glob is on both gates, so
	# these three now arm the determinism matrix as well. Listed here rather
	# than left in ifa_live_gate_fault_only_seams so the widening is written
	# down where the proof runs, not just in a commit message.
	'go/internal/reducer/**|go/internal/reducer/defaults_additive_domains.go'
	'go/internal/reducer/**|go/internal/reducer/defaults_additive_domains_cloud_nodes.go'
	'go/internal/reducer/**|go/internal/reducer/defaults_additive_domains_gcp.go'
	'go/internal/storage/cypher/cloud_resource_node_writer.go|go/internal/storage/cypher/cloud_resource_node_writer.go'
	'go/internal/storage/cypher/cloud_resource_node_writer_teeth_off.go|go/internal/storage/cypher/cloud_resource_node_writer_teeth_off.go'
	'docker-compose.yaml|docker-compose.yaml'
	'testdata/cassettes/gcpcloud/supply-chain-demo.json|testdata/cassettes/gcpcloud/supply-chain-demo.json'
	'testdata/golden/e2e-20repo-snapshot.json|testdata/golden/e2e-20repo-snapshot.json'
	'go/cmd/golden-corpus-gate/main.go|go/cmd/golden-corpus-gate/main.go'
	'go/cmd/golden-corpus-gate/runner.go|go/cmd/golden-corpus-gate/runner.go'
	'go/cmd/golden-corpus-gate/drains.go|go/cmd/golden-corpus-gate/drains.go'
	'go/cmd/golden-corpus-gate/shared.go|go/cmd/golden-corpus-gate/shared.go'
	'go/internal/goldengate/snapshot.go|go/internal/goldengate/snapshot.go'
	'go/internal/goldengate/evaluate_drains.go|go/internal/goldengate/evaluate_drains.go'
	'go/internal/goldengate/report.go|go/internal/goldengate/report.go'
	'go/internal/replay/cassette/**|go/internal/replay/cassette/source.go'
	'go/internal/replay/cassette/**|go/internal/replay/cassette/format.go'
	'go/internal/replay/concurrentreplay/**|go/internal/replay/concurrentreplay/driver.go'
	'go/internal/replay/concurrentreplay/**|go/internal/replay/concurrentreplay/source.go'
	'go/internal/storage/postgres/ingestion.go|go/internal/storage/postgres/ingestion.go'
	'go/internal/storage/postgres/ingestion_queries.go|go/internal/storage/postgres/ingestion_queries.go'
	'go/internal/storage/postgres/ingestion_catalog*.go|go/internal/storage/postgres/ingestion_catalog_cache.go'
	'go/internal/storage/postgres/ingestion_catalog*.go|go/internal/storage/postgres/ingestion_catalog_parse.go'
	'go/internal/storage/postgres/ingestion_backfill_generation_guard.go|go/internal/storage/postgres/ingestion_backfill_generation_guard.go'
	'go/internal/storage/postgres/facts_streaming.go|go/internal/storage/postgres/facts_streaming.go'
	'go/internal/storage/postgres/facts.go|go/internal/storage/postgres/facts.go'
	'go/internal/storage/postgres/migrations/008_shared_projection_intents.sql|go/internal/storage/postgres/migrations/008_shared_projection_intents.sql'
	'go/internal/storage/postgres/migrations/011_shared_projection_acceptance.sql|go/internal/storage/postgres/migrations/011_shared_projection_acceptance.sql'
	'go/internal/storage/postgres/migrations/012_graph_projection_phase_state.sql|go/internal/storage/postgres/migrations/012_graph_projection_phase_state.sql'
	'go/cmd/bootstrap-data-plane/schema_adoption.go|go/cmd/bootstrap-data-plane/schema_adoption.go'
	'go/internal/graph/schema*.go|go/internal/graph/schema.go'
	'go/internal/graph/schema*.go|go/internal/graph/schema_application.go'
	'go/internal/graph/schema*.go|go/internal/graph/schema_execution.go'
	'go/cmd/projector/config.go|go/cmd/projector/config.go'
	'go/cmd/projector/nornicdb_wiring.go|go/cmd/projector/nornicdb_wiring.go'
	'go/internal/projector/service.go|go/internal/projector/service.go'
	'go/internal/projector/service_superseded.go|go/internal/projector/service_superseded.go'
	'go/internal/storage/postgres/projector_queue.go|go/internal/storage/postgres/projector_queue.go'
	'go/internal/storage/postgres/projector_queue_claim_sql.go|go/internal/storage/postgres/projector_queue_claim_sql.go'
	'go/internal/storage/postgres/projector_queue_scan.go|go/internal/storage/postgres/projector_queue_scan.go'
	'go/internal/storage/postgres/projector_queue_sql.go|go/internal/storage/postgres/projector_queue_sql.go'
	'go/internal/storage/nornicdb/**|go/internal/storage/nornicdb/config.go'
	'go/internal/storage/nornicdb/**|go/internal/storage/nornicdb/phase_group_executor.go'
	'go/internal/storage/nornicdb/**|go/internal/storage/nornicdb/phase_group_executor_retract.go'
	'go/internal/storage/cypher/canonical_node_writer_options.go|go/internal/storage/cypher/canonical_node_writer_options.go'
	'go/internal/storage/cypher/instrumented.go|go/internal/storage/cypher/instrumented.go'
	'go/internal/storage/cypher/retrying_executor.go|go/internal/storage/cypher/retrying_executor.go'
	'go/internal/storage/cypher/retryable_error.go|go/internal/storage/cypher/retryable_error.go'
	'go/internal/storage/cypher/timeout_executor.go|go/internal/storage/cypher/timeout_executor.go'
	'go/internal/graphbackpressure/**|go/internal/graphbackpressure/backpressure.go'
	'go/internal/graphbackpressure/**|go/internal/graphbackpressure/materializer_backpressure.go'
	'go/cmd/reducer/observed_service_wiring.go|go/cmd/reducer/observed_service_wiring.go'
	'go/cmd/reducer/neo4j_wiring.go|go/cmd/reducer/neo4j_wiring.go'
	'go/cmd/reducer/reducer_executor_adapters.go|go/cmd/reducer/reducer_executor_adapters.go'
	'go/cmd/reducer/graph_write_backpressure_wiring.go|go/cmd/reducer/graph_write_backpressure_wiring.go'
	'go/cmd/reducer/worker_gauge.go|go/cmd/reducer/worker_gauge.go'
	'go/internal/storage/postgres/reducer_queue.go|go/internal/storage/postgres/reducer_queue.go'
	'go/internal/storage/postgres/reducer_queue_batch.go|go/internal/storage/postgres/reducer_queue_batch.go'
	'go/internal/storage/postgres/reducer_queue_batch_query.go|go/internal/storage/postgres/reducer_queue_batch_query.go'
	'go/internal/storage/postgres/reducer_queue_helpers.go|go/internal/storage/postgres/reducer_queue_helpers.go'
	'go/internal/storage/postgres/reducer_queue_readiness_sql.go|go/internal/storage/postgres/reducer_queue_readiness_sql.go'
	'go/internal/storage/postgres/reducer_queue_validation.go|go/internal/storage/postgres/reducer_queue_validation.go'
	'go/internal/storage/postgres/reducer_queue_ack.go|go/internal/storage/postgres/reducer_queue_ack.go'
	# go/internal/ifa/*.go replaced 12 filenames and two <family>_* globs on
	# both live gates (#6200). Formerly-dark paths first -- the shared Odù
	# machinery every family's derivation runs through, and the two compiled
	# Odù the issue's own follow-up comment named -- then paths the old list
	# did cover, so a narrowed pattern fails here instead of arming nothing.
	'go/internal/ifa/*.go|go/internal/ifa/code_call_family_odu.go'
	'go/internal/ifa/*.go|go/internal/ifa/rationale_family_odu.go'
	'go/internal/ifa/*.go|go/internal/ifa/odu.go'
	'go/internal/ifa/*.go|go/internal/ifa/catalog.go'
	'go/internal/ifa/*.go|go/internal/ifa/expectations.go'
	'go/internal/ifa/*.go|go/internal/ifa/coverage.go'
	'go/internal/ifa/*.go|go/internal/ifa/roundtrip.go'
	'go/internal/ifa/*.go|go/internal/ifa/schema.go'
	'go/internal/ifa/*.go|go/internal/ifa/sql_relationship_odu.go'
	'go/internal/ifa/*.go|go/internal/ifa/repo_dependency_odu.go'
	'go/internal/ifa/*.go|go/internal/ifa/symbol_runtime_family_cassette.go'
	'go/internal/ifa/*.go|go/internal/ifa/catalog_seed.go'
	'go/internal/ifa/*.go|go/internal/ifa/codeowners_family_odu.go'
	'go/internal/ifa/*.go|go/internal/ifa/documentation_family_odu.go'
	'go/internal/ifa/*.go|go/internal/ifa/submodule_pin_family_odu.go'
	'go/internal/ifa/*.go|go/internal/ifa/symbol_runtime_family_odu.go'
	'go/internal/ifa/*.go|go/internal/ifa/repo_dependency_family_catalog.go'
	'go/internal/ifa/*.go|go/internal/ifa/workload_dependency_family_odu.go'
	'go/internal/ifa/materializededges/**|go/internal/ifa/materializededges/materialized_edges_code_calls.go'
	'go/internal/ifa/materializededges/**|go/internal/ifa/materializededges/materialized_edges_documentation.go'
	'testdata/cassettes/documentation/**|testdata/cassettes/documentation/ifa-documentation-family.json'
	'go/internal/ifa/testdata/documentation/**|go/internal/ifa/testdata/documentation/ifa-documentation-family-live-expected-edges.json'
	'sdk/go/factschema/documentation/v1/**|sdk/go/factschema/documentation/v1/shared.go'
	'go/internal/storage/cypher/*documentation*.go|go/internal/storage/cypher/edge_writer_documentation_labels.go'
	'scripts/lib/ifa_*_live*.sh|scripts/lib/ifa_documentation_live.sh'
	'go/internal/ifa/materializededges/**|go/internal/ifa/materializededges/materialized_edges_codeowners.go'
	'sdk/go/factschema/codeowners/v1/**|sdk/go/factschema/codeowners/v1/ownership.go'
	'go/internal/storage/cypher/*codeowners*.go|go/internal/storage/cypher/canonical_codeowners_edges.go'
	'testdata/cassettes/codeowners/**|testdata/cassettes/codeowners/ifa-codeowners-family.json'
	'go/internal/ifa/testdata/codeowners/**|go/internal/ifa/testdata/codeowners/ifa-codeowners-family-expected-edges.json'
	'scripts/lib/ifa_*_live*.sh|scripts/lib/ifa_codeowners_live.sh'
	# submodule_pin_edges (#6002): offline Odù/catalog/cassette landed with no
	# gate trigger at all (`rg -c submodule specs/ci-gates.v1.yaml` returned 0
	# before this row), so this family had never retriggered either live gate
	# on any of these paths -- the gate was dark for its entire surface, not
	# merely for the new live lib. Its Odù and catalog rows moved into the
	# go/internal/ifa/*.go group near the top of this array when #6200
	# replaced the family's filenames with the package glob; what remains
	# here is the family's own non-ifa surface.
	'go/internal/ifa/materializededges/**|go/internal/ifa/materializededges/materialized_edges_submodule_pin.go'
	'sdk/go/factschema/submodule/v1/**|sdk/go/factschema/submodule/v1/pin.go'
	# The SDK-side decode seam (DecodeSubmodulePin, FactKindSubmodulePin)
	# sits directly under sdk/go/factschema/, not submodule/v1/, so the glob
	# above misses it. #6198 covered it with a '*submodule*.go' row here;
	# #6200 replaced that with the package glob, whose rows are grouped near
	# the top of this array (decode_submodule.go and fact_kinds_submodule.go
	# are both in that group).
	'go/internal/storage/cypher/*submodule*.go|go/internal/storage/cypher/canonical_submodule_edges.go'
	'testdata/cassettes/submodulepin/**|testdata/cassettes/submodulepin/ifa-submodule-pin-family.json'
	'go/internal/ifa/testdata/submodulepin/**|go/internal/ifa/testdata/submodulepin/ifa-submodule-pin-family-expected-edges.json'
	'scripts/lib/ifa_*_live*.sh|scripts/lib/ifa_submodule_pin_live.sh'
	'scripts/lib/ifa_*_live*.sh|scripts/lib/ifa_inheritance_live.sh'
	'scripts/lib/ifa_fault_injection_inheritance_cells.sh|scripts/lib/ifa_fault_injection_inheritance_cells.sh'
	'scripts/lib/ifa_*_live*.sh|scripts/lib/ifa_shell_exec_live.sh'
	'scripts/lib/ifa_fault_injection_shell_exec_cells.sh|scripts/lib/ifa_fault_injection_shell_exec_cells.sh'
	# handles_route/runs_in/invokes_cloud_action trio (#5995/#6000/#5997).
	# Measured per-path with `ci-gates select --tier ci-heavy`, not assumed
	# from a sibling: ifa_symbol_runtime_live.sh is sourced by BOTH gates
	# (verify-ifa-determinism.sh directly; verify-ifa-fault-injection.sh
	# transitively via ifa_fault_injection_sources.sh) -- common seam. Its
	# fault-cells sibling is fault-ONLY (see ifa_live_gate_fault_only_seams
	# below): verify-ifa-determinism.sh never sources a *_cells.sh file for
	# any family, so following the inheritance/shell_exec pair's BOTH-gates
	# placement here would have been wrong -- submodule_pin's cells file
	# (fault-only, below) is the correct precedent, not theirs.
	'scripts/lib/ifa_*_live*.sh|scripts/lib/ifa_symbol_runtime_live.sh'
	# One shared cassette + Odù (symbolRuntimeFamilyOdu() is registered in
	# catalog_seed.go's catalogSeed, so it is live-binary-consumed by both
	# gates), three SEPARATE expected-edge directories -- one per family's
	# own exact-set assertion. symbol_runtime_family_cassette.go and
	# go/internal/reducer/{ifa_family_registry_anchor,
	# materialized_edge_family_blocker_shape}_test.go used to be excluded
	# here as Go-test-only, on the grounds that no binary either live gate
	# invokes ever reads them. #6200 retired that carve-out: all three sit
	# inside packages both gates now glob, and file membership rather than
	# call graph is what decides whether an edit reaches the binary. The
	# cassette file has its own row in the ifa package group near the top of
	# this array.
	'testdata/cassettes/symbolruntime/**|testdata/cassettes/symbolruntime/ifa-symbol-runtime-family.json'
	'go/internal/ifa/testdata/handlesroute/**|go/internal/ifa/testdata/handlesroute/ifa-handles-route-family-expected-edges.json'
	'go/internal/ifa/testdata/runsin/**|go/internal/ifa/testdata/runsin/ifa-runs-in-family-expected-edges.json'
	'go/internal/ifa/testdata/invokescloudaction/**|go/internal/ifa/testdata/invokescloudaction/ifa-invokes-cloud-action-family-expected-edges.json'
	'scripts/lib/ifa_*_live*.sh|scripts/lib/ifa_deployable_unit_live.sh'
	'scripts/lib/ifa_*_live*.sh|scripts/lib/ifa_deployable_unit_live_diagnostics.sh'
	'scripts/lib/ifa_*_live*.sh|scripts/lib/ifa_deployable_unit_live_converge.sh'
	'go/cmd/bootstrap-data-plane/main.go|go/cmd/bootstrap-data-plane/main.go'
	'go/cmd/reducer/main.go|go/cmd/reducer/main.go'
	'go/cmd/reducer/run.go|go/cmd/reducer/run.go'
	'go/cmd/reducer/config_projection.go|go/cmd/reducer/config_projection.go'
	'go/internal/storage/cypher/*code_call*.go|go/internal/storage/cypher/canonical_code_call_edges.go'
	'go/internal/storage/cypher/edge_writer_payload.go|go/internal/storage/cypher/edge_writer_payload.go'
	'go/internal/storage/cypher/canonical_instantiates_edges.go|go/internal/storage/cypher/canonical_instantiates_edges.go'
	'go/internal/storage/cypher/edge_writer.go|go/internal/storage/cypher/edge_writer.go'
	'go/internal/storage/cypher/canonical_retract.go|go/internal/storage/cypher/canonical_retract.go'
	'go/internal/storage/cypher/edge_writer_retract*.go|go/internal/storage/cypher/edge_writer_retract.go'
	'go/internal/storage/cypher/edge_writer_retract*.go|go/internal/storage/cypher/edge_writer_retract_scope.go'
	'go/internal/content/writer.go|go/internal/content/writer.go'
	'go/internal/projector/canonical_builder.go|go/internal/projector/canonical_builder.go'
	'go/internal/projector/canonical_codegraph_extract.go|go/internal/projector/canonical_codegraph_extract.go'
	'go/internal/projector/canonical_delta.go|go/internal/projector/canonical_delta.go'
	'go/internal/projector/factschema_decode_codegraph.go|go/internal/projector/factschema_decode_codegraph.go'
	'go/internal/projector/stage_facts.go|go/internal/projector/stage_facts.go'
	'go/internal/projector/canonical.go|go/internal/projector/canonical.go'
	'go/internal/projector/runtime.go|go/internal/projector/runtime.go'
	'go/internal/projector/runtime_reducer_intent.go|go/internal/projector/runtime_reducer_intent.go'
	'go/internal/projector/payload.go|go/internal/projector/payload.go'
	'go/internal/projector/runtime_phase.go|go/internal/projector/runtime_phase.go'
	'go/internal/projector/canonical_entity_identity.go|go/internal/projector/canonical_entity_identity.go'
	'go/internal/projector/runtime_stages.go|go/internal/projector/runtime_stages.go'
	'go/cmd/projector/main.go|go/cmd/projector/main.go'
	'go/cmd/projector/runtime_wiring.go|go/cmd/projector/runtime_wiring.go'
	'go/internal/storage/cypher/canonical_node_writer.go|go/internal/storage/cypher/canonical_node_writer.go'
	'go/internal/storage/cypher/canonical_node_writer_phases.go|go/internal/storage/cypher/canonical_node_writer_phases.go'
	'go/internal/storage/cypher/canonical_node_writer_entities.go|go/internal/storage/cypher/canonical_node_writer_entities.go'
	'go/internal/storage/cypher/canonical_node_cypher.go|go/internal/storage/cypher/canonical_node_cypher.go'
	'go/internal/storage/cypher/canonical_node_writer_retract.go|go/internal/storage/cypher/canonical_node_writer_retract.go'
	'go/internal/storage/cypher/canonical_node_writer_delta_retract.go|go/internal/storage/cypher/canonical_node_writer_delta_retract.go'
	'go/internal/storage/cypher/canonical_node_writer_retract_labels.go|go/internal/storage/cypher/canonical_node_writer_retract_labels.go'
	'go/internal/storage/postgres/code_call_intent_writer.go|go/internal/storage/postgres/code_call_intent_writer.go'
	'go/internal/storage/postgres/facts_active_code_call_symbols.go|go/internal/storage/postgres/facts_active_code_call_symbols.go'
	'go/internal/storage/postgres/facts_filtered.go|go/internal/storage/postgres/facts_filtered.go'
	'go/internal/storage/postgres/facts_payload.go|go/internal/storage/postgres/facts_payload.go'
	'go/internal/storage/postgres/shared_intent_acceptance_writer.go|go/internal/storage/postgres/shared_intent_acceptance_writer.go'
	'go/internal/storage/postgres/shared_projection_acceptance*.go|go/internal/storage/postgres/shared_projection_acceptance.go'
	'go/internal/storage/postgres/shared_projection_acceptance*.go|go/internal/storage/postgres/shared_projection_acceptance_rowcount_test.go'
	'go/internal/storage/postgres/accepted_generation.go|go/internal/storage/postgres/accepted_generation.go'
	'go/internal/storage/postgres/graph_projection_phase_state.go|go/internal/storage/postgres/graph_projection_phase_state.go'
	'go/internal/storage/postgres/reducer_queue_conflict.go|go/internal/storage/postgres/reducer_queue_conflict.go'
	'go/internal/storage/postgres/schema.go|go/internal/storage/postgres/schema.go'
	'go/internal/storage/postgres/schema_bootstrap_lock.go|go/internal/storage/postgres/schema_bootstrap_lock.go'
	'go/internal/storage/postgres/shared_intents*.go|go/internal/storage/postgres/shared_intents.go'
	'go/internal/storage/postgres/shared_intents*.go|go/internal/storage/postgres/shared_intents_upsert.go'
	'go/internal/storage/postgres/shared_intents*.go|go/internal/storage/postgres/shared_intents_history.go'
	'go/internal/storage/postgres/shared_intents*.go|go/internal/storage/postgres/shared_intents_partition_candidates.go'
	'schema/data-plane/postgres/008_shared_projection_intents.sql|schema/data-plane/postgres/008_shared_projection_intents.sql'
	'schema/data-plane/postgres/011_shared_projection_acceptance.sql|schema/data-plane/postgres/011_shared_projection_acceptance.sql'
	'schema/data-plane/postgres/012_graph_projection_phase_state.sql|schema/data-plane/postgres/012_graph_projection_phase_state.sql'
	'go/cmd/reducer/config.go|go/cmd/reducer/config.go'
	'go/cmd/reducer/main_helpers.go|go/cmd/reducer/main_helpers.go'
	'go/internal/replay/canonical.go|go/internal/replay/canonical.go'
	'go/internal/storage/cypher/canonical_rationale_edges.go|go/internal/storage/cypher/canonical_rationale_edges.go'
	'go/internal/storage/cypher/edge_writer_rationale_labels.go|go/internal/storage/cypher/edge_writer_rationale_labels.go'
	'go/internal/storage/cypher/canonical.go|go/internal/storage/cypher/canonical.go'
	'go/internal/storage/cypher/backpressure_executor.go|go/internal/storage/cypher/backpressure_executor.go'
	'testdata/cassettes/rationale/**|testdata/cassettes/rationale/ifa-rationale-family.json'
	'testdata/cassettes/rationale/**|testdata/cassettes/rationale/ifa-rationale-family-delta.json'
	'go/internal/ifa/testdata/rationale/**|go/internal/ifa/testdata/rationale/ifa-rationale-family-expected-edges.json'
	'go/internal/ifa/testdata/rationale/**|go/internal/ifa/testdata/rationale/ifa-rationale-family-delta-live-expected-records.json'
	'scripts/lib/ifa_*_live*.sh|scripts/lib/ifa_rationale_live.sh'
	'go/internal/storage/cypher/canonical_implements_edges.go|go/internal/storage/cypher/canonical_implements_edges.go'
	'go/internal/storage/cypher/canonical_inheritance_retract.go|go/internal/storage/cypher/canonical_inheritance_retract.go'
	'go/internal/storage/cypher/edge_writer_inheritance_labels.go|go/internal/storage/cypher/edge_writer_inheritance_labels.go'
	'go/internal/storage/cypher/edge_writer_shell_exec.go|go/internal/storage/cypher/edge_writer_shell_exec.go'
	'testdata/cassettes/inheritance/**|testdata/cassettes/inheritance/ifa-inheritance-family.json'
	'testdata/cassettes/shellexec/**|testdata/cassettes/shellexec/ifa-shell-exec-family.json'
	'go/internal/ifa/testdata/inheritance/**|go/internal/ifa/testdata/inheritance/ifa-inheritance-family-expected-edges.json'
	'go/internal/ifa/testdata/shellexec/**|go/internal/ifa/testdata/shellexec/ifa-shell-exec-family-expected-edges.json'
	'scripts/lib/ifa_family_*.sh|scripts/lib/ifa_family_fixtures.sh'
	'scripts/lib/ifa_*_live*.sh|scripts/lib/ifa_repo_dependency_live.sh'
	'scripts/lib/ifa_fault_injection_repo_dependency_cells.sh|scripts/lib/ifa_fault_injection_repo_dependency_cells.sh'
	'scripts/lib/test-ifa-fault-injection-repo-dependency-cases.sh|scripts/lib/test-ifa-fault-injection-repo-dependency-cases.sh'
	'testdata/cassettes/repodependency/**|testdata/cassettes/repodependency/ifa-repo-dependency-family.json'
	'go/internal/ifa/testdata/repodependency/**|go/internal/ifa/testdata/repodependency/ifa-repo-dependency-family-expected-edges.json'
	# kubernetes_namespace_environment + iam_instance_profile_role (#6228, #6309):
	# moved here from the determinism-only table when the fault cells landed.
	'go/internal/storage/cypher/kubernetes_namespace_node_writer.go|go/internal/storage/cypher/kubernetes_namespace_node_writer.go'
	'go/internal/storage/cypher/iam_instance_profile_role_edge_writer.go|go/internal/storage/cypher/iam_instance_profile_role_edge_writer.go'
	'testdata/cassettes/kubernetesnamespaceenvironment/**|testdata/cassettes/kubernetesnamespaceenvironment/ifa-kubernetes-namespace-environment-family.json'
	'testdata/cassettes/iaminstanceprofilerole/**|testdata/cassettes/iaminstanceprofilerole/ifa-iam-instance-profile-role-family.json'
	'go/internal/ifa/testdata/kubernetesnamespaceenvironment/**|go/internal/ifa/testdata/kubernetesnamespaceenvironment/ifa-kubernetes-namespace-environment-family-expected-edges.json'
	'go/internal/ifa/testdata/iaminstanceprofilerole/**|go/internal/ifa/testdata/iaminstanceprofilerole/ifa-iam-instance-profile-role-family-expected-edges.json'
	'scripts/lib/ifa_fault_injection_kubernetes_namespace_environment_cells.sh|scripts/lib/ifa_fault_injection_kubernetes_namespace_environment_cells.sh'
	'scripts/lib/ifa_fault_injection_iam_instance_profile_role_cells.sh|scripts/lib/ifa_fault_injection_iam_instance_profile_role_cells.sh'
	'go/internal/collector/gitrepo/git_fact_builder*.go|go/internal/collector/gitrepo/git_fact_builder.go'
	'go/internal/collector/gitrepo/git_fact_builder*.go|go/internal/collector/gitrepo/git_fact_builder_delta.go'
	'go/internal/collector/gitrepo/git_followup_facts.go|go/internal/collector/gitrepo/git_followup_facts.go'
	'go/internal/projector/canonical_import_extract.go|go/internal/projector/canonical_import_extract.go'
	'go/internal/projector/workload_dependency_cassette_admission_test.go|go/internal/projector/workload_dependency_cassette_admission_test.go'
	'go/internal/storage/postgres/ingestion_reopen*.go|go/internal/storage/postgres/ingestion_reopen_correlation.go'
	'go/internal/storage/postgres/ingestion_reopen*.go|go/internal/storage/postgres/ingestion_reopen_deployment_mapping.go'
	'go/internal/storage/cypher/materialized_edge_repo_dependency.go|go/internal/storage/cypher/materialized_edge_repo_dependency.go'
	'go/internal/storage/cypher/canonical_relationships.go|go/internal/storage/cypher/canonical_relationships.go'
	'specs/ifa-materialized-edge-coverage.v1.yaml|specs/ifa-materialized-edge-coverage.v1.yaml'
	# #6147 PR-0: sourced directly by verify-ifa-determinism.sh and
	# transitively by verify-ifa-fault-injection.sh (via
	# ifa_fault_generic_cells.sh); a wrong row changes what both live gates
	# actually drive/assert or which blocker a kill cell takes.
	'scripts/lib/ifa_family_*.sh|scripts/lib/ifa_family_registry.sh'
)

ifa_live_gate_fault_only_seams=(
	# Split OUT of test-ifa-fault-injection-repo-dependency-cases.sh (which
	# sits in ifa_live_gate_common_seams above -- an inherited both-gates
	# wiring this pass did not revisit) once that file crossed the 500-line
	# cap. This sibling is genuinely fault-only: it is sourced ONLY by
	# scripts/test-verify-ifa-fault-injection.sh, never by
	# test-verify-ifa-determinism.sh, matching the same reasoning applied to
	# ifa_fault_injection_symbol_runtime_cells.sh below.
	'scripts/lib/test-ifa-fault-injection-*.sh|scripts/lib/test-ifa-fault-injection-repo-dependency-lease-cases.sh'
	'scripts/lib/ifa_fault_*.sh|scripts/lib/ifa_fault_injection_collateral_nodes.sh'
	'scripts/lib/ifa_fault_*.sh|scripts/lib/ifa_fault_injection_documentation_cells.sh'
	'scripts/lib/ifa_fault_*.sh|scripts/lib/ifa_fault_injection_documentation_ack_barrier.sh'
	'scripts/lib/ifa_fault_*.sh|scripts/lib/ifa_fault_injection_documentation_ack_setup.sh'
	'scripts/lib/test-ifa-fault-injection-*.sh|scripts/lib/test-ifa-fault-injection-documentation-cases.sh'
	'scripts/lib/test-ifa-fault-injection-*.sh|scripts/lib/test-ifa-fault-injection-generic-table-lock-cases.sh'
	'scripts/lib/test-ifa-fault-injection-*.sh|scripts/lib/test-ifa-fault-injection-generic-shared-intent-lock-cases.sh'
	'scripts/lib/test-ifa-fault-injection-*.sh|scripts/lib/test-ifa-fault-injection-generic-family-drive-cases.sh'
	'scripts/lib/test-ifa-fault-injection-*.sh|scripts/lib/test-ifa-fault-injection-generic-runner-lease-hold-cases.sh'
	'scripts/lib/test-ifa-fault-injection-*.sh|scripts/lib/test-ifa-fault-injection-generic-modules.sh'
	'scripts/lib/ifa_fault_*.sh|scripts/lib/ifa_fault_generic_shared_intent_lock.sh'
	'scripts/lib/ifa_fault_*.sh|scripts/lib/ifa_fault_injection_codeowners_cells.sh'
	'scripts/lib/test-ifa-fault-injection-*.sh|scripts/lib/test-ifa-fault-injection-codeowners-cases.sh'
	'scripts/lib/ifa_fault_*.sh|scripts/lib/ifa_fault_injection_submodule_pin_cells.sh'
	'scripts/lib/test-ifa-fault-injection-*.sh|scripts/lib/test-ifa-fault-injection-submodule-pin-cases.sh'
	# k8s + IAM (#6309) fault-only cases files, sourced solely by the fault
	# verifier like the codeowners pair above: pinned here so a matcher
	# refactor cannot silently drop their gate selection.
	'scripts/lib/test-ifa-fault-injection-*.sh|scripts/lib/test-ifa-fault-injection-kubernetes-namespace-environment-cases.sh'
	'scripts/lib/test-ifa-fault-injection-*.sh|scripts/lib/test-ifa-fault-injection-iam-instance-profile-role-cases.sh'
	# handles_route/runs_in/invokes_cloud_action trio (#5995/#6000/#5997):
	# fault-only, same shape as submodule_pin's cells file immediately
	# above -- verify-ifa-determinism.sh never sources a *_cells.sh file for
	# any family, so this belongs here and NOT in ifa_live_gate_common_seams.
	'scripts/lib/ifa_fault_*.sh|scripts/lib/ifa_fault_injection_symbol_runtime_cells.sh'
	'scripts/lib/ifa_fault_*.sh|scripts/lib/ifa_fault_injection_deployable_unit_cells.sh'
	'scripts/lib/ifa_fault_*.sh|scripts/lib/ifa_fault_injection_deployable_unit_lock.sh'
	'scripts/lib/test-ifa-fault-injection-*.sh|scripts/lib/test-ifa-fault-injection-deployable-unit-cases.sh'
	'scripts/lib/test-ifa-fault-injection-*.sh|scripts/lib/test-ifa-fault-injection-deployable-unit-ordering-cases.sh'
	'scripts/lib/test-ifa-fault-injection-*.sh|scripts/lib/test-ifa-fault-injection-marker-cases.sh'
	'scripts/lib/test-ifa-fault-injection-*.sh|scripts/lib/test-ifa-fault-injection-documentation-ack-barrier-cases.sh'
	'scripts/lib/test-ifa-fault-injection-*.sh|scripts/lib/test-ifa-fault-injection-documentation-ack-cleanup-cases.sh'
	'scripts/lib/test-ifa-fault-injection-*.sh|scripts/lib/test-ifa-fault-injection-code-call-cases.sh'
	'scripts/lib/test-ifa-fault-injection-*.sh|scripts/lib/test-ifa-fault-injection-entrypoint-cases.sh'
	'go/internal/storage/cypher/fault_executor_marker.go|go/internal/storage/cypher/fault_executor_marker.go'
	'go/internal/storage/cypher/canonical_node_writer_metadata.go|go/internal/storage/cypher/canonical_node_writer_metadata.go'
	'go/internal/projector/scope_generation_intents.go|go/internal/projector/scope_generation_intents.go'
	'go/internal/projector/reducer_intent_fact_index.go|go/internal/projector/reducer_intent_fact_index.go'
	'go/internal/projector/gcp/resource_materialization_intents.go|go/internal/projector/gcp/resource_materialization_intents.go'
	'go/internal/projector/gcp/relationship_materialization_intents.go|go/internal/projector/gcp/relationship_materialization_intents.go'
	'go/internal/projector/security/group_reachability_intents.go|go/internal/projector/security/group_reachability_intents.go'
	'go/cmd/reducer/canonical_graph_writers.go|go/cmd/reducer/canonical_graph_writers.go'
	'go/internal/graphowner/family_writers.go|go/internal/graphowner/family_writers.go'
	'go/internal/graphowner/gated_writer.go|go/internal/graphowner/gated_writer.go'
	'go/internal/storage/postgres/graph_node_owner_store.go|go/internal/storage/postgres/graph_node_owner_store.go'
	'scripts/lib/ifa_fault_*.sh|scripts/lib/ifa_fault_injection_rationale_cells.sh'
	'scripts/lib/test-ifa-fault-injection-*.sh|scripts/lib/test-ifa-fault-injection-rationale-cases.sh'
	# #6147 PR-0 family-registry extraction: the generic per-family fault
	# cells, the shard-dispatch mechanism verify-ifa-fault-injection.sh uses,
	# and that mechanism's own static mirror module. All three execute only
	# inside the fault-injection gate/mirror.
	'scripts/lib/ifa_fault_*.sh|scripts/lib/ifa_fault_shard.sh'
	'scripts/lib/ifa_fault_*.sh|scripts/lib/ifa_fault_generic_cells.sh'
	'scripts/lib/test-ifa-fault-injection-*.sh|scripts/lib/test-ifa-fault-injection-shard-cases.sh'
	# The two files that were DARK on main (#6200): both split out under the
	# 500-line cap, both absent from the registry and from
	# ifa-determinism-gate.yml, so editing either started no Ifá job at all.
	# They are pinned here and not merely fixed, because the enumeration
	# that lost them looked complete the whole time it was wrong.
	'scripts/lib/test-ifa-fault-injection-*.sh|scripts/lib/test-ifa-fault-injection-deployable-unit-kill-isolation-cases.sh'
	'scripts/lib/test-ifa-fault-injection-*.sh|scripts/lib/test-ifa-fault-injection-generic-runner-lease-audit-cases.sh'
)
