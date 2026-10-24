#!/usr/bin/env bash
# shellcheck shell=bash
# Committed materialized-edge family fixtures shared by the two Ifá live gates
# (scripts/verify-ifa-determinism.sh and scripts/verify-ifa-fault-injection.sh).
#
# Both gates drive the SAME committed family cassettes and assert against the
# SAME expected-set files, and both had byte-identical copies of these paths
# and of the fail-fast existence guards below. Two sibling families landing at
# once (#5993 deployable_unit_edges and #5998 rationale_edges) pushed both
# copies past the repo's 500-line file cap, which is what surfaced the
# duplication. One definition means a new family is registered once and cannot
# drift between the determinism gate and the fault-injection gate.
#
# Sourcing contract: source this AFTER `repo_root` is set. Every path below is
# resolved at source time, exactly as the inline blocks did.

# SQL relationship family cassette (#5351): a committed cassette (unlike the
# generated synth cassette) that exercises the reducer's SQL relationship edge
# materialization across all nine materialized edge types (QUERIES_TABLE,
# READS_FROM, HAS_COLUMN, TRIGGERS, EXECUTES, INDEXES, MIGRATES,
# REFERENCES_TABLE, WRITES_TO). It is driven into every cell of both gates
# alongside the demo-org + synth-multiscope cassettes, and the
# materialized_edges:sql_relationships manifest row's proof_gate claim is
# backed by an ADDITIONAL per-cell absolute-set assertion (the `ifa
# assert-edges` call after each drain): digest equality across N, or against a
# fault-free baseline, cannot catch a family silently empty in ALL cells; the
# absolute expected set can.
sql_cassette="${repo_root}/testdata/cassettes/sqlrelationships/ifa-sql-family.json"
sql_expected_edges="${repo_root}/go/internal/ifa/testdata/sqlrelationships/ifa-sql-family-expected-edges.json"
sql_delta_cassette="${repo_root}/testdata/cassettes/sqlrelationships/ifa-sql-family-delta.json"
sql_delta_expected_edges="${repo_root}/go/internal/ifa/testdata/sqlrelationships/ifa-sql-family-delta-live-expected-edges.json"

# code_call_edges family cassette (#5991): the CALLS edge family, exact-set
# asserted at five edges by both gates.
code_call_cassette="${repo_root}/testdata/cassettes/codecalls/ifa-code-call-family.json"
code_call_expected_edges="${repo_root}/go/internal/ifa/testdata/codecalls/ifa-code-call-family-expected-edges.json"

# rationale_edges family cassette (#5998): the EXPLAINS edge family. Driven
# into every cell of both gates; the delta cassette carries the generation-2
# retract that must converge to exact-one EXPLAINS plus the exact surviving
# Charge node, asserted against the full expected-record set (not a digest).
rationale_cassette="${repo_root}/testdata/cassettes/rationale/ifa-rationale-family.json"
rationale_expected_edges="${repo_root}/go/internal/ifa/testdata/rationale/ifa-rationale-family-expected-edges.json"
rationale_delta_cassette="${repo_root}/testdata/cassettes/rationale/ifa-rationale-family-delta.json"
rationale_delta_expected_records="${repo_root}/go/internal/ifa/testdata/rationale/ifa-rationale-family-delta-live-expected-records.json"

# documentation_edges family cassette (#5994): a committed cassette exercising
# the reducer's DOCUMENTS edge materialization, including the SqlTable-target
# case (batchCanonicalDocumentationEntityEdgeCypher's MATCH label alternation).
# Driven into every cell alongside the SQL + code-call families; on the
# fault-injection gate cell_killworker_documentation and
# cell_failgraphwrite_documentation back its manifest row's proof_gate claim.
documentation_cassette="${repo_root}/testdata/cassettes/documentation/ifa-documentation-family.json"
documentation_expected_edges="${repo_root}/go/internal/ifa/testdata/documentation/ifa-documentation-family-live-expected-edges.json"

# deployable_unit_edges family cassette (#5993). On the determinism gate this
# is driven in its OWN standalone cell after the N-loop, not into the shared
# N={1,2,4} cells -- see scripts/lib/ifa_deployable_unit_live.sh's header for
# why (a bootstrap-index maintenance pass is required for this family to
# materialize anything, and folding that into the shared loop would move every
# other family's digest terminal for a reason unrelated to what that loop
# tests). On the fault-injection gate it is driven only by the three
# deployable_unit-targeted cells, never by drive_all_cassettes.
deployable_unit_cassette="${repo_root}/testdata/cassettes/deployableunit/ifa-deployable-unit-family.json"
deployable_unit_expected_edges="${repo_root}/go/internal/ifa/testdata/deployableunit/ifa-deployable-unit-family-expected-edges.json"

# codeowners_ownership_edges family cassette (#5992): the DECLARES_CODEOWNER
# edge family, exact-set asserted at five edges by both gates. Driven uniformly
# into every N-cell on the determinism gate (a plain reducer family needing no
# maintenance pass), and only by the three codeowners-targeted cells on the
# fault-injection gate -- never by drive_all_cassettes, per that driver's rule
# that a new family cassette must not join the shared drive.
codeowners_cassette="${repo_root}/testdata/cassettes/codeowners/ifa-codeowners-family.json"
codeowners_expected_edges="${repo_root}/go/internal/ifa/testdata/codeowners/ifa-codeowners-family-expected-edges.json"

