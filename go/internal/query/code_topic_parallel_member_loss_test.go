// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/codequery"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

type codeTopicTestMemberLost struct{}

func (codeTopicTestMemberLost) Error() string          { return "test reader member lost" }
func (codeTopicTestMemberLost) ReaderMemberLost() bool { return true }

type codeTopicRetryTestStore struct {
	codeTopicTestSnapshotStore
	attempts       int
	beginFailure   error
	failEveryBegin bool
}

func (store *codeTopicRetryTestStore) BeginReadOnlySnapshotSet(ctx context.Context, count int) (db.ReadSnapshotSet, error) {
	store.attempts++
	if (store.attempts == 1 || store.failEveryBegin) && store.beginFailure != nil {
		return nil, store.beginFailure
	}
	return store.codeTopicTestSnapshotStore.BeginReadOnlySnapshotSet(ctx, count)
}

func TestInvestigateCodeTopicMemberLossRetryIsBounded(t *testing.T) {
	recorder := &codeTopicParallelRecorder{}
	pool := openCodeTopicParallelDB(t, recorder)
	store := &codeTopicRetryTestStore{
		codeTopicTestSnapshotStore: codeTopicTestSnapshotStore{
			ReadStore: postgres.NewSQLReadStore(pool), handle: pool,
		},
		beginFailure: codeTopicTestMemberLost{}, failEveryBegin: true,
	}
	reader := NewContentReaderWithReadStore(store)
	terms := []string{"0", "1", "2", "3", "4", "5", "6", "7", "8", "9", "10", "11", "12", "13", "14", "15"}
	_, err := reader.InvestigateCodeTopic(t.Context(), codequery.CodeTopicInvestigationRequest{Terms: terms, Limit: 1})
	var lost codeTopicTestMemberLost
	if !errors.As(err, &lost) || store.attempts != 2 {
		t.Fatalf("bounded retry err=%v attempts=%d", err, store.attempts)
	}
}

func TestInvestigateCodeTopicRetriesWholeReadAfterMemberLoss(t *testing.T) {
	recorder := &codeTopicParallelRecorder{}
	pool := openCodeTopicParallelDB(t, recorder)
	store := &codeTopicRetryTestStore{
		codeTopicTestSnapshotStore: codeTopicTestSnapshotStore{
			ReadStore: postgres.NewSQLReadStore(pool), handle: pool,
		},
		beginFailure: fmt.Errorf("wrapped member failure: %w", codeTopicTestMemberLost{}),
	}
	reader := NewContentReaderWithReadStore(store)
	terms := []string{"same", "other", "same", "3", "4", "5", "6", "7", "8", "9", "10", "11", "12", "13", "14", "15"}
	result, err := reader.InvestigateCodeTopic(t.Context(), codequery.CodeTopicInvestigationRequest{Terms: terms, Limit: 1})
	if err != nil || len(result) != 1 {
		t.Fatalf("whole-read retry result=%#v err=%v", result, err)
	}
	if store.attempts != 2 {
		t.Fatalf("snapshot attempts=%d, want 2", store.attempts)
	}
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	if recorder.probes != 4 || recorder.imports != 3 || recorder.active != 0 || recorder.assemblyJSON == "" {
		t.Fatalf("retry probes=%d imports=%d active=%d assembly=%q", recorder.probes, recorder.imports, recorder.active, recorder.assemblyJSON)
	}
}

func TestInvestigateCodeTopicDoesNotRetryMemberLossAfterCancellation(t *testing.T) {
	recorder := &codeTopicParallelRecorder{}
	pool := openCodeTopicParallelDB(t, recorder)
	store := &codeTopicRetryTestStore{
		codeTopicTestSnapshotStore: codeTopicTestSnapshotStore{
			ReadStore: postgres.NewSQLReadStore(pool), handle: pool,
		},
		beginFailure: codeTopicTestMemberLost{},
	}
	reader := NewContentReaderWithReadStore(store)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	terms := []string{"0", "1", "2", "3", "4", "5", "6", "7", "8", "9", "10", "11", "12", "13", "14", "15"}
	_, err := reader.InvestigateCodeTopic(ctx, codequery.CodeTopicInvestigationRequest{Terms: terms, Limit: 1})
	var lost codeTopicTestMemberLost
	if !errors.As(err, &lost) || store.attempts != 1 {
		t.Fatalf("canceled read err=%v attempts=%d", err, store.attempts)
	}
}

func TestInvestigateCodeTopicRetriesAllProbesAfterMemberLoss(t *testing.T) {
	recorder := &codeTopicParallelRecorder{probeFailureOnce: codeTopicTestMemberLost{}}
	pool := openCodeTopicParallelDB(t, recorder)
	store := &codeTopicRetryTestStore{codeTopicTestSnapshotStore: codeTopicTestSnapshotStore{
		ReadStore: postgres.NewSQLReadStore(pool), handle: pool,
	}}
	reader := NewContentReaderWithReadStore(store)
	terms := []string{"same", "other", "same", "3", "4", "5", "6", "7", "8", "9", "10", "11", "12", "13", "14", "15"}
	result, err := reader.InvestigateCodeTopic(t.Context(), codequery.CodeTopicInvestigationRequest{Terms: terms, Limit: 1})
	if err != nil || len(result) != 1 {
		t.Fatalf("whole-read probe retry result=%#v err=%v", result, err)
	}
	if store.attempts != 2 {
		t.Fatalf("snapshot attempts=%d, want 2", store.attempts)
	}
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	if recorder.probes < 5 || recorder.imports != 6 || recorder.active != 0 || recorder.assemblyJSON == "" {
		t.Fatalf("retry probes=%d imports=%d active=%d assembly=%q", recorder.probes, recorder.imports, recorder.active, recorder.assemblyJSON)
	}
}

func TestInvestigateCodeTopicParallelRollsBackAfterProbeError(t *testing.T) {
	recorder := &codeTopicParallelRecorder{probeFailure: errors.New("injected probe failure")}
	reader := newCodeTopicParallelTestReader(openCodeTopicParallelDB(t, recorder))
	terms := []string{"0", "1", "2", "3", "4", "5", "6", "7", "8", "9", "10", "11", "12", "13", "14", "15"}
	_, err := reader.InvestigateCodeTopic(context.Background(), codequery.CodeTopicInvestigationRequest{Terms: terms, Limit: 26})
	if err == nil || !strings.Contains(err.Error(), "injected probe failure") {
		t.Fatalf("error = %v, want injected failure", err)
	}
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	if recorder.active != 0 || recorder.assemblyJSON != "" {
		t.Fatalf("active=%d assembly=%q", recorder.active, recorder.assemblyJSON)
	}
}

var (
	_ error                      = codeTopicTestMemberLost{}
	_ db.ReadSnapshotSetBeginner = (*codeTopicRetryTestStore)(nil)
)
