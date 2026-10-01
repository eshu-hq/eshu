// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// TestServiceStoryTargetSupportReadsOnOneSnapshot guards the #7463 review
// finding: the link read, the PagerDuty routing read and the source-only summary
// each filter on the active generation, so they must share one read-only
// repeatable-read snapshot. A generation activated between autocommit reads
// could otherwise put rows of two generations in one section.
func TestServiceStoryTargetSupportReadsOnOneSnapshot(t *testing.T) {
	t.Parallel()

	sqlDB := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns:       []string{"payload"},
			rows:          [][]driver.Value{},
			queryContains: []string{"'work_item.external_link'", "linked_repository_id"},
		},
		{
			columns:       []string{"payload"},
			rows:          [][]driver.Value{},
			queryContains: []string{"reducer_incident_repository_correlation", "correlated.provider_service_id"},
		},
		{
			columns:       []string{"support_source_only_count", "work_item_source_only_count", "incident_routing_source_only_count"},
			rows:          [][]driver.Value{{int64(3), int64(2), int64(1)}},
			queryContains: []string{"COUNT(*) AS support_source_only_count"},
		},
	})
	guard := &countedReadStore{ReadStore: postgres.NewSQLReadStore(sqlDB)}
	reader := NewContentReaderWithReadStore(guard)

	model, err := reader.ServiceStoryTargetSupportEvidence(context.Background(), serviceStoryTargetSupportFilter{
		Repository: "repo-x", TargetKind: "repository", TargetID: "repo-x", Limit: 10,
	})
	if err != nil {
		t.Fatalf("ServiceStoryTargetSupportEvidence() error = %v", err)
	}
	coverage := mapValue(model.Support, "coverage")
	if got := IntVal(coverage, "source_only_count"); got != 3 {
		t.Fatalf("coverage.source_only_count = %d, want 3 from the summary read; support = %#v", got, model.Support)
	}
	if guard.snapshots != 1 {
		t.Fatalf("snapshots = %d, want one shared by every read", guard.snapshots)
	}
	if guard.rows != 0 || guard.singles != 0 {
		t.Fatalf("autocommit reads on the store = rows %d, singles %d, want 0: every statement must run on the snapshot", guard.rows, guard.singles)
	}
}

// failingSnapshotStore refuses to open a snapshot, so a begin error must reach
// the caller instead of falling back to autocommit reads.
type failingSnapshotStore struct {
	db.ReadStore
}

func (failingSnapshotStore) BeginReadOnlySnapshot(context.Context) (db.ReadTransaction, error) {
	return nil, errors.New("begin boom")
}

func TestServiceStoryTargetSupportSurfacesASnapshotBeginError(t *testing.T) {
	t.Parallel()

	sqlDB := openContentReaderTestDB(t, nil)
	reader := NewContentReaderWithReadStore(failingSnapshotStore{ReadStore: postgres.NewSQLReadStore(sqlDB)})

	_, err := reader.ServiceStoryTargetSupportEvidence(context.Background(), serviceStoryTargetSupportFilter{
		Repository: "repo-x", TargetKind: "repository", TargetID: "repo-x", Limit: 10,
	})
	if err == nil || !strings.Contains(err.Error(), "begin boom") {
		t.Fatalf("ServiceStoryTargetSupportEvidence() error = %v, want the snapshot begin error", err)
	}
}

// A closed gate makes no round trip at all: no snapshot, no read.
func TestServiceStoryTargetSupportClosedGateOpensNoSnapshot(t *testing.T) {
	t.Parallel()

	sqlDB := openContentReaderTestDB(t, nil)
	guard := &countedReadStore{ReadStore: postgres.NewSQLReadStore(sqlDB)}
	reader := NewContentReaderWithReadStore(guard)

	model, err := reader.ServiceStoryTargetSupportEvidence(context.Background(), serviceStoryTargetSupportFilter{})
	if err != nil {
		t.Fatalf("ServiceStoryTargetSupportEvidence() error = %v", err)
	}
	if model.Support != nil || guard.snapshots != 0 || guard.rows != 0 {
		t.Fatalf("closed gate = support %v, snapshots %d, rows %d, want none", model.Support, guard.snapshots, guard.rows)
	}
}