# repo_dependency is resolver-owned and requires a fixture-only Platform
# prerequisite after its first, deliberately gated drain.
repo_dependency_cassette="${repo_root}/testdata/cassettes/repodependency/ifa-repo-dependency-family.json"
repo_dependency_expected_edges="${repo_root}/go/internal/ifa/testdata/repodependency/ifa-repo-dependency-family-expected-edges.json"
# submodule_pin_edges family cassette (#6002): the PINS_SUBMODULE edge family,
# exact-set asserted by both gates. Driven uniformly into every N-cell on the
# determinism gate (a plain reducer family needing no maintenance pass), and
# only by the three submodule-pin-targeted cells on the fault-injection gate
# -- never by drive_all_cassettes, per that driver's rule that a new family
# cassette must not join the shared drive.
submodule_pin_cassette="${repo_root}/testdata/cassettes/submodulepin/ifa-submodule-pin-family.json"
submodule_pin_expected_edges="${repo_root}/go/internal/ifa/testdata/submodulepin/ifa-submodule-pin-family-expected-edges.json"

# inheritance_edges family cassette (#5996): the INHERITS / IMPLEMENTS /
# OVERRIDES / ALIASES edge family, exact-set asserted at five edges by both
# gates -- one per type plus a second ALIASES, so a regression that keeps only
# the INHERITS template cannot pass. Driven uniformly into every N-cell on the
# determinism gate (a plain reducer family needing no maintenance pass), and
# only by its own targeted cells on the fault-injection gate -- never by
# drive_all_cassettes, per that driver's rule that a new family cassette must
# not join the shared drive.
inheritance_cassette="${repo_root}/testdata/cassettes/inheritance/ifa-inheritance-family.json"
inheritance_expected_edges="${repo_root}/go/internal/ifa/testdata/inheritance/ifa-inheritance-family-expected-edges.json"

# shell_exec family cassette (#6001): the EXECUTES_SHELL edge family, exact-set
# asserted at two edges by both gates. Two rather than three because
# ExtractShellExecRows collapses an exact-duplicate command through its own
# edgeKey dedup; the exact-set assertion is what makes that collapse provable
# rather than assumed. Same drive placement as the families above: uniform on
# the determinism N-cells, targeted-only on the fault gate, never
# drive_all_cassettes.
shell_exec_cassette="${repo_root}/testdata/cassettes/shellexec/ifa-shell-exec-family.json"
shell_exec_expected_edges="${repo_root}/go/internal/ifa/testdata/shellexec/ifa-shell-exec-family-expected-edges.json"

# workload_dependency is maintenance-backed too, but its prerequisite is the
# three Repository DEPENDS_ON edges resolved from this family's own cassette.
workload_dependency_cassette="${repo_root}/testdata/cassettes/workloaddependency/ifa-workload-dependency-family.json"
workload_dependency_expected_edges="${repo_root}/go/internal/ifa/testdata/workloaddependency/ifa-workload-dependency-family-expected-edges.json"
workload_dependency_repo_expected_edges="${repo_root}/go/internal/ifa/testdata/workloaddependency/ifa-workload-dependency-family-repo-prerequisite-expected-edges.json"

# symbol-runtime trio cassette (#5995 handles_route / #6000 runs_in / #5997
# invokes_cloud_action): ONE shared cassette drives all three families --
# they come from the same production entry point,
# reducer.ExtractSymbolRuntimeIntentRows, called inside
# CodeCallMaterializationHandler.Handle. Same drive placement as the plain
# reducer families above: uniform on the determinism N-cells (via the
# registry's shared drive_fn), targeted-only (this trio's own cells) on the
# fault gate, never drive_all_cassettes. Three SEPARATE expected-edge files
# because each family asserts its own relationship type's exact set.
symbol_runtime_cassette="${repo_root}/testdata/cassettes/symbolruntime/ifa-symbol-runtime-family.json"
handles_route_expected_edges="${repo_root}/go/internal/ifa/testdata/handlesroute/ifa-handles-route-family-expected-edges.json"
runs_in_expected_edges="${repo_root}/go/internal/ifa/testdata/runsin/ifa-runs-in-family-expected-edges.json"
invokes_cloud_action_expected_edges="${repo_root}/go/internal/ifa/testdata/invokescloudaction/ifa-invokes-cloud-action-family-expected-edges.json"

