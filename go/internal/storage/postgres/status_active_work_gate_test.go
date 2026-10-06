// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// SHA-256 of the #7009 S5 shim renders of the gated a2v variant (harness
// 7009-c3-shim-src, render_s5.py). The measured statement has no mode
// section; activeWorkSummaryMeasuredText removes it before hashing.
const (
	shimA2vThresholdSHA256     = "89d33b86f51f8ae44e7d6f980d6e4d84c0d2eca0738dd78bbf4e561294f421bf" // s5_a2v_T.sql, T=0.4
	shimA2vForcedGroupedSHA256 = "3150325fb8458c0add089b5e7fb049cbdb5e111d0c1f5914032a9b251b736e28" // s5_a2v_g.sql
	shimA2vForcedDetailSHA256  = "5d42b8fd2daa53bb05788f66ef38009aaf00da6d5f12df94f5881d87450b410c" // s5_a2v_d.sql
	shimA2vMutDropHistorySHA   = "e00fd31617ac915df012b4535e6e6a41dc4854580227aa644c5f22a641a6c24f" // s5_mut_a2v_b_drop_hist_notin.sql
	shimA2vMutGateNullSHA      = "30ac0672a5dd2f872099afccea18c5a7e57bca56596ffaec1d997647b785c26a" // s5_mut_a2v_c_gate_null.sql
	shimA2vMutGroupedNullSHA   = "cfffedaad0b7d3643bb677c7d51040dec833d2bfbaebca1e175ceb4283541a2d" // s5_mut_a2v_c2_grouped_null.sql
)

// Forced-branch threshold literals of the shim's _g/_d renderings: the MCV
// frequency sum is always in [0, 1], so "< 2" always groups and "< -1" never
// does. Same pg_stats lookup, only the literal differs.
const (
	activeWorkForceGrouped = "2"
	activeWorkForceDetail  = "-1"
)

// activeWorkSummaryModeUnion is the result-stream arm of the mode section.
const activeWorkSummaryModeUnion = "UNION ALL\nSELECT '" + activeWorkSectionMode + "', ordinal, section_json::text FROM active_work_mode\n"

// replaceOnce replaces old with replacement when old occurs exactly once.
func replaceOnce(query, old, replacement string) (string, error) {
	if n := strings.Count(query, old); n != 1 {
		return "", fmt.Errorf("anchor matched %d times, want 1: %q", n, old)
	}
	return strings.Replace(query, old, replacement, 1), nil
}

// activeWorkSummaryMeasuredText removes the mode section (#7009 S5 ruling
// D5.2), which the S5 shim did not measure, leaving the measured statement.
func activeWorkSummaryMeasuredText(query string) (string, error) {
	query, err := replaceOnce(query, ",\n"+activeWorkSummaryModeSection, "")
	if err != nil {
		return "", err
	}
	return replaceOnce(query, activeWorkSummaryModeUnion, "")
}

// activeWorkSummaryForcedGate renders the summary with the gate threshold
// literal replaced by activeWorkForceGrouped or activeWorkForceDetail. Test
// only: production never forces a branch.
func activeWorkSummaryForcedGate(query, threshold string) (string, error) {
	return replaceOnce(query, " < "+activeWorkGroupedThreshold+" AS grouped", " < "+threshold+" AS grouped")
}

// activeWorkGateMutant is one seeded S5 mutation (ruling D5.6) of the gated
// summary; wantEqual says whether it must still equal the oracle.
type activeWorkGateMutant struct {
	name             string
	old, replacement string
	wantEqual        bool
	shimSHA          string
}

// activeWorkGateMutants are mutations (b), (c), and (c2). Mutation (a), the
// forced wrong branch, is the forced renders every differential case runs.
func activeWorkGateMutants() []activeWorkGateMutant {
	history := "  WHERE status_group.status NOT IN " + activeWorkDetailStatuses + "\n  GROUP BY status_group.stage, status_group.status"
	return []activeWorkGateMutant{
		{
			name:        "(b) drop the history NOT IN (six) predicate",
			old:         history,
			replacement: "  GROUP BY status_group.stage, status_group.status",
			shimSHA:     shimA2vMutDropHistorySHA,
		},
		{
			name:        "(c) gate stats subquery returns NULL",
			old:         activeWorkLiveFractionEstimate + " < " + activeWorkGroupedThreshold + " AS grouped",
			replacement: "COALESCE((SELECT NULL::real), 0) < " + activeWorkGroupedThreshold + " AS grouped",
			wantEqual:   true,
			shimSHA:     shimA2vMutGateNullSHA,
		},
		{
			name:        "(c2) the grouped flag itself is NULL",
			old:         "), 0) < " + activeWorkGroupedThreshold + " AS grouped",
			replacement: "), 0) < " + activeWorkGroupedThreshold + " AND NULL AS grouped",
			shimSHA:     shimA2vMutGroupedNullSHA,
		},
	}
}

