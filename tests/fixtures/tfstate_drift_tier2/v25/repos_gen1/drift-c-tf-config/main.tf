# Bucket C gen-1: drift_removed_from_state — gen-1 state has aws_s3_bucket.cached,
# gen-2 state has the same lineage with serial=2 and the resource removed. The
# loader's priorStateSnapshotMetadataQuery
# (go/internal/storage/postgres/tfstate_drift_evidence_sql.go:135) selects the
# prior generation by `serial = currentSerial - 1` on the same lineage, so when
# Phase 3.5 fires for the gen-2 snapshot it finds gen-1 as the prior and emits
# drift_kind="removed_from_state" for aws_s3_bucket.cached.
#
# Config does not change between generations; only the state file changes.

terraform {
  backend "s3" {
    bucket = "eshu-drift-c"
    key    = "prod/terraform.tfstate"
    region = "us-east-1"
  }
}

resource "aws_s3_bucket" "cached" {
  acl = "private"
}
