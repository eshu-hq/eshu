// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_language_imports_grant

// Impossible-value controls for the language-query grant statements.
//
// Every other live grant assertion in this package is positive: it asks for
// something that exists and checks the right rows come back. A positive-only
// suite cannot distinguish "the predicate filtered correctly" from "the
// predicate was ignored and every row happened to match", and the fixture makes
// that indistinguishable by construction -- seedLiveGrantGraph writes every File
// with the single constant liveGrantLanguage, so "filter to python" and "return
// everything" produce identical output.
//
// The controls here ask questions with exactly one correct answer: a language
// that no node carries, and a grant that admits no repository. Both must return
// zero rows. If a predicate is dropped by the backend, these fail and the
// positive tests do not.
//
// Both cases keep the bounded call shape the rest of the suite uses: an explicit
// limit, the shipped builder's own deterministic ordering, and the same
// context timeout, so a failure is a filtering failure rather than a timeout or
// an unbounded scan.
package query

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/language"
)

const (
	// liveGrantImpossibleLanguage is carried by no node in the fixture and is
	// not a language any parser emits.
	liveGrantImpossibleLanguage = "zzz-not-a-language"

	// liveGrantImpossibleRepo is not the id of any seeded repository.
	liveGrantImpossibleRepo = "repo://live-alpha/no-such-repository"

	// liveGrantNegativeControlLimit bounds both controls, satisfying the call
	// contract's required-limit rule.
	//
	// The limit is NOT load-bearing for these assertions, and an earlier version
	// of this comment claimed it was ("a limit that could truncate would let a
	// leaking query return zero rows for the wrong reason"). That reasoning is
	// backwards: with any limit >= 1, a leaking query matching k >= 1 rows
	// returns min(limit, k) >= 1, so `len(rows) != 0` still fails as intended.
	// Truncation cannot manufacture the zero-row pass it warned about.
	//
	// The real vacuous-pass risk is an EMPTY OR UNREACHABLE FIXTURE, which no
	// choice of limit affects. 50 is simply a bound generous enough not to
	// invite the reader into the wrong inference above.
	liveGrantNegativeControlLimit = 50
)

// liveGrantImpossibleAccess is a scoped filter naming only repositories that do
// not exist. It is the tenancy analogue of the impossible language: a caller
// whose grant admits nothing must be shown nothing.
func liveGrantImpossibleAccess() repositoryAccessFilter {
	return repositoryAccessFilter{
		AllowedRepositoryIDs: []string{liveGrantImpossibleRepo},
	}
}

// liveGrantNegativeControlLabels are the four dispatch branches of
// language.BuildCypherWithSemanticFilter that reach the graph.
var liveGrantNegativeControlLabels = []string{"Repository", "Directory", "File", "Function"}

// TestLiveNornicDBLanguageQueryImpossibleLanguageReturnsNoRows is the control a
// positive-only suite cannot provide.
//
// It asks each shipped builder for a language no node carries. The only correct
// answer is zero rows for every shape. A non-zero count means the language
// predicate did not filter, and the caller received rows for languages it did
// not ask for -- an accuracy failure that returns a plausible, non-empty page,
// so no empty-result check anywhere would catch it.
func TestLiveNornicDBLanguageQueryImpossibleLanguageReturnsNoRows(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	driver := openLiveGrantDriver(ctx, t)
	defer func() { _ = driver.Close(context.Background()) }()
	seedLiveGrantGraph(ctx, t, driver)

	for _, label := range liveGrantNegativeControlLabels {
		t.Run(label, func(t *testing.T) {
			// Unscoped deliberately: with no grant clause the language predicate
			// is the only thing that can filter, so a non-empty result isolates
			// the failure to it.
			cypher, params := language.BuildCypherWithSemanticFilter(
				liveGrantImpossibleLanguage, label, "", "",
				liveGrantNegativeControlLimit, "", "", liveGrantUnscopedAccess(),
			)
			rows := runLiveGrantStatement(ctx, t, driver, label+" impossible-language", cypher, params)
			if len(rows) != 0 {
				t.Fatalf("%s with language=%q returned %d rows, want 0; the language predicate did not filter, so this page carries languages the caller never asked for\ncypher: %s",
					label, liveGrantImpossibleLanguage, len(rows), cypher)
			}
		})
	}
}

// TestLiveNornicDBLanguageQueryImpossibleGrantReturnsNoRows applies the same
// control to the tenancy grant, and it is the more serious of the two.
//
// repositoryAccessFilter.GraphConditionOnProperty builds the whole grant as
// "(alias.prop IN $allowed_repository_ids OR alias.prop IN $allowed_scope_ids)",
// and every scoped graph read in the product funnels through it. A caller whose
// grant names only repositories that do not exist must receive zero rows from
// every shape. Any other answer is cross-tenant exposure on whatever backend is
// under test, not an accuracy bug.
//
// Which backend matters. The 1.2.x line the gates and Compose run
// (verify-replay-tier.sh pins v1.2.3; docker-compose defaults to the pr290
// image, 1.2.1) filters this correctly when measured. The embedded 1.0.0
// library -- linked only under the nolocalllm build tag -- does not. So a
// failure here on a local profile is expected against 1.0.0 and would be a
// genuine regression against 1.2.x.
//
// The existing grant tests cannot catch this: they assert that a GRANTED caller
// sees the granted rows, which stays true whether or not the predicate is
// enforced. Only an impossible grant separates "filtered" from "ignored".
func TestLiveNornicDBLanguageQueryImpossibleGrantReturnsNoRows(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	driver := openLiveGrantDriver(ctx, t)
	defer func() { _ = driver.Close(context.Background()) }()
	seedLiveGrantGraph(ctx, t, driver)

	for _, label := range liveGrantNegativeControlLabels {
		t.Run(label, func(t *testing.T) {
			// The language is the real one here, so every seeded row satisfies
			// it. The grant is then the only predicate that can exclude them,
			// which is what makes a non-empty result unambiguous.
			cypher, params := language.BuildCypherWithSemanticFilter(
				liveGrantLanguage, label, "", "",
				liveGrantNegativeControlLimit, "", "", liveGrantImpossibleAccess(),
			)
			rows := runLiveGrantStatement(ctx, t, driver, label+" impossible-grant", cypher, params)
			if len(rows) != 0 {
				t.Fatalf("%s with a grant admitting only %q returned %d rows, want 0; the grant predicate did not filter, so a scoped caller receives rows outside its grant\ncypher: %s",
					label, liveGrantImpossibleRepo, len(rows), cypher)
			}
		})
	}
}
