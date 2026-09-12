// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package cleanup removes reducer-owned value-flow evidence from older
// generations beside the normal reducer intent loop (issue #6061). [Runner]
// scans active repository-scope generations through [CurrentGenerationReader]
// and retracts stale taint and interproc evidence for every generation that
// is no longer current, either through the direct retractor ports
// ([TaintStaleEvidenceRetractor], [InterprocStaleEvidenceRetractor])
// or, when a projected-node/edge ledger is wired, through the anchored
// by-UIDs delete path that also prunes the ledger.
//
// The reducer root imports this package as cleanup. It keeps the exported
// CodeValueFlowStaleCleanupRunner/CodeValueFlowStaleCleanupRunnerConfig/
// CodeValueFlowCurrentGeneration/TaintStaleEvidenceRetractor/
// InterprocStaleEvidenceRetractor spellings for cmd/reducer's wiring and
// internal/storage/postgres' generation reader, through the value-flow
// stanza of the reducer root's compat surface. The runner's Service.
// startSideRunners wiring test stays in root with its own minimal fakes
// (Go test files cannot share unexported symbols across a package
// boundary).
//
// Dependency rule: from the reducer tree this package imports only the
// shared tier (sharedintent) and the sibling leaf code/taint (for its
// evidence writer/ledger ports and evidence-source constants); outside it,
// the telemetry package and the standard library. It never imports the
// reducer root.
package cleanup
