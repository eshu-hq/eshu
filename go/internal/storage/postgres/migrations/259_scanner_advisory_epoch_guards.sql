-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- Epochs are database assigned on every authority change, including a
-- revoke/regrant with identical config and a delete/reinsert of one key.
CREATE OR REPLACE FUNCTION eshu_security.bump_advisory_registration_epoch_v1()
RETURNS TRIGGER LANGUAGE plpgsql SECURITY INVOKER
SET search_path = pg_catalog, pg_temp AS $$
BEGIN
    IF TG_OP='INSERT' THEN
        NEW.authority_epoch := nextval('public.scanner_authority_epoch_seq');
    ELSIF ROW(NEW.configuration_sha256,NEW.pinned_collector_instance_epoch,
              NEW.trusted_source_class,NEW.provider_id,NEW.content_profile,
              NEW.allowed_fact_kinds,NEW.max_age_seconds,NEW.active)
          IS DISTINCT FROM
          ROW(OLD.configuration_sha256,OLD.pinned_collector_instance_epoch,
              OLD.trusted_source_class,OLD.provider_id,OLD.content_profile,
              OLD.allowed_fact_kinds,OLD.max_age_seconds,OLD.active) THEN
        NEW.authority_epoch := nextval('public.scanner_authority_epoch_seq');
    ELSE
        NEW.authority_epoch := OLD.authority_epoch;
    END IF;
    RETURN NEW;
END;
$$;
REVOKE ALL ON FUNCTION eshu_security.bump_advisory_registration_epoch_v1()
    FROM PUBLIC;
CREATE OR REPLACE TRIGGER scanner_advisory_registration_epoch_guard
BEFORE INSERT OR UPDATE ON public.scanner_advisory_public_registrations
FOR EACH ROW EXECUTE FUNCTION
    eshu_security.bump_advisory_registration_epoch_v1();

CREATE OR REPLACE FUNCTION eshu_security.bump_advisory_producer_grant_epoch_v1()
RETURNS TRIGGER LANGUAGE plpgsql SECURITY INVOKER
SET search_path = pg_catalog, pg_temp AS $$
BEGIN
    IF TG_OP='INSERT' THEN
        NEW.grant_epoch := nextval('public.scanner_authority_epoch_seq');
    ELSIF NEW.active IS DISTINCT FROM OLD.active
       OR NEW.principal_oid IS DISTINCT FROM OLD.principal_oid
       OR NEW.collector_instance_id IS DISTINCT FROM OLD.collector_instance_id
       OR NEW.advisory_scope_id IS DISTINCT FROM OLD.advisory_scope_id THEN
        NEW.grant_epoch := nextval('public.scanner_authority_epoch_seq');
    ELSE
        NEW.grant_epoch := OLD.grant_epoch;
    END IF;
    RETURN NEW;
END;
$$;
REVOKE ALL ON FUNCTION eshu_security.bump_advisory_producer_grant_epoch_v1()
    FROM PUBLIC;
CREATE OR REPLACE TRIGGER scanner_advisory_producer_grant_epoch_guard
BEFORE INSERT OR UPDATE ON public.scanner_advisory_producer_grants
FOR EACH ROW EXECUTE FUNCTION
    eshu_security.bump_advisory_producer_grant_epoch_v1();

-- Approved generation identity and evidence are immutable. Retirement is a
-- one-way transition; a new accepted receipt and activation is required.
CREATE OR REPLACE FUNCTION eshu_security.guard_advisory_generation_retirement_v1()
RETURNS TRIGGER LANGUAGE plpgsql SECURITY INVOKER
SET search_path = pg_catalog, pg_temp AS $$
BEGIN
    IF NOT OLD.active OR NEW.active
       OR ROW(NEW.registration_id,NEW.advisory_generation_id,
              NEW.approved_configuration_sha256,NEW.approved_authority_epoch,
              NEW.pinned_advisory_scope_epoch,NEW.generation_receipt_id,
              NEW.producer_principal_oid,NEW.producer_grant_epoch,
              NEW.pinned_collector_instance_epoch,NEW.protected_activation_epoch,
              NEW.fact_set_sha256,NEW.approved_observed_at,NEW.expires_at,
              NEW.approved_max_age_seconds,NEW.pinned_role_policy_revision,
              NEW.pinned_database_identity)
          IS DISTINCT FROM
          ROW(OLD.registration_id,OLD.advisory_generation_id,
              OLD.approved_configuration_sha256,OLD.approved_authority_epoch,
              OLD.pinned_advisory_scope_epoch,OLD.generation_receipt_id,
              OLD.producer_principal_oid,OLD.producer_grant_epoch,
              OLD.pinned_collector_instance_epoch,OLD.protected_activation_epoch,
              OLD.fact_set_sha256,OLD.approved_observed_at,OLD.expires_at,
              OLD.approved_max_age_seconds,OLD.pinned_role_policy_revision,
              OLD.pinned_database_identity) THEN
        RAISE EXCEPTION 'public advisory approved generation is immutable';
    END IF;
    RETURN NEW;
END;
$$;
REVOKE ALL ON FUNCTION eshu_security.guard_advisory_generation_retirement_v1()
    FROM PUBLIC;
CREATE OR REPLACE TRIGGER scanner_advisory_generation_retirement_guard
BEFORE UPDATE ON public.scanner_advisory_public_generations
FOR EACH ROW EXECUTE FUNCTION
    eshu_security.guard_advisory_generation_retirement_v1();

CREATE OR REPLACE FUNCTION eshu_security.guard_advisory_desired_revision_v1()
RETURNS TRIGGER LANGUAGE plpgsql SECURITY INVOKER
SET search_path = pg_catalog, pg_temp AS $$
BEGIN
    IF TG_OP='DELETE' THEN
        RAISE EXCEPTION 'public advisory desired policy tombstone is retained';
    END IF;
    IF NEW.desired_revision<=OLD.desired_revision THEN
        RAISE EXCEPTION 'public advisory desired revision must advance';
    END IF;
    RETURN NEW;
END;
$$;
REVOKE ALL ON FUNCTION eshu_security.guard_advisory_desired_revision_v1()
    FROM PUBLIC;
CREATE OR REPLACE TRIGGER scanner_advisory_desired_revision_guard
BEFORE UPDATE OR DELETE ON public.scanner_advisory_desired_policy
FOR EACH ROW EXECUTE FUNCTION
    eshu_security.guard_advisory_desired_revision_v1();
