// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package group owns the phase-group execution helpers: statement-parameter
// sanitization (SanitizeStatementParameters, SanitizeStatement,
// SanitizeStatements), the all-retract phase detector
// (StatementsAllUseOperation), profiled sequential statement execution
// (ExecuteProfiledStatementGroup), and the closed file-statement diagnostic
// allowlist (CanonicalFileStatementProfile).
//
// The statement metadata vocabulary (StatementMetadata* keys, CanonicalPhase*
// values, PhaseGroupMode) stays in the parent cypher package
// (phase_group_metadata.go): 115 files reference it, so moving it would
// force a root-to-leaf and leaf-to-parent import cycle.
//
// This is a Pattern-B leaf: it imports the parent cypher package for the
// Statement and Operation types (edge/writer precedent). The parent and
// white-box parent tests must never import this package.
package group
