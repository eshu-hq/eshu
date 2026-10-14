// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backfill

// Per-block telemetry proofs for RunAllRelationshipEvidence (#8082). The
// maintenance pass takes 62 to 99 s and the deferred_backfill_completed line
// only reports the total, so each block must be separately visible: one child
// span of relationship.backfill_deferred and one deferred_backfill_block_completed
// log line per block per pass. The tests drive the real pass over a fake
// database that answers each shipped query by its text.

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// blockSpanPrefix is the span-name prefix every per-block span carries.
const blockSpanPrefix = "relationship.backfill."

// blockLogMarker is the structured log line the pass emits once per block.
const blockLogMarker = "deferred_backfill_block_completed "

// blockPassDB answers every query a full RunAllRelationshipEvidence pass issues,
// keyed by the shipped query text, and refuses anything it does not recognise so
// a new query shows up as a named failure instead of an arity mismatch. It is
// its own transaction beginner: batches and the fan-in share the same answers.
type blockPassDB struct {
	catalogRows  [][]any
	catalogErr   error
	partitions   [][]any
	factsByScope map[string][][]any
	activeGens   [][]any
	// argoCDProbeDelay makes every ArgoCD-bearing-partition probe sleep, so a
	// test can assert the fan-in's summed probe time rather than wall time.
	argoCDProbeDelay time.Duration
	// memoErr fails the partition memo lookup, the gate's one non-fatal error.
	memoErr error
}

func (d *blockPassDB) answer(query string, args []any) (db.Rows, error) {
	switch {
	case query == listRepositoryCatalogQuery:
		if d.catalogErr != nil {
			return nil, d.catalogErr
		}
		return &queueFakeRows{rows: d.catalogRows}, nil
	case query == ActiveScopeGenerationPartitionsQuery:
		return &queueFakeRows{rows: d.partitions}, nil
	case query == ListDeferredScopedRelationshipFactRecordsQuery:
		scopeID, _ := args[2].(string)
		return &queueFakeRows{rows: d.factsByScope[scopeID]}, nil
	case query == ListArgoCDBearingPartitionsQuery:
		time.Sleep(d.argoCDProbeDelay)
		return &queueFakeRows{}, nil
	case query == LookupDeferredBackfillPartitionMemosQuery:
		if d.memoErr != nil {
			return nil, d.memoErr
		}
		return &queueFakeRows{}, nil
	case strings.HasPrefix(query, ActiveRepositoryGenerationsQuery):
		return &queueFakeRows{rows: d.activeGens}, nil
	case strings.HasPrefix(query, ActiveScopeGenerationQuery):
		scopeID, _ := args[0].(string)
		for _, row := range d.activeGens {
			if row[1] == scopeID {
				return &queueFakeRows{rows: [][]any{{row[2]}}}, nil
			}
		}
		return &queueFakeRows{}, nil
	default:
		return nil, fmt.Errorf("blockPassDB: unrecognized query: %s", firstLine(query))
	}
}

func (d *blockPassDB) QueryContext(_ context.Context, query string, args ...any) (db.Rows, error) {
	return d.answer(query, args)
}

func (d *blockPassDB) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return fakeResult{}, nil
}

func (d *blockPassDB) Begin(context.Context) (db.Transaction, error) {
	return &blockPassTx{database: d}, nil
}

type blockPassTx struct{ database *blockPassDB }

func (tx *blockPassTx) QueryContext(_ context.Context, query string, args ...any) (db.Rows, error) {
	return tx.database.answer(query, args)
}

func (tx *blockPassTx) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return fakeResult{}, nil
}

func (tx *blockPassTx) Commit() error   { return nil }
func (tx *blockPassTx) Rollback() error { return nil }

