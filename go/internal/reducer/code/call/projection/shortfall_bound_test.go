// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projection

import (
	"context"
	"errors"
	"testing"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/crossscope"
	"github.com/eshu-hq/eshu/go/internal/reducer/intents/shared/worker"
	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
)

// TestCodeCallShortfallWaitExceededReasonIsBounded pins the dead-letter
// vocabulary: the unroutable-row reason and the stale-intents reason are one
// bounded string, so the table and the series stay queryable together.
func TestCodeCallShortfallWaitExceededReasonIsBounded(t *testing.T) {
	t.Parallel()

	if got, want := sharedintent.UnroutableReasonCodeCallShortfallWaitExceeded, "code_call_shortfall_wait_exceeded"; got != want {
		t.Fatalf("unroutable reason = %q, want %q", got, want)
	}
	if got := worker.StaleReasonCodeCallShortfallWaitExceeded; got != sharedintent.UnroutableReasonCodeCallShortfallWaitExceeded {
		t.Fatalf("stale reason = %q, want the unroutable reason %q", got, sharedintent.UnroutableReasonCodeCallShortfallWaitExceeded)
	}
}

// unclassedShortfallError scripts a retryable write failure that carries no
// failure class: the shortfall bound keys on the class, so this error must
// keep the legacy behavior (fail the visit, stamp nothing, dead-letter
// nothing).
type unclassedShortfallError struct{ msg string }

func (e *unclassedShortfallError) Error() string   { return e.msg }
func (e *unclassedShortfallError) Retryable() bool { return true }

func boundTestRunner(
	reader *fakeCodeCallIntentStore,
	writer sharedintent.EdgeWriter,
	unroutable *capturingCodeCallUnroutableWriter,
) Runner {
	return Runner{
		IntentReader:     reader,
		LeaseManager:     reader,
		EdgeWriter:       writer,
		UnroutableWriter: unroutable,
		AcceptedGen: func(key sharedintent.AcceptanceKey) (string, bool) {
			return "gen-1", key.ScopeID == "scope-a" && key.AcceptanceUnitID == "repo-a" && key.SourceRunID == "run-1"
		},
		Config: RunnerConfig{BatchLimit: 10},
	}
}

func boundTestSetup(now time.Time) (*fakeCodeCallIntentStore, *shortfallCodeCallWriter, *capturingCodeCallUnroutableWriter) {
	reader := &fakeCodeCallIntentStore{
		pendingByDomain: []sharedintent.Row{
			codeCallProjectionTestRow("edge-2", "gen-1", now),
		},
		pendingByAcceptance: map[string][]sharedintent.Row{
			"scope-a|repo-a|run-1": {
				codeCallProjectionTestRow("edge-2", "gen-1", now),
			},
		},
		leaseGranted: true,
	}
	writer := &shortfallCodeCallWriter{err: &retryableShortfallError{msg: "code_calls CALLS batch matched 0 of 1 submitted rows"}}
	return reader, writer, &capturingCodeCallUnroutableWriter{}
}

// TestCodeCallProjectionFreshShortfallParksWithReportCounts pins the park
// half of the #10947 bound: a first shortfall stamps the batch and fails the
// visit before anything completes, while the terminal submitted/matched
// counts still reach the visit result.
func TestCodeCallProjectionFreshShortfallParksWithReportCounts(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 19, 12, 0, 0, 0, time.UTC)
	reader, writer, unroutable := boundTestSetup(now)
	runner := boundTestRunner(reader, writer, unroutable)

	result, err := runner.processOnce(context.Background(), now)
	if err == nil {
		t.Fatal("processOnce() with a fresh shortfall = nil error, want the park error so the intent stays open")
	}
	if !reducercontract.IsRetryable(err) {
		t.Fatalf("processOnce() error = %v, want retryable so the visit re-polls", err)
	}
	var classed interface{ FailureClass() string }
	if !errors.As(err, &classed) || classed.FailureClass() != codeCallShortfallFailureClass {
		t.Fatalf("processOnce() error = %v, want the shortfall failure class preserved", err)
	}
	if got, want := result.CodeCallSubmittedRows, 1; got != want {
		t.Fatalf("CodeCallSubmittedRows = %d, want %d (terminal counts ride the park error)", got, want)
	}
	if got, want := result.CodeCallMatchedRows, 0; got != want {
		t.Fatalf("CodeCallMatchedRows = %d, want %d", got, want)
	}
	if got := len(reader.marked); got != 0 {
		t.Fatalf("marked completed = %v, want none (parked batch completes nothing)", reader.marked)
	}
	if got := len(unroutable.rows); got != 0 {
		t.Fatalf("dead-lettered rows = %d, want 0 (fresh park keeps waiting)", got)
	}
	stamp, ok := reader.shortfallStamps["edge-2"]
	if !ok {
		t.Fatal("no first-shortfall stamp recorded, want one so the 30-minute bound has an anchor")
	}
	if !stamp.Equal(now) {
		t.Fatalf("first-shortfall stamp = %v, want %v (COALESCE-once anchors on the first park)", stamp, now)
	}
}

