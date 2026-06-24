// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package partitionguard is shared test-support code that mechanizes the AWS
// scanner ARN-partition contract: a scanner must never synthesize an ARN with a
// hardcoded commercial `aws` partition. An ARN's second segment is its partition
// (aws / aws-cn / aws-us-gov) and is not optional — a GovCloud or China resource
// ARN carries aws-us-gov / aws-cn. Synthesizing `arn:aws:<service>:...` for a
// resource in another partition produces an identity that never joins the real
// resource node, silently dangling the graph edge. This was the recurring defect
// class behind issue #866 (and the S3 sub-class in #862/#863); the package makes
// the contract a test so a new scanner cannot reintroduce it.
//
// ScanForHardcodedPartitions AST-walks every non-test Go file under the scanner
// services tree (recursively, including awssdk adapters) and flags a string
// literal whose value begins with the commercial prefix `arn:aws:` only when it
// is used to BUILD an ARN: as an operand of string concatenation, or as a
// printf-family format string, or via an identifier bound to such a literal. A
// literal in a parse context (strings.HasPrefix, Contains, TrimPrefix, ...) is
// never the operand of a `+` nor a format string, so it is not flagged — the
// guard distinguishes synthesis from inspection without type information.
//
// The repo-level guard test (TestLiveScannerTreeHasNoHardcodedPartitions) feeds
// the live tree through it. Scanners derive the partition instead of hardcoding
// it with awscloud.PartitionForRegion, awscloud.PartitionForBoundary, or
// awscloud.PartitionFromARN.
//
// What the guard does NOT catch: a partition baked into a value that is not a
// literal beginning with `arn:aws:` (for example, a partition fetched from a
// remote field and concatenated), and a runtime mismatch between a correctly
// partitioned consumer ARN and a node whose own ARN is wrong for a different
// reason. Those remain the scanner author's responsibility.
package partitionguard