# kubernetes_namespace_environment, iam_instance_profile_role, iam_can_assume,
# iam_can_perform, and workload_cloud_relationship (#6228) are the
# DIRECT-materialization families in either gate. Every family above reaches the graph through the ordering-safe
# shared-projection intent path; the reducer writes these five straight to a
# go/internal/storage/cypher writer with no intent row in between.
#
# That difference shows up here in one concrete way: none needs a
# shared_followup fact to get its handler scheduled. The projector fans a
# kubernetes_namespace_materialization /
# iam_instance_profile_role_materialization / iam_can_assume_materialization
# work item out of the ordinary source-local drain, confirmed against a live
# stack before these cassettes were committed. The codeowners cassette needs
# such a fact and says so in capitals; these must not carry one, since it would
# enqueue a domain no handler owns.
#
# All five cassettes are projections of their compiled Odù rather than
# second, independently written fixtures, and
# TestDirectFamilyCassettesMatchTheirCompiledOdu holds them to it.
#
# Each cassette owns its scope: scopes carry one ACTIVE generation
# (scope.active_generation_id), so driving a second generation into an
# occupied scope supersedes the first family's generation -- its handler
# never runs and the sibling exact-set assert fails with zero edges. The
# iam_can_assume cassette first shipped sharing aws:eshu-fixture-account
# with iam_instance_profile_role and did exactly that (diagnosed live
# 2026-09-27); it now drives aws:eshu-fixture-can-assume-account.
kubernetes_namespace_environment_cassette="${repo_root}/testdata/cassettes/kubernetesnamespaceenvironment/ifa-kubernetes-namespace-environment-family.json"
kubernetes_namespace_environment_expected_edges="${repo_root}/go/internal/ifa/testdata/kubernetesnamespaceenvironment/ifa-kubernetes-namespace-environment-family-expected-edges.json"
iam_instance_profile_role_cassette="${repo_root}/testdata/cassettes/iaminstanceprofilerole/ifa-iam-instance-profile-role-family.json"
iam_instance_profile_role_expected_edges="${repo_root}/go/internal/ifa/testdata/iaminstanceprofilerole/ifa-iam-instance-profile-role-family-expected-edges.json"
iam_can_assume_cassette="${repo_root}/testdata/cassettes/iamcanassume/ifa-iam-can-assume-family.json"
iam_can_assume_expected_edges="${repo_root}/go/internal/ifa/testdata/iamcanassume/ifa-iam-can-assume-family-expected-edges.json"
iam_can_perform_cassette="${repo_root}/testdata/cassettes/iamcanperform/ifa-iam-can-perform-family.json"
iam_can_perform_expected_edges="${repo_root}/go/internal/ifa/testdata/iamcanperform/ifa-iam-can-perform-family-expected-edges.json"
workload_cloud_relationship_cassette="${repo_root}/testdata/cassettes/workloadcloudrelationship/ifa-workload-cloud-relationship-family.json"
workload_cloud_relationship_expected_edges="${repo_root}/go/internal/ifa/testdata/workloadcloudrelationship/ifa-workload-cloud-relationship-family-expected-edges.json"
iam_escalation_cassette="${repo_root}/testdata/cassettes/iamescalation/ifa-iam-escalation-family.json"
iam_escalation_expected_edges="${repo_root}/go/internal/ifa/testdata/iamescalation/ifa-iam-escalation-family-expected-edges.json"
ec2_uses_profile_cassette="${repo_root}/testdata/cassettes/ec2usesprofile/ifa-ec2-uses-profile-family.json"
ec2_uses_profile_expected_edges="${repo_root}/go/internal/ifa/testdata/ec2usesprofile/ifa-ec2-uses-profile-family-expected-edges.json"
s3_logs_to_cassette="${repo_root}/testdata/cassettes/s3logsto/ifa-s3-logs-to-family.json"
s3_logs_to_expected_edges="${repo_root}/go/internal/ifa/testdata/s3logsto/ifa-s3-logs-to-family-expected-edges.json"
authenticates_vault_role_cassette="${repo_root}/testdata/cassettes/authenticatesvaultrole/ifa-authenticates-vault-role-family.json"
authenticates_vault_role_expected_edges="${repo_root}/go/internal/ifa/testdata/authenticatesvaultrole/ifa-authenticates-vault-role-family-expected-edges.json"
authenticates_vault_role_live_expected_edges="${repo_root}/go/internal/ifa/testdata/authenticatesvaultrole/ifa-authenticates-vault-role-family-expected-edges-live.json"
uses_vault_policy_cassette="${repo_root}/testdata/cassettes/usesvaultpolicy/ifa-uses-vault-policy-family.json"
uses_vault_policy_expected_edges="${repo_root}/go/internal/ifa/testdata/usesvaultpolicy/ifa-uses-vault-policy-family-expected-edges.json"
kubernetes_correlation_cassette="${repo_root}/testdata/cassettes/kubernetescorrelation/ifa-kubernetes-correlation-family.json"
kubernetes_correlation_expected_edges="${repo_root}/go/internal/ifa/testdata/kubernetescorrelation/ifa-kubernetes-correlation-family-expected-edges.json"
# semantic_entity_containment family cassette (#8359, #6228 arbiter verdict
# (a)): the repo-scoped four-edge CONTAINS family, driven through the shared
# N={1,2,4} determinism cell via registry row 22. Like every cassette
# above, a projection of its compiled Odù -- and
# TestDirectFamilyCassettesMatchTheirCompiledOdu holds it to that -- carrying
# the repository fact, the three typed file facts the File endpoints project
# from, and the six content entities the semantic subset admits four of. The
# three canonical-owned content entities carry the exact CanonicalEntityID
# derivation the projector recomputes (#8359 ruling); the NornicDB
# expected-edge variant records the canonical writer's stamp on those three
# pairs (see the assert fn in scripts/lib/ifa_direct_family_live.sh).
semantic_entity_containment_cassette="${repo_root}/testdata/cassettes/semanticentitycontainment/ifa-semantic-entity-containment-family.json"
semantic_entity_containment_expected_edges="${repo_root}/go/internal/ifa/testdata/semanticentitycontainment/ifa-semantic-entity-containment-family-expected-edges.json"
semantic_entity_containment_expected_edges_nornicdb="${repo_root}/go/internal/ifa/testdata/semanticentitycontainment/ifa-semantic-entity-containment-family-expected-edges.nornicdb.json"
# cloud_resource (#8358) is the predicate-scoped direct family: one cassette
# carrying THREE scope generations (one per provider writer), each owning
# its scope like every sibling above.
cloud_resource_cassette="${repo_root}/testdata/cassettes/cloudresource/ifa-cloud-resource-family.json"
cloud_resource_expected_edges="${repo_root}/go/internal/ifa/testdata/cloudresource/ifa-cloud-resource-family-expected-edges.json"
# built_from (#6228 slice C live leaf): a projection of the compiled
# BuiltFromFamilyOdu by construction (TestDirectFamilyCassettesMatchTheirCompiledOdu
# holds it to that) -- one repository fact, four OCI manifest facts (two
# edge-producing builds plus the no-build-evidence and unscanned-builder
# non-producers), and the deploy-only Kubernetes reference proving a runtime
# reference never confers build provenance (#5796). The single git
# repository scope owns its own ACTIVE generation, like every sibling above.
built_from_cassette="${repo_root}/testdata/cassettes/builtfrom/ifa-built-from-family.json"
built_from_expected_edges="${repo_root}/go/internal/ifa/testdata/builtfrom/ifa-built-from-family-expected-edges.json"
# cloud_resource_container_image (#6228 slice D live leaf): a projection of
# the compiled CloudResourceContainerImageFamilyOdu by construction
# (TestDirectFamilyCassettesMatchTheirCompiledOdu holds it to that) -- five
# scanned aws_resource facts, seven aws_relationship facts, and one OCI
# image manifest for the shared digest (the canonical projector
# materializes the target ContainerImage node from it; without the node
# the image writer drops both edge rows as target_not_materialized).
# Two edge-producing lambda_function_uses_image facts on one shared digest
# plus five deliberate non-producers (tag-only ECS, no-digest zip,
# unparseable tag-only ref, unscanned ghost source, foreign verb). The
# single aws region scope owns its own ACTIVE generation, like every
# sibling above.
cloud_resource_container_image_cassette="${repo_root}/testdata/cassettes/cloudresourcecontainerimage/ifa-cloud-resource-container-image-family.json"
cloud_resource_container_image_expected_edges="${repo_root}/go/internal/ifa/testdata/cloudresourcecontainerimage/ifa-cloud-resource-container-image-family-expected-edges.json"
# crossplane_satisfied_by (#6228 crossplane live leaf): a projection of the
# compiled CrossplaneSatisfiedByFamilyOdu by construction
# (TestDirectFamilyCassettesMatchTheirCompiledOdu holds it to that) -- two
# repository facts, ten typed file facts (one per referenced path) and ten
# content_entity facts (six Claim candidates in the claims scope, four XRDs
# in the platform scope). The repo/file facts are load-bearing, not padding:
# the entities phase writes nodes with containment (MATCH File first, and
# File hangs off Repository), so without them the entity statements complete
# having written nothing. Two edge-producing claims join
# (database.example.com, PostgreSQL) against the single postgresql XRD;
# five deliberate non-producer branches cover the ambiguous-match, zero-match,
# core-group, cluster-scoped-XRD and tombstone branches. Both git scopes
# own their per-scope generation and carry repo_id in scope metadata (the
# projector keys content materialization off it); the platform generation
# must be ACTIVE before the handler's cross-scope XRD load can see its
# candidates.
crossplane_satisfied_by_cassette="${repo_root}/testdata/cassettes/crossplanesatisfiedby/ifa-crossplane-satisfied-by-family.json"
crossplane_satisfied_by_expected_edges="${repo_root}/go/internal/ifa/testdata/crossplanesatisfiedby/ifa-crossplane-satisfied-by-family-expected-edges.json"
# derived_from (#6228 derived_from live leaf): a projection of the compiled
# DerivedFromFamilyOdu by construction
# (TestDirectFamilyCassettesMatchTheirCompiledOdu holds it to that) -- one
# repository fact, one single-base Dockerfile fact, four OCI manifest facts
# (the base, two edge-producing children, and the deploy-only third-party
# image) and the deploy-only Kubernetes reference proving a runtime
# reference never confers derivation. The single git repository scope owns
# its own ACTIVE generation, like every sibling above.
derived_from_cassette="${repo_root}/testdata/cassettes/derivedfrom/ifa-derived-from-family.json"
derived_from_expected_edges="${repo_root}/go/internal/ifa/testdata/derivedfrom/ifa-derived-from-family-expected-edges.json"
# observability_coverage (#6228 observability_coverage live leaf): a projection
# of the compiled ObservabilityCoverageFamilyOdu by construction
# (TestDirectFamilyCassettesMatchTheirCompiledOdu holds it to that) -- five
# edge-producing observability objects (one alarm, one composite alarm, one
# dashboard, one log group, one X-Ray sampling rule, one per coverage
# signal), five scanned CloudResource targets (four resolved by ARN
# dimension, the dashboard-to-database pair by bare id), and
# three deliberate non-producers (metric-name-only alarm, service-name X-Ray
# rule, alarm naming an unscanned target). The single aws account scope owns
# its own ACTIVE generation, like every sibling above.
observability_coverage_cassette="${repo_root}/testdata/cassettes/obscoverage/ifa-observability-coverage-family.json"
observability_coverage_expected_edges="${repo_root}/go/internal/ifa/testdata/obscoverage/ifa-observability-coverage-family-expected-edges.json"
# code_interproc_evidence (#6228 code_interproc_evidence live leaf): a
# projection of the compiled CodeInterprocEvidenceFamilyOdu by construction
# (TestDirectFamilyCassettesMatchTheirCompiledOdu holds it to that) -- one
# repository fact, two typed file facts (one per referenced path) and four
# content_entity facts (the Function endpoints of the two live findings).
# The repo/file facts are load-bearing, not padding: the entities phase
# writes nodes with containment (MATCH File first, and File hangs off
# Repository), and the edge writer MATCHes both endpoint Function nodes, so
# without them every statement completes having written nothing. Two
# edge-producing findings join their CanonicalEntityID-resolved endpoints
# (credential to payment_capture, http_request to search_exec with the
# two-step why-trail); the tombstoned legacy finding resolves to nothing.
# The single git scope owns its generation and carries repo_id in scope
# metadata (the projector keys content materialization off it).
code_interproc_evidence_cassette="${repo_root}/testdata/cassettes/codeinterproc/ifa-code-interproc-evidence-family.json"
code_interproc_evidence_expected_edges="${repo_root}/go/internal/ifa/testdata/codeinterproc/ifa-code-interproc-evidence-family-expected-edges.json"
# code_taint_evidence (#6228 code_taint_evidence live leaf): a
# projection of the compiled CodeTaintEvidenceFamilyOdu by construction
# (TestDirectFamilyCassettesMatchTheirCompiledOdu holds it to that) -- one
# repository fact, two typed file facts (one per referenced path) and two
# content_entity facts (the Function endpoints of the two live findings,
# carrying the CanonicalEntityID uids the entities phase stamps for
# Function labels). The repo/file facts are load-bearing, not padding: the
# entities phase writes nodes with containment (MATCH File first, and File
# hangs off Repository), and the edge writer MATCHes the Function node, so
# without them every statement completes having written nothing. Two
# edge-producing findings name their CanonicalEntityID-resolved Function
# uids (http-request to sql-exec in handlePayment, queue-message to
# command-exec in refundPayment); the tombstoned voidPayment finding
# resolves to nothing. The single git scope owns its generation and
# carries repo_id in scope metadata (the projector keys content
# materialization off it).
code_taint_evidence_cassette="${repo_root}/testdata/cassettes/codetaint/ifa-code-taint-evidence-family.json"
code_taint_evidence_expected_edges="${repo_root}/go/internal/ifa/testdata/codetaint/ifa-code-taint-evidence-family-expected-edges.json"
# incident_routing_evidence (#6228 incident_routing_evidence live leaf): a
# projection of the compiled IncidentRoutingEvidenceFamilyOdu by construction
# (TestDirectFamilyCassettesMatchTheirCompiledOdu holds it to that) -- one
# incident.record anchor, one applied PagerDuty service resource, one
# tombstoned duplicate applied fact (the negative control, skipped before
# decoding so it contributes no edge), one observed PagerDuty service, and
# one PagerDutyDeclaration content_entity fact (the declared half the
# production loader reads out of content_entities). The declaration rides
# the SAME PagerDuty scope: the projector commits content before it
# enqueues that generation's reducer intent, so the handler's
# service-name-scoped declared read is race-free. The single PagerDuty
# scope owns its own ACTIVE generation, like every sibling above.
incident_routing_evidence_cassette="${repo_root}/testdata/cassettes/incidentrouting/ifa-incident-routing-evidence-family.json"
incident_routing_evidence_expected_edges="${repo_root}/go/internal/ifa/testdata/incidentroutingevidence/ifa-incident-routing-evidence-family-expected-edges.json"
# publishes (#6228 publishes live leaf): a projection of the compiled
# PublishesFamilyOdu by construction
# (TestDirectFamilyCassettesMatchTheirCompiledOdu holds it to that) -- four
# repository facts, the owned package fact, six source hints (one
# package-level exact, one version-scoped exact, and four deliberate
# non-producers: tombstoned-only, homepage-only, unmatched, ambiguous) and
# two versions. The package fact is load-bearing, not padding: the
# provenance writer MATCHes the Package node, and no hint or version fact
# produces it. The single package-registry scope owns its own ACTIVE
# generation, like every sibling above.
publishes_cassette="${repo_root}/testdata/cassettes/publishes/ifa-publishes-family.json"
publishes_expected_edges="${repo_root}/go/internal/ifa/testdata/publishes/ifa-publishes-family-expected-edges.json"

