// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package vulnscan holds the logic behind `eshu vuln-scan repo` and
// `eshu vuln-scan provider-parity`: turning the reducer-owned impact-findings
// envelope into a scope plan, a vulnerability report, a SARIF or VEX export,
// and an exit classification, plus the one-shot local runtime the repo
// subcommand starts when no service URL is configured.
//
// The package never decides the process exit code. Every fail-closed path
// returns a [Failure] carrying the message and the number this family has
// always used, and go/cmd/eshu converts that into its own exit-error type,
// which is declared there. The same split applies to cobra: flag reading,
// stream selection, and environment lookups stay in the command wrapper, so
// every function here takes its inputs as arguments.
//
// Two seams exist because the concrete types they name live in package main
// and cannot be imported. Transport is the [EnvelopeFetcher] interface rather
// than the CLI's API client. [Result].Scan is typed any because the scan
// result belongs to the `eshu scan` family; this package carries it into the
// JSON envelope without reading it.
//
// Fail-closed is the contract that matters here. A clean `ready_zero_findings`
// answer is only reported when the readiness envelope proves advisory and
// package-registry evidence covering the observed dependencies is present and
// fresh. [ApplyScopedGuards] holds those rules; --broad relaxes the advisory
// freshness guard and nothing else.
package vulnscan
