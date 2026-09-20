-- #6785: admit the value-flow refresh producer domains to the cross-scope
-- completion path on installs that already applied migration 093.
--
-- Fresh bootstraps get the six-domain CHECK and trigger WHEN list from 093
-- directly; the guards below converge older installs once and are no-ops on
-- every boot after it. That matters because this directory has no
-- applied-migration ledger: BootstrapDefinitions enumerates every file under
-- migrations/ and ApplyDefinitions Execs all of them on EVERY bootstrap.
--
-- The four refresh producers (workload_materialization,
-- workload_cloud_relationship_materialization,
-- iam_can_perform_materialization, aws_resource_materialization) emit their
-- completion event in the same statement as their ACK when the affected-repo
-- emit gate passes. Their domains must satisfy the events-table CHECK, and
-- the rolling-upgrade fallback trigger
-- (fact_work_items_cross_scope_completion, which emits only when an old
-- producer ACK preserves cross_scope_completion_ack_epoch) must watch them:
-- new-path ACKs bump the epoch, so the trigger fires only for old reducers.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'cross_scope_completion_events_producer_domain_check'
          AND conrelid = 'cross_scope_completion_events'::regclass
          AND pg_get_constraintdef(oid) NOT LIKE '%workload_materialization%'
    ) THEN
        ALTER TABLE cross_scope_completion_events
            DROP CONSTRAINT cross_scope_completion_events_producer_domain_check;
        ALTER TABLE cross_scope_completion_events
            ADD CONSTRAINT cross_scope_completion_events_producer_domain_check
            CHECK (producer_domain IN ('aws_resource_materialization', 'ci_cd_run_correlation', 'container_image_identity', 'iam_can_perform_materialization', 'workload_cloud_relationship_materialization', 'workload_materialization'));
    END IF;
    IF EXISTS (
        SELECT 1
        FROM pg_trigger
        WHERE tgname = 'fact_work_items_cross_scope_completion'
          AND tgrelid = 'fact_work_items'::regclass
          AND NOT tgisinternal
          AND pg_get_triggerdef(oid) NOT LIKE '%workload_materialization%'
    ) THEN
        DROP TRIGGER fact_work_items_cross_scope_completion ON fact_work_items;
        CREATE TRIGGER fact_work_items_cross_scope_completion
        AFTER UPDATE OF status ON fact_work_items
        FOR EACH ROW
        WHEN (
            OLD.stage = 'reducer'
            AND NEW.stage = 'reducer'
            AND OLD.domain = NEW.domain
            AND NEW.domain IN ('aws_resource_materialization', 'ci_cd_run_correlation', 'container_image_identity', 'iam_can_perform_materialization', 'workload_cloud_relationship_materialization', 'workload_materialization')
            AND OLD.status IN ('claimed', 'running')
            AND NEW.status = 'succeeded'
            AND OLD.cross_scope_completion_ack_epoch = NEW.cross_scope_completion_ack_epoch
        )
        EXECUTE FUNCTION enqueue_cross_scope_completion_event();
    END IF;
END
$$;
