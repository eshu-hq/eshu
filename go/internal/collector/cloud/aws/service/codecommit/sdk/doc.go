// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts the AWS SDK for Go v2 CodeCommit client into the
// scanner-owned, metadata-only CodeCommit read surface.
//
// The package lists repository names, resolves repository metadata in bounded
// BatchGetRepositories chunks, and reads trigger and tag metadata. It owns
// CodeCommit API pagination, batch chunking, AWS API telemetry, and throttle
// detection. It never reads commits, refs, blobs, file contents, pull-request
// bodies, or comment text, and exposes no mutation API; the exclusion
// reflection guard test asserts the omission. Scanner packages own fact
// selection and do not import the AWS SDK directly.
package awssdk
