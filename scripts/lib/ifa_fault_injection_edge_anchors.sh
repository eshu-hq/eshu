#!/usr/bin/env bash
# Edge-operation MERGE anchors for scripts/verify-ifa-fault-injection.sh.
# This is a plain library, not an executable: it deliberately does not change
# the caller's shell options. The block lived inline in the gate script until
# that script hit its 500-line cap; the gate sources this file where the block
# stood, so the variables are set before any cell function reads them.
# Keeping the anchors here leaves the gate entrypoint focused on configuration
# and the independently mirrored ordered dispatch list.
#
# Every anchor is a fixed, grep-stable substring of the MERGE clause its cell's
# fault targets, read off the writer's executed statement. The mirror pins
# each one through require_anchors_lib
# (scripts/lib/test-ifa-fault-injection-assertions.sh), not require_code
# against the gate script -- the same narrow-exception shape as
# require_catalog for the cell catalog that moved out earlier.

# The CloudResource MERGE anchor cell_failgraphwrite (cell 4) targets
# (go/internal/storage/cypher/cloud/resource/node_writer.go's
# baseCloudResourceUpsertCypher): a fixed, grep-stable substring regardless of
# this run's own call interleaving, unlike a statement_ordinal.
cloud_resource_operation_match="MERGE (r:CloudResource"

# The SQL edge MERGE anchor cell_failgraphwrite_sql (cell 12, #5555) targets:
# go/internal/storage/cypher/canonical.go's batchCanonicalSQLQueriesTableUpsertCypher
# and edge_writer_sql.go's buildLabelScopedSQLRelationshipCypher both emit
# this exact MERGE clause text for a QUERIES_TABLE edge regardless of which
# source/target node labels the label-scoped writer path picks (only the
# preceding MATCH clauses vary by label) -- a fixed, grep-stable substring,
# same rationale as cloud_resource_operation_match above. QUERIES_TABLE is
# one of the SQL family's nine materialized edge types and is present in the
# committed sql_cassette, so this fault genuinely fires during that drive.
sql_edge_operation_match="MERGE (source)-[rel:QUERIES_TABLE]->(target)"
codeowners_edge_operation_match="MERGE (repo)-[rel:DECLARES_CODEOWNER"  # PREFIX, verified vs canonical/node/codeowners_edges.go:35; see the cells lib header
submodule_pin_edge_operation_match="MERGE (parent)-[rel:PINS_SUBMODULE"  # PREFIX, verified vs canonical/node/submodule_edges.go:32; see the cells lib header

