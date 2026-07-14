// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

func TestRepoDependencyProjectionRunnerReplaysAmbiguousCommittedWriteExactly(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 13, 12, 0, 0, 0, time.UTC)
	const (
		repoID    = "repository:r_repo_a"
		oldTarget = "repository:r_old"
		newTarget = "repository:r_target"
	)

	for _, tc := range []struct {
		name        string
		includeOld  bool
		initialEdge string
		wantRetract int
		wantMarked  int
	}{
		{name: "direct upsert", wantMarked: 1},
		{name: "retract then rewrite", includeOld: true, initialEdge: oldTarget, wantRetract: 2, wantMarked: 2},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			active := repoDependencyAmbiguousCommitIntent("active", repoID, newTarget, "run-new", "gen-new", now)
			rows := []SharedProjectionIntentRow{active}
			if tc.includeOld {
				stale := repoDependencyAmbiguousCommitIntent("stale", repoID, oldTarget, "run-old", "gen-old", now.Add(-time.Second))
				rows = append([]SharedProjectionIntentRow{stale}, rows...)
			}
			reader := &fakeRepoDependencyIntentStore{
				pendingByDomain:         append([]SharedProjectionIntentRow(nil), rows...),
				pendingByAcceptanceUnit: map[string][]SharedProjectionIntentRow{repoID: append([]SharedProjectionIntentRow(nil), rows...)},
				leaseGranted:            true,
			}
			writer := newCommitAmbiguityRepoDependencyWriter(repoID, tc.initialEdge)
			runner := RepoDependencyProjectionRunner{
				IntentReader:       reader,
				LeaseManager:       reader,
				AcceptanceUnitGate: reader,
				EdgeWriter:         writer,
				AcceptedGen: func(key SharedProjectionAcceptanceKey) (string, bool) {
					if key.AcceptanceUnitID != repoID {
						return "", false
					}
					if key.SourceRunID == "run-old" {
						return "gen-current", true
					}
					return "gen-new", true
				},
				Config: RepoDependencyProjectionRunnerConfig{PollInterval: time.Millisecond},
			}

			if _, err := runner.processOnce(context.Background(), now); err == nil {
				t.Fatal("first processOnce() error = nil, want lost commit response")
			}
			if got := len(reader.marked); got != 0 {
				t.Fatalf("marked after ambiguous commit = %d, want 0", got)
			}
			writer.assertExactState(t, repoID, newTarget, "resolved-new")

			if _, err := runner.processOnce(context.Background(), now.Add(time.Second)); err != nil {
				t.Fatalf("second processOnce() error = %v, want idempotent replay success", err)
			}
			if got := len(reader.marked); got != tc.wantMarked {
				t.Fatalf("marked intents = %d, want %d from one completion", got, tc.wantMarked)
			}
			if got, want := writer.writeAttempts, 2; got != want {
				t.Fatalf("write attempts = %d, want %d", got, want)
			}
			if got := writer.retractAttempts; got != tc.wantRetract {
				t.Fatalf("retract attempts = %d, want %d", got, tc.wantRetract)
			}
			writer.assertExactState(t, repoID, newTarget, "resolved-new")
		})
	}
}

func repoDependencyAmbiguousCommitIntent(
	intentID, repoID, targetID, sourceRunID, generationID string,
	createdAt time.Time,
) SharedProjectionIntentRow {
	return repoDependencyIntentRow(
		intentID, "scope-a", repoID, repoID, sourceRunID, generationID, createdAt,
		map[string]any{
			"repo_id":           repoID,
			"target_repo_id":    targetID,
			"relationship_type": "DEPENDS_ON",
			"evidence_source":   crossRepoEvidenceSource,
			"resolved_id":       "resolved-new",
		},
	)
}

type commitAmbiguityRepoDependencyWriter struct {
	edges           map[string]struct{}
	artifacts       map[string]struct{}
	writeAttempts   int
	retractAttempts int
}

func newCommitAmbiguityRepoDependencyWriter(repoID, initialTarget string) *commitAmbiguityRepoDependencyWriter {
	w := &commitAmbiguityRepoDependencyWriter{
		edges:     make(map[string]struct{}),
		artifacts: make(map[string]struct{}),
	}
	if initialTarget != "" {
		w.edges[repoID+"|"+initialTarget] = struct{}{}
		w.artifacts[repoID+"|resolved-old"] = struct{}{}
	}
	return w
}

func (w *commitAmbiguityRepoDependencyWriter) RetractEdges(
	_ context.Context, _ string, rows []SharedProjectionIntentRow, _ string,
) error {
	w.retractAttempts++
	for _, row := range rows {
		repoID := repoDependencyPayloadString(row, "repo_id")
		for key := range w.edges {
			if len(key) > len(repoID) && key[:len(repoID)+1] == repoID+"|" {
				delete(w.edges, key)
			}
		}
		for key := range w.artifacts {
			if len(key) > len(repoID) && key[:len(repoID)+1] == repoID+"|" {
				delete(w.artifacts, key)
			}
		}
	}
	return nil
}

func (w *commitAmbiguityRepoDependencyWriter) WriteEdges(
	_ context.Context, _ string, rows []SharedProjectionIntentRow, _ string,
) error {
	w.writeAttempts++
	for _, row := range rows {
		repoID := repoDependencyPayloadString(row, "repo_id")
		targetID := repoDependencyPayloadString(row, "target_repo_id")
		resolvedID := repoDependencyPayloadString(row, "resolved_id")
		w.edges[repoID+"|"+targetID] = struct{}{}
		w.artifacts[repoID+"|"+resolvedID] = struct{}{}
	}
	if w.writeAttempts == 1 {
		return &neo4jdriver.ConnectivityError{Inner: errors.New("Connection lost during commit: EOF")}
	}
	return nil
}

func (w *commitAmbiguityRepoDependencyWriter) assertExactState(
	t *testing.T, repoID, targetID, resolvedID string,
) {
	t.Helper()
	if len(w.edges) != 1 {
		t.Fatalf("edge count = %d, want 1: %v", len(w.edges), w.edges)
	}
	if _, ok := w.edges[repoID+"|"+targetID]; !ok {
		t.Fatalf("edge state = %v, want %s -> %s", w.edges, repoID, targetID)
	}
	if len(w.artifacts) != 1 {
		t.Fatalf("artifact count = %d, want 1: %v", len(w.artifacts), w.artifacts)
	}
	if _, ok := w.artifacts[repoID+"|"+resolvedID]; !ok {
		t.Fatalf("artifact state = %v, want deterministic %s", w.artifacts, resolvedID)
	}
}
