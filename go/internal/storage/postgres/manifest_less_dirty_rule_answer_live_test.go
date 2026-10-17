// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8243 disposable PostgreSQL 18 same-answer proof: the real loader returns the identical fact ids under the previous and the narrowed manifest-less rule while binding fewer producer scopes, and the rejected naive superseded rule loses the hole scope; enrolled in the live-postgres-readiness runner; wall time is logged, not gated
//eshu:readiness:env ESHU_AWS_DRIFT_ARN_PROOF_DISPOSABLE ESHU_AWS_DRIFT_ARN_PROOF_DSN ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE ESHU_DEFERRED_PARTITION_PROOF_DSN ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DSN ESHU_REDUCER_FAIRNESS_PROOF_DSN

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"testing"

	producerstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/code/producers"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// manifestLessLastFullLateral and manifestLessCoveredWriterExemption bound the
// two pieces #8243 added to the manifest-less leg. Cutting both from the
// shipped producerstore.ManifestLessLeg yields the previous leg, so the proofs
// below derive the old rule from the shipped constant instead of a hand copy.
const (
	manifestLessLastFullLateralStart = "LEFT JOIN LATERAL (\n"
	manifestLessLastFullLateralEnd   = ") AS last_full ON true\n"
	manifestLessExemptionStart       = "      AND NOT (\n"
	manifestLessExemptionEnd         = "      )\n"
	manifestLessCoveredWrapper       = "COALESCE(COALESCE(pending.projection_write_started_at, '-infinity'::timestamptz) <= last_full.pws, false)"
	manifestLessUnwrappedComparison  = "COALESCE(pending.projection_write_started_at, '-infinity'::timestamptz) <= last_full.pws"
)

// cutOnce removes the single span of s that starts with start and ends with
// end, failing the test unless exactly one such span exists.
func cutOnce(t *testing.T, s, start, end, replacement string) string {
	t.Helper()
	if strings.Count(s, start) != 1 {
		t.Fatalf("span start %q occurs %d times, want 1", start, strings.Count(s, start))
	}
	i := strings.Index(s, start)
	j := strings.Index(s[i:], end)
	if j < 0 {
		t.Fatalf("span end %q not found after %q", end, start)
	}
	return s[:i] + replacement + s[i+j+len(end):]
}

// previousManifestLessLeg derives the pre-#8243 leg: every never-activated
// generation marks the scope dirty.
func previousManifestLessLeg(t *testing.T) string {
	t.Helper()
	leg := cutOnce(t, producerstore.ManifestLessLeg, manifestLessLastFullLateralStart, manifestLessLastFullLateralEnd, "")
	return cutOnce(t, leg, manifestLessExemptionStart, manifestLessExemptionEnd, "")
}

// naiveManifestLessLeg derives the rejected naive rule: drop every superseded
// never-activated generation.
func naiveManifestLessLeg(t *testing.T) string {
	t.Helper()
	leg := cutOnce(t, producerstore.ManifestLessLeg, manifestLessLastFullLateralStart, manifestLessLastFullLateralEnd, "")
	return cutOnce(t, leg, manifestLessExemptionStart, manifestLessExemptionEnd, "      AND pending.status <> 'superseded'\n")
}

// unwrappedManifestLessLeg derives the leg without the COALESCE(..., false)
// wrapper: a NULL newest-full marker then makes the exemption NULL, NOT NULL
// filters the row out, and the scope silently leaves the dirty set.
func unwrappedManifestLessLeg(t *testing.T) string {
	t.Helper()
	if strings.Count(producerstore.ManifestLessLeg, manifestLessCoveredWrapper) != 1 {
		t.Fatalf("ManifestLessLeg lacks the covered-writer wrapper %q", manifestLessCoveredWrapper)
	}
	return strings.Replace(producerstore.ManifestLessLeg, manifestLessCoveredWrapper, manifestLessUnwrappedComparison, 1)
}

// legSwapQueryer runs each manifest read with its manifest-less leg replaced,
// so the real loader can be driven under a different dirty rule.
type legSwapQueryer struct {
	*recordingCodeCallSymbolQueryer
	leg string
}

func (q legSwapQueryer) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	switch query {
	case producerstore.PackageManifestsQuery, producerstore.ComposerManifestsQuery, producerstore.GoModuleManifestsQuery:
		query = strings.TrimSuffix(query, producerstore.ManifestLessLeg) + q.leg
	}
	return q.recordingCodeCallSymbolQueryer.QueryContext(ctx, query, args...)
}

