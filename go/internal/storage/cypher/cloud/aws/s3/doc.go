// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package s3 owns the AWS S3 graph writers: the external-principal grant
// writer, the internet-exposure posture node writer, and the logs-to edge
// writer (S3LogsToEdgeWriter).
//
// The parent cypher package owns the shared Executor, Statement, Operation,
// retry, and posture-existence contracts; this leaf imports them as
// sourcecypher and the parent never imports this leaf. Reducer wiring in
// go/cmd/reducer imports this leaf directly for construction.
package s3
