// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package vulnscan holds the logic behind `eshu vuln-scan repo` and
// `eshu vuln-scan provider-parity`: turning the reducer-owned impact-findings
// envelope into a scope plan, a vulnerability report, a SARIF or VEX export,
// and an exit classification, plus the one-shot local runtime the repo
// subcommand starts when no service URL is configured.
//
// [RunRepo] is the repo subcommand end to end once the wrapper has resolved
// its inputs: the scan and readiness wait, repository resolution, the
// findings read, the scope guards, the performance stamp, local runtime
// shutdown, and the output document -- JSON envelope, SARIF, VEX, or the
// human summary -- on the writer it is given. It is the only entry point
// in this package that runs a whole subcommand; provider-parity's
// orchestration is still `runVulnScanProviderParity` in go/cmd/eshu, calling
// the parity functions here one at a time. The steps
// RunRepo composes -- [NewResult], [FetchImpactFindings], [ApplyScope],
// [ExitFailure], [RecordPerformance], [BuildReport], the export writers and
// [RenderSummary] -- remain callable one at a time.
//
// The package never decides the process exit code. RunRepo returns a
// [Failure] for every scanner verdict -- findings present, evidence not
// established, unsupported target evidence, a scan that did not reach ready
// -- carrying the message and the number this family has always used, and
// go/cmd/eshu converts that into its own exit-error type, which is declared
// there. Any other error is returned unwrapped and reaches the operator with
// its own text. The same split applies to cobra and to the process: flag
// reading, the decision to start a local runtime, the API client, and the
// choice of streams stay in the command wrapper, which passes them in through
// [RepoDeps] and [RepoOptions]. Nothing here calls os.Exit or writes to
// os.Stdout.
//
// Two seams exist because the concrete types they name live in package main
// and cannot be imported. Transport is the [RepoClient] interface (a plain
// GET plus the envelope GET, [EnvelopeFetcher]) rather than the CLI's API
// client. [Result].Scan is typed any because the scan result belongs to the
// `eshu scan` family; this package carries it into the JSON envelope without
// reading it. A third seam exists for a different reason: the scan itself
// arrives as a scan.Runtime -- an importable internal/cli/scan type -- that
// the wrapper wires, because PATH lookup, the bootstrap child and the
// inherited environment are process contact this package must not own.
//
// Fail-closed is the contract that matters here. A clean `ready_zero_findings`
// answer is only reported when the readiness envelope proves advisory and
// package-registry evidence covering the observed dependencies is present and
// fresh. [ApplyScopedGuards] holds those rules; --broad relaxes the advisory
// freshness guard and nothing else.
package vulnscan