// TestReducerContentionGateManifestLessDirtyRuleReturnsTheSameDefinitions is
// the same-answer proof (#8243): the real loader returns the identical fact
// ids, and so the identical symbol_definition_fact_count, under the previous
// rule and the narrowed one, while the narrowed rule visits fewer scopes.
func TestReducerContentionGateManifestLessDirtyRuleReturnsTheSameDefinitions(t *testing.T) {
	ctx, database := openActiveCodeCallSymbolContentSchema(t)
	fixtures := seedDirtyRuleFixtures(t, ctx, database)
	keys := []string{dirtyRuleKeys["composer.json"], dirtyRuleKeys["go.mod"]}
	for _, fixture := range fixtures {
		keys = append(keys, "package:fx-"+fixture.name+"#run")
	}

	load := func(queryer db.ExecQueryer) []string {
		loaded, err := NewFactStore(queryer).LoadActiveCodeCallSymbolDefinitionFacts(ctx, keys)
		if err != nil {
			t.Fatalf("LoadActiveCodeCallSymbolDefinitionFacts() error = %v", err)
		}
		ids := factIDs(loaded)
		slices.Sort(ids)
		return ids
	}
	before := &recordingCodeCallSymbolQueryer{SQLDB: SQLDB{DB: database}}
	after := &recordingCodeCallSymbolQueryer{SQLDB: SQLDB{DB: database}}
	beforeIDs := load(legSwapQueryer{recordingCodeCallSymbolQueryer: before, leg: previousManifestLessLeg(t)})
	afterIDs := load(after)

	if !reflect.DeepEqual(beforeIDs, afterIDs) {
		t.Fatalf("fact ids differ:\nprevious rule = %v\nnarrowed rule = %v", beforeIDs, afterIDs)
	}
	var want []string
	for _, fixture := range fixtures {
		if fixture.dirty {
			want = append(want, "fact-"+fixture.name)
		}
	}
	slices.Sort(want)
	if !reflect.DeepEqual(afterIDs, want) {
		t.Fatalf("fact ids = %v, want every dirty fixture's definition %v", afterIDs, want)
	}
	if len(beforeIDs) != len(afterIDs) {
		t.Fatalf("symbol_definition_fact_count previous = %d, narrowed = %d", len(beforeIDs), len(afterIDs))
	}
	if previous, narrowed := anchoredScopeCount(before), anchoredScopeCount(after); narrowed >= previous {
		t.Fatalf("narrowed rule bound %d producer scopes, previous %d; want fewer", narrowed, previous)
	}
}

// anchoredScopeCount sums the producer scopes bound to every anchored scan the
// queryer recorded.
func anchoredScopeCount(queryer *recordingCodeCallSymbolQueryer) int {
	total := 0
	for i, query := range queryer.queries {
		if !strings.Contains(query, "fact.scope_id = ANY($5::text[])") {
			continue
		}
		if scopes, ok := queryer.args[i][4].([]string); ok {
			total += len(scopes)
		}
	}
	return total
}

// TestReducerContentionGateManifestLessDirtyRuleNaiveRuleLosesTheHole is the
// seeded RED/GREEN for the rejected naive rule: dropping every superseded
// never-activated generation loses the supersede-then-delta hole scope, whose
// active truth still holds the manifest the refused delta deleted; the shipped
// rule keeps it.
func TestReducerContentionGateManifestLessDirtyRuleNaiveRuleLosesTheHole(t *testing.T) {
	ctx, database := openActiveCodeCallSymbolContentSchema(t)
	seedDirtyRuleFixtures(t, ctx, database)
	hole := dirtyRuleScopeID("hole")
	key := "package:fx-hole#run"

	naive := &recordingCodeCallSymbolQueryer{SQLDB: SQLDB{DB: database}}
	naiveScopes := dirtyRuleProducerScopes(t, ctx, naive, legSwapQueryer{recordingCodeCallSymbolQueryer: naive, leg: naiveManifestLessLeg(t)}, key)
	if slices.Contains(naiveScopes, hole) {
		t.Fatalf("the naive rule kept %s; the RED side of this proof is gone", hole)
	}

	shipped := &recordingCodeCallSymbolQueryer{SQLDB: SQLDB{DB: database}}
	if scopes := dirtyRuleProducerScopes(t, ctx, shipped, shipped, key); !slices.Contains(scopes, hole) {
		t.Fatalf("shipped rule producer scopes = %v, want %s (the hole must stay dirty)", scopes, hole)
	}
}

// TestReducerContentionGateManifestLessDirtyRuleSeededRedGreen is the seeded
// RED/GREEN for each of the three manifest reads (#8243): the previous rule
// keeps a legacy row a later marked full covers (RED for the narrowing), and a
// rule without the COALESCE(..., false) wrapper drops the scopes whose newest
// full is unmarked or missing (RED for exactness); the shipped rule gets both
// right (GREEN).
func TestReducerContentionGateManifestLessDirtyRuleSeededRedGreen(t *testing.T) {
	ctx, database := openActiveCodeCallSymbolContentSchema(t)
	seedDirtyRuleFixtures(t, ctx, database)
	covered := dirtyRuleScopeID("legacy-row-later-marked-full")
	unmarked := dirtyRuleScopeID("legacy-row-older-than-unmarked-full")
	noFull := dirtyRuleScopeID("no-activated-full")

	for manifest, key := range dirtyRuleKeys {
		load := func(leg string) []string {
			recorder := &recordingCodeCallSymbolQueryer{SQLDB: SQLDB{DB: database}}
			if leg == "" {
				return dirtyRuleProducerScopes(t, ctx, recorder, recorder, key)
			}
			return dirtyRuleProducerScopes(t, ctx, recorder, legSwapQueryer{recordingCodeCallSymbolQueryer: recorder, leg: leg}, key)
		}
		shipped := load("")
		if slices.Contains(shipped, covered) || !slices.Contains(shipped, unmarked) || !slices.Contains(shipped, noFull) {
			t.Errorf("%s read, shipped rule: covered=%v unmarked=%v noFull=%v, want false true true",
				manifest, slices.Contains(shipped, covered), slices.Contains(shipped, unmarked), slices.Contains(shipped, noFull))
		}
		if previous := load(previousManifestLessLeg(t)); !slices.Contains(previous, covered) {
			t.Errorf("%s read: the previous rule dropped %s; the RED side of the narrowing is gone", manifest, covered)
		}
		unwrapped := load(unwrappedManifestLessLeg(t))
		if slices.Contains(unwrapped, unmarked) || slices.Contains(unwrapped, noFull) {
			t.Errorf("%s read: the unwrapped rule kept %s or %s; the RED side of the wrapper is gone", manifest, unmarked, noFull)
		}
	}
}
