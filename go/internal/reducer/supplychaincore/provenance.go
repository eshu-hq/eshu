// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package supplychaincore

// AlternateSeverity is one source-attributed severity that was not selected
// for the finding but is preserved so callers can see vendor/source
// disagreement.
type AlternateSeverity struct {
	Source string
	Score  float64
	Vector string
	Label  string
}

// FixedVersionBranch records one source-attributed fixed-version branch.
type FixedVersionBranch struct {
	Version string
	Source  string
}

// AdvisorySourceObservation is the bounded provenance row surfaced through
// the finding payload. It carries source identity, advisory identifier,
// update timestamp, and withdrawal timestamp so API/MCP callers can explain
// why one severity was selected over alternates without re-reading raw
// source facts.
type AdvisorySourceObservation struct {
	Source          string
	AdvisoryID      string
	SourceUpdatedAt string
	WithdrawnAt     string
}
