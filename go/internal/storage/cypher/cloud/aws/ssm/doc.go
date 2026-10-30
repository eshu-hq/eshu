// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package ssm owns the AWS SSM host inventory graph writers: the observation
// node writer (HostNodeWriter) for SSMHostApplication and SSMHostPlatform
// nodes, the REPORTED_WITH pairing-edge writer, the committed-capture read,
// and the supersession retracts (eshu-hq/eshu#9558).
//
// The parent cypher package owns the shared Executor, Statement, Operation,
// retry, and posture-existence contracts; this leaf imports them as
// sourcecypher and the parent never imports this leaf. Reducer wiring in
// go/cmd/reducer imports this leaf directly for construction and adapts it
// onto the reducer's HostGraphWriter interface.
package ssm
