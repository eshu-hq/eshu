package postgres

const claimReducerWorkQuery = `
WITH candidate AS (
    SELECT work_item_id
    FROM fact_work_items
    WHERE stage = 'reducer'
      AND status IN ('pending', 'retrying', 'claimed', 'running')
      AND (visible_at IS NULL OR visible_at <= $1)
      AND (claim_until IS NULL OR claim_until <= $1)
      AND ($2::text[] IS NULL OR domain = ANY($2::text[]))
      -- NornicDB local_authoritative first-generation runs must not let
      -- reducer graph writes contend with source-local canonical projection
      -- for the same scope. Unrelated scopes can continue draining.
      AND ($5 = false OR NOT EXISTS (
          SELECT 1
          FROM fact_work_items AS projector_work
          WHERE projector_work.stage = 'projector'
            AND projector_work.scope_id = fact_work_items.scope_id
            AND projector_work.status IN ('pending', 'retrying', 'claimed', 'running')
      ))
      -- Semantic entity materialization writes labels onto source-local
      -- content-entity nodes. On NornicDB, running those writes while any
      -- source-local projection is still active causes cross-scope graph
      -- backend contention and retry storms; non-semantic reducer domains can
      -- still drain once their own scope has passed the gate above.
      AND ($5 = false OR domain <> 'semantic_entity_materialization' OR NOT EXISTS (
          SELECT 1
          FROM fact_work_items AS projector_any
          WHERE projector_any.stage = 'projector'
            AND projector_any.domain = 'source_local'
            AND projector_any.status IN ('pending', 'retrying', 'claimed', 'running')
      ))
      -- In local-host watch mode the ingester discovers and enqueues source
      -- projector work incrementally. A temporary enqueue gap is not proof
      -- that the whole corpus has drained, so semantic reducers can also wait
      -- for the owner-discovered source-local success count.
      AND ($5 = false OR domain <> 'semantic_entity_materialization' OR $6 <= 0 OR (
          SELECT count(*)
          FROM fact_work_items AS projector_done
          WHERE projector_done.stage = 'projector'
            AND projector_done.domain = 'source_local'
            AND projector_done.status = 'succeeded'
      ) >= $6)
      -- NornicDB's semantic label update path is backed by the same label/uid
      -- indexes touched by source-local canonical entities. Eight concurrent
      -- semantic writers have repeatedly timed out on tiny row sets, so cap
      -- only this reducer domain while preserving concurrency for unrelated
      -- reducer domains.
      AND ($5 = false OR domain <> 'semantic_entity_materialization' OR (
          SELECT count(*)
          FROM fact_work_items AS semantic_inflight
          WHERE semantic_inflight.stage = 'reducer'
            AND semantic_inflight.domain = 'semantic_entity_materialization'
            AND semantic_inflight.work_item_id <> fact_work_items.work_item_id
            AND semantic_inflight.status IN ('claimed', 'running')
            AND semantic_inflight.claim_until > $1
      ) < $7)
      -- AWS relationship edges, observability COVERS edges, IAM CAN_ASSUME trust
      -- edges, S3 LOGS_TO log-delivery edges, S3 external-principal grant
      -- edges, RDS posture node-property updates, IAM instance-profile HAS_ROLE
      -- edges, and S3/EC2 internet-exposure node properties all consume
      -- CloudResource nodes produced by their payload entity-key readiness slice.
      -- Keep those graph-write domains pending or retrying until canonical nodes
      -- are visibly committed instead of claiming them and recording retryable
      -- reducer failures.
      AND (domain NOT IN ('aws_relationship_materialization', 'observability_coverage_materialization', 'iam_can_assume_materialization', 's3_logs_to_materialization', 's3_external_principal_grant_materialization', 'rds_posture_materialization', 'iam_instance_profile_role_materialization', 'ec2_internet_exposure_materialization', 's3_internet_exposure_materialization') OR EXISTS (
          SELECT 1
          FROM graph_projection_phase_state AS aws_nodes
          WHERE aws_nodes.scope_id = fact_work_items.scope_id
            AND aws_nodes.acceptance_unit_id = COALESCE(NULLIF(fact_work_items.payload->>'entity_key', ''), fact_work_items.scope_id)
            AND aws_nodes.source_run_id = fact_work_items.generation_id
            AND aws_nodes.generation_id = fact_work_items.generation_id
            AND aws_nodes.keyspace = 'cloud_resource_uid'
            AND aws_nodes.phase = 'canonical_nodes_committed'
      ))
      -- The EC2 USES_PROFILE edge (#1146 PR-B) consumes TWO CloudResource node
      -- families that publish their canonical_nodes_committed phase under DIFFERENT
      -- entity keys for the same scope/generation: the EC2 instance source node
      -- (ec2_instance_node_materialization:<scope>, #1146 PR-A) and the IAM
      -- instance-profile target node (aws_resource_materialization:<scope>, #805).
      -- A single payload->>'entity_key' match cannot express a two-key requirement,
      -- so the gate requires both literal-prefix entity keys derived from scope_id.
      -- Keep the edge domain pending or retrying until BOTH node phases are visibly
      -- committed instead of claiming it and resolving an edge against a
      -- not-yet-materialized endpoint (a silent missed edge).
      AND (domain <> 'ec2_uses_profile_materialization' OR (
          EXISTS (
              SELECT 1 FROM graph_projection_phase_state AS ec2_uses_profile_instance_node
              WHERE ec2_uses_profile_instance_node.scope_id = fact_work_items.scope_id
                AND ec2_uses_profile_instance_node.acceptance_unit_id = 'ec2_instance_node_materialization:' || fact_work_items.scope_id
                AND ec2_uses_profile_instance_node.source_run_id = fact_work_items.generation_id
                AND ec2_uses_profile_instance_node.generation_id = fact_work_items.generation_id
                AND ec2_uses_profile_instance_node.keyspace = 'cloud_resource_uid'
                AND ec2_uses_profile_instance_node.phase = 'canonical_nodes_committed'
          )
          AND EXISTS (
              SELECT 1 FROM graph_projection_phase_state AS ec2_uses_profile_profile_node
              WHERE ec2_uses_profile_profile_node.scope_id = fact_work_items.scope_id
                AND ec2_uses_profile_profile_node.acceptance_unit_id = 'aws_resource_materialization:' || fact_work_items.scope_id
                AND ec2_uses_profile_profile_node.source_run_id = fact_work_items.generation_id
                AND ec2_uses_profile_profile_node.generation_id = fact_work_items.generation_id
                AND ec2_uses_profile_profile_node.keyspace = 'cloud_resource_uid'
                AND ec2_uses_profile_profile_node.phase = 'canonical_nodes_committed'
          )
      ))
      -- The live-workload RUNS_IMAGE edge consumes KubernetesWorkload nodes
      -- produced by the kubernetes_workload_materialization domain for the exact
      -- same scope/generation/entity-key readiness slice, but on the
      -- kubernetes_workload_uid keyspace (a different node family than the AWS and
      -- observability edges above). Keep the edge domain pending or retrying until
      -- those workload nodes are visibly committed instead of claiming it and
      -- recording a retryable reducer failure.
      AND (domain <> 'kubernetes_correlation_materialization' OR EXISTS (
          SELECT 1
          FROM graph_projection_phase_state AS kube_nodes
          WHERE kube_nodes.scope_id = fact_work_items.scope_id
            AND kube_nodes.acceptance_unit_id = COALESCE(NULLIF(fact_work_items.payload->>'entity_key', ''), fact_work_items.scope_id)
            AND kube_nodes.source_run_id = fact_work_items.generation_id
            AND kube_nodes.generation_id = fact_work_items.generation_id
            AND kube_nodes.keyspace = 'kubernetes_workload_uid'
            AND kube_nodes.phase = 'canonical_nodes_committed'
      ))
      -- The security-group reachability edge (ALLOWS_INGRESS/EGRESS + TO, #1135
      -- PR2b Option D) consumes THREE node families for the exact same
      -- scope/generation/entity-key readiness slice: the :SecurityGroupRule nodes
      -- (security_group_rule_uid), the CidrBlock/PrefixList endpoint nodes
      -- (security_group_endpoint_uid, #1135 PR2a), and the SecurityGroup
      -- CloudResource nodes (cloud_resource_uid, #805). Keep the edge domain
      -- pending or retrying until ALL THREE phases are visibly committed instead
      -- of claiming it and recording a retryable reducer failure.
      AND (domain <> 'security_group_reachability_materialization' OR (
          EXISTS (
              SELECT 1 FROM graph_projection_phase_state AS sg_rule_nodes
              WHERE sg_rule_nodes.scope_id = fact_work_items.scope_id
                AND sg_rule_nodes.acceptance_unit_id = COALESCE(NULLIF(fact_work_items.payload->>'entity_key', ''), fact_work_items.scope_id)
                AND sg_rule_nodes.source_run_id = fact_work_items.generation_id
                AND sg_rule_nodes.generation_id = fact_work_items.generation_id
                AND sg_rule_nodes.keyspace = 'security_group_rule_uid'
                AND sg_rule_nodes.phase = 'canonical_nodes_committed'
          )
          AND EXISTS (
              SELECT 1 FROM graph_projection_phase_state AS sg_endpoint_nodes
              WHERE sg_endpoint_nodes.scope_id = fact_work_items.scope_id
                AND sg_endpoint_nodes.acceptance_unit_id = COALESCE(NULLIF(fact_work_items.payload->>'entity_key', ''), fact_work_items.scope_id)
                AND sg_endpoint_nodes.source_run_id = fact_work_items.generation_id
                AND sg_endpoint_nodes.generation_id = fact_work_items.generation_id
                AND sg_endpoint_nodes.keyspace = 'security_group_endpoint_uid'
                AND sg_endpoint_nodes.phase = 'canonical_nodes_committed'
          )
          AND EXISTS (
              SELECT 1 FROM graph_projection_phase_state AS sg_cloud_nodes
              WHERE sg_cloud_nodes.scope_id = fact_work_items.scope_id
                AND sg_cloud_nodes.acceptance_unit_id = COALESCE(NULLIF(fact_work_items.payload->>'entity_key', ''), fact_work_items.scope_id)
                AND sg_cloud_nodes.source_run_id = fact_work_items.generation_id
                AND sg_cloud_nodes.generation_id = fact_work_items.generation_id
                AND sg_cloud_nodes.keyspace = 'cloud_resource_uid'
                AND sg_cloud_nodes.phase = 'canonical_nodes_committed'
          )
      ))
      -- Reducer domains can touch the same graph nodes for a scope. Fence by
      -- explicit conflict key so unrelated graph families can still overlap.
      AND NOT EXISTS (
          SELECT 1
          FROM fact_work_items AS inflight
          WHERE inflight.stage = 'reducer'
            AND inflight.conflict_domain = fact_work_items.conflict_domain
            AND COALESCE(inflight.conflict_key, inflight.scope_id) = COALESCE(fact_work_items.conflict_key, fact_work_items.scope_id)
            AND inflight.work_item_id <> fact_work_items.work_item_id
            AND inflight.status IN ('claimed', 'running')
            AND inflight.claim_until > $1
      )
    ORDER BY updated_at ASC, work_item_id ASC
    LIMIT 1
    FOR UPDATE SKIP LOCKED
),
claimed AS (
    UPDATE fact_work_items AS work
    SET status = 'claimed',
        attempt_count = work.attempt_count + 1,
        lease_owner = $3,
        claim_until = $4,
        last_attempt_at = $1,
        updated_at = $1
    FROM candidate
    WHERE work.work_item_id = candidate.work_item_id
    RETURNING
        work.work_item_id,
        work.scope_id,
        work.generation_id,
        work.domain,
        work.attempt_count,
        work.created_at,
        COALESCE(work.visible_at, work.created_at) AS available_at,
        work.payload
)
SELECT
    work_item_id,
    scope_id,
    generation_id,
    domain,
    attempt_count,
    created_at,
    available_at,
    payload
FROM claimed
`