// newBlockPassStore builds a Store over a fixture with two repositories in two
// scope-generation partitions, three repository fact rows (one older duplicate,
// so rows=3 and entries=2 differ), and one cross-repo content fact that resolves
// to evidence. BatchSize 1 yields two write batches.
func newBlockPassStore(t *testing.T) (Store, *blockPassDB) {
	t.Helper()

	observedNew := time.Date(2026, time.June, 24, 12, 0, 0, 0, time.UTC)
	observedOld := observedNew.Add(-time.Hour)
	database := &blockPassDB{
		catalogRows: [][]any{
			{[]byte(`{"repo_id":"repo-infra","name":"infra-repo"}`), observedNew},
			{[]byte(`{"repo_id":"repo-app","name":"app-repo"}`), observedNew},
			{[]byte(`{"repo_id":"repo-app","name":"app-repo"}`), observedOld},
		},
		partitions: [][]any{
			{"scope-app", "gen-app"},
			{"scope-infra", "gen-infra"},
		},
		factsByScope: map[string][][]any{
			"scope-infra": {
				contentFactRow(
					"fact-cross", "scope-infra", "gen-infra", "content",
					`{"repo_id":"repo-infra","artifact_type":"terraform","relative_path":"main.tf","content":"app_repo = \"app-repo\""}`,
				),
			},
		},
		activeGens: [][]any{
			{"repo-app", "scope-app", "gen-app"},
			{"repo-infra", "scope-infra", "gen-infra"},
		},
	}
	store, _, _ := testStore(database)
	store.Now = func() time.Time { return time.Unix(0, 0).UTC() }
	store.LoadCatalog = LoadRepositoryCatalog
	store.BatchSize = 1
	store.Workers = 2
	return store, database
}

// blockSpanAttr returns the value of a required attribute of a recorded block
// span, failing the test when the span does not carry it.
func blockSpanAttr(t *testing.T, span sdktrace.ReadOnlySpan, key string) attribute.Value {
	t.Helper()
	for _, attr := range span.Attributes() {
		if string(attr.Key) == key {
			return attr.Value
		}
	}
	t.Fatalf("span %q missing attribute %q", span.Name(), key)
	return attribute.Value{}
}

// blockSpanInt reads a required int attribute from a recorded block span.
func blockSpanInt(t *testing.T, span sdktrace.ReadOnlySpan, key string) int64 {
	t.Helper()
	return blockSpanAttr(t, span, key).AsInt64()
}

// blockSpanFloat reads a required float attribute from a recorded block span.
func blockSpanFloat(t *testing.T, span sdktrace.ReadOnlySpan, key string) float64 {
	t.Helper()
	return blockSpanAttr(t, span, key).AsFloat64()
}

// blockLogFields returns the key=value fields of the single block log line for
// block, failing when the line is absent or emitted more than once.
func blockLogFields(t *testing.T, output, block string) map[string]string {
	t.Helper()
	var matches []string
	for _, line := range strings.Split(output, "\n") {
		idx := strings.Index(line, blockLogMarker)
		if idx < 0 {
			continue
		}
		rest := line[idx+len(blockLogMarker):]
		fields := strings.Fields(rest)
		if len(fields) > 0 && fields[0] == "block="+block {
			matches = append(matches, rest)
		}
	}
	if len(matches) != 1 {
		t.Fatalf("block %q: got %d %q lines, want exactly 1\noutput:\n%s", block, len(matches), blockLogMarker, output)
	}
	out := map[string]string{}
	for _, field := range strings.Fields(matches[0]) {
		key, value, ok := strings.Cut(field, "=")
		if !ok {
			t.Fatalf("block %q: malformed field %q in %q", block, field, matches[0])
		}
		out[key] = value
	}
	return out
}

// wantBlock is the expected int attributes of one block span.
type wantBlock struct {
	block string
	attrs map[string]int64
}

// anchorTermCount derives the params_build expectation from the shipped builder
// over the catalog the fixture serves, so the test does not freeze a number.
func anchorTermCount(t *testing.T, store Store) int64 {
	t.Helper()
	catalog, _, err := LoadRepositoryCatalog(context.Background(), store.DB)
	if err != nil {
		t.Fatalf("LoadRepositoryCatalog() error = %v", err)
	}
	params, ok := BuildDeferredScopedFactQueryParams(catalog)
	if !ok {
		t.Fatal("BuildDeferredScopedFactQueryParams ok=false; fixture has no anchor")
	}
	return int64(len(params.NonRepoIDLike) + len(params.RepoIDValues))
}

