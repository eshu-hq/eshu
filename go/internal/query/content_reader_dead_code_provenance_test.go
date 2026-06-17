package query

import (
	"context"
	"database/sql/driver"
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