// TestCodeCallProjectionExpiredShortfallDeadLettersBeforeCompletion pins the
// expiry half of the #10947 bound: a shortfall whose oldest stamp is past
// crossscope.ProducerReadinessMaxWait dead-letters every active intent
// through the unroutable sink BEFORE completing them, instead of parking
// forever. A deleted endpoint in a never-superseded generation terminates
// here instead of starving its partition.
func TestCodeCallProjectionExpiredShortfallDeadLettersBeforeCompletion(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 19, 12, 0, 0, 0, time.UTC)
	reader, writer, unroutable := boundTestSetup(now)
	reader.shortfallStamps = map[string]time.Time{
		"edge-2": now.Add(-crossscope.ProducerReadinessMaxWait - time.Minute),
	}
	runner := boundTestRunner(reader, writer, unroutable)

	result, err := runner.processOnce(context.Background(), now)
	if err != nil {
		t.Fatalf("processOnce() with an expired shortfall = %v, want nil (dead-letter terminates the park)", err)
	}
	if got := len(unroutable.rows); got != 1 {
		t.Fatalf("dead-lettered rows = %d, want 1 (one unroutable row per active intent)", got)
	}
	deadLetter := unroutable.rows[0]
	if deadLetter.IntentID != "edge-2" {
		t.Fatalf("dead-letter intent = %q, want %q", deadLetter.IntentID, "edge-2")
	}
	if deadLetter.Reason != sharedintent.UnroutableReasonCodeCallShortfallWaitExceeded {
		t.Fatalf("dead-letter reason = %q, want %q", deadLetter.Reason, sharedintent.UnroutableReasonCodeCallShortfallWaitExceeded)
	}
	if !deadLetter.DecidedAt.Equal(now) {
		t.Fatalf("dead-letter decided_at = %v, want %v", deadLetter.DecidedAt, now)
	}
	if got := len(reader.marked); got != 1 || reader.marked[0] != "edge-2" {
		t.Fatalf("marked completed = %v, want [edge-2] (dead-lettered intents complete, never silently)", reader.marked)
	}
	if got, want := result.CodeCallSubmittedRows, 1; got != want {
		t.Fatalf("CodeCallSubmittedRows = %d, want %d", got, want)
	}
	if got, want := result.CodeCallMatchedRows, 0; got != want {
		t.Fatalf("CodeCallMatchedRows = %d, want %d", got, want)
	}
}

// noStampCodeCallIntentStore wraps the fake without the shortfall-stamp port:
// a reader without the durable stamp cannot anchor the bound, so every
// shortfall defers (NULL defers) exactly as before #10947.
type noStampCodeCallIntentStore struct {
	inner *fakeCodeCallIntentStore
}

func (s *noStampCodeCallIntentStore) ListPendingDomainIntents(ctx context.Context, domain string, limit int) ([]sharedintent.Row, error) {
	return s.inner.ListPendingDomainIntents(ctx, domain, limit)
}

func (s *noStampCodeCallIntentStore) ListPendingAcceptanceUnitIntents(
	ctx context.Context,
	key sharedintent.AcceptanceKey,
	domain string,
	limit int,
) ([]sharedintent.Row, error) {
	return s.inner.ListPendingAcceptanceUnitIntents(ctx, key, domain, limit)
}

func (s *noStampCodeCallIntentStore) MarkIntentsCompleted(ctx context.Context, intentIDs []string, completedAt time.Time) (int64, error) {
	return s.inner.MarkIntentsCompleted(ctx, intentIDs, completedAt)
}

func (s *noStampCodeCallIntentStore) ClaimPartitionLease(ctx context.Context, domain string, partitionID, partitionCount int, leaseOwner string, leaseTTL time.Duration) (bool, error) {
	return s.inner.ClaimPartitionLease(ctx, domain, partitionID, partitionCount, leaseOwner, leaseTTL)
}

func (s *noStampCodeCallIntentStore) ReleasePartitionLease(ctx context.Context, domain string, partitionID, partitionCount int, leaseOwner string) error {
	return s.inner.ReleasePartitionLease(ctx, domain, partitionID, partitionCount, leaseOwner)
}

// TestCodeCallProjectionShortfallDefersWithoutStamper pins the NULL-defers
// rule: without the stamp port the runner cannot anchor the bound, so the
// park stays open and completes nothing.
func TestCodeCallProjectionShortfallDefersWithoutStamper(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 19, 12, 0, 0, 0, time.UTC)
	reader, writer, unroutable := boundTestSetup(now)
	runner := boundTestRunner(reader, writer, unroutable)
	nostamp := &noStampCodeCallIntentStore{inner: reader}
	runner.IntentReader = nostamp
	runner.LeaseManager = nostamp

	_, err := runner.processOnce(context.Background(), now)
	if err == nil {
		t.Fatal("processOnce() without a stamper = nil error, want the park error (NULL defers)")
	}
	if got := len(reader.marked); got != 0 {
		t.Fatalf("marked completed = %v, want none", reader.marked)
	}
	if got := len(unroutable.rows); got != 0 {
		t.Fatalf("dead-lettered rows = %d, want 0", got)
	}
}

