// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql/driver"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
)

func TestContentReaderDeadCodeIncomingDerivesConfidenceFromResolutionMethod(t *testing.T) {
	t.Parallel()

	db, recorder := openRecordingContentReaderDB(t, []recordingContentReaderQueryResult{
		{
			columns: []string{"incoming_entity_id", "resolution_method"},
			rows: [][]driver.Value{
				{"content-entity:weak", codeprovenance.MethodRepoUniqueName},
				{"content-entity:strong", codeprovenance.MethodSCIP},
				{"content-entity:legacy", ""},
			},
		},
	})

	reader := NewContentReader(db)
	incoming, err := reader.DeadCodeIncomingEntityIDs(
		context.Background(),
		"repository:r_payments",
		[]string{"content-entity:weak", "content-entity:strong", "content-entity:legacy"},
	)
	if err != nil {
		t.Fatalf("DeadCodeIncomingEntityIDs() error = %v, want nil", err)
	}
	if got, want := incoming["content-entity:weak"].MaxConfidence, codeprovenance.Confidence(codeprovenance.MethodRepoUniqueName); got != want {
		t.Fatalf("weak MaxConfidence = %v, want %v", got, want)
	}
	if !deadCodeIncomingEdgeIsWeak(incoming["content-entity:weak"].MaxConfidence) {
		t.Fatalf("weak edge classified strong: %#v", incoming["content-entity:weak"])
	}
	if deadCodeIncomingEdgeIsWeak(incoming["content-entity:strong"].MaxConfidence) {
		t.Fatalf("strong edge classified weak: %#v", incoming["content-entity:strong"])
	}
	if deadCodeIncomingEdgeIsWeak(incoming["content-entity:legacy"].MaxConfidence) {
		t.Fatalf("legacy (empty method) edge classified weak: %#v", incoming["content-entity:legacy"])
	}
	if got, want := len(recorder.queries), 1; got != want {
		t.Fatalf("len(recorder.queries) = %d, want %d", got, want)
	}
	if !containsAllSubstrings(recorder.queries[0], "resolution_method") {
		t.Fatalf("query missing resolution_method projection:\n%s", recorder.queries[0])
	}
}

func TestContentReaderDeadCodeIncomingKeepsMaxConfidencePerEntity(t *testing.T) {
	t.Parallel()

	db, _ := openRecordingContentReaderDB(t, []recordingContentReaderQueryResult{
		{
			columns: []string{"incoming_entity_id", "resolution_method"},
			rows: [][]driver.Value{
				{"content-entity:mixed", codeprovenance.MethodRepoUniqueName},
				{"content-entity:mixed", codeprovenance.MethodImportBinding},
			},
		},
	})

	reader := NewContentReader(db)
	incoming, err := reader.DeadCodeIncomingEntityIDs(
		context.Background(),
		"repository:r_payments",
		[]string{"content-entity:mixed"},
	)
	if err != nil {
		t.Fatalf("DeadCodeIncomingEntityIDs() error = %v, want nil", err)
	}
	if got, want := incoming["content-entity:mixed"].MaxConfidence, codeprovenance.Confidence(codeprovenance.MethodImportBinding); got != want {
		t.Fatalf("mixed MaxConfidence = %v, want %v (strongest wins)", got, want)
	}
	if deadCodeIncomingEdgeIsWeak(incoming["content-entity:mixed"].MaxConfidence) {
		t.Fatalf("mixed edge classified weak despite strong import_binding edge")
	}
}

func TestContentReaderCodeReachabilityIncomingEntityIDsUsesCrossRepoRows(t *testing.T) {
	t.Parallel()

	db, recorder := openRecordingContentReaderDB(t, []recordingContentReaderQueryResult{
		{
			columns: []string{"entity_id", "min_resolution_method"},
			rows: [][]driver.Value{
				{"content-entity:library-symbol", codeprovenance.MethodImportBinding},
			},
		},
	})

	reader := NewContentReader(db)
	incoming, err := reader.CodeReachabilityIncomingEntityIDs(
		context.Background(),
		"repository:library",
		[]string{"content-entity:library-symbol"},
	)
	if err != nil {
		t.Fatalf("CodeReachabilityIncomingEntityIDs() error = %v, want nil", err)
	}
	if got, want := incoming["content-entity:library-symbol"].MaxConfidence, codeprovenance.Confidence(codeprovenance.MethodImportBinding); got != want {
		t.Fatalf("cross-repo MaxConfidence = %v, want %v", got, want)
	}
	if got, want := len(recorder.queries), 1; got != want {
		t.Fatalf("len(recorder.queries) = %d, want %d", got, want)
	}
	query := recorder.queries[0]
	if strings.Contains(query, "row.repository_id =") {
		t.Fatalf("query is repo-scoped and misses cross-repo reachability rows:\n%s", query)
	}
	if !containsAllSubstrings(
		query,
		"FROM code_reachability_rows AS row",
		"scope.active_generation_id = row.generation_id",
		"generation.status = 'active'",
		"row.entity_id IN ($1)",
		"row.depth > 0",
	) {
		t.Fatalf("query missing active-generation entity lookup clauses:\n%s", query)
	}
	if got, want := len(recorder.args[0]), 1; got != want {
		t.Fatalf("len(args) = %d, want %d for bounded entity lookup args %#v", got, want, recorder.args[0])
	}
	if got, want := recorder.args[0][0], driver.Value("content-entity:library-symbol"); got != want {
		t.Fatalf("first arg = %#v, want %#v", got, want)
	}
}
