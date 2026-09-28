#!/usr/bin/env bash
# shellcheck shell=bash
# Live-gate drive/assert callbacks for the seven DIRECT-materialization families
# (#6228): kubernetes_namespace_environment, iam_instance_profile_role,
# iam_can_assume, iam_can_perform, workload_cloud_relationship,
# iam_escalation, and ec2_uses_profile.
#
# SOURCED BY scripts/verify-ifa-determinism.sh AND, since #6309, the fault
# gate through scripts/lib/ifa_fault_injection_sources.sh. The fault cells
# (scripts/lib/ifa_fault_injection_kubernetes_namespace_environment_cells.sh
# and scripts/lib/ifa_fault_injection_iam_instance_profile_role_cells.sh)
# call the drive/assert callbacks below; the families' registry rows carry
# cell_kind=custom. iam_can_assume, iam_can_perform,
# workload_cloud_relationship, iam_escalation, and ec2_uses_profile have no
# fault cells yet: their rows carry cell_kind=custom prospectively and the
# fault gate never dispatches them, so the fault-injection area stays
# untouched by this change. Callers own strict mode and cleanup.
#
# ONE FILE FOR SEVEN FAMILIES, unlike the shared-projection families' one file
# each. Their drive and assert bodies differ only in a cassette path, a domain
# name and a log filename, and each is four lines of real work; seven files
# would be one contract in seven places. The per-family REGISTRY ROWS stay
# separate, which is where the split that matters already is.
#
# WHY THESE ARE DIRECT, and why that changes nothing here: the reducer writes
# these families straight to a go/internal/storage/cypher writer rather than
# through a shared-projection intent row. The gate does not care -- it drives a
# cassette and asserts an exact edge set either way -- but it does mean the
# handler is scheduled by an ordinary fact_work_items domain
# (kubernetes_namespace_materialization / iam_instance_profile_role_materialization
# / iam_can_assume_materialization / iam_can_perform_materialization /
# workload_cloud_relationship_materialization / iam_escalation_materialization
# / ec2_uses_profile_materialization)
# rather than by a shared_followup fact the cassette has to carry.

# ifa_direct_family_drive replays one committed family cassette into a matrix
# cell. The caller performs the aggregate fact_work_items non-vacuity check.
#
# label/slug are separate arguments because the label carries the cell identity
# (n1, N=2, "post-delta N=4") and would make an unusable filename, while the
# slug is the stable per-family log name.
_ifa_direct_family_drive() {
	local slug="$1" label="$2" bin_dir="$3" cassette="$4" workers="$5" log_dir="$6"
	printf '\n=== %s: drive %s family cassette (-workers %s) ===\n' "${label}" "${slug}" "${workers}"
	if ! "${bin_dir}/eshu-ifa" drive -cassette "${cassette}" -workers "${workers}" \
		>"${log_dir}/ifa-drive-${slug}-${label}.log" 2>&1; then
		tail -40 "${log_dir}/ifa-drive-${slug}-${label}.log" >&2 || true
		return 1
	fi
	cat "${log_dir}/ifa-drive-${slug}-${label}.log"
}

# Each assert below spells its `-domain <family>` flag out literally rather
# than taking the domain as a parameter. That is deliberate: the domain is the
# one thing a shared helper must not abstract away, because it is what makes
# these three families' assertions distinguishable from each other, and
# scripts/lib/test-ifa-determinism-family-cases.sh greps each family's own flag
# out of this file to prove the wiring exists. A parameterized call would let
# one family's coverage stand in for another's and satisfy that check with a
# single needle.

# ifa_kubernetes_namespace_environment_drive replays the namespace cassette.
ifa_kubernetes_namespace_environment_drive() {
	local label="$1" bin_dir="$2" cassette="$3" workers="$4" log_dir="$5"
	_ifa_direct_family_drive kubernetes-namespace-environment \
		"${label}" "${bin_dir}" "${cassette}" "${workers}" "${log_dir}"
}

