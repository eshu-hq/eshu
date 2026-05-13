# Bucket F gen-1: drift_removed_from_config — gen-1 config declares
# aws_s3_bucket.legacy and gen-1 state carries the matching applied resource;
# gen-2 config drops the resource block while gen-2 state still carries the
# applied bucket (state does not change between generations — only config
# does). The loader's listPriorConfigAddressesQuery
# (go/internal/storage/postgres/tfstate_drift_evidence_sql.go:170) walks prior
# scope_generations rows for the same repo scope_id, ordered by ingested_at
# DESC, excluding the current generation_id. When Phase 3.5 fires for the
# gen-2 generation, hasStateOnlyAddress
# (go/internal/storage/postgres/tfstate_drift_evidence_helpers.go:20) detects
# the state-only address aws_s3_bucket.legacy, the prior-config walk finds
# gen-1's superseded generation declaring it, mergeDriftRows sets
# PreviouslyDeclaredInConfig=true on the state row, and the classifier emits
# drift_kind="removed_from_config" via classifyRemovedFromConfig
# (go/internal/correlation/drift/tfconfigstate/classify.go:136).
#
# Bucket F's state JSON MUST carry the resource across both generations.
# An empty state file would make hasStateOnlyAddress return false (no
# state-only addresses), the prior-config walk would be short-circuited, and
# the candidate keyspace union in mergeDriftRows would produce no candidate
# for aws_s3_bucket.legacy — removed_from_config requires state to still hold
# the applied resource (the canonical positive testdata at
# go/internal/correlation/drift/tfconfigstate/testdata/removed_from_config/positive.json
# enforces the same contract).

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
