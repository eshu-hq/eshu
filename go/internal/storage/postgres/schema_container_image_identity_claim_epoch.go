// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

const containerImageIdentityClaimEpochTriggerSchemaSQL = `    EXECUTE $ddl$
        CREATE OR REPLACE FUNCTION advance_container_image_identity_claim_epoch()
        RETURNS trigger
        LANGUAGE plpgsql
        AS $function$
        BEGIN
            IF NEW.container_image_identity_claim_epoch =
                OLD.container_image_identity_claim_epoch THEN
                IF OLD.container_image_identity_v2_required THEN
                    RAISE EXCEPTION USING
                        ERRCODE = '55000',
                        MESSAGE = 'legacy container image identity claim is incompatible with completed image_ref_v2 cutover';
                END IF;
                NEW.container_image_identity_claim_epoch :=
                    OLD.container_image_identity_claim_epoch + 1;
            ELSIF NEW.container_image_identity_claim_epoch <>
                OLD.container_image_identity_claim_epoch + 1 THEN
                RAISE EXCEPTION USING
                    ERRCODE = '55000',
                    MESSAGE = 'container image identity claim epoch must advance exactly once';
            END IF;
            IF OLD.container_image_identity_v2_required THEN
                NEW.status := 'running';
                NEW.container_image_identity_v2_authorized_status :=
                    'running';
            END IF;
            RETURN NEW;
        END;
        $function$
    $ddl$;

    EXECUTE $ddl$
        CREATE TRIGGER fact_work_items_container_image_identity_claim_epoch_advance
        BEFORE UPDATE OF
            last_attempt_at,
            container_image_identity_claim_epoch
        ON fact_work_items
        FOR EACH ROW
        WHEN (
            OLD.domain = 'container_image_identity'
            AND (
                OLD.container_image_identity_v2_required
                OR NEW.container_image_identity_claim_epoch <>
                    OLD.container_image_identity_claim_epoch + 1
            )
        )
        EXECUTE FUNCTION advance_container_image_identity_claim_epoch()
    $ddl$;`
