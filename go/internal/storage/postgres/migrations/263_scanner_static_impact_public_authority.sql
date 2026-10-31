-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- A static finding fact ID can be rewritten by a later impact pass. Preserve
-- each protected authority revision and match readers to the fact's current
-- impact fence and payload hash. A missing revision in protected read mode
-- means unknown authority, including for historical pre-registration facts.
CREATE TABLE IF NOT EXISTS scanner_static_impact_finding_authority_revisions (
    finding_fact_id TEXT NOT NULL,
    impact_scope_id TEXT NOT NULL,
    impact_generation_id TEXT NOT NULL,
    impact_fencing_token BIGINT NOT NULL CHECK (impact_fencing_token > 0),
    finding_payload_sha256 TEXT NOT NULL CHECK
      (finding_payload_sha256 ~ '^[0-9a-f]{64}$'),
    advisory_dependency_count INTEGER NOT NULL CHECK
      (advisory_dependency_count >= 0),
    pinned_role_policy_revision BIGINT NOT NULL CHECK
      (pinned_role_policy_revision > 0),
    pinned_database_identity UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (finding_fact_id,impact_fencing_token)
);
CREATE INDEX IF NOT EXISTS scanner_static_impact_authority_owner_idx
    ON scanner_static_impact_finding_authority_revisions
    (impact_scope_id,impact_generation_id,impact_fencing_token,finding_fact_id);

-- Every advisory evidence fact used by that static finding must point to its
-- exact accepted producer receipt. The protected member FK prevents a writer
-- from inventing an unrelated fact ID. Current registration, producer,
-- activation, expiry, fact hash, and role witnesses are rechecked on read.
CREATE TABLE IF NOT EXISTS scanner_static_impact_public_advisory_evidence (
    finding_fact_id TEXT NOT NULL,
    impact_fencing_token BIGINT NOT NULL,
    evidence_fact_id TEXT NOT NULL,
    registration_id TEXT NOT NULL REFERENCES
      scanner_advisory_public_registrations(registration_id),
    generation_receipt_id TEXT NOT NULL,
    registration_epoch BIGINT NOT NULL CHECK (registration_epoch > 0),
    activation_epoch BIGINT NOT NULL CHECK (activation_epoch > 0),
    evidence_payload_sha256 TEXT NOT NULL CHECK
      (evidence_payload_sha256 ~ '^[0-9a-f]{64}$'),
    PRIMARY KEY (finding_fact_id,impact_fencing_token,evidence_fact_id),
    FOREIGN KEY (finding_fact_id,impact_fencing_token) REFERENCES
      scanner_static_impact_finding_authority_revisions
      (finding_fact_id,impact_fencing_token),
    FOREIGN KEY (generation_receipt_id,evidence_fact_id) REFERENCES
      scanner_advisory_generation_facts(receipt_id,fact_id)
);
CREATE INDEX IF NOT EXISTS scanner_static_impact_public_evidence_authority_idx
    ON scanner_static_impact_public_advisory_evidence
    (registration_id,generation_receipt_id,evidence_fact_id);

REVOKE ALL ON scanner_static_impact_finding_authority_revisions,
    scanner_static_impact_public_advisory_evidence FROM PUBLIC;
