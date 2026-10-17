// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// CodeTopicIsRepoScoped reports whether a code-topic request names an explicit
// repository without a language filter. Both the root single-statement builder
// and the codetopicparallel partition builder gate bind-hiding on this one
// predicate (#8370), so a future scoping-rule change cannot silently diverge
// the two decisions.
func CodeTopicIsRepoScoped(req CodeTopicInvestigationRequest) bool {
	return strings.TrimSpace(req.RepoID) != "" && strings.TrimSpace(req.Language) == ""
}

// CodeTopicTermHasTrigram reports whether an ILIKE '%term%' pattern has a run
// of three ASCII letters or digits, the shortest word pg_trgm can reliably
// extract a trigram from. In a LIKE pattern "_" and "%" are wildcards and any
// other character that is not an ASCII letter or digit breaks a word, so "db_"
// and "a_b" carry no trigram: both trigram GIN scans then return every row.
// Only ASCII counts because, under a C-ctype database, PostgreSQL treats a
// multibyte character as non-alphanumeric for trigram extraction, so a purely
// non-ASCII term has none there. A term this reports false for (for example
// "ab-cd", which does have trigrams, or a non-ASCII word) keeps the plain
// statement, which is the base, so the guard is safe to over-reject. A
// repo-scoped file-content bind hides behind MATERIALIZED term_param (#7246
// one-term, #7098 multi-term, #8370 parallel probes) only when this holds
// (measured 3.3 to 4.6 s plain against 14.9 to 16.7 s hidden for a term
// with none, on QA).
//
// Shared by the root single-statement builder (codeTopicFileBranch) and the
// codetopicparallel partition builder (ProbeSQL); it lives here so both can
// name one guard without the parallel leaf importing the root package.
func CodeTopicTermHasTrigram(term string) bool {
	run := 0
	for _, r := range term {
		if r < utf8.RuneSelf && (unicode.IsLetter(r) || unicode.IsDigit(r)) {
			run++
			if run >= 3 {
				return true
			}
			continue
		}
		run = 0
	}
	return false
}