# ifa_kubernetes_namespace_environment_assert pins the two-edge exact set.
#
# CALLED TWICE PER CELL, pre-delta inside the registry loop and again after
# ifa_det_run_sql_delta_live. The second call is not belt-and-braces: the matrix
# compares one canonicalized digest per N, so a generation-2 regression that
# retracted or mutated these edges IDENTICALLY at N=1, 2 and 4 leaves all three
# digests equal and the gate green while the graph no longer matches the
# expected set. Only re-running the exact-set assertion after generation 2 can
# see that. Both direct families were asserted pre-delta only when they first
# landed (#6309).
#
# Two of the Odù's four namespaces bind an Environment and two deliberately do
# not, so this assertion is as much about the two edges that must NOT exist as
# the two that must. The targets are the CANONICAL environment names ("prod",
# "stage"), not the raw labels ("production", "staging"): the reducer
# canonicalizes through environment.Canonical, and asserting the raw values
# would pass on a build that had dropped that step.
#
# The target endpoints are name-keyed Environment nodes carrying no uid and no
# id, which `ifa assert-edges` resolves through endpointID's Environment-scoped
# "name" fallback. Before that fallback existed this family's edges materialized
# correctly and the gate still reported every one of them an unmaterialized
# endpoint.
ifa_kubernetes_namespace_environment_assert() {
	local label="$1" bin_dir="$2" expected_edges="$3"
	printf '\n=== %s: assert kubernetes_namespace_environment materialized edges (two-edge exact set) ===\n' "${label}"
	"${bin_dir}/eshu-ifa" assert-edges \
		-domain kubernetes_namespace_environment \
		-expected "${expected_edges}"
}

# ifa_iam_instance_profile_role_drive replays the instance-profile cassette.
ifa_iam_instance_profile_role_drive() {
	local label="$1" bin_dir="$2" cassette="$3" workers="$4" log_dir="$5"
	_ifa_direct_family_drive iam-instance-profile-role \
		"${label}" "${bin_dir}" "${cassette}" "${workers}" "${log_dir}"
}

# ifa_fault_count_retry_attempts totals the EXCESS attempts
# (sum(attempt_count - 1)) for one materialization domain, and
# ifa_fault_assert_retry_attempts_above proves the fault ADDED attempts the
# fault-free baseline lacked by requiring a strict increase over it.
#
# Why a second retry signal next to ifa_fault_count_retried's row COUNT in
# ifa_fault_injection_common.sh: each direct-family cassette creates exactly
# ONE targeted work item, so the count form saturates at 1. A single natural
# counting-class retry in the fault-free baseline (a real NornicDB deadlock or
# a transient EntityNotFound under the concurrent projector+reducer this gate
# runs -- both documented on the count helper) makes the baseline 1; the
# forced kill then adds another attempt to the SAME row, the kill-run count
# stays 1, and 1 > 1 false-fails a correct recovery. The sum form has no
# ceiling: the kill always adds attempts the baseline lacked. Multi-row
# domains keep the count form -- saturating every row naturally is
# implausible there, and the count form's "measured inert" reasoning (a
# natural retry also appears in the identical baseline drive, so it cannot
# green the check while the decorator sits inert) carries over unchanged:
# only attempts the baseline lacked move this sum.
#
# Args (count): compose_project use_compose dsn compose_file [domain].
# Args (assert): compose_project use_compose dsn compose_file baseline [budget_seconds=15] [domain].
ifa_fault_count_retry_attempts() {
	local compose_project="$1" use_compose="$2" dsn="$3" compose_file="$4"
	local domain="${5:-kubernetes_namespace_materialization}"
	if [[ ! "${domain}" =~ ^[a-z0-9_]+$ ]]; then
		echo "ifa_fault_count_retry_attempts: domain must match ^[a-z0-9_]+$, got ${domain}" >&2
		return 1
	fi
	ifa_det_pg "${compose_project}" "${use_compose}" "${dsn}" \
		"SELECT coalesce(sum(attempt_count - 1), 0) FROM fact_work_items WHERE stage = 'reducer' AND status = 'succeeded' AND attempt_count > 1 AND domain = '${domain}';" \
		"${compose_file}" | tr -d '[:space:]'
}

ifa_fault_assert_retry_attempts_above() {
	local compose_project="$1" use_compose="$2" dsn="$3" compose_file="$4"
	local baseline="$5" budget="${6:-15}" domain="${7:-kubernetes_namespace_materialization}"
	local i count
	for i in $(seq 1 "${budget}"); do
		count="$(ifa_fault_count_retry_attempts "${compose_project}" "${use_compose}" "${dsn}" "${compose_file}" "${domain}")"
		if [[ -n "${count}" && "${count}" -gt "${baseline}" ]]; then
			printf '%s' "${count}"
			return 0
		fi
		sleep 1
	done
	return 1
}