// TestRunAllRelationshipEvidenceEmitsBlockSpans pins the eight in-pass block
// spans as direct children of relationship.backfill_deferred, each with its
// attribute keys and the values the fixture determines. The ninth block, reopen,
// runs after this method returns and is pinned by the root package tests.
func TestRunAllRelationshipEvidenceEmitsBlockSpans(t *testing.T) {
	t.Parallel()

	store, _ := newBlockPassStore(t)
	anchors := anchorTermCount(t, store)

	recorder, tracer := recordingTracer()
	if _, err := store.RunAllRelationshipEvidence(context.Background(), tracer, nil); err != nil {
		t.Fatalf("RunAllRelationshipEvidence() error = %v, want nil", err)
	}

	ended := recorder.Ended()
	parent := findEndedSpan(t, ended, backfillDeferredSpanName)

	evidenceOut := blockSpanInt(t, findEndedSpan(t, ended, blockSpanPrefix+"discover"), "evidence_out")
	if evidenceOut < 1 {
		t.Fatalf("discover evidence_out = %d, want >= 1 from the cross-repo fixture fact", evidenceOut)
	}

	want := []wantBlock{
		{"catalog_load", map[string]int64{"rows": 3, "entries": 2}},
		{"params_build", map[string]int64{"anchor_terms": anchors}},
		{"partition_list", map[string]int64{"partitions": 2}},
		{"memo_gate", map[string]int64{"candidate_partitions": 2, "skipped": 0, "loaded": 2}},
		{"fact_load", map[string]int64{"partitions": 2, "facts": 1, "workers": 2}},
		{"discover", map[string]int64{"facts_in": 1, "evidence_out": evidenceOut}},
		{"write_batches", map[string]int64{"batches": 2, "readiness_rows": 2, "inserted_rows": evidenceOut}},
		{"fanin", map[string]int64{"partitions": 2, "published": 2, "skipped": 0}},
	}
	for _, block := range want {
		span := findEndedSpan(t, ended, blockSpanPrefix+block.block)
		if got := span.Parent().SpanID(); got != parent.SpanContext().SpanID() {
			t.Errorf("%s parent span = %s, want relationship.backfill_deferred %s",
				span.Name(), got, parent.SpanContext().SpanID())
		}
		if span.Status().Code == codes.Error {
			t.Errorf("%s status = Error, want unset on success", span.Name())
		}
		for key, wantValue := range block.attrs {
			if got := blockSpanInt(t, span, key); got != wantValue {
				t.Errorf("%s %s = %d, want %d", span.Name(), key, got, wantValue)
			}
		}
	}

	// decode_seconds is the time spent decoding the catalog payloads. The
	// fixture decodes three rows, so a loader that stops feeding the stat
	// leaves it at zero, and one that reports more than the span lasted
	// cannot be measuring the decode inside the loader call.
	catalog := findEndedSpan(t, ended, blockSpanPrefix+"catalog_load")
	decode := blockSpanFloat(t, catalog, "decode_seconds")
	if decode <= 0 {
		t.Errorf("catalog_load decode_seconds = %v, want > 0 after decoding %d rows", decode, 3)
	}
	if spanSeconds := catalog.EndTime().Sub(catalog.StartTime()).Seconds(); decode > spanSeconds {
		t.Errorf("catalog_load decode_seconds = %v, want <= span duration %v", decode, spanSeconds)
	}
}