# The TARGETS_ENVIRONMENT MERGE anchor cell_failgraphwrite_kubernetes_namespace_environment
# (#6309) targets: go/internal/storage/cypher/kubernetes/namespace.go:92 emits
# this exact MERGE clause text -- read off the writer's executed statement, same
# rationale as cloud_resource_operation_match above.
kubernetes_namespace_environment_edge_operation_match="MERGE (n)-[env_rel:TARGETS_ENVIRONMENT]->(env)"
# The HAS_ROLE MERGE anchor cell_failgraphwrite_iam_instance_profile_role (#6309)
# targets: go/internal/storage/cypher/cloud/aws/iam/instance_profile_role_edge_writer.go:24
# emits MERGE (profile)-[rel:%s]->(role), filled from a closed single-member
# vocabulary, so the anchor carries the INTERPOLATED type. A literal %s would
# match no executed statement and the fault would never fire. It is NOT
# IAM_INSTANCE_PROFILE_HAS_ROLE, which is statement metadata beside the query.
iam_instance_profile_role_edge_operation_match="MERGE (profile)-[rel:HAS_ROLE]->(role)"
# The CAN_ASSUME MERGE anchor cell_failgraphwrite_iam_can_assume (#6228 fault
# half) targets: go/internal/storage/cypher/cloud/aws/iam/can_assume_edge_writer.go:45
# emits MERGE (principal)-[rel:%s]->(role), filled from a closed
# single-member vocabulary, so the anchor carries the INTERPOLATED type. A
# literal %s would match no executed statement and the fault would never
# fire. It is NOT IAM_CAN_ASSUME, which is statement metadata beside the
# query.
iam_can_assume_edge_operation_match="MERGE (principal)-[rel:CAN_ASSUME]->(role)"
# The CAN_PERFORM MERGE anchor cell_failgraphwrite_iam_can_perform (#6228
# fault half) targets: go/internal/storage/cypher/cloud/aws/iam/can_perform_edge_writer.go:49
# bakes the static CAN_PERFORM token directly into the template
# (CanonicalIAMCanPerformEdgeUpsertCypher carries no %s to fill), so the
# anchor carries the literal. A `%s` form would match no executed statement
# and the fault would never fire. It is NOT IAM_CAN_PERFORM, which appears
# nowhere in code.
iam_can_perform_edge_operation_match="MERGE (p)-[rel:CAN_PERFORM]->(r)"
# The USES_PROFILE MERGE anchor cell_failgraphwrite_ec2_uses_profile (#6228
# fault half) targets: go/internal/storage/cypher/cloud/aws/ec2/uses_profile_edge_writer.go:47
# emits MERGE (source)-[rel:%s]->(target), filled per row from a closed
# single-member vocabulary screened by validateEC2UsesProfileRelationshipType,
# so the anchor carries the INTERPOLATED type. A literal %s would match no
# executed statement and the fault would never fire. It is NOT
# EC2_USES_PROFILE, which is statement metadata beside the query.
ec2_uses_profile_edge_operation_match="MERGE (source)-[rel:USES_PROFILE]->(target)"
# The LOGS_TO MERGE anchor cell_failgraphwrite_s3_logs_to (#6228 fault
# half) targets: go/internal/storage/cypher/cloud/aws/s3/logs_to_edge_writer.go:45
# emits MERGE (source)-[rel:%s]->(target), filled from a closed
# single-member vocabulary, so the anchor carries the INTERPOLATED type. A
# literal %s would match no executed statement and the fault would never
# fire. It is NOT S3_LOGS_TO, which is statement metadata beside the
# query.
s3_logs_to_edge_operation_match="MERGE (source)-[rel:LOGS_TO]->(target)"
# The AWS writer MERGE anchor cell_failgraphwrite_cloud_resource (#8358)
# targets: go/internal/storage/cypher/cloud/resource/edge_writer.go's
# canonicalCloudResourceEdgeUpsertCypherFormat emits
# MERGE (source)-[rel:%s]->(target), filled per row from
# AWSRelationshipCypherTypePrefix + the collector verb (an OPEN set), so the
# anchor carries the interpolated type of the one verb the family cassette
# drives first. A literal %s would match no executed statement and the fault
# would never fire. It is NOT AWS_RELATIONSHIP, which is statement metadata
# beside the query.
cloud_resource_edge_operation_match="MERGE (source)-[rel:AWS_lambda_function_uses_execution_role]->(target)"
# The CAN_ESCALATE_TO MERGE anchor cell_failgraphwrite_iam_escalation (#6228
# fault half) targets:
# go/internal/storage/cypher/cloud/aws/iam/escalation_edge_writer.go:42 bakes
# the static token into the template (no %s), so the anchor carries the
# literal. It is NOT IAM_ESCALATION, which appears nowhere in code.
iam_escalation_edge_operation_match="MERGE (p)-[rel:CAN_ESCALATE_TO]->(t)"
# The SECRETS_IAM_AUTHENTICATES_TO_VAULT_ROLE MERGE anchor
# cell_failgraphwrite_authenticates_vault_role (#6228 slice A) targets:
# go/internal/storage/cypher/cloud/aws/iam/secrets_graph_writer.go:100 bakes
# the static token into the template (no %s), so the anchor carries the
# literal. None of the port-suffix labels appears in code.
authenticates_vault_role_edge_operation_match="MERGE (s)-[rel:SECRETS_IAM_AUTHENTICATES_TO_VAULT_ROLE]->(v)"
# The SECRETS_IAM_USES_VAULT_POLICY MERGE anchor
# cell_failgraphwrite_uses_vault_policy and cell_kill_uses_vault_policy
# (#6228 slice A + #8539 seam) target -- both cells match on this same
# substring, the once-fault failing it and the hold engaging on it:
# go/internal/storage/cypher/cloud/aws/iam/secrets_graph_writer.go:111 bakes
# the static token into the template (no %s), so the anchor carries the
# literal. None of the port-suffix labels appears in code.
uses_vault_policy_edge_operation_match="MERGE (v)-[rel:SECRETS_IAM_USES_VAULT_POLICY]->(p)"
# The BUILT_FROM MERGE anchor cell_failgraphwrite_built_from (#6228 slice C
# live leaf) targets: go/internal/storage/cypher/edge/writer/provenance.go:172
# emits MERGE (img)-[rel:BUILT_FROM { with the scope_id/evidence_source
# identity map, so the anchor carries the PREFIX form. A full `]->(repo)`
# spelling would match no executed statement and the fault would never fire.
built_from_edge_operation_match="MERGE (img)-[rel:BUILT_FROM"
# The AWS_lambda_function_uses_image MERGE anchor
# cell_failgraphwrite_cloud_resource_container_image (#6228 slice D live
# leaf) targets:
# go/internal/storage/cypher/cloud/aws/image/container_edge_writer.go:72-75
# emits MERGE (source)-[rel:%s]->(target) with the %s filled per row from the
# writer's closed single-member vocabulary, so the anchor carries the
# interpolated AWS_lambda_function_uses_image form. A literal `%s` spelling
# would match no executed statement and the fault would never fire.
cloud_resource_container_image_edge_operation_match="MERGE (source)-[rel:AWS_lambda_function_uses_image]->(target)"
# The SATISFIED_BY MERGE anchor cell_failgraphwrite_crossplane_satisfied_by
# (#6228 crossplane live leaf) targets:
# go/internal/storage/cypher/canonical/crossplane/edges.go:33-42 emits
# MERGE (claim)-[rel:SATISFIED_BY]->(xrd) with no %s vocabulary
# substitution, so the anchor carries the template's own fixed spelling. A
# parameterized form would match no executed statement and the fault would
# never fire.
crossplane_satisfied_by_edge_operation_match="MERGE (claim)-[rel:SATISFIED_BY]->(xrd)"
# The DERIVED_FROM MERGE anchor cell_failgraphwrite_derived_from
# (#6228 derived_from live leaf) targets:
# go/internal/storage/cypher/edge/writer/derived_from.go emits
# MERGE (img)-[rel:DERIVED_FROM {scope_id, evidence_source}] with the
# identity the writer stamps per run, so the anchor carries the PREFIX
# form (the same rationale as the built_from pin). A full `]->(base)`
# spelling would match no executed statement and the fault would never
# fire.
derived_from_edge_operation_match="MERGE (img)-[rel:DERIVED_FROM"
# The AWS_COVERS_* MERGE anchor cell_failgraphwrite_observability_coverage
# (#6228 observability_coverage live leaf) targets:
# go/internal/storage/cypher/edge/writer/observability_coverage.go emits
# MERGE (obs)-[rel:AWS_COVERS_<signal>]->(target) with one statement per
# signal from the closed 5-member coverage-signal vocabulary, so the anchor
# carries the PREFIX form (the same rationale as the built_from pin). A
# full `]->(target)` spelling matches no single executed statement, and
# pinning one filled type would leave the other four unfaulted.
observability_coverage_edge_operation_match="MERGE (obs)-[rel:AWS_COVERS_"
# The PUBLISHES MERGE anchor cell_failgraphwrite_publishes (#6228
# publishes live leaf) targets:
# go/internal/storage/cypher/edge/writer/provenance.go:140,154 emits
# MERGE (repo)-[rel:PUBLISHES { with the scope_id/evidence_source
# identity map, so the anchor carries the PREFIX form. A full `]->(target)`
# spelling would match no executed statement and the fault would never fire.
publishes_edge_operation_match="MERGE (repo)-[rel:PUBLISHES"
# The TAINT_FLOWS_TO MERGE anchor cell_failgraphwrite_code_interproc_evidence
# (#6228 code_interproc_evidence live leaf) targets:
# go/internal/storage/cypher/edge/writer/code_interproc_evidence.go:29 emits
# MERGE (s)-[rel:TAINT_FLOWS_TO {evidence_uid: row.uid}]->(t) with no
# vocabulary substitution, so the anchor carries the PREFIX form that
# matches executed statement text (the template's MERGE carries the property
# map; the bare `]->(t)` spelling never appears there). A parameterized form
# would match the retract DELETEs instead and the fault would fire outside
# the upsert the cell asserts.
code_interproc_evidence_edge_operation_match="MERGE (s)-[rel:TAINT_FLOWS_TO"
# The HAS_TAINT_EVIDENCE MERGE anchor cell_failgraphwrite_code_taint_evidence
# (#6228 code_taint_evidence live leaf) targets:
# go/internal/storage/cypher/edge/writer/code_taint_evidence.go:47 emits
# MERGE (f)-[rel:HAS_TAINT_EVIDENCE]->(ev) with no vocabulary
# substitution and no property map on the MERGE itself (the SETs follow
# it), so the anchor carries the FULL form that matches executed statement
# text. A parameterized form would match the retract DELETEs instead, and a
# PREFIX form stopping before `->(ev)` would also match the node MERGE
# above it -- either way the fault would fire outside the upsert the cell
# asserts.
code_taint_evidence_edge_operation_match="MERGE (f)-[rel:HAS_TAINT_EVIDENCE]->(ev)"
# The HAS_*_ROUTING MERGE anchor cell_failgraphwrite_incident_routing_evidence
# (#6228 incident_routing_evidence live leaf) targets:
# go/internal/storage/cypher/edge/writer/incident_routing_evidence.go:73 emits
# MERGE (incident)-[rel:%s]->(routing) with one statement per slot from the
# closed 3-member routing-slot vocabulary, so the anchor carries the PREFIX
# form (the same rationale as the built_from pin). A full `]->(routing)`
# spelling matches no single executed statement, and pinning one filled type
# would leave the other two slots unfaulted.
incident_routing_evidence_edge_operation_match="MERGE (incident)-[rel:HAS_"

# The DOCUMENTS edge MERGE anchor cell_failgraphwrite_documentation targets:
# go/internal/storage/cypher/canonical/node/documentation_edges.go's
# batchCanonicalDocumentationEntityEdgeCypher emits this exact MERGE clause
# text regardless of which target label (Function/Class/.../SqlTable) the
# preceding MATCH picked -- a fixed, grep-stable substring, same rationale as
# cloud_resource_operation_match/sql_edge_operation_match above.
documentation_edge_operation_match="MERGE (section)-[rel:DOCUMENTS]->(target)"

# The CORRELATES_DEPLOYABLE_UNIT MERGE anchor cell_failgraphwrite_deployable_unit
# (#5993) targets: go/internal/storage/cypher/canonical/node/deployable_unit_edges.go's
# batchCanonicalDeployableUnitCorrelationUpsertCypher emits this exact MERGE
# clause text -- a fixed, grep-stable substring, same rationale as
# cloud_resource_operation_match above. Byte-exact copy of that file's line 9.
deployable_unit_edge_operation_match="MERGE (source_repo)-[rel:CORRELATES_DEPLOYABLE_UNIT]->(deployment_repo)"