# ifa_iam_instance_profile_role_assert pins the two-edge exact set. Called twice
# per cell for the reason recorded on the namespace assert above: post-delta is
# the only place an identical-across-N generation-2 mutation shows up.
#
# Both edges come from ONE instance profile attaching two scanned roles, so a
# regression that emitted one edge per profile instead of one per attachment
# still produces "some edges" and fails only against an exact set. The Odù's
# other two profiles -- one naming a role ARN nothing scanned, one with an empty
# attachment list -- must contribute nothing; the extractor drops an unresolved
# target rather than inventing an endpoint, and this set is what holds it to
# that.
#
# The relationship type is HAS_ROLE, read off the writer's MERGE. It is NOT
# IAM_INSTANCE_PROFILE_HAS_ROLE, which is statement metadata carried beside the
# query and never reaches the graph.
ifa_iam_instance_profile_role_assert() {
	local label="$1" bin_dir="$2" expected_edges="$3"
	printf '\n=== %s: assert iam_instance_profile_role materialized edges (two-edge exact set) ===\n' "${label}"
	"${bin_dir}/eshu-ifa" assert-edges \
		-domain iam_instance_profile_role \
		-expected "${expected_edges}"
}

# ifa_iam_can_assume_drive replays the can-assume cassette.
ifa_iam_can_assume_drive() {
	local label="$1" bin_dir="$2" cassette="$3" workers="$4" log_dir="$5"
	_ifa_direct_family_drive iam-can-assume \
		"${label}" "${bin_dir}" "${cassette}" "${workers}" "${log_dir}"
}

# ifa_iam_can_assume_assert pins the two-edge exact set. Called twice per
# cell for the reason recorded on the namespace assert above: post-delta is
# the only place an identical-across-N generation-2 mutation shows up.
#
# Both edges come from ONE Allow trust statement on the eshu-runtime role
# fanning out to the two scanned principals (the ci-deployer role and the
# breakglass user), so a regression that emitted one edge per statement
# instead of one per resolved principal still produces "some edges" and fails
# only against an exact set. The fixture's other seven statements -- deny,
# wildcard, service principal, unscanned foreign ARN, unscanned source role,
# self-assume, non-trust source -- must contribute nothing; the extractor
# drops an unresolved principal rather than inventing an endpoint, and this
# set is what holds it to that.
#
# The relationship type is CAN_ASSUME, read off the writer's MERGE. It is NOT
# IAM_CAN_ASSUME, which is statement metadata carried beside the query and
# never reaches the graph.
ifa_iam_can_assume_assert() {
	local label="$1" bin_dir="$2" expected_edges="$3"
	printf '\n=== %s: assert iam_can_assume materialized edges (two-edge exact set) ===\n' "${label}"
	"${bin_dir}/eshu-ifa" assert-edges \
		-domain iam_can_assume \
		-expected "${expected_edges}"
}

# ifa_iam_can_perform_drive replays the can-perform cassette.
ifa_iam_can_perform_drive() {
	local label="$1" bin_dir="$2" cassette="$3" workers="$4" log_dir="$5"
	_ifa_direct_family_drive iam-can-perform \
		"${label}" "${bin_dir}" "${cassette}" "${workers}" "${log_dir}"
}

# ifa_iam_can_perform_assert pins the three-edge exact set. Called twice per
# cell for the reason recorded on the namespace assert above: post-delta is
# the only place an identical-across-N generation-2 mutation shows up.
#
# The three edges come from three Allow identity statements: two catalog
# actions (s3:getobject + s3:putobject) converging on ONE S3 edge with the
# merged sorted action set, one KMS action on a second service family, and
# one DynamoDB action on a role principal. A regression that emitted one edge
# per action instead of one per resolved (principal, resource) pair would
# still produce "some edges" and fail only against an exact set. The
# fixture's other eleven statements -- type-mismatch, deny, conditioned,
# NotAction, uncatalogued, absorbed wildcard, standalone wildcard, unscanned
# target, non-identity source, unscanned principal, wrong target -- must
# contribute nothing; the extractor drops an unresolvable grant rather than
# inventing an endpoint, and this set is what holds it to that.
#
# The relationship type is CAN_PERFORM, read off the writer's MERGE and the
# iamCanPerformEdgeLabel const (which IS "CAN_PERFORM"). IAM_CAN_PERFORM
# appears nowhere in code; the type is never derived from the port or family
# name.
ifa_iam_can_perform_assert() {
	local label="$1" bin_dir="$2" expected_edges="$3"
	printf '\n=== %s: assert iam_can_perform materialized edges (three-edge exact set) ===\n' "${label}"
	"${bin_dir}/eshu-ifa" assert-edges \
		-domain iam_can_perform \
		-expected "${expected_edges}"
}

