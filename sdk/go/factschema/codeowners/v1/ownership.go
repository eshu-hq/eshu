// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

// Ownership is the schema-version-1 typed payload for the
// "codeowners.ownership" fact kind (Contract System v1 §3.1,
// docs/internal/design/contract-system-v1.md, issue #5419).
//
// One Ownership fact is a single CODEOWNERS pattern-to-owners mapping: one
// line of a repository's CODEOWNERS file. RepoID, SourcePath, Pattern, and
// Owners are all required: a CODEOWNERS line with no owners is not a
// meaningful ownership claim, and the collector unconditionally has all four
// values in hand when it parses a line (it would not emit a fact for a line
// it could not resolve to a repo, a source file, a pattern, and at least one
// owner token). OrderIndex is likewise required and unconditionally known:
// it is the line's 0-based position in the parsed file, which the collector
// always has by construction. CollectorInstanceID stays optional: it
// identifies the collector run, not the ownership claim itself.
//
// CODEOWNERS resolves ownership last-match-wins: for a given path, the LAST
// pattern in the file that matches wins, overriding any earlier match. A
// consumer resolving effective ownership for a path MUST sort candidate
// Ownership facts by OrderIndex and take the highest-index match, never the
// first.
type Ownership struct {
	// CollectorInstanceID identifies the collector instance run that emitted
	// this fact. Optional: it is operational metadata about the run, not part
	// of the ownership claim's identity.
	CollectorInstanceID *string `json:"collector_instance_id,omitempty"`

	// RepoID is the canonical repository identifier this CODEOWNERS line
	// belongs to. Required: without it the fact cannot be attributed to any
	// repository.
	RepoID string `json:"repo_id"`

	// SourcePath is the CODEOWNERS file path within the repository (for
	// example ".github/CODEOWNERS", "CODEOWNERS", or "docs/CODEOWNERS").
	// Required: it is the source-of-truth location for this ownership claim,
	// and GitHub honors exactly these three locations in this precedence
	// order.
	SourcePath string `json:"source_path"`

	// Pattern is the CODEOWNERS glob pattern from this line (for example
	// "*.go" or "/services/payments/"). Required: a line with no pattern is
	// not parseable as a CODEOWNERS rule at all.
	Pattern string `json:"pattern"`

	// Owners lists this line's owner tokens verbatim, in file order (for
	// example "@org/team", "@user", or an email address). Required
	// non-empty: a pattern line with zero owners carries no ownership claim
	// and the collector never emits one.
	Owners []string `json:"owners"`

	// OrderIndex is this line's 0-based position within the parsed
	// CODEOWNERS file. Required: CODEOWNERS resolves ownership last-match-
	// wins, so a consumer needs this index to pick the correct match among
	// several patterns that match the same path.
	OrderIndex int `json:"order_index"`
}
