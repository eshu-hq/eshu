// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package auditpreflight validates competitive-audit issues against the Eshu
// preflight contract: every audit issue must state the competitor source and
// the Eshu code, docs, and proof evidence inspected, the duplicate search, a
// gap classification, the owner surface, and a verification plan before it
// becomes work.
//
// ParseIssue splits a GitHub issue body into its "### Heading" sections and
// Validate returns a Finding for every absent or empty required section, invalid
// gap class, or invalid owner surface. The GapClasses and OwnerSurfaces taxonomy
// is the shared vocabulary reused by the audit-preflight command (issue gate)
// and the local competitive audit report generator (#2716).
package auditpreflight
