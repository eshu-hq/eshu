-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- The scanner's source-fenced fact commit has a separate receipt from the
-- reducer's later finding publication. A fact ID appears in at most one
-- boundary-local scan result, and an absent association never means static.
CREATE TABLE IF NOT EXISTS scanner_image_scan_results (
    result_receipt_id TEXT PRIMARY KEY,
    work_item_id TEXT NOT NULL UNIQUE
      REFERENCES scanner_image_target_attempts(work_item_id),
    binding_id TEXT NOT NULL
      REFERENCES scanner_image_target_sources(binding_id),
    scan_scope_id TEXT NOT NULL REFERENCES scanner_image_targets(scan_scope_id),
    scan_generation_id TEXT NOT NULL,
    claim_id TEXT NOT NULL,
    claim_fencing_token BIGINT NOT NULL CHECK (claim_fencing_token > 0),
    claim_owner_id TEXT NOT NULL,
    completed_authority_vector JSONB NOT NULL,
    completed_at TIMESTAMPTZ NOT NULL,
    UNIQUE (result_receipt_id,work_item_id,binding_id,scan_scope_id,
            scan_generation_id)
);
CREATE TABLE IF NOT EXISTS scanner_image_scan_fact_associations (
    fact_id TEXT PRIMARY KEY,
    result_receipt_id TEXT NOT NULL
      REFERENCES scanner_image_scan_results(result_receipt_id),
    scan_scope_id TEXT NOT NULL REFERENCES scanner_image_targets(scan_scope_id),
    scan_generation_id TEXT NOT NULL,
    fact_kind TEXT NOT NULL CHECK (fact_kind IN
      ('scanner_worker.analysis','scanner_worker.warning',
       'vulnerability.os_package','vulnerability.warning'))
);
CREATE INDEX IF NOT EXISTS scanner_image_scan_fact_associations_target_idx
    ON scanner_image_scan_fact_associations
    (scan_scope_id,scan_generation_id,fact_kind,fact_id);
ALTER TABLE scanner_image_result_publications
    ADD COLUMN IF NOT EXISTS scan_result_receipt_id TEXT
      REFERENCES scanner_image_scan_results(result_receipt_id);

REVOKE ALL ON scanner_image_scan_results,
    scanner_image_scan_fact_associations FROM PUBLIC;
