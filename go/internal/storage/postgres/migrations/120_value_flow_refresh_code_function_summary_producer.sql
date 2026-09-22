-- #6923: admit code_function_summary as the fifth value-flow refresh
-- producer domain on installs that already applied migration 112 (which
-- itself converged 093's six-domain baseline).
--
-- Mirrors 112: fresh bootstraps that have not yet run this file still see
-- the older six-domain CHECK/trigger from 093/112, so the guards below
-- converge them once and no-op on every boot after. This directory has no
-- applied-migration ledger: BootstrapDefinitions enumerates every file under
-- migrations/ and ApplyDefinitions Execs all of them on EVERY bootstrap.
--
-- code_function_summary stopped solving the global value-flow fixpoint
-- inline (issue #6923: the inline solve was an unfenced second entry point
-- to the same global computation the #6785 refresh singleton runs, so a
-- generation with N repos ran the solve up to N+1 times and at least one run
-- could read the CAN_PERFORM/USES/RUNS_IN chain before it finished
-- materializing). It now ACKs into cross_scope_completion_events like the
-- other four producers when its affected-repo emit gate passes, and its
-- domain must satisfy the events-table CHECK; the rolling-upgrade fallback
-- trigger (fact_work_items_cross_scope_completion, which emits only when an
-- old producer ACK preserves cross_scope_completion_ack_epoch) must watch it
-- too: new-path ACKs bump the epoch, so the trigger fires only for old
-- reducers.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'cross_scope_completion_events_producer_domain_check'
          AND conrelid = 'cross_scope_completion_events'::regclass
          AND pg_get_constraintdef(oid) NOT LIKE '%code_function_summary%'
    ) THEN
        ALTER TABLE cross_scope_completion_events
            DROP CONSTRAINT cross_scope_completion_events_producer_domain_check;
        ALTER TABLE cross_scope_completion_events
            ADD CONSTRAINT cross_scope_completion_events_producer_domain_check
            CHECK (producer_domain IN ('aws_resource_materialization', 'ci_cd_run_correlation', 'code_function_summary', 'container_image_identity', 'iam_can_perform_materialization', 'workload_cloud_relationship_materialization', 'workload_materialization'));
    END IF;
    IF EXISTS (
        SELECT 1
        FROM pg_trigger
        WHERE tgname = 'fact_work_items_cross_scope_completion'
          AND tgrelid = 'fact_work_items'::regclass
          AND NOT tgisinternal
          AND pg_get_triggerdef(oid) NOT LIKE '%code_function_summary%'
    ) THEN
        DROP TRIGGER fact_work_items_cross_scope_completion ON fact_work_items;
        CREATE TRIGGER fact_work_items_cross_scope_completion
        AFTER UPDATE OF status ON fact_work_items
        FOR EACH ROW
        WHEN (
            OLD.stage = 'reducer'
            AND NEW.stage = 'reducer'
            AND OLD.domain = NEW.domain
            AND NEW.domain IN ('aws_resource_materialization', 'ci_cd_run_correlation', 'code_function_summary', 'container_image_identity', 'iam_can_perform_materialization', 'workload_cloud_relationship_materialization', 'workload_materialization')
            AND OLD.status IN ('claimed', 'running')
            AND NEW.status = 'succeeded'
            AND OLD.cross_scope_completion_ack_epoch = NEW.cross_scope_completion_ack_epoch
        )
        EXECUTE FUNCTION enqueue_cross_scope_completion_event();
    END IF;
END
$$;