// TestCodeCallProjectionExpiredShortfallKeepsParkingWithoutSink pins the
// fail-closed rule: an expired park with no unroutable sink keeps parking
// instead of completing without the durable dead-letter row (#5984).
func TestCodeCallProjectionExpiredShortfallKeepsParkingWithoutSink(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 19, 12, 0, 0, 0, time.UTC)
	reader, writer, _ := boundTestSetup(now)
	reader.shortfallStamps = map[string]time.Time{
		"edge-2": now.Add(-crossscope.ProducerReadinessMaxWait - time.Minute),
	}
	runner := shortfallTestRunner(writer, reader)

	_, err := runner.processOnce(context.Background(), now)
	if err == nil {
		t.Fatal("processOnce() with an expired shortfall and no sink = nil error, want the park error")
	}
	if got := len(reader.marked); got != 0 {
		t.Fatalf("marked completed = %v, want none (no sink means no completion)", reader.marked)
	}
}

// TestCodeCallProjectionUnclassedWriteErrorSkipsShortfallBound pins the
// failure-class key: a retryable write error without the shortfall class
// (e.g. an unverifiable batch) keeps the legacy behavior and stamps
// nothing.
func TestCodeCallProjectionUnclassedWriteErrorSkipsShortfallBound(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 19, 12, 0, 0, 0, time.UTC)
	reader := &fakeCodeCallIntentStore{
		pendingByDomain: []sharedintent.Row{
			codeCallProjectionTestRow("edge-2", "gen-1", now),
		},
		pendingByAcceptance: map[string][]sharedintent.Row{
			"scope-a|repo-a|run-1": {
				codeCallProjectionTestRow("edge-2", "gen-1", now),
			},
		},
		leaseGranted: true,
	}
	writer := &shortfallCodeCallWriter{err: &unclassedShortfallError{msg: "counted-write seam unavailable"}}
	runner := boundTestRunner(reader, writer, &capturingCodeCallUnroutableWriter{})

	_, err := runner.processOnce(context.Background(), now)
	if err == nil {
		t.Fatal("processOnce() with an unclassed write error = nil error, want the write error")
	}
	if len(reader.shortfallStamps) != 0 {
		t.Fatalf("shortfall stamps = %v, want none (unclassed errors never anchor the bound)", reader.shortfallStamps)
	}
	if got := len(reader.marked); got != 0 {
		t.Fatalf("marked completed = %v, want none", reader.marked)
	}
}

// TestCodeCallProjectionStampFailureKeepsParking pins the stamp fail-closed
// rule (#10947): when the first-shortfall stamp write fails, the visit keeps
// parking with the retryable park error instead of completing, and it records
// no stamp, no completion, and no dead-letter row.
func TestCodeCallProjectionStampFailureKeepsParking(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 19, 12, 0, 0, 0, time.UTC)
	reader, writer, unroutable := boundTestSetup(now)
	reader.stampErr = errors.New("stamp write failed")
	runner := boundTestRunner(reader, writer, unroutable)

	_, err := runner.processOnce(context.Background(), now)
	if err == nil {
		t.Fatal("processOnce() with a failed shortfall stamp = nil error, want the park error so the intent stays open")
	}
	if !reducercontract.IsRetryable(err) {
		t.Fatalf("processOnce() error = %v, want retryable so the visit re-polls", err)
	}
	if got := len(reader.marked); got != 0 {
		t.Fatalf("marked completed = %v, want none (a failed stamp completes nothing)", reader.marked)
	}
	if got := len(unroutable.rows); got != 0 {
		t.Fatalf("dead-lettered rows = %d, want 0 (a failed stamp dead-letters nothing)", got)
	}
	if got := len(reader.shortfallStamps); got != 0 {
		t.Fatalf("shortfall stamps = %v, want none (the failed stamp records nothing)", reader.shortfallStamps)
	}
}

// TestCodeCallProjectionDeadLetterWriteFailureFailsCycle pins the
// dead-letter fail-closed rule (#10947): when the unroutable-row write fails
// on an expired shortfall, the cycle fails with that error instead of
// completing, and it marks nothing completed and dead-letters nothing.
func TestCodeCallProjectionDeadLetterWriteFailureFailsCycle(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 19, 12, 0, 0, 0, time.UTC)
	reader, writer, unroutable := boundTestSetup(now)
	reader.shortfallStamps = map[string]time.Time{
		"edge-2": now.Add(-crossscope.ProducerReadinessMaxWait - time.Minute),
	}
	unroutable.err = errors.New("unroutable write failed")
	runner := boundTestRunner(reader, writer, unroutable)

	_, err := runner.processOnce(context.Background(), now)
	if err == nil {
		t.Fatal("processOnce() with a failed dead-letter write = nil error, want the write error so nothing completes silently")
	}
	if got := len(reader.marked); got != 0 {
		t.Fatalf("marked completed = %v, want none (a failed dead-letter write completes nothing)", reader.marked)
	}
	if got := len(unroutable.rows); got != 0 {
		t.Fatalf("dead-lettered rows = %d, want 0 (the failed write persisted nothing)", got)
	}
}
