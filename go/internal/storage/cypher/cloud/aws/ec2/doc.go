// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package ec2 owns the AWS EC2 graph writers: the instance node writer
// (EC2InstanceNodeWriter), the instance-identity writer, the internet
// exposure writer, the block-device KMS posture writer, and the
// uses-profile edge writer (EC2UsesProfileEdgeWriter).
//
// The parent cypher package owns the shared Executor, Statement, Operation,
// retry, and posture-existence contracts; this leaf imports them as
// sourcecypher and the parent never imports this leaf. Reducer wiring in
// go/cmd/reducer imports this leaf directly for construction.
package ec2