// TestRunAllRelationshipEvidenceLogsOneLinePerBlock pins the operator log: one
// deferred_backfill_block_completed line per in-pass block per pass, with
// duration_s and the block's fields, emitted even with no tracer, while the
// pre-existing deferred_backfill_completed line keeps its exact shape.
func TestRunAllRelationshipEvidenceLogsOneLinePerBlock(t *testing.T) {
	store, _ := newBlockPassStore(t)
	anchors := anchorTermCount(t, store)

	original := log.Writer()
	t.Cleanup(func() { log.SetOutput(original) })
	var buf bytes.Buffer
	log.SetOutput(&buf)

	if _, err := store.RunAllRelationshipEvidence(context.Background(), nil, nil); err != nil {
		t.Fatalf("RunAllRelationshipEvidence() error = %v, want nil", err)
	}
	output := buf.String()

	completed := regexp.MustCompile(
		`deferred_backfill_completed evidence_facts=(\d+) readiness_rows=(\d+) duration_s=\d+\.\d{2} batch_size=32\n`,
	).FindStringSubmatch(output)
	if completed == nil {
		t.Fatalf("existing deferred_backfill_completed line missing or reshaped\noutput:\n%s", output)
	}
	evidenceFacts, _ := strconv.Atoi(completed[1])

	wantFields := map[string]map[string]string{
		"catalog_load":   {"rows": "3", "entries": "2"},
		"params_build":   {"anchor_terms": strconv.FormatInt(anchors, 10)},
		"partition_list": {"partitions": "2"},
		"memo_gate":      {"candidate_partitions": "2", "skipped": "0", "loaded": "2"},
		"fact_load":      {"partitions": "2", "facts": "1", "workers": "2"},
		"discover":       {"facts_in": "1", "evidence_out": strconv.Itoa(evidenceFacts)},
		"write_batches":  {"batches": "2", "readiness_rows": "2", "inserted_rows": strconv.Itoa(evidenceFacts)},
		"fanin":          {"partitions": "2", "published": "2", "skipped": "0"},
	}
	duration := regexp.MustCompile(`^\d+\.\d{2}$`)
	for block, fields := range wantFields {
		got := blockLogFields(t, output, block)
		if !duration.MatchString(got["duration_s"]) {
			t.Errorf("%s duration_s = %q, want %%.2f seconds", block, got["duration_s"])
		}
		for key, want := range fields {
			if got[key] != want {
				t.Errorf("%s %s = %q, want %q", block, key, got[key], want)
			}
		}
	}
	// The pre-existing fan-in line must keep its exact shape (#8082).
	if !regexp.MustCompile(
		`deferred_backfill_fanin_completed partitions=2 published=2 skipped=0 duration_s=\d+\.\d{2} workers=2\n`,
	).MatchString(output) {
		t.Errorf("existing deferred_backfill_fanin_completed line missing or reshaped\noutput:\n%s", output)
	}
	if got := blockLogFields(t, output, "fanin"); !duration.MatchString(got["argocd_probe_seconds"]) {
		t.Errorf("fanin argocd_probe_seconds = %q, want %%.2f seconds", got["argocd_probe_seconds"])
	}
	if got := blockLogFields(t, output, "catalog_load"); !duration.MatchString(got["decode_seconds"]) {
		t.Errorf("catalog_load decode_seconds = %q, want %%.2f seconds", got["decode_seconds"])
	}
	if got := strings.Count(output, blockLogMarker); got != len(wantFields) {
		t.Errorf("%q lines = %d, want %d (one per block)", blockLogMarker, got, len(wantFields))
	}
}

// TestRunAllRelationshipEvidenceCatalogLoadFailureEndsSpanWithError pins that a
// failing block still ends its span and records the error on it, emits exactly
// one deferred_backfill_block_failed line (and no completed line) for that
// block, records no histogram sample, and produces no later block span for a
// pass that never reached it.
func TestRunAllRelationshipEvidenceCatalogLoadFailureEndsSpanWithError(t *testing.T) {
	store, database := newBlockPassStore(t)
	database.catalogErr = errors.New("boom catalog")

	original := log.Writer()
	t.Cleanup(func() { log.SetOutput(original) })
	var buf bytes.Buffer
	log.SetOutput(&buf)

	recorder, tracer := recordingTracer()
	reader, instruments := fanInMetricHarness(t)
	_, err := store.RunAllRelationshipEvidence(context.Background(), tracer, instruments)
	if err == nil {
		t.Fatal("RunAllRelationshipEvidence() error = nil, want catalog load failure")
	}

	ended := recorder.Ended()
	span := findEndedSpan(t, ended, blockSpanPrefix+"catalog_load")
	if span.Status().Code != codes.Error {
		t.Fatalf("catalog_load status = %v, want Error", span.Status().Code)
	}
	var recorded bool
	for _, event := range span.Events() {
		if event.Name == "exception" {
			recorded = true
		}
	}
	if !recorded {
		t.Fatal("catalog_load span has no recorded error event")
	}
	for _, other := range ended {
		if strings.HasPrefix(other.Name(), blockSpanPrefix) && other.Name() != blockSpanPrefix+"catalog_load" {
			t.Errorf("unexpected block span %q after catalog load failure", other.Name())
		}
	}

	output := buf.String()
	failed := regexp.MustCompile(
		`deferred_backfill_block_failed block=catalog_load duration_s=\d+\.\d{2} error="boom catalog"\n`,
	).FindAllString(output, -1)
	if len(failed) != 1 {
		t.Fatalf("deferred_backfill_block_failed lines = %d, want exactly 1\noutput:\n%s", len(failed), output)
	}
	if strings.Contains(output, blockLogMarker+"block=catalog_load") {
		t.Errorf("failed catalog_load also emitted a completed line\noutput:\n%s", output)
	}
	if points := collectBlockDurationPoints(t, reader); len(points) != 0 {
		t.Errorf("failed pass recorded block duration points %v, want none", points)
	}
}