# ifa_family_fixtures_require fails fast, before any Compose stack is started,
# when a committed fixture is missing. Each message names the specific fixture
# so a missing file is identifiable from the failure line alone; "$1" is the
# calling gate's name so the prefix matches the gate that aborted.
ifa_family_fixtures_require() {
	local gate="$1"
	[[ -f "${sql_cassette}" ]] || { echo "${gate}: SQL cassette not found: ${sql_cassette}" >&2; exit 1; }
	[[ -f "${sql_expected_edges}" ]] || { echo "${gate}: SQL expected-edge set not found: ${sql_expected_edges}" >&2; exit 1; }
	[[ -f "${sql_delta_cassette}" ]] || { echo "${gate}: SQL delta cassette not found: ${sql_delta_cassette}" >&2; exit 1; }
	[[ -f "${sql_delta_expected_edges}" ]] || { echo "${gate}: SQL delta expected-edge set not found: ${sql_delta_expected_edges}" >&2; exit 1; }
	[[ -f "${code_call_cassette}" ]] || { echo "${gate}: code-call cassette not found: ${code_call_cassette}" >&2; exit 1; }
	[[ -f "${code_call_expected_edges}" ]] || { echo "${gate}: code-call expected-edge set not found: ${code_call_expected_edges}" >&2; exit 1; }
	[[ -f "${documentation_cassette}" ]] || { echo "${gate}: documentation cassette not found: ${documentation_cassette}" >&2; exit 1; }
	[[ -f "${documentation_expected_edges}" ]] || { echo "${gate}: documentation expected-edge set not found: ${documentation_expected_edges}" >&2; exit 1; }
	[[ -f "${deployable_unit_cassette}" ]] || { echo "${gate}: deployable-unit cassette not found: ${deployable_unit_cassette}" >&2; exit 1; }
	[[ -f "${deployable_unit_expected_edges}" ]] || { echo "${gate}: deployable-unit expected-edge set not found: ${deployable_unit_expected_edges}" >&2; exit 1; }
	[[ -f "${rationale_cassette}" ]] || { echo "${gate}: rationale cassette not found: ${rationale_cassette}" >&2; exit 1; }
	[[ -f "${rationale_expected_edges}" ]] || { echo "${gate}: rationale expected-edge set not found: ${rationale_expected_edges}" >&2; exit 1; }
	[[ -f "${rationale_delta_cassette}" ]] || { echo "${gate}: rationale delta cassette not found: ${rationale_delta_cassette}" >&2; exit 1; }
	[[ -f "${rationale_delta_expected_records}" ]] || { echo "${gate}: rationale delta expected-record set not found: ${rationale_delta_expected_records}" >&2; exit 1; }
	[[ -f "${codeowners_cassette}" ]] || { echo "${gate}: codeowners cassette not found: ${codeowners_cassette}" >&2; exit 1; }
	[[ -f "${codeowners_expected_edges}" ]] || { echo "${gate}: codeowners expected-edge set not found: ${codeowners_expected_edges}" >&2; exit 1; }
	[[ -f "${repo_dependency_cassette}" ]] || { echo "${gate}: repo-dependency cassette not found: ${repo_dependency_cassette}" >&2; exit 1; }
	[[ -f "${repo_dependency_expected_edges}" ]] || { echo "${gate}: repo-dependency expected-edge set not found: ${repo_dependency_expected_edges}" >&2; exit 1; }
	[[ -f "${submodule_pin_cassette}" ]] || { echo "${gate}: submodule-pin cassette not found: ${submodule_pin_cassette}" >&2; exit 1; }
	[[ -f "${submodule_pin_expected_edges}" ]] || { echo "${gate}: submodule-pin expected-edge set not found: ${submodule_pin_expected_edges}" >&2; exit 1; }
	[[ -f "${inheritance_cassette}" ]] || { echo "${gate}: inheritance cassette not found: ${inheritance_cassette}" >&2; exit 1; }
	[[ -f "${inheritance_expected_edges}" ]] || { echo "${gate}: inheritance expected-edge set not found: ${inheritance_expected_edges}" >&2; exit 1; }
	[[ -f "${shell_exec_cassette}" ]] || { echo "${gate}: shell-exec cassette not found: ${shell_exec_cassette}" >&2; exit 1; }
	[[ -f "${shell_exec_expected_edges}" ]] || { echo "${gate}: shell-exec expected-edge set not found: ${shell_exec_expected_edges}" >&2; exit 1; }
	[[ -f "${workload_dependency_cassette}" ]] || { echo "${gate}: workload-dependency cassette not found: ${workload_dependency_cassette}" >&2; exit 1; }
	[[ -f "${workload_dependency_expected_edges}" ]] || { echo "${gate}: workload-dependency expected-edge set not found: ${workload_dependency_expected_edges}" >&2; exit 1; }
	[[ -f "${workload_dependency_repo_expected_edges}" ]] || { echo "${gate}: workload-dependency repo-prerequisite expected-edge set not found: ${workload_dependency_repo_expected_edges}" >&2; exit 1; }
	[[ -f "${symbol_runtime_cassette}" ]] || { echo "${gate}: symbol-runtime cassette not found: ${symbol_runtime_cassette}" >&2; exit 1; }
	[[ -f "${handles_route_expected_edges}" ]] || { echo "${gate}: handles-route expected-edge set not found: ${handles_route_expected_edges}" >&2; exit 1; }
	[[ -f "${runs_in_expected_edges}" ]] || { echo "${gate}: runs-in expected-edge set not found: ${runs_in_expected_edges}" >&2; exit 1; }
	[[ -f "${invokes_cloud_action_expected_edges}" ]] || { echo "${gate}: invokes-cloud-action expected-edge set not found: ${invokes_cloud_action_expected_edges}" >&2; exit 1; }
	[[ -f "${kubernetes_namespace_environment_cassette}" ]] || { echo "${gate}: kubernetes-namespace-environment cassette not found: ${kubernetes_namespace_environment_cassette}" >&2; exit 1; }
	[[ -f "${kubernetes_namespace_environment_expected_edges}" ]] || { echo "${gate}: kubernetes-namespace-environment expected-edge set not found: ${kubernetes_namespace_environment_expected_edges}" >&2; exit 1; }
	[[ -f "${iam_instance_profile_role_cassette}" ]] || { echo "${gate}: iam-instance-profile-role cassette not found: ${iam_instance_profile_role_cassette}" >&2; exit 1; }
	[[ -f "${iam_instance_profile_role_expected_edges}" ]] || { echo "${gate}: iam-instance-profile-role expected-edge set not found: ${iam_instance_profile_role_expected_edges}" >&2; exit 1; }
	[[ -f "${iam_can_assume_cassette}" ]] || { echo "${gate}: iam-can-assume cassette not found: ${iam_can_assume_cassette}" >&2; exit 1; }
	[[ -f "${iam_can_assume_expected_edges}" ]] || { echo "${gate}: iam-can-assume expected-edge set not found: ${iam_can_assume_expected_edges}" >&2; exit 1; }
	[[ -f "${iam_can_perform_cassette}" ]] || { echo "${gate}: iam-can-perform cassette not found: ${iam_can_perform_cassette}" >&2; exit 1; }
	[[ -f "${iam_can_perform_expected_edges}" ]] || { echo "${gate}: iam-can-perform expected-edge set not found: ${iam_can_perform_expected_edges}" >&2; exit 1; }
	[[ -f "${workload_cloud_relationship_cassette}" ]] || { echo "${gate}: workload-cloud-relationship cassette not found: ${workload_cloud_relationship_cassette}" >&2; exit 1; }
	[[ -f "${workload_cloud_relationship_expected_edges}" ]] || { echo "${gate}: workload-cloud-relationship expected-edge set not found: ${workload_cloud_relationship_expected_edges}" >&2; exit 1; }
	[[ -f "${iam_escalation_cassette}" ]] || { echo "${gate}: iam-escalation cassette not found: ${iam_escalation_cassette}" >&2; exit 1; }
	[[ -f "${iam_escalation_expected_edges}" ]] || { echo "${gate}: iam-escalation expected-edge set not found: ${iam_escalation_expected_edges}" >&2; exit 1; }
	[[ -f "${ec2_uses_profile_cassette}" ]] || { echo "${gate}: ec2-uses-profile cassette not found: ${ec2_uses_profile_cassette}" >&2; exit 1; }
	[[ -f "${ec2_uses_profile_expected_edges}" ]] || { echo "${gate}: ec2-uses-profile expected-edge set not found: ${ec2_uses_profile_expected_edges}" >&2; exit 1; }
	[[ -f "${s3_logs_to_cassette}" ]] || { echo "${gate}: s3-logs-to cassette not found: ${s3_logs_to_cassette}" >&2; exit 1; }
	[[ -f "${s3_logs_to_expected_edges}" ]] || { echo "${gate}: s3-logs-to expected-edge set not found: ${s3_logs_to_expected_edges}" >&2; exit 1; }
	[[ -f "${authenticates_vault_role_cassette}" ]] || { echo "${gate}: authenticates-vault-role cassette not found: ${authenticates_vault_role_cassette}" >&2; exit 1; }
	[[ -f "${authenticates_vault_role_expected_edges}" ]] || { echo "${gate}: authenticates-vault-role expected-edge set not found: ${authenticates_vault_role_expected_edges}" >&2; exit 1; }
	[[ -f "${authenticates_vault_role_live_expected_edges}" ]] || { echo "${gate}: authenticates-vault-role live expected-edge set not found: ${authenticates_vault_role_live_expected_edges}" >&2; exit 1; }
	[[ -f "${uses_vault_policy_cassette}" ]] || { echo "${gate}: uses-vault-policy cassette not found: ${uses_vault_policy_cassette}" >&2; exit 1; }
	[[ -f "${uses_vault_policy_expected_edges}" ]] || { echo "${gate}: uses-vault-policy expected-edge set not found: ${uses_vault_policy_expected_edges}" >&2; exit 1; }
	[[ -f "${kubernetes_correlation_cassette}" ]] || { echo "${gate}: kubernetes-correlation cassette not found: ${kubernetes_correlation_cassette}" >&2; exit 1; }
	[[ -f "${kubernetes_correlation_expected_edges}" ]] || { echo "${gate}: kubernetes-correlation expected-edge set not found: ${kubernetes_correlation_expected_edges}" >&2; exit 1; }
	[[ -f "${semantic_entity_containment_cassette}" ]] || { echo "${gate}: semantic-entity-containment cassette not found: ${semantic_entity_containment_cassette}" >&2; exit 1; }
	[[ -f "${semantic_entity_containment_expected_edges}" ]] || { echo "${gate}: semantic-entity-containment expected-edge set not found: ${semantic_entity_containment_expected_edges}" >&2; exit 1; }
	[[ -f "${semantic_entity_containment_expected_edges_nornicdb}" ]] || { echo "${gate}: semantic-entity-containment NornicDB expected-edge set not found: ${semantic_entity_containment_expected_edges_nornicdb}" >&2; exit 1; }
	[[ -f "${cloud_resource_cassette}" ]] || { echo "${gate}: cloud-resource cassette not found: ${cloud_resource_cassette}" >&2; exit 1; }
	[[ -f "${cloud_resource_expected_edges}" ]] || { echo "${gate}: cloud-resource expected-edge set not found: ${cloud_resource_expected_edges}" >&2; exit 1; }
	[[ -f "${built_from_cassette}" ]] || { echo "${gate}: built-from cassette not found: ${built_from_cassette}" >&2; exit 1; }
	[[ -f "${built_from_expected_edges}" ]] || { echo "${gate}: built-from expected-edge set not found: ${built_from_expected_edges}" >&2; exit 1; }
	[[ -f "${cloud_resource_container_image_cassette}" ]] || { echo "${gate}: cloud-resource-container-image cassette not found: ${cloud_resource_container_image_cassette}" >&2; exit 1; }
	[[ -f "${cloud_resource_container_image_expected_edges}" ]] || { echo "${gate}: cloud-resource-container-image expected-edge set not found: ${cloud_resource_container_image_expected_edges}" >&2; exit 1; }
	[[ -f "${crossplane_satisfied_by_cassette}" ]] || { echo "${gate}: crossplane-satisfied-by cassette not found: ${crossplane_satisfied_by_cassette}" >&2; exit 1; }
	[[ -f "${crossplane_satisfied_by_expected_edges}" ]] || { echo "${gate}: crossplane-satisfied-by expected-edge set not found: ${crossplane_satisfied_by_expected_edges}" >&2; exit 1; }
	[[ -f "${derived_from_cassette}" ]] || { echo "${gate}: derived-from cassette not found: ${derived_from_cassette}" >&2; exit 1; }
	[[ -f "${derived_from_expected_edges}" ]] || { echo "${gate}: derived-from expected-edge set not found: ${derived_from_expected_edges}" >&2; exit 1; }
	[[ -f "${observability_coverage_cassette}" ]] || { echo "${gate}: observability-coverage cassette not found: ${observability_coverage_cassette}" >&2; exit 1; }
	[[ -f "${observability_coverage_expected_edges}" ]] || { echo "${gate}: observability-coverage expected-edge set not found: ${observability_coverage_expected_edges}" >&2; exit 1; }
	[[ -f "${code_interproc_evidence_cassette}" ]] || { echo "${gate}: code-interproc-evidence cassette not found: ${code_interproc_evidence_cassette}" >&2; exit 1; }
	[[ -f "${code_interproc_evidence_expected_edges}" ]] || { echo "${gate}: code-interproc-evidence expected-edge set not found: ${code_interproc_evidence_expected_edges}" >&2; exit 1; }
	[[ -f "${code_taint_evidence_cassette}" ]] || { echo "${gate}: code-taint-evidence cassette not found: ${code_taint_evidence_cassette}" >&2; exit 1; }
	[[ -f "${code_taint_evidence_expected_edges}" ]] || { echo "${gate}: code-taint-evidence expected-edge set not found: ${code_taint_evidence_expected_edges}" >&2; exit 1; }
	[[ -f "${incident_routing_evidence_cassette}" ]] || { echo "${gate}: incident-routing-evidence cassette not found: ${incident_routing_evidence_cassette}" >&2; exit 1; }
	[[ -f "${incident_routing_evidence_expected_edges}" ]] || { echo "${gate}: incident-routing-evidence expected-edge set not found: ${incident_routing_evidence_expected_edges}" >&2; exit 1; }
	[[ -f "${publishes_cassette}" ]] || { echo "${gate}: publishes cassette not found: ${publishes_cassette}" >&2; exit 1; }
	[[ -f "${publishes_expected_edges}" ]] || { echo "${gate}: publishes expected-edge set not found: ${publishes_expected_edges}" >&2; exit 1; }
	# The family scope MUST be stamped source_system='oci_registry': replay
	# stamps fact.source_system from the scope, and the
	# container-image-identity loader the handler reads only accepts OCI
	# facts with source_system='oci_registry'. A lost stamp drives ten
	# facts the identity loader cannot see and the cell materializes zero
	# edges (diagnosed live 2026-09-28). Fail fast here with the cause
	# named instead of late at the exact-set assert.
	kubernetes_correlation_scope_system="$(jq -r '.scopes[0].source_system' "${kubernetes_correlation_cassette}")"
	[[ "${kubernetes_correlation_scope_system}" == "oci_registry" ]] || { echo "${gate}: kubernetes-correlation cassette scope source_system is ${kubernetes_correlation_scope_system:-missing}, want oci_registry" >&2; exit 1; }
	# The built_from scope is a git repository scope, so its stamp stays
	# 'git' (the semantic_entity_containment precedent). Unlike
	# kubernetes_correlation -- whose handler reads ONLY through the
	# cross-scope identity loader that filters on source_system -- the
	# writing handler here is ContainerImageIdentityHandler itself, whose
	# scope-local LoadFactsForKinds (repository, content_entity, OCI
	# manifest) carries no source_system predicate; the cross-scope active
	# loads are supplement-only and dedupe-covered. A 'git' stamp that
	# drifted to something else would mislabel the owning scope without
	# changing what the handler reads, so pin the honest value.
	built_from_scope_system="$(jq -r '.scopes[0].source_system' "${built_from_cassette}")"
	[[ "${built_from_scope_system}" == "git" ]] || { echo "${gate}: built-from cassette scope source_system is ${built_from_scope_system:-missing}, want git" >&2; exit 1; }
	# The derived_from scope is the same shape -- a git repository scope
	# driven through the same ContainerImageIdentityHandler, whose
	# scope-local LoadFactsForKinds carries no source_system predicate --
	# so its stamp stays 'git' for the same reason as built_from's above.
	derived_from_scope_system="$(jq -r '.scopes[0].source_system' "${derived_from_cassette}")"
	[[ "${derived_from_scope_system}" == "git" ]] || { echo "${gate}: derived-from cassette scope source_system is ${derived_from_scope_system:-missing}, want git" >&2; exit 1; }
}