# ifa_workload_cloud_relationship_drive replays the workload-cloud-relationship
# cassette, then seeds the positive anchor's endpoints.
#
# The cassette carries aws_resource facts only, so the cell has no
# workload-domain facts for the workload pipeline to turn into nodes -- yet
# the writer MATCHes (workload:Workload)<-[:INSTANCE_OF]-(instance) and the
# handler fails instances_not_ready without them (observed live 2026-09-27).
# The seed creates exactly the Odù's own positive anchor
# (workload:orders-api in prod; instance id derived by the single source of
# truth the guard mapper shares) and nothing else: the service-name-only,
# ambiguous, and environment-less anchors get no nodes, so the extractor's
# drop-never-invent restraint still has something to prove live. Follows the
# repo_dependency materialize-platform-prerequisite precedent (seed + exact
# output check), not a new idea: MERGEs make it idempotent under handler
# retries and repeated N-cell drives, and a seed/output drift fails here,
# not later as zero assert-edges edges.
ifa_workload_cloud_relationship_drive() {
	local label="$1" bin_dir="$2" cassette="$3" workers="$4" log_dir="$5"
	_ifa_direct_family_drive workload-cloud-relationship \
		"${label}" "${bin_dir}" "${cassette}" "${workers}" "${log_dir}"
	ifa_workload_cloud_relationship_seed_endpoints "${bin_dir}"
}

ifa_workload_cloud_relationship_seed_endpoints() {
	local bin_dir="$1" output
	output="$("${bin_dir}/eshu-ifa" materialize-workload-endpoints \
		-workload-id workload:orders-api -environment prod)" || return 1
	printf '%s\n' "${output}"
	[[ "${output}" == 'instance_id=workload-instance:orders-api:prod verified=1' ]]
}

# ifa_workload_cloud_relationship_assert pins the two-edge exact set. Called
# twice per cell for the reason recorded on the namespace assert above:
# post-delta is the only place an identical-across-N generation-2 mutation
# shows up.
#
# Both edges are explicit_workload_anchor in prod off ONE workload
# (orders-api): the ssm parameter and the sqs queue, each resolving the same
# workload-instance:orders-api:prod source to its own CloudResource target. A
# regression that emitted one edge per anchor attribute instead of one per
# resolved (instance, resource) pair would still produce "some edges" and fail
# only against an exact set. The fixture's other three resources -- the
# service-only sns topic (environment but no workload anchor), the ambiguous
# dynamodb table (a bare name matching more than one workload), and the
# environment-less s3 bucket (workload anchor but no environment) -- must
# contribute nothing; the extractor drops an unresolvable anchor rather than
# inventing an endpoint, and this set is what holds it to that.
#
# The relationship type is USES, read off the writer's MERGE template filled
# from the closed single-member workloadCloudRelationshipVocabulary. It is NOT
# WORKLOAD_USES_CLOUD_RESOURCE, which is the workloadCloudRelationshipEdgeLabel
# const: statement metadata carried beside the query that never reaches the
# graph.
ifa_workload_cloud_relationship_assert() {
	local label="$1" bin_dir="$2" expected_edges="$3"
	printf '\n=== %s: assert workload_cloud_relationship materialized edges (two-edge exact set) ===\n' "${label}"
	"${bin_dir}/eshu-ifa" assert-edges \
		-domain workload_cloud_relationship \
		-expected "${expected_edges}"
}

# ifa_iam_escalation_drive replays the iam-escalation cassette.
ifa_iam_escalation_drive() {
	local label="$1" bin_dir="$2" cassette="$3" workers="$4" log_dir="$5"
	_ifa_direct_family_drive iam-escalation \
		"${label}" "${bin_dir}" "${cassette}" "${workers}" "${log_dir}"
}

