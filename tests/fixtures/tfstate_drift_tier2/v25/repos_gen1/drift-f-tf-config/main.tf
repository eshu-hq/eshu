# Bucket F gen-1: drift_removed_from_config — gen-1 config declares
# aws_s3_bucket.legacy; gen-2 config drops it. State on bucket F never carries
# the resource (only the active config declared it). The loader's
# listPriorConfigAddressesQuery
# (go/internal/storage/postgres/tfstate_drift_evidence_sql.go:170) walks prior
# scope_generations rows for the same repo scope_id, ordered by ingested_at
# DESC, excluding the current generation_id. When Phase 3.5 fires for the
# gen-2 generation it finds gen-1's superseded generation containing the
# resource and emits drift_kind="removed_from_config" for aws_s3_bucket.legacy.
#
# Bucket F's state JSON is empty across both generations because this drift
# kind is about config-side removal, not state-side change.

terraform {
  backend "s3" {
    bucket = "eshu-drift-f"
    key    = "prod/terraform.tfstate"
    region = "us-east-1"
  }
}

resource "aws_s3_bucket" "legacy" {
  acl = "private"
}
