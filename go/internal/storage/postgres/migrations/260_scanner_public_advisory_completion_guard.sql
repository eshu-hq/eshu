-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- A stale collector must not report success for a current public scope by
-- using the generic CompleteClaim path. This trigger also covers direct DML.
-- It only reads protected witnesses and does not create public authority.
CREATE OR REPLACE FUNCTION eshu_security.guard_public_advisory_completion_v1()
RETURNS TRIGGER LANGUAGE plpgsql SECURITY INVOKER
SET search_path = pg_catalog, pg_temp AS $$
BEGIN
    IF NEW.status<>'completed' OR OLD.status='completed'
       OR OLD.collector_kind<>'vulnerability_intelligence'
       OR NOT EXISTS (
         SELECT 1 FROM public.scanner_advisory_public_registrations AS reg
         WHERE reg.collector_instance_id=OLD.collector_instance_id
           AND reg.advisory_scope_id=OLD.scope_id AND reg.active) THEN
        RETURN NEW;
    END IF;
    IF NEW.scope_id<>OLD.scope_id OR OLD.status<>'claimed'
       OR OLD.current_claim_id IS NULL OR OLD.current_owner_id IS NULL
       OR OLD.current_fencing_token<=0
       OR OLD.lease_expires_at<=clock_timestamp()
       OR NOT EXISTS (
         SELECT 1
         FROM public.scanner_advisory_generation_receipts AS receipt
         JOIN public.scanner_advisory_public_registrations AS reg
           ON reg.registration_id=receipt.registration_id
          AND reg.collector_instance_id=OLD.collector_instance_id
          AND reg.advisory_scope_id=OLD.scope_id
          AND reg.active
          AND reg.authority_epoch=receipt.pinned_registration_epoch
          AND reg.max_age_seconds=receipt.pinned_max_age_seconds
         JOIN public.scanner_advisory_public_generations AS approved
           ON approved.registration_id=reg.registration_id
          AND approved.advisory_generation_id=receipt.advisory_generation_id
          AND approved.generation_receipt_id=receipt.receipt_id
          AND approved.active
          AND approved.approved_authority_epoch=reg.authority_epoch
          AND approved.fact_set_sha256=receipt.fact_set_sha256
          AND approved.approved_max_age_seconds=reg.max_age_seconds
          AND approved.expires_at>clock_timestamp()
         JOIN public.scanner_advisory_public_activations AS activation
           ON activation.collector_instance_id=reg.collector_instance_id
          AND activation.advisory_scope_id=reg.advisory_scope_id
          AND activation.active
          AND activation.active_generation_id=approved.advisory_generation_id
          AND activation.activation_epoch=approved.protected_activation_epoch
         JOIN public.collector_instances AS instance
           ON instance.instance_id=reg.collector_instance_id
          AND instance.collector_kind='vulnerability_intelligence'
          AND instance.enabled AND instance.claims_enabled
          AND reg.configuration_sha256=encode(pg_catalog.sha256(
              pg_catalog.convert_to(instance.configuration::text,'UTF8')),'hex')
         JOIN public.scanner_authority_epochs AS instance_epoch
           ON instance_epoch.authority_kind='collector_instance'
          AND instance_epoch.authority_key=jsonb_build_array(instance.instance_id)::text
          AND instance_epoch.authority_epoch=reg.pinned_collector_instance_epoch
          AND instance_epoch.active
         JOIN public.scanner_advisory_producer_grants AS producer_grant
           ON producer_grant.principal_oid=receipt.producer_principal_oid
          AND producer_grant.collector_instance_id=reg.collector_instance_id
          AND producer_grant.advisory_scope_id=reg.advisory_scope_id
          AND producer_grant.grant_epoch=receipt.pinned_producer_grant_epoch
          AND producer_grant.active
         JOIN eshu_security.role_policy_state AS role_policy
           ON role_policy.singleton AND role_policy.active
          AND role_policy.policy_revision=receipt.pinned_role_policy_revision
          AND role_policy.database_identity=receipt.pinned_database_identity
         WHERE receipt.work_item_id=OLD.work_item_id
           AND receipt.claim_id=OLD.current_claim_id
           AND receipt.claim_fencing_token=OLD.current_fencing_token
           AND receipt.claim_owner_id=OLD.current_owner_id
           AND receipt.status='published'
           AND receipt.fact_set_sha256=approved.fact_set_sha256
           AND receipt.pinned_collector_instance_epoch=instance_epoch.authority_epoch
           AND receipt.pinned_configuration_sha256=reg.configuration_sha256
           AND receipt.pinned_role_policy_revision=role_policy.policy_revision
           AND receipt.pinned_database_identity=role_policy.database_identity) THEN
        RAISE EXCEPTION 'public advisory claim lacks current protected publication';
    END IF;
    RETURN NEW;
END;
$$;
REVOKE ALL ON FUNCTION eshu_security.guard_public_advisory_completion_v1()
    FROM PUBLIC;
CREATE OR REPLACE TRIGGER scanner_public_advisory_completion_guard
BEFORE UPDATE OF status ON public.workflow_work_items
FOR EACH ROW EXECUTE FUNCTION
    eshu_security.guard_public_advisory_completion_v1();