// TestBlockSpanFailedLineQuotesError pins the failed-line format for an error
// message that needs quoting.
func TestBlockSpanFailedLineQuotesError(t *testing.T) {
	original := log.Writer()
	t.Cleanup(func() { log.SetOutput(original) })
	var buf bytes.Buffer
	log.SetOutput(&buf)

	StartBlock(context.Background(), nil, nil, BlockDiscover).End(errors.New(`bad "x"`))

	want := regexp.MustCompile(`deferred_backfill_block_failed block=discover duration_s=\d+\.\d{2} error="bad \\"x\\""\n`)
	if !want.MatchString(buf.String()) {
		t.Fatalf("failed line = %q, want %v", buf.String(), want)
	}
}

// TestRunAllRelationshipEvidenceFanInReportsSummedArgoCDProbeTime pins
// argocd_probe_seconds: the SUM of the per-partition ArgoCD probe durations
// across the fan-in workers, not the fan-in wall time. Two partitions publish
// concurrently on two workers and each probe sleeps 40 ms, so the sum is at
// least 80 ms while the wall time of the overlapped probes is about 40 ms.
func TestRunAllRelationshipEvidenceFanInReportsSummedArgoCDProbeTime(t *testing.T) {
	t.Parallel()

	store, database := newBlockPassStore(t)
	const probeDelay = 40 * time.Millisecond
	database.argoCDProbeDelay = probeDelay

	recorder, tracer := recordingTracer()
	if _, err := store.RunAllRelationshipEvidence(context.Background(), tracer, nil); err != nil {
		t.Fatalf("RunAllRelationshipEvidence() error = %v, want nil", err)
	}

	span := findEndedSpan(t, recorder.Ended(), blockSpanPrefix+"fanin")
	var got float64
	var found bool
	for _, attr := range span.Attributes() {
		if string(attr.Key) == "argocd_probe_seconds" {
			got, found = attr.Value.AsFloat64(), true
		}
	}
	if !found {
		t.Fatal("fanin span missing argocd_probe_seconds")
	}
	if want := 2 * probeDelay.Seconds(); got < want {
		t.Fatalf("argocd_probe_seconds = %.3f, want >= %.3f (sum over two partitions)", got, want)
	}
}

// TestArgoCDProbeTimerSumsConcurrentObservations pins the accumulator itself:
// concurrent observations sum exactly, and a context without a timer ignores
// them.
func TestArgoCDProbeTimerSumsConcurrentObservations(t *testing.T) {
	t.Parallel()

	timer := &argoCDProbeTimer{}
	ctx := withArgoCDProbeTimer(context.Background(), timer)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				recordArgoCDProbe(ctx, 250*time.Microsecond)
			}
		}()
	}
	wg.Wait()
	recordArgoCDProbe(context.Background(), time.Hour)

	if got, want := timer.seconds(), 800*0.00025; got != want {
		t.Fatalf("seconds() = %v, want %v", got, want)
	}
}