# ifa_iam_escalation_assert pins the five-edge exact set. Called twice per
# cell for the reason recorded on the namespace assert above: post-delta is
# the only place an identical-across-N generation-2 mutation shows up.
#
# The five edges come from six Allow identity statements converging on five
# resolved (principal, target) pairs: a policy-target primitive, three
# primitives on one role merging into a single three-token edge, a
# group-target primitive, a PassRole-family three-action primitive, and a
# second principal resolving a user target. A regression
# that emitted one edge per primitive instead of one per resolved pair would
# still produce "some edges" and fail only against an exact set. The
# fixture's other nine statements -- self-loop, deny, conditioned, NotAction,
# wildcard, unscanned target, unscanned principal, deferred sts:AssumeRole,
# wrong target -- must contribute nothing; the extractor drops an
# unresolvable primitive rather than inventing an endpoint, and this set is
# what holds it to that.
#
# The relationship type is CAN_ESCALATE_TO, read off the writer's MERGE and
# the iamEscalationEdgeLabel const (which IS "CAN_ESCALATE_TO").
# IAM_ESCALATION appears nowhere in code; the type is never derived from the
# port or family name.
ifa_iam_escalation_assert() {
	local label="$1" bin_dir="$2" expected_edges="$3"
	printf '\n=== %s: assert iam_escalation materialized edges (five-edge exact set) ===\n' "${label}"
	"${bin_dir}/eshu-ifa" assert-edges \
		-domain iam_escalation \
		-expected "${expected_edges}"
}

# ifa_ec2_uses_profile_drive replays the ec2-uses-profile cassette.
ifa_ec2_uses_profile_drive() {
	local label="$1" bin_dir="$2" cassette="$3" workers="$4" log_dir="$5"
	_ifa_direct_family_drive ec2-uses-profile \
		"${label}" "${bin_dir}" "${cassette}" "${workers}" "${log_dir}"
}

# ifa_s3_logs_to_drive replays the s3-logs-to cassette.
ifa_s3_logs_to_drive() {
	local label="$1" bin_dir="$2" cassette="$3" workers="$4" log_dir="$5"
	_ifa_direct_family_drive s3-logs-to \
		"${label}" "${bin_dir}" "${cassette}" "${workers}" "${log_dir}"
}

# ifa_kubernetes_correlation_drive replays the kubernetes-correlation cassette.
#
# ONE drive, not two: an earlier shape of this slice drove a second OCI
# sources seed cassette (the Odù's five OCI facts in per-repo oci_registry
# scopes) because the handler joins across scopes by design and the
# container-image-identity loader it reads (ListActiveContainerImageIdentityFacts,
# go/internal/storage/postgres/facts_active_container_image_identity.go)
# only accepts oci_registry.image_manifest / image_index /
# image_tag_observation rows with source_system='oci_registry'. That seed
# fixed the zero-edge cell (diagnosed live 2026-09-28: handler ran with
# fact_count=6 edge_count=0) but broke determinism instead: the same
# logical OCI facts then existed in TWO scopes, and the OCI node
# projection raced last-writer-wins on the scope-stamped provenance
# columns (scope_id, source_system, generation_id, source_fact_id), so
# N=1 vs N=4 dumps diverged on nodes no edge logic touches. Duplicate
# substrate across scopes is inherently nondeterministic here; the family
# cassette therefore carries both substrates in its ONE scope, stamped
# source_system='oci_registry' so the identity loader accepts the OCI
# facts. The kind-scoped loads the handler and the node projectors use
# (ListFactsByKind: scope+generation+kind, no source_system predicate)
# are unaffected by the stamp, and the per-fact collector_kind values
# stay honest (kubernetes_live for pod templates, oci_registry for the
# sources). Scope descriptors are replay-transport concerns, not fixture
# truth -- the same reason loadDirectFamilyOdu declines to project them.
ifa_kubernetes_correlation_drive() {
	local label="$1" bin_dir="$2" cassette="$3" workers="$4" log_dir="$5"
	_ifa_direct_family_drive kubernetes-correlation \
		"${label}" "${bin_dir}" "${cassette}" "${workers}" "${log_dir}"
}

