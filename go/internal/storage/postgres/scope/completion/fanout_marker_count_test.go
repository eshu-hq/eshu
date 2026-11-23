// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package completionstore

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/fake"
)

// fanoutStatementStub answers Fanout's two statements: the fan-out statement
// with fanoutRows and the post-commit marker count with countRows, countErr, or
// a block until the context ends. It records every statement and the deadline
// of the context each ran under, and it fails any other query.
type fanoutStatementStub struct {
	fanoutRows [][]any
	countRows  [][]any
	countErr   error
	countBlock bool
	queries    []string
	deadlines  []time.Time
}

func (s *fanoutStatementStub) QueryContext(ctx context.Context, query string, _ ...any) (db.Rows, error) {
	s.queries = append(s.queries, query)
	deadline, _ := ctx.Deadline()
	s.deadlines = append(s.deadlines, deadline)
	switch query {
	case FanoutCrossScopeCompletionQuery:
		return &fake.Rows{Data: s.fanoutRows}, nil
	case manifestMarkerCountQuery:
		if s.countBlock {
			<-ctx.Done()
			return nil, ctx.Err()
		}
		if s.countErr != nil {
			return nil, s.countErr
		}
		return &fake.Rows{Data: s.countRows}, nil
	}
	return nil, errors.New("unexpected query: " + query)
}

func (s *fanoutStatementStub) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, errors.New("fanout must not exec")
}

func committedFanoutRows() [][]any { return [][]any{{1, int64(4), 3, 1}} }

func fanoutLease(producer reducercontract.Domain) reducer.CrossScopeCompletionLease {
	return reducer.CrossScopeCompletionLease{EventID: 7, ProducerDomain: producer, LeaseOwner: "owner", ClaimEpoch: 1}
}

// TestFanoutCountsSkippedMarkersAfterTheFanoutCommits pins that the marker
// count is its own statement, run after the fan-out statement returned, under a
// bounded context, only for a producer with a supply_chain_impact consumer edge.
func TestFanoutCountsSkippedMarkersAfterTheFanoutCommits(t *testing.T) {
	t.Parallel()

	stub := &fanoutStatementStub{fanoutRows: committedFanoutRows(), countRows: [][]any{{5}}}
	store := NewCrossScopeCompletionStore(stub)
	start := time.Now()

	result, err := store.Fanout(context.Background(), fanoutLease(reducer.DomainCICDRunCorrelation), 500)
	if err != nil {
		t.Fatalf("Fanout: %v", err)
	}
	if result.ManifestMarkersSkipped != 5 || result.IntentsEnqueued != 3 || result.EventsProcessed != 1 {
		t.Fatalf("result = %+v, want 5 markers skipped, 3 intents, 1 event", result)
	}
	if len(stub.queries) != 2 || stub.queries[0] != FanoutCrossScopeCompletionQuery || stub.queries[1] != manifestMarkerCountQuery {
		t.Fatalf("statements = %d, want the fan-out statement then the marker count", len(stub.queries))
	}
	if !stub.deadlines[0].IsZero() {
		t.Fatal("the fan-out statement must keep the caller's context unbounded by the marker count timeout")
	}
	if d := stub.deadlines[1]; d.IsZero() || d.After(start.Add(defaultManifestMarkerCountTimeout+time.Second)) {
		t.Fatalf("marker count deadline = %v, want a bound of %s", d, defaultManifestMarkerCountTimeout)
	}
}

// TestFanoutSkipsTheMarkerCountForAProducerWithoutASupplyChainEdge pins that a
// producer feeding no supply_chain_impact consumer pays no count statement.
func TestFanoutSkipsTheMarkerCountForAProducerWithoutASupplyChainEdge(t *testing.T) {
	t.Parallel()

	stub := &fanoutStatementStub{fanoutRows: committedFanoutRows(), countRows: [][]any{{9}}}
	store := NewCrossScopeCompletionStore(stub)

	result, err := store.Fanout(context.Background(), fanoutLease(reducer.DomainCodeFunctionSummary), 500)
	if err != nil {
		t.Fatalf("Fanout: %v", err)
	}
	if len(stub.queries) != 1 || result.ManifestMarkersSkipped != 0 {
		t.Fatalf("statements = %d markers = %d, want one statement and no marker count", len(stub.queries), result.ManifestMarkersSkipped)
	}
}

// TestFanoutDoesNotCountMarkersForARejectedClaim pins that a fan-out that did
// not commit reports no marker count.
func TestFanoutDoesNotCountMarkersForARejectedClaim(t *testing.T) {
	t.Parallel()

	stub := &fanoutStatementStub{fanoutRows: nil, countRows: [][]any{{9}}}
	store := NewCrossScopeCompletionStore(stub)

	_, err := store.Fanout(context.Background(), fanoutLease(reducer.DomainCICDRunCorrelation), 500)
	if !errors.Is(err, ErrCrossScopeCompletionClaimRejected) {
		t.Fatalf("err = %v, want ErrCrossScopeCompletionClaimRejected", err)
	}
	if len(stub.queries) != 1 {
		t.Fatalf("statements = %d, want only the fan-out statement", len(stub.queries))
	}
}

// TestFanoutMarkerCountFailureDoesNotFailTheCommittedFanout pins the one place
// a failure is tolerated: the fan-out already committed, the count only feeds a
// counter, so an error or a timeout logs a warning, leaves ManifestMarkersSkipped
// at zero, and returns the committed result.
func TestFanoutMarkerCountFailureDoesNotFailTheCommittedFanout(t *testing.T) {
	t.Parallel()

	for name, stub := range map[string]*fanoutStatementStub{
		"query error": {fanoutRows: committedFanoutRows(), countErr: errors.New("replica gone")},
		"scan error":  {fanoutRows: committedFanoutRows(), countRows: [][]any{{"not a number"}}},
		"no row":      {fanoutRows: committedFanoutRows()},
		"timeout":     {fanoutRows: committedFanoutRows(), countBlock: true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var logs bytes.Buffer
			store := NewCrossScopeCompletionStore(stub)
			store.Logger = slog.New(slog.NewTextHandler(&logs, nil))
			store.markerCountTimeout = 20 * time.Millisecond

			result, err := store.Fanout(context.Background(), fanoutLease(reducer.DomainContainerImageIdentity), 500)
			if err != nil {
				t.Fatalf("Fanout failed a committed fan-out over a count failure: %v", err)
			}
			if result.ManifestMarkersSkipped != 0 || result.IntentsEnqueued != 3 || result.EventsProcessed != 1 {
				t.Fatalf("result = %+v, want the committed counts and zero markers skipped", result)
			}
			if len(stub.queries) != 2 {
				t.Fatalf("statements = %d, want the fan-out statement and one count attempt", len(stub.queries))
			}
			out := logs.String()
			if !strings.Contains(out, "level=WARN") || !strings.Contains(out, "manifest marker count failed") || !strings.Contains(out, "error=") {
				t.Fatalf("log = %q, want a WARN with the error", out)
			}
		})
	}
}
