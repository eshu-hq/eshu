// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package component holds the command bodies of the `eshu component` family:
// registry operations (RunInspect, RunVerify, RunInstall, RunList, RunEnable,
// RunDisable, RunUninstall), conformance runs (RunConform), index
// verification (RunIndexVerify), the collector scaffolder (RunInitCollector),
// the advisory readiness and schema reports (RunExtractionReadiness,
// RunSchemaVersions), and the API-backed inventory/diagnostics readbacks
// (FetchInventory, FetchDiagnostics, FinishAPI).
//
// The package reads no cobra flags, no environment variables, and no exit
// codes: go/cmd/eshu resolves all of that and passes plain values in, which
// is what makes these bodies testable outside the binary. It does name five
// flags, because it prints them: InstanceFlag, VersionFlag, InitIDFlag,
// InitPublisherFlag, and InitFactKindFlag are the flags the Run functions
// reject empty input with ("--<flag> is required"), so they are declared here
// and go/cmd/eshu registers what this package declares rather than repeating
// the strings. Every function renders
// its own success and failure output onto the writer it is given -- text by
// default, the eshu.component.cli.v1 payload (CLIOutput) under --json -- and
// returns the error the command should exit with. Mapping that error onto a
// process exit code stays in go/cmd/eshu.
//
// The API commands consume the canonical Eshu envelope through the
// EnvelopeFetcher interface, declared here because the concrete HTTP client
// lives in package main and cannot be imported. The envelope readers in
// values.go are deliberate private copies of go/cmd/eshu's trace* helpers,
// except boolValue, whose original left go/cmd/eshu with its last caller in
// #6059; TestEnvelopeReaderParity in go/cmd/eshu compares each reader across
// the copies that reader has, here and in the sibling cli families.
package component
