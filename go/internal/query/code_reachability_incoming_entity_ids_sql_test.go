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

// TestCodeReachabilityIncomingEntityIDsBindsTheConsumerGrant moved out of
// codequery/auth_scoped_code_dead_code_hidden_consumer_test.go at the #6060
// CodeHandler move: it drives the real root *ContentReader over a recording
// SQL fake, which cannot move to codequery without recreating ContentReader
// there.
func TestCodeReachabilityIncomingEntityIDsBindsTheConsumerGrant(t *testing.T) {
	t.Parallel()

	t.Run("scoped", func(t *testing.T) {
		t.Parallel()

		db, recorder := openRecordingContentReaderDB(t, []recordingContentReaderQueryResult{
			{
				columns: []string{"entity_id", "min_resolution_method", "consumer_in_grant"},
				rows: [][]driver.Value{
					{"content-entity:library-symbol", codeprovenance.MethodImportBinding, false},
				},
			},
		})
		reader := NewContentReader(db)
		incoming, err := reader.CodeReachabilityIncomingEntityIDs(
			context.Background(),
			"repository:library",
			[]string{"content-entity:library-symbol"},
			[]string{codeGrantGrantedRepo},
		)
		if err != nil {
			t.Fatalf("CodeReachabilityIncomingEntityIDs() error = %v, want nil", err)
		}
		edge := incoming["content-entity:library-symbol"]
		if !edge.HiddenConsumer {
			t.Fatalf("edge = %#v, want the out-of-grant consumer reported as hidden", edge)
		}
		if edge.MaxConfidence != 0 {
			t.Fatalf("MaxConfidence = %v, want 0: an edge the caller cannot see is not evidence", edge.MaxConfidence)
		}
		want := "(row.repository_id = ANY($2)) AS consumer_in_grant"
		if !strings.Contains(recorder.queries[0], want) {
			t.Fatalf("reachability SQL is missing %q, so an ungranted consumer still reads as evidence:\n%s", want, recorder.queries[0])
		}
	})

	t.Run("unscoped", func(t *testing.T) {
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
		if _, err := reader.CodeReachabilityIncomingEntityIDs(
			context.Background(),
			"repository:library",
			[]string{"content-entity:library-symbol"},
			nil,
		); err != nil {
			t.Fatalf("CodeReachabilityIncomingEntityIDs() error = %v, want nil", err)
		}
		if strings.Contains(recorder.queries[0], "consumer_in_grant") {
			t.Fatalf("unscoped reachability SQL gained a grant column:\n%s", recorder.queries[0])
		}
	})

	// A weak granted edge beside an ungranted one merges to a hidden-consumer
	// edge that keeps the granted method and its confidence. codequery's
	// deadCodeWeakGrantedPlusUngrantedFromSQL hand-builds this same map for
	// the backend-parity proof there (it cannot drive the real ContentReader
	// across the package boundary); this subtest pins the SQL half of that
	// contract so the two literals cannot drift apart silently.
	t.Run("weak granted edge beside an ungranted one", func(t *testing.T) {
		t.Parallel()

		db, _ := openRecordingContentReaderDB(t, []recordingContentReaderQueryResult{{
			columns: []string{"entity_id", "min_resolution_method", "consumer_in_grant"},
			rows: [][]driver.Value{
				{"hidden-consumer-1", codeprovenance.MethodRepoUniqueName, true},
				{"hidden-consumer-1", codeprovenance.MethodImportBinding, false},
			},
		}})
		reader := NewContentReader(db)
		incoming, err := reader.CodeReachabilityIncomingEntityIDs(
			context.Background(),
			codeGrantGrantedRepo,
			[]string{"hidden-consumer-1"},
			[]string{codeGrantGrantedRepo},
		)
		if err != nil {
			t.Fatalf("CodeReachabilityIncomingEntityIDs() error = %v, want nil", err)
		}
		if got, want := len(incoming), 1; got != want {
			t.Fatalf("len(incoming) = %d, want %d: both rows describe the same entity", got, want)
		}
		edge := incoming["hidden-consumer-1"]
		if !edge.HiddenConsumer {
			t.Fatalf("edge = %#v, want the ungranted row reported as hidden", edge)
		}
		if got, want := edge.MaxConfidence, codeprovenance.Confidence(codeprovenance.MethodRepoUniqueName); got != want {
			t.Fatalf("edge.MaxConfidence = %v, want %v: the granted edge keeps its confidence", got, want)
		}
		if got, want := edge.Method, string(codeprovenance.MethodRepoUniqueName); got != want {
			t.Fatalf("edge.Method = %q, want %q: the granted edge keeps its method", got, want)
		}
	})
}
