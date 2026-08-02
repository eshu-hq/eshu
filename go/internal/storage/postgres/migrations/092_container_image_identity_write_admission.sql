-- #5874: begin-before-mutate admission watermark for the
-- container_image_identity reducer writer, mirroring
-- aws_cloud_runtime_drift_write_admission (migration 087, #5848). One row per
-- (scope_id, generation_id) records the highest evidence-read fencing_token
-- any pass has been admitted to write with. A pass whose own token is older
-- is rejected before it publishes or retires anything -- see
-- go/internal/reducer/container_image_identity_admission.go.
--
-- container_image_identity already stamped fact_records.fencing_token on
-- every row (UnixMicro of the handler's wall clock, #5847), so the shared
-- reducerFactBatchInsertQuery per-row conflict guard already existed. What it
-- lacked is what this table adds: a per-(scope, generation) CAS that a
-- RECLASSIFICATION -- a decision landing at a DIFFERENT fact_id than the one
-- it retires -- can still bypass, since the per-row guard only fires when two
-- passes collide on the SAME fact_id (see awsCloudRuntimeDriftAdmissionQuery's
-- doc comment for the identical reasoning on the sibling domain).
CREATE TABLE IF NOT EXISTS container_image_identity_write_admission (
    scope_id TEXT NOT NULL,
    generation_id TEXT NOT NULL,
    fencing_token BIGINT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (scope_id, generation_id)
);
