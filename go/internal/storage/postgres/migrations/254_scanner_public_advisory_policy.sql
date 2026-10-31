-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- The desired revision is retained across removal. A stale coordinator must
-- never recreate a public registration through the legacy collector upsert.
CREATE TABLE IF NOT EXISTS scanner_advisory_desired_policy (
    singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (singleton),
    desired_revision BIGINT NOT NULL CHECK (desired_revision > 0),
    desired_sha256 TEXT NOT NULL CHECK (desired_sha256 ~ '^[0-9a-f]{64}$'),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE TABLE IF NOT EXISTS scanner_advisory_owned_instances (
    instance_id TEXT PRIMARY KEY,
    adopted_revision BIGINT NOT NULL CHECK (adopted_revision > 0),
    active BOOLEAN NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
-- A private binding must pin the configuration that authorized its source.
-- Old rows have no such witness and remain ineligible for authorized reads.
ALTER TABLE scanner_advisory_private_bindings
    ADD COLUMN IF NOT EXISTS source_configuration_sha256 TEXT;
ALTER TABLE scanner_advisory_private_bindings
    ADD COLUMN IF NOT EXISTS advisory_collector_instance_id TEXT;
ALTER TABLE scanner_advisory_public_registrations
    ADD COLUMN IF NOT EXISTS max_age_seconds INTEGER;
REVOKE ALL ON scanner_advisory_desired_policy,
    scanner_advisory_owned_instances FROM PUBLIC;

-- Legacy binaries may still run their ordinary per-row upsert and stale-list
-- deactivation. Suppress only adopted instance rows, preserving siblings.
CREATE OR REPLACE FUNCTION eshu_security.fence_advisory_owned_instance_v1()
RETURNS TRIGGER LANGUAGE plpgsql SECURITY INVOKER
SET search_path = pg_catalog, pg_temp AS $$
DECLARE
    v_instance_id TEXT;
    v_owner_oid OID;
    v_current_oid OID;
BEGIN
    v_instance_id := CASE WHEN TG_OP='DELETE' THEN OLD.instance_id
                          ELSE NEW.instance_id END;
    IF NOT EXISTS (SELECT 1 FROM public.scanner_advisory_owned_instances AS owned
                   WHERE owned.instance_id=v_instance_id) THEN
        IF TG_OP='DELETE' THEN RETURN OLD; END IF;
        RETURN NEW;
    END IF;
    SELECT relowner INTO v_owner_oid FROM pg_catalog.pg_class
    WHERE oid='public.scanner_advisory_owned_instances'::regclass;
    SELECT oid INTO v_current_oid FROM pg_catalog.pg_roles
    WHERE rolname=CURRENT_USER;
    IF v_current_oid<>v_owner_oid THEN
        RETURN NULL;
    END IF;
    IF TG_OP='DELETE' THEN RETURN OLD; END IF;
    RETURN NEW;
END;
$$;
REVOKE ALL ON FUNCTION eshu_security.fence_advisory_owned_instance_v1()
    FROM PUBLIC;
CREATE OR REPLACE TRIGGER scanner_advisory_owned_instance_fence
BEFORE INSERT OR UPDATE OR DELETE ON public.collector_instances
FOR EACH ROW EXECUTE FUNCTION eshu_security.fence_advisory_owned_instance_v1();

-- The control principal supplies a desired revision and declarative instance
-- configuration. The DB derives each public grant from an exact configured
-- target; source labels, allowed kinds and profile are fixed here.
CREATE OR REPLACE FUNCTION eshu_security.reconcile_public_advisories_v1(
    p_expected_revision BIGINT,p_new_revision BIGINT,p_instances JSONB,
    p_role_revision BIGINT,p_database_identity UUID)
RETURNS BIGINT LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, pg_temp AS $$
DECLARE
    v_principal_oid OID;
    v_policy public.scanner_advisory_desired_policy%ROWTYPE;
    v_instance JSONB;
    v_config JSONB;
    v_declaration JSONB;
    v_target JSONB;
    v_instance_id TEXT;
    v_scope_id TEXT;
    v_source TEXT;
    v_kinds JSONB;
    v_seen_instances JSONB := '{}'::jsonb;
    v_seen_scopes JSONB;
    v_instance_ids TEXT[] := ARRAY[]::TEXT[];
    v_registration_ids TEXT[] := ARRAY[]::TEXT[];
    v_registration_id TEXT;
    v_fingerprint TEXT;
    v_config_hash TEXT;
    v_instance_epoch BIGINT;
    v_registration_epoch BIGINT;
    v_existing public.scanner_advisory_public_registrations%ROWTYPE;
    v_expected_source TEXT;
    v_registration_active BOOLEAN;
    v_max_age_seconds INTEGER;
BEGIN
    v_principal_oid := eshu_security.assert_protected_capability_v1(
        p_role_revision,p_database_identity,'authority-control');
    IF v_principal_oid IS NULL OR p_instances IS NULL
       OR jsonb_typeof(p_instances)<>'array'
       OR jsonb_array_length(p_instances)>256
       OR p_expected_revision<0 OR p_new_revision<=0 THEN
        RAISE EXCEPTION 'public advisory policy input invalid';
    END IF;
    SELECT encode(pg_catalog.sha256(pg_catalog.convert_to(
        COALESCE(string_agg(value::text,',' ORDER BY value->>'instance_id'),''),
        'UTF8')),'hex') INTO v_fingerprint
    FROM jsonb_array_elements(p_instances) AS desired(value);
    SELECT * INTO v_policy FROM public.scanner_advisory_desired_policy
    WHERE singleton FOR UPDATE;
    IF FOUND THEN
        IF p_new_revision=v_policy.desired_revision
           AND v_fingerprint=v_policy.desired_sha256 THEN
            RETURN v_policy.desired_revision;
        END IF;
        IF p_expected_revision<>v_policy.desired_revision
           OR p_new_revision<=v_policy.desired_revision THEN
            RAISE EXCEPTION 'public advisory policy revision conflict';
        END IF;
    ELSIF p_expected_revision<>0 THEN
        RAISE EXCEPTION 'public advisory initial revision conflict';
    END IF;
    -- This row lock is held through collector and registration writes. A
    -- competing HA coordinator waits, then observes the advanced revision.
    FOR v_instance IN SELECT value FROM jsonb_array_elements(p_instances)
                      AS desired(value) ORDER BY value->>'instance_id'
    LOOP
        v_instance_id := v_instance->>'instance_id';
        v_config := v_instance->'configuration';
        IF v_instance_id IS NULL OR v_instance_id=''
           OR length(v_instance_id)>256
           OR v_seen_instances ? v_instance_id
           OR v_instance->>'collector_kind'<>'vulnerability_intelligence'
           OR v_instance->>'mode' NOT IN ('continuous','scheduled','manual')
           OR jsonb_typeof(v_config)<>'object'
           OR octet_length(v_config::text)>65536
           OR jsonb_typeof(v_instance->'enabled')<>'boolean'
           OR jsonb_typeof(v_instance->'claims_enabled')<>'boolean'
           OR jsonb_typeof(v_instance->'bootstrap')<>'boolean'
           OR COALESCE(jsonb_typeof(v_config->'public_advisory_targets'),'array')<>'array'
           OR COALESCE(jsonb_array_length(v_config->'public_advisory_targets'),0)>64 THEN
            RAISE EXCEPTION 'public advisory instance invalid';
        END IF;
        v_seen_instances := v_seen_instances||jsonb_build_object(v_instance_id,TRUE);
        v_instance_ids := array_append(v_instance_ids,v_instance_id);
        INSERT INTO public.scanner_advisory_owned_instances
            (instance_id,adopted_revision,active,updated_at)
        VALUES (v_instance_id,p_new_revision,TRUE,NOW())
        ON CONFLICT ON CONSTRAINT scanner_advisory_owned_instances_pkey
        DO UPDATE SET adopted_revision=EXCLUDED.adopted_revision,
                      active=TRUE,updated_at=EXCLUDED.updated_at;
        INSERT INTO public.collector_instances
            (instance_id,collector_kind,mode,enabled,bootstrap,claims_enabled,
             display_name,configuration,last_observed_at,deactivated_at,
             created_at,updated_at)
        VALUES (v_instance_id,'vulnerability_intelligence',v_instance->>'mode',
                (v_instance->>'enabled')::BOOLEAN,
                (v_instance->>'bootstrap')::BOOLEAN,
                (v_instance->>'claims_enabled')::BOOLEAN,
                NULLIF(v_instance->>'display_name',''),v_config,NOW(),
                CASE WHEN (v_instance->>'enabled')::BOOLEAN THEN NULL ELSE NOW() END,
                NOW(),NOW())
        ON CONFLICT ON CONSTRAINT collector_instances_pkey DO UPDATE SET
            mode=EXCLUDED.mode,enabled=EXCLUDED.enabled,
            bootstrap=EXCLUDED.bootstrap,claims_enabled=EXCLUDED.claims_enabled,
            display_name=EXCLUDED.display_name,
            configuration=EXCLUDED.configuration,
            last_observed_at=EXCLUDED.last_observed_at,
            deactivated_at=EXCLUDED.deactivated_at,updated_at=EXCLUDED.updated_at;
        SELECT epoch.authority_epoch INTO v_instance_epoch
        FROM public.scanner_authority_epochs AS epoch
        WHERE epoch.authority_kind='collector_instance'
          AND epoch.authority_key=jsonb_build_array(v_instance_id)::text;
        IF v_instance_epoch IS NULL THEN
            RAISE EXCEPTION 'public advisory collector epoch unavailable';
        END IF;
        v_config_hash := encode(pg_catalog.sha256(
            pg_catalog.convert_to(v_config::text,'UTF8')),'hex');
        v_seen_scopes := '{}'::jsonb;
        FOR v_declaration IN
            SELECT value FROM jsonb_array_elements(
                COALESCE(v_config->'public_advisory_targets','[]'::jsonb))
                AS declaration(value)
            ORDER BY value->>'scope_id'
        LOOP
            v_scope_id := v_declaration->>'scope_id';
            IF v_scope_id IS NULL OR v_scope_id='' OR length(v_scope_id)>512
               OR v_seen_scopes ? v_scope_id
               OR jsonb_typeof(v_declaration->'max_age_seconds')<>'number'
               OR jsonb_typeof(v_config->'targets')<>'array' THEN
                RAISE EXCEPTION 'public advisory target declaration invalid';
            END IF;
            v_max_age_seconds := (v_declaration->>'max_age_seconds')::INTEGER;
            IF v_max_age_seconds NOT BETWEEN 1 AND 2592000
               OR v_declaration<>jsonb_build_object('scope_id',v_scope_id,
                                                     'max_age_seconds',v_max_age_seconds) THEN
                RAISE EXCEPTION 'public advisory max age invalid';
            END IF;
            v_seen_scopes := v_seen_scopes||jsonb_build_object(v_scope_id,TRUE);
            SELECT value INTO v_target FROM jsonb_array_elements(v_config->'targets')
                AS configured(value)
            WHERE value->>'scope_id'=v_scope_id;
            IF NOT FOUND OR (SELECT count(*) FROM jsonb_array_elements(
                v_config->'targets') AS configured(value)
                WHERE value->>'scope_id'=v_scope_id)<>1 THEN
                RAISE EXCEPTION 'public advisory target has no unique configured source';
            END IF;
            v_source := v_target->>'source';
            v_expected_source := CASE v_source
                WHEN 'nvd' THEN 'nvd'
                WHEN 'osv' THEN 'osv'
                WHEN 'cisa_kev' THEN 'cisa_kev'
                WHEN 'first_epss' THEN 'first_epss' END;
            v_kinds := CASE v_source
                WHEN 'nvd' THEN '["vulnerability.cve","vulnerability.affected_product"]'::jsonb
                WHEN 'osv' THEN '["vulnerability.cve","vulnerability.affected_package"]'::jsonb
                WHEN 'cisa_kev' THEN '["vulnerability.known_exploited"]'::jsonb
                WHEN 'first_epss' THEN '["vulnerability.epss_score"]'::jsonb END;
            IF v_expected_source IS NULL THEN
                RAISE EXCEPTION 'public advisory target source unsupported';
            END IF;
            v_registration_active := (v_instance->>'enabled')::BOOLEAN
                AND (v_instance->>'claims_enabled')::BOOLEAN;
            v_registration_id := 'public-advisory:'||encode(pg_catalog.sha256(
                pg_catalog.convert_to(jsonb_build_array(v_instance_id,v_scope_id)::text,
                                      'UTF8')),'hex');
            v_registration_ids := array_append(v_registration_ids,v_registration_id);
            SELECT * INTO v_existing FROM public.scanner_advisory_public_registrations
            WHERE collector_instance_id=v_instance_id
              AND advisory_scope_id=v_scope_id FOR UPDATE;
            IF NOT FOUND THEN
                INSERT INTO public.scanner_advisory_public_registrations
                    (registration_id,collector_instance_id,advisory_scope_id,
                     configuration_sha256,pinned_collector_instance_epoch,
                     trusted_source_class,provider_id,content_profile,
                     allowed_fact_kinds,max_age_seconds,active)
                VALUES (v_registration_id,v_instance_id,v_scope_id,
                        v_config_hash,v_instance_epoch,v_expected_source,
                        v_source,'supply_chain_impact_v1',v_kinds,
                        v_max_age_seconds,
                        v_registration_active);
            ELSIF v_existing.registration_id<>v_registration_id THEN
                RAISE EXCEPTION 'public advisory registration identity conflict';
            ELSIF v_existing.active<>v_registration_active
               OR v_existing.configuration_sha256<>v_config_hash
               OR v_existing.pinned_collector_instance_epoch<>v_instance_epoch
               OR v_existing.trusted_source_class<>v_expected_source
               OR v_existing.provider_id<>v_source
               OR v_existing.content_profile<>'supply_chain_impact_v1'
               OR v_existing.max_age_seconds IS DISTINCT FROM v_max_age_seconds
               OR v_existing.allowed_fact_kinds<>v_kinds THEN
                v_registration_epoch := nextval('public.scanner_authority_epoch_seq');
                UPDATE public.scanner_advisory_public_registrations SET
                    configuration_sha256=v_config_hash,
                    pinned_collector_instance_epoch=v_instance_epoch,
                    trusted_source_class=v_expected_source,provider_id=v_source,
                    content_profile='supply_chain_impact_v1',
                    allowed_fact_kinds=v_kinds,max_age_seconds=v_max_age_seconds,
                    authority_epoch=v_registration_epoch,
                    active=v_registration_active
                WHERE registration_id=v_registration_id;
            END IF;
        END LOOP;
    END LOOP;
    UPDATE public.scanner_advisory_public_registrations AS registration
    SET active=FALSE,authority_epoch=nextval('public.scanner_authority_epoch_seq')
    WHERE registration.active
      AND NOT (registration.registration_id=ANY(v_registration_ids));
    UPDATE public.collector_instances AS instance SET
        enabled=FALSE,claims_enabled=FALSE,deactivated_at=NOW(),updated_at=NOW()
    WHERE instance.instance_id IN
        (SELECT owned.instance_id FROM public.scanner_advisory_owned_instances AS owned
         WHERE owned.active AND NOT (owned.instance_id=ANY(v_instance_ids)))
      AND (instance.enabled OR instance.claims_enabled);
    UPDATE public.scanner_advisory_owned_instances AS owned SET
        active=FALSE,adopted_revision=p_new_revision,updated_at=NOW()
    WHERE owned.active AND NOT (owned.instance_id=ANY(v_instance_ids));
    INSERT INTO public.scanner_advisory_desired_policy
        (singleton,desired_revision,desired_sha256,updated_at)
    VALUES (TRUE,p_new_revision,v_fingerprint,NOW())
    ON CONFLICT ON CONSTRAINT scanner_advisory_desired_policy_pkey
    DO UPDATE SET desired_revision=EXCLUDED.desired_revision,
                  desired_sha256=EXCLUDED.desired_sha256,
                  updated_at=EXCLUDED.updated_at;
    RETURN p_new_revision;
END;
$$;
REVOKE ALL ON FUNCTION eshu_security.reconcile_public_advisories_v1(
    BIGINT,BIGINT,JSONB,BIGINT,UUID) FROM PUBLIC;
