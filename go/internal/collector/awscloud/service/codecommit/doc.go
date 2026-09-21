// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package codecommit scans AWS CodeCommit repository metadata into AWS cloud
// fact observations.
//
// The package owns scanner-level CodeCommit fact selection for repositories,
// their KMS-key encryption edge, and their SNS-topic trigger edges. It is
// metadata-only: it never reads commits, refs, blobs, file contents,
// pull-request bodies, or comment text, and never mutates any CodeCommit
// resource. The repository resource is a code-to-cloud correlation anchor; it
// publishes the repository name and clone URLs as correlation anchors so a
// CodeBuild project, CodePipeline source action, or Amplify app whose Git
// source points at the repository joins it.
//
// SDK adapters belong in the awssdk subpackage so scanner tests can use small
// fakes instead of AWS SDK clients.
package codecommit
