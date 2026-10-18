// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package rds owns the AWS RDS posture node writer (RDSPostureNodeWriter),
// which persists database posture (exposure, encryption, backup) onto the
// shared CloudResource nodes without ever fabricating them.
//
// The parent cypher package owns the shared Executor, Statement, Operation,
// retry, and posture-existence contracts; this leaf imports them as
// sourcecypher and the parent never imports this leaf. Reducer wiring in
// go/cmd/reducer imports this leaf directly for construction.
package rds
