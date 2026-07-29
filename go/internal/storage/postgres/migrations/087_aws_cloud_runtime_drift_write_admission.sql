-- #5848: begin-before-mutate admission watermark for the aws_cloud_runtime_drift
-- reducer writer. One row per (scope_id, generation_id) records the highest
-- evidence-read fencing_token any pass has been admitted to write with. A pass
-- whose own token is older is rejected before it inserts or retires anything --
-- see go/internal/reducer/aws_cloud_runtime_drift_admission.go.
CREATE TABLE IF NOT EXISTS aws_cloud_runtime_drift_write_admission (
    scope_id TEXT NOT NULL,
    generation_id TEXT NOT NULL,
    fencing_token BIGINT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (scope_id, generation_id)
);
