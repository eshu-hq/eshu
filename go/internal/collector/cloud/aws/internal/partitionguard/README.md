# AWS Scanner ARN-Partition Guard

## Purpose

`internal/collector/awscloud/internal/partitionguard` is test-support code that
mechanizes the AWS scanner ARN-partition contract. An AWS ARN's second segment is
its **partition** — `aws` (commercial), `aws-cn` (China), or `aws-us-gov`
(GovCloud) — and it is not optional. A bucket in GovCloud has the ARN
`arn:aws-us-gov:s3:::bucket`, not `arn:aws:s3:::bucket`.

A scanner that synthesizes an ARN with a hardcoded `aws` partition produces an
identity that is correct only in the commercial partition. The graph join
resolves a relationship target to a node by ARN/resource-id equality, so in
GovCloud and China a hardcoded-`aws` target never matches the real node and the
edge silently dangles. This was the recurring defect class behind issue #866
(and the S3 sub-class fixed in #862/#863). This package makes the contract a
test.

## The contract

Scanner code must never synthesize an ARN with a hardcoded partition. The
partition is derived from one of the shared helpers in `awscloud/partition.go`:

- `awscloud.PartitionForRegion(region)` — from an AWS region.
- `awscloud.PartitionForBoundary(boundary)` — from the scan boundary's region
  (the common case: a resource in the scanner's own claimed boundary).
- `awscloud.PartitionFromARN(arn)` — from a source ARN observed in the same
  describe response (e.g. a model ARN that references an S3 bucket).

## How the guard works

`ScanForHardcodedPartitions` AST-walks every non-test `.go` file under
`services/` (recursively, including `awssdk/` adapters). It flags a string
literal whose value begins with the commercial prefix `arn:aws:` **only** when it
is used to build an ARN:

- an operand of string concatenation (`"arn:aws:ec2:" + region`),
- a printf-family format string (`fmt.Sprintf("arn:aws:codedeploy:%s...", ...)`),
- an identifier bound to such a literal and then concatenated/formatted.

A literal in a parse context (`strings.HasPrefix(s, "arn:aws:s3:::")`,
`Contains`, `TrimPrefix`, ...) is never the operand of a `+` nor a format string,
so it is not flagged. The guard needs no type information, stays fast, and has no
`go/packages` dependency.

The repo-level guard test `TestLiveScannerTreeHasNoHardcodedPartitions` runs it
over the live tree, so a new scanner that hardcodes a partition fails CI here
with the exact file:line and the helper to use.

## What it does not catch

A partition baked into a non-literal value (e.g. a partition string fetched from
a remote field and concatenated), and a runtime ARN mismatch caused by something
other than a hardcoded partition. Those remain the scanner author's
responsibility, with per-service GovCloud/China unit tests as the second layer.
