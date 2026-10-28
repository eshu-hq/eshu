// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"strings"
	"sync"
	"testing"
	"time"

	sdkcollector "github.com/eshu-hq/eshu-sdk/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/extensionhost"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// countingExecQueryer records every statement the source-state recorder runs.
type countingExecQueryer struct {
	mu    sync.Mutex
	execs []string
}

func (c *countingExecQueryer) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	return nil, nil
}

func (c *countingExecQueryer) ExecContext(_ context.Context, query string, _ ...any) (sql.Result, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.execs = append(c.execs, query)
	return oneRow{}, nil
}

type oneRow struct{}

func (oneRow) LastInsertId() (int64, error) { return 0, nil }
func (oneRow) RowsAffected() (int64, error) { return 1, nil }

// TestSourceStateRecorderIsWiredOnlyForVulnerabilityIntelligence proves the
// #9420 wiring: the host owns vulnerability_source_states only for the
// vulnerability_intelligence kind, through the vulnerability store, and every
// other host gets no recorder at all.
func TestSourceStateRecorderIsWiredOnlyForVulnerabilityIntelligence(t *testing.T) {
	t.Parallel()

	if got, err := sourceStateRecorder(scope.CollectorKind("scorecard"), &countingExecQueryer{}, "scorecard-local", time.Second, nil, nil); err != nil || got != nil {
		t.Fatalf("sourceStateRecorder(scorecard) = (%v, %v), want (nil, nil)", got, err)
	}

	database := &countingExecQueryer{}
	recorder, err := sourceStateRecorder(scope.CollectorVulnerabilityIntelligence, database, "vuln-intel-cx", time.Second, nil, nil)
	if err != nil || recorder == nil {
		t.Fatalf("sourceStateRecorder(vulnerability_intelligence) = (%v, %v), want a recorder", recorder, err)
	}
	outcome := extensionhost.ClaimOutcome{
		Item: workflow.WorkItem{
			WorkItemID: "work-1", GenerationID: "generation-1", CollectorKind: scope.CollectorVulnerabilityIntelligence,
			CollectorInstanceID: "vuln-intel-cx", ScopeID: "vuln-intel://cisa/kev", AttemptCount: 1,
		},
		State:       sdkcollector.ResultComplete,
		AttemptedAt: time.Now(),
		ObservedAt:  time.Now(),
	}
	if err := recorder.RecordClaimOutcome(context.Background(), outcome); err != nil {
		t.Fatalf("RecordClaimOutcome() error = %v", err)
	}
	if err := recorder.RecordClaimCommitted(context.Background(), outcome); err != nil {
		t.Fatalf("RecordClaimCommitted() error = %v", err)
	}
	// Performance contract (ruling 1.7): at most two single-row statements
	// per claim, the same as the retired in-tree path.
	if len(database.execs) != 2 {
		t.Fatalf("statements = %d, want 2 for one completed claim", len(database.execs))
	}
	for _, query := range database.execs {
		if !strings.Contains(query, "INSERT INTO vulnerability_source_states") {
			t.Fatalf("statement is not the source-state upsert:\n%s", query)
		}
	}
}
