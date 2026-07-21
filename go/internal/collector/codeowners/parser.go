// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codeowners

import "strings"

// Rule is one parsed CODEOWNERS pattern-to-owners mapping: a single
// non-comment, non-blank, non-section rule line that declares at least one
// owner.
type Rule struct {
	// Pattern is the rule line's glob pattern (its first whitespace-separated
	// token), for example "*.go" or "/services/payments/".
	Pattern string
	// Owners lists the rule line's owner tokens verbatim, in file order.
	Owners []string
	// OrderIndex is this rule's 0-based position among the EMITTED rule lines
	// only — comments, blank lines, section headers, and pattern-only lines
	// (dropped for carrying no ownership claim) do not consume an index. A
	// consumer resolving ownership for a path sorts by OrderIndex and takes
	// the highest match, per CODEOWNERS' last-match-wins resolution.
	OrderIndex int
}

// Parse reads one CODEOWNERS file body and returns its rules in file order.
//
// Parse follows GitHub's documented CODEOWNERS syntax
// (https://docs.github.com/en/repositories/managing-your-repositorys-settings-and-features/customizing-your-repository/about-code-owners):
//
//   - Blank lines (including whitespace-only lines) are ignored.
//   - A line whose first non-whitespace character is '#' is a whole-line
//     comment and is ignored. CODEOWNERS has no inline trailing-comment
//     syntax, so a '#' appearing after a pattern or owner token is not
//     treated as a comment start.
//   - A line whose first non-whitespace characters are "[" or "^[" is a
//     CODEOWNERS section header (GitHub's optional-section-with-minimum-
//     approvers feature). Section membership and per-section approval counts
//     are out of scope for this parser: a section header line, including one
//     that carries default owners on the same line, is treated as a non-rule
//     and skipped entirely rather than partially interpreted.
//   - Every other non-blank line is a rule line: whitespace-separated tokens
//     where the first token is the glob pattern and every remaining token is
//     an owner (a "@user" handle, an "@org/team" handle, or an email
//     address), carried verbatim.
//   - A rule line with a pattern and zero owner tokens removes default
//     ownership for that pattern in GitHub's own semantics; it asserts no
//     ownership claim, so Parse drops it rather than emitting an owner-less
//     rule.
//
// CRLF line endings are normalized transparently: the trailing '\r' of a
// CRLF-terminated line is trimmed before the comment/section/rule checks
// above run.
func Parse(body string) []Rule {
	var rules []Rule
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSuffix(line, "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		if isSectionHeaderLine(trimmed) {
			continue
		}

		tokens := strings.Fields(trimmed)
		if len(tokens) < 2 {
			// A pattern with no owner tokens carries no ownership claim
			// (GitHub uses this shape to remove default ownership), so it is
			// not emitted as a rule.
			continue
		}

		rules = append(rules, Rule{
			Pattern:    tokens[0],
			Owners:     append([]string(nil), tokens[1:]...),
			OrderIndex: len(rules),
		})
	}
	return rules
}

// isSectionHeaderLine reports whether a trimmed line opens a CODEOWNERS
// section ("[Section-name]" or the optional-section form
// "^[Section-name][2]"). GitHub's sections feature is out of scope for this
// parser (see Parse's doc comment); this predicate only recognizes the header
// shape so Parse can skip it as a non-rule.
func isSectionHeaderLine(trimmed string) bool {
	return strings.HasPrefix(trimmed, "[") || strings.HasPrefix(trimmed, "^[")
}