func sha256Hex(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

// TestActiveWorkSummaryIsTheMeasuredA2vRender binds the shipped statement to
// the S5 shim text: without the mode section it is s5_a2v_T.sql byte for
// byte, its forced renders are s5_a2v_g.sql and s5_a2v_d.sql, and the gate
// mutations are the shim's mutation files.
func TestActiveWorkSummaryIsTheMeasuredA2vRender(t *testing.T) {
	t.Parallel()

	measured, err := activeWorkSummaryMeasuredText(activeWorkSummaryQuery)
	if err != nil {
		t.Fatalf("strip mode section: %v", err)
	}
	if got := sha256Hex(measured); got != shimA2vThresholdSHA256 {
		t.Fatalf("measured text sha256 = %s, want s5_a2v_T.sql %s\n%s", got, shimA2vThresholdSHA256, measured)
	}
	for threshold, want := range map[string]string{
		activeWorkForceGrouped: shimA2vForcedGroupedSHA256,
		activeWorkForceDetail:  shimA2vForcedDetailSHA256,
	} {
		forced, err := activeWorkSummaryForcedGate(measured, threshold)
		if err != nil {
			t.Fatalf("force threshold %s: %v", threshold, err)
		}
		if got := sha256Hex(forced); got != want {
			t.Errorf("forced threshold %s sha256 = %s, want %s", threshold, got, want)
		}
	}
	for _, m := range activeWorkGateMutants() {
		mutated, err := replaceOnce(measured, m.old, m.replacement)
		if err != nil {
			t.Fatalf("%s: %v", m.name, err)
		}
		if got := sha256Hex(mutated); got != m.shimSHA {
			t.Errorf("%s sha256 = %s, want shim %s", m.name, got, m.shimSHA)
		}
	}
}

// TestActiveWorkSummaryGateShape pins the gate properties the S5 ruling
// requires: first CTE, table resolved by regclass (never current_schema()),
// a non-nullable COALESCEd estimate, the threshold constant, both one-time
// filtered arms, the gated grouped pass, and the branch-aware queue counts.
func TestActiveWorkSummaryGateShape(t *testing.T) {
	t.Parallel()

	query := activeWorkSummaryQuery
	if !strings.HasPrefix(query, "\nWITH fact_work_summary_mode AS MATERIALIZED (\n") {
		t.Fatalf("the gate must be the first CTE:\n%.200s", query)
	}
	if strings.Contains(query, "current_schema()") {
		t.Errorf("the gate must resolve fact_work_items by regclass, not current_schema()")
	}
	grouped := "(SELECT grouped FROM fact_work_summary_mode)"
	for name, want := range map[string]string{
		"threshold":          "), 0) < " + activeWorkGroupedThreshold + " AS grouped\n)",
		"grouped detail arm": "(SELECT * FROM fact_work_items WHERE status IN " + activeWorkDetailStatuses + " AND " + grouped + ")",
		"detail arm":         "(SELECT * FROM fact_work_items WHERE NOT " + grouped + ")",
		"grouped pass gated": "  FROM fact_work_items\n  WHERE " + grouped + "\n  GROUP BY scope_id, generation_id, stage, status",
		"total by branch":    "SELECT CASE WHEN " + grouped + "\n            THEN (SELECT COALESCE(SUM(row_count), 0)::BIGINT FROM fact_work_status_groups)\n            ELSE (SELECT COUNT(*) FROM fact_work_items) END AS total_count",
		"succeeded both":     "WHERE status = 'succeeded')\n         + COUNT(*) FILTER (WHERE status = 'succeeded') AS succeeded_count",
		"mode section":       "SELECT '" + activeWorkSectionMode + "', ordinal, section_json::text FROM active_work_mode",
	} {
		if strings.Count(query, want) != 1 {
			t.Errorf("%s: want exactly one %q in the summary query", name, want)
		}
	}
	// The estimate is COALESCEd to 0 so the gate is never NULL: a NULL gate
	// drops both detail arms (S5 mutation c2). It appears in the gate and in
	// the mode section's estimate.
	if strings.Count(activeWorkLiveFractionEstimate, "WHERE c.oid = 'fact_work_items'::regclass)") != 1 {
		t.Errorf("the gate estimate must resolve the schema of 'fact_work_items'::regclass:\n%s", activeWorkLiveFractionEstimate)
	}
	if !strings.HasPrefix(activeWorkLiveFractionEstimate, "COALESCE((SELECT SUM(m.f) FROM pg_stats AS s") ||
		!strings.HasSuffix(activeWorkLiveFractionEstimate, "), 0)") ||
		strings.Count(query, activeWorkLiveFractionEstimate) != 2 {
		t.Errorf("gate estimate must be one COALESCE(..., 0) shared by the gate and the mode row:\n%s", activeWorkLiveFractionEstimate)
	}
	if activeWorkGroupedThreshold != "0.4" {
		t.Errorf("threshold = %s; T is re-measured (S5 R2.4), never tuned in place", activeWorkGroupedThreshold)
	}
}

// TestActiveWorkSummaryDecodesTheModeRow covers the mode section decoder:
// both branch values and the estimate decode, and a value outside the closed
// set, a missing estimate, or a text estimate is rejected.
func TestActiveWorkSummaryDecodesTheModeRow(t *testing.T) {
	t.Parallel()

	var summary activeWorkSummary
	if err := summary.add(activeWorkSectionMode, `{"mode":"detail","estimate":0.8019}`); err != nil {
		t.Fatalf("add(mode) error = %v", err)
	}
	if summary.Mode != activeWorkModeDetail || summary.Estimate != 0.8019 {
		t.Fatalf("mode row decoded to %q %v, want detail 0.8019", summary.Mode, summary.Estimate)
	}
	if err := summary.add(activeWorkSectionMode, `{"mode":"grouped","estimate":0}`); err != nil || summary.Mode != activeWorkModeGrouped {
		t.Fatalf("grouped mode row: mode %q err %v", summary.Mode, err)
	}
	for name, raw := range map[string]string{
		"unknown mode":     `{"mode":"sometimes","estimate":0.1}`,
		"missing estimate": `{"mode":"grouped"}`,
		"estimate is text": `{"mode":"grouped","estimate":"0.1"}`,
	} {
		var bad activeWorkSummary
		if err := bad.add(activeWorkSectionMode, raw); err == nil {
			t.Errorf("%s: accepted %s", name, raw)
		}
	}
}

// modeQueryer answers the active-work summary with a stage row and, when
// modeRow is set, a mode row; everything else returns no rows.
type modeQueryer struct{ modeRow string }

func (q modeQueryer) QueryContext(_ context.Context, query string, _ ...any) (db.Rows, error) {
	if query != activeWorkSummaryQuery {
		return &fakeRows{}, nil
	}
	rows := [][]any{{"stage", int64(1), `{"stage":"reducer","status":"pending","count":1}`}}
	if q.modeRow != "" {
		rows = append(rows, []any{"mode", int64(1), q.modeRow})
	}
	return &fakeRows{rows: rows}, nil
}

// TestReadStatusSnapshotRecordsActiveWorkSummaryMode proves an operator can
// see which gate branch a status poll took and what the gate estimated: the
// span active on the read context carries both attributes, and the snapshot
// the API serves is unchanged by the mode row.
func TestReadStatusSnapshotRecordsActiveWorkSummaryMode(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		modeRow  string
		wantMode string
		wantEst  float64
	}{
		"detail branch":  {`{"mode":"detail","estimate":0.8019}`, "detail", 0.8019},
		"grouped branch": {`{"mode":"grouped","estimate":0.001}`, "grouped", 0.001},
		"no mode row":    {"", "", 0},
	} {
		recorder := tracetest.NewSpanRecorder()
		provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
		ctx, span := provider.Tracer("test").Start(context.Background(), "postgres.status_snapshot")
		snapshot, err := NewStatusStore(modeQueryer{modeRow: tc.modeRow}).ReadStatusSnapshotFiltered(
			ctx, time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC), statuspkg.FullSnapshotSelection())
		span.End()
		if err != nil {
			t.Fatalf("%s: ReadStatusSnapshotFiltered() error = %v", name, err)
		}
		if len(snapshot.StageCounts) != 1 {
			t.Fatalf("%s: stage counts = %#v, want the one stage row", name, snapshot.StageCounts)
		}
		attrs := map[attribute.Key]attribute.Value{}
		for _, kv := range recorder.Ended()[0].Attributes() {
			attrs[kv.Key] = kv.Value
		}
		mode, hasMode := attrs[statusActiveWorkSummaryModeKey]
		estimate, hasEstimate := attrs[statusActiveWorkSummaryEstimateKey]
		if tc.wantMode == "" {
			if hasMode || hasEstimate {
				t.Errorf("%s: recorded %v without a mode row", name, attrs)
			}
			continue
		}
		if !hasMode || mode.AsString() != tc.wantMode {
			t.Errorf("%s: %s = %v, want %s", name, statusActiveWorkSummaryModeKey, mode.String(), tc.wantMode)
		}
		if !hasEstimate || estimate.AsFloat64() != tc.wantEst {
			t.Errorf("%s: %s = %v, want %v", name, statusActiveWorkSummaryEstimateKey, estimate.String(), tc.wantEst)
		}
	}
}