# ifa_ec2_uses_profile_assert pins the three-edge exact set. Called twice per
# cell for the reason recorded on the namespace assert above: post-delta is
# the only place an identical-across-N generation-2 mutation shows up.
#
# The three edges come from two instance-id-resolved postures (i-0aaa1111 to
# the app profile, i-0bbb2222 to the batch profile) plus one blank-id
# posture whose source keys on the full instance ARN to the batch profile
# (the legacy-inventory fallback). A regression that emitted one edge per
# posture instead of one per resolved (instance, profile) pair would still
# produce "some edges" and fail only against an exact set. The fixture's
# other three postures -- a bare instance with a blank profile ARN, a
# terminated (tombstoned) instance, and an instance naming an unscanned
# ghost profile -- must contribute nothing, and the scanned idle profile
# with no posture must gain no edge; the extractor drops an unresolvable
# target rather than inventing an endpoint, and this set is what holds it
# to that.
#
# The relationship type is USES_PROFILE, read off the writer's MERGE
# template filled per row from the closed single-member
# ec2UsesProfileRelationshipVocabulary. It is NOT EC2_USES_PROFILE, which
# is the ec2UsesProfileEdgeLabel const: statement metadata carried beside
# the query that never reaches the graph.
ifa_ec2_uses_profile_assert() {
	local label="$1" bin_dir="$2" expected_edges="$3"
	printf '\n=== %s: assert ec2_uses_profile materialized edges (three-edge exact set) ===\n' "${label}"
	"${bin_dir}/eshu-ifa" assert-edges \
		-domain ec2_uses_profile \
		-expected "${expected_edges}"
}

# ifa_s3_logs_to_assert pins the three-edge exact set. Called twice per
# cell for the reason recorded on the namespace assert above: post-delta is
# the only place an identical-across-N generation-2 mutation shows up.
#
# The three edges come from two name-resolved postures (orders-bucket to
# logs-bucket, audit-bucket to itself -- a legal S3 self-target that DOES
# emit an edge) plus one ARN-only posture whose source name derives from
# the ARN tail to logs-bucket. A regression that emitted one edge per
# posture instead of one per resolved (bucket, log-bucket) pair would still
# produce "some edges" and fail only against an exact set. The fixture's
# other three postures -- a bucket with logging disabled (blank target),
# a bucket naming the unscanned ghost-log-bucket, and an orphan posture
# with no scanned node -- must contribute nothing, and the scanned idle
# bucket with no posture must gain no edge; the extractor drops an
# unresolvable endpoint rather than inventing a node, and this set is what
# holds it to that.
#
# The relationship type is LOGS_TO, read off the writer's MERGE template
# filled per row from the closed single-member s3LogsToRelationshipVocabulary.
# It is NOT S3_LOGS_TO, which is the s3LogsToEdgeLabel const: statement
# metadata carried beside the query that never reaches the graph.
ifa_s3_logs_to_assert() {
	local label="$1" bin_dir="$2" expected_edges="$3"
	printf '\n=== %s: assert s3_logs_to materialized edges (three-edge exact set) ===\n' "${label}"
	"${bin_dir}/eshu-ifa" assert-edges \
		-domain s3_logs_to \
		-expected "${expected_edges}"
}

# ifa_kubernetes_correlation_assert pins the two-edge exact set. Called twice
# per cell for the reason recorded on the namespace assert above: post-delta
# is the only place an identical-across-N generation-2 mutation shows up.
#
# The two edges are the exact-digest resolutions: the checkout deployment's
# digest-form ref matches the active manifest digest (OciImageManifest
# target) and the billing deployment's matches the active index digest
# (OciImageIndex target) -- the two digest-addressed source labels the
# template MATCHes. A regression that promoted every decision to an edge
# regardless of outcome would still produce "some edges" and fail only
# against an exact set. The fixture's other three workloads -- legacy
# naming a tombstone-only digest (stale), canary naming a tag two digests
# share (ambiguous), and phantom naming an unobserved digest
# (unresolved) -- must contribute nothing; the extractor drops a
# non-exact decision rather than inventing an edge, and this set is what
# holds it to that.
#
# The relationship type is RUNS_IMAGE, a static token in the writer's
# template filled per row only with the source-node label. It is NOT
# KUBERNETES_CORRELATION, which is statement metadata carried beside the
# query that never reaches the graph.
ifa_kubernetes_correlation_assert() {
	local label="$1" bin_dir="$2" expected_edges="$3"
	printf '\n=== %s: assert kubernetes_correlation materialized edges (two-edge exact set) ===\n' "${label}"
	"${bin_dir}/eshu-ifa" assert-edges \
		-domain kubernetes_correlation \
		-expected "${expected_edges}"
}
