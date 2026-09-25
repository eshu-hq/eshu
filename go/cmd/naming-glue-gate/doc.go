// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Command naming-glue-gate flags newly introduced Go package directories
// whose name glues two or more full words together where
// docs/internal/naming.md rule 3 requires nesting instead (e.g.
// "workloadinstance" should be "workload/instance").
//
// # Why this exists
//
// Two earlier gates already cover naming.md rules 2 and 3 mechanically:
// scripts/verify-filename-stutter.sh (a file repeating its directory name, a
// directory repeating its parent's name) and tools/golangci-lint-dirgate's
// naming check (a file that belongs in a sibling subpackage). Neither
// catches a brand-new directory whose OWN name is a glued compound
// unrelated to its parent -- "workloadinstance" under internal/reducer does
// not repeat "reducer", so nothing flagged it, even though the directory
// glues "workload" and "instance" exactly like the already-documented
// "workloadmaterialization" and "iamcanassume" precedents in
// docs/internal/design/naming-remediation.md.
//
// Detecting this mechanically would require either a maintained dictionary
// of accepted proper nouns and product names (AWS service names like
// "cloudformation" and "guardduty" are real compounds that must NOT be
// flagged) or a maintained list of known-bad glued prefixes -- both grow
// unbounded as the repository adds new domains. Instead, this gate asks a
// language model (DeepSeek's deepseek-flash) to make the same judgment call
// a reviewer already makes, grounded only in naming.md's rule 3 text and a
// handful of precedents already recorded in naming-remediation.md for other
// reasons -- no new list is maintained for this gate specifically.
//
// # Scope
//
// This gate classifies newly introduced directory basenames only (Added,
// not pre-existing at -base-ref) under -dirs (default and pre-commit wiring:
// go/internal, go/cmd, go/pkg -- every root that holds real Go packages),
// matching the same "new names only, never re-litigate legacy debt" design
// as verify-filename-stutter.sh. It does not
// classify file names: Go files in this repository already use underscores
// to separate words (workload_selection.go), so the no-separator glue shape
// rule 3 targets is specific to directories, which Go idiom keeps
// underscore-free because a directory's name becomes its package name.
//
// Candidates are pre-filtered to the shape a glued compound actually takes
// here -- lowercase letters only, no separator, at least 8 characters --
// before any network call, so a typical PR with zero or one new directory
// costs nothing.
//
// # Blocking behavior
//
// This is the first gate in the repository whose verdict is not
// deterministic: a model update can change a borderline call with no code
// change on either side. -blocking controls how that risk is carried:
//
//   - Pre-commit (-blocking=true): a finding fails the commit immediately,
//     the cheapest point to catch a bad name before it exists anywhere.
//   - CI (-blocking=false): findings are reported (as JSON on stdout and a
//     human summary on stderr) but the process always exits 0, so this gate
//     is advisory only at merge time while its false-positive rate is
//     measured against real PRs.
//
// Missing DEEPSEEK_API_KEY or a DeepSeek API/network failure both fail
// open (exit 0 with a stderr note): an infrastructure problem is not a
// naming violation, and this gate must never be the reason a commit or a
// CI job is blocked for an unrelated cause. An unresolvable git state (a
// bad -base-ref) fails closed (exit 2), matching
// verify-filename-stutter.sh's "never green on an unscanned tree" rule.
//
// # Exemptions
//
// A glued name the owner has deliberately approved is recorded in
// scripts/lib/naming-glue-exempt.tsv (one row per directory: repo-relative
// path, reason, approver or issue) instead of argued with on every commit.
// The bar for a row is an owner decision on the cited issue: nesting the
// compound per rule 3 must lose to another naming rule at the call site,
// with measured collision evidence for the kept name. Candidates matching a
// row by full path are filtered before classification, in both blocking and
// advisory modes; a same-named directory anywhere else is still classified.
// A malformed ledger fails closed (exit 2): a corrupted file must never
// silently unexempt a name.
package main
