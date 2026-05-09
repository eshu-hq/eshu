package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

func TestFactStoreUpsertFactsPersistsPayload(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	store := NewFactStore(db)

	envelope := facts.Envelope{
		FactID:        "fact-1",
		ScopeID:       "scope-123",
		GenerationID:  "generation-456",
		FactKind:      "repository",
		StableFactKey: "repository:scope-123",
		ObservedAt:    time.Date(2026, time.April, 12, 8, 0, 0, 0, time.UTC),
		Payload:       map[string]any{"name": "eshu"},
		SourceRef: facts.Ref{
			SourceSystem:   "git",
			FactKey:        "fact-key",
			SourceURI:      "file:///repo/path",
			SourceRecordID: "record-123",
		},
	}

	if err := store.UpsertFacts(context.Background(), []facts.Envelope{envelope}); err != nil {
		t.Fatalf("UpsertFacts() error = %v, want nil", err)
	}

	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
	if !strings.Contains(db.execs[0].query, "INSERT INTO fact_records") {
		t.Fatalf("query = %q, want fact_records insert", db.execs[0].query)
	}
	payload, ok := db.execs[0].args[16].([]byte)
	if !ok || !strings.Contains(string(payload), "eshu") {
		t.Fatalf("payload arg = %#v, want json payload", db.execs[0].args[16])
	}
}

func TestFactStoreUpsertFactsPersistsCollectorContractFields(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	store := NewFactStore(db)

	envelope := facts.Envelope{
		FactID:           "fact-1",
		ScopeID:          "scope-123",
		GenerationID:     "generation-456",
		FactKind:         "terraform_state_resource",
		StableFactKey:    "terraform_state_resource:aws_instance.app",
		SchemaVersion:    "1.0.0",
		CollectorKind:    "terraform_state",
		FencingToken:     42,
		SourceConfidence: facts.SourceConfidenceObserved,
		ObservedAt:       time.Date(2026, time.May, 9, 9, 0, 0, 0, time.UTC),
		SourceRef: facts.Ref{
			SourceSystem: "terraform_state",
			FactKey:      "aws_instance.app",
		},
	}

	if err := store.UpsertFacts(context.Background(), []facts.Envelope{envelope}); err != nil {
		t.Fatalf("UpsertFacts() error = %v, want nil", err)
	}

	if got, want := len(db.execs[0].args), columnsPerFactRow; got != want {
		t.Fatalf("arg count = %d, want %d", got, want)
	}
	if got, want := db.execs[0].args[5], "1.0.0"; got != want {
		t.Fatalf("schema_version arg = %q, want %q", got, want)
	}
	if got, want := db.execs[0].args[6], "terraform_state"; got != want {
		t.Fatalf("collector_kind arg = %q, want %q", got, want)
	}
	if got, want := db.execs[0].args[7], int64(42); got != want {
		t.Fatalf("fencing_token arg = %v, want %v", got, want)
	}
	if got, want := db.execs[0].args[8], facts.SourceConfidenceObserved; got != want {
		t.Fatalf("source_confidence arg = %q, want %q", got, want)
	}
}

func TestFactStoreLoadFactsReturnsEnvelope(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{
				rows: [][]any{{
					"fact-1",
					"scope-123",
					"generation-456",
					"repository",
					"repository:scope-123",
					"1.0.0",
					"git",
					int64(7),
					facts.SourceConfidenceObserved,
					"git",
					"fact-key",
					"file:///repo/path",
					"record-123",
					time.Date(2026, time.April, 12, 8, 0, 0, 0, time.UTC),
					false,
					[]byte(`{"name":"eshu"}`),
				}},
			},
		},
	}
	store := NewFactStore(db)

	work := projector.ScopeGenerationWork{
		Scope: scope.IngestionScope{ScopeID: "scope-123"},
		Generation: scope.ScopeGeneration{
			GenerationID: "generation-456",
		},
	}

	loaded, err := store.LoadFacts(context.Background(), work)
	if err != nil {
		t.Fatalf("LoadFacts() error = %v, want nil", err)
	}
	if got, want := len(loaded), 1; got != want {
		t.Fatalf("LoadFacts() len = %d, want %d", got, want)
	}
	if got, want := loaded[0].SourceRef.SourceSystem, "git"; got != want {
		t.Fatalf("LoadFacts()[0].SourceRef.SourceSystem = %q, want %q", got, want)
	}
	if got, want := loaded[0].SchemaVersion, "1.0.0"; got != want {
		t.Fatalf("LoadFacts()[0].SchemaVersion = %q, want %q", got, want)
	}
	if got, want := loaded[0].CollectorKind, "git"; got != want {
		t.Fatalf("LoadFacts()[0].CollectorKind = %q, want %q", got, want)
	}
	if got, want := loaded[0].FencingToken, int64(7); got != want {
		t.Fatalf("LoadFacts()[0].FencingToken = %d, want %d", got, want)
	}
	if got, want := loaded[0].SourceConfidence, facts.SourceConfidenceObserved; got != want {
		t.Fatalf("LoadFacts()[0].SourceConfidence = %q, want %q", got, want)
	}
	if got, want := loaded[0].Payload["name"], "eshu"; got != want {
		t.Fatalf("LoadFacts()[0].Payload[name] = %v, want %v", got, want)
	}
}

func TestUpsertFactsBatchesLargeEnvelopes(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}

	// Create factBatchSize + 1 envelopes to force exactly 2 batches.
	envelopes := make([]facts.Envelope, factBatchSize+1)
	for i := range envelopes {
		envelopes[i] = facts.Envelope{
			FactID:        fmt.Sprintf("fact-%d", i),
			ScopeID:       "scope-123",
			GenerationID:  "generation-456",
			FactKind:      "file",
			StableFactKey: fmt.Sprintf("file:fact-%d", i),
			ObservedAt:    time.Date(2026, time.April, 14, 0, 0, 0, 0, time.UTC),
			SourceRef: facts.Ref{
				SourceSystem: "git",
				FactKey:      fmt.Sprintf("key-%d", i),
			},
		}
	}

	if err := upsertFacts(context.Background(), db, envelopes); err != nil {
		t.Fatalf("upsertFacts() error = %v, want nil", err)
	}

	if got, want := len(db.execs), 2; got != want {
		t.Fatalf("exec count = %d, want %d (two batches)", got, want)
	}
	// First batch should have factBatchSize * columnsPerFactRow args.
	if got, want := len(db.execs[0].args), factBatchSize*columnsPerFactRow; got != want {
		t.Fatalf("first batch arg count = %d, want %d", got, want)
	}
	// Second batch should have 1 * columnsPerFactRow args.
	if got, want := len(db.execs[1].args), 1*columnsPerFactRow; got != want {
		t.Fatalf("second batch arg count = %d, want %d", got, want)
	}
	// Both queries should be multi-row inserts.
	for i, exec := range db.execs {
		if !strings.Contains(exec.query, "INSERT INTO fact_records") {
			t.Fatalf("exec[%d] query missing INSERT INTO fact_records", i)
		}
		if !strings.Contains(exec.query, "ON CONFLICT") {
			t.Fatalf("exec[%d] query missing ON CONFLICT upsert clause", i)
		}
	}
}

func TestUpsertFactsDeduplicatesByFactID(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}

	envelopes := []facts.Envelope{
		{
			FactID:        "dup-1",
			ScopeID:       "scope-123",
			GenerationID:  "generation-456",
			FactKind:      "file",
			StableFactKey: "file:old",
			ObservedAt:    time.Date(2026, time.April, 14, 0, 0, 0, 0, time.UTC),
			Payload:       map[string]any{"version": "old"},
			SourceRef:     facts.Ref{SourceSystem: "git", FactKey: "key-old"},
		},
		{
			FactID:        "unique-1",
			ScopeID:       "scope-123",
			GenerationID:  "generation-456",
			FactKind:      "file",
			StableFactKey: "file:unique",
			ObservedAt:    time.Date(2026, time.April, 14, 0, 0, 0, 0, time.UTC),
			SourceRef:     facts.Ref{SourceSystem: "git", FactKey: "key-unique"},
		},
		{
			FactID:        "dup-1",
			ScopeID:       "scope-123",
			GenerationID:  "generation-456",
			FactKind:      "file",
			StableFactKey: "file:new",
			ObservedAt:    time.Date(2026, time.April, 14, 0, 0, 0, 0, time.UTC),
			Payload:       map[string]any{"version": "new"},
			SourceRef:     facts.Ref{SourceSystem: "git", FactKey: "key-new"},
		},
	}

	if err := upsertFacts(context.Background(), db, envelopes); err != nil {
		t.Fatalf("upsertFacts() error = %v, want nil", err)
	}

	// Should produce 1 exec with 2 rows (dup-1 deduplicated, last wins).
	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
	if got, want := len(db.execs[0].args), 2*columnsPerFactRow; got != want {
		t.Fatalf("arg count = %d, want %d (2 deduplicated rows)", got, want)
	}
	// First row should be unique-1, second should be dup-1 with "new" payload.
	if got, want := db.execs[0].args[0].(string), "unique-1"; got != want {
		t.Fatalf("first row fact_id = %q, want %q", got, want)
	}
	if got, want := db.execs[0].args[columnsPerFactRow].(string), "dup-1"; got != want {
		t.Fatalf("second row fact_id = %q, want %q", got, want)
	}
	// Verify "new" payload won (last occurrence).
	payload := db.execs[0].args[2*columnsPerFactRow-1].([]byte)
	if !strings.Contains(string(payload), "new") {
		t.Fatalf("deduped payload = %s, want 'new' version", payload)
	}
}

func TestUpsertFactsEmptySliceNoOp(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	if err := upsertFacts(context.Background(), db, nil); err != nil {
		t.Fatalf("upsertFacts(nil) error = %v, want nil", err)
	}
	if got := len(db.execs); got != 0 {
		t.Fatalf("exec count = %d, want 0 for empty envelopes", got)
	}
}

func TestUpsertFactsRejectsNonSemanticSchemaVersion(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	envelope := facts.Envelope{
		FactID:        "fact-1",
		ScopeID:       "scope-123",
		GenerationID:  "generation-456",
		FactKind:      "terraform_state_resource",
		StableFactKey: "terraform_state_resource:aws_instance.app",
		SchemaVersion: "terraform_state_resource.v1",
		ObservedAt:    time.Date(2026, time.May, 9, 9, 0, 0, 0, time.UTC),
		SourceRef: facts.Ref{
			SourceSystem: "terraform_state",
			FactKey:      "aws_instance.app",
		},
	}

	err := upsertFacts(context.Background(), db, []facts.Envelope{envelope})
	if err == nil {
		t.Fatal("upsertFacts() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "schema_version") {
		t.Fatalf("upsertFacts() error = %v, want schema_version context", err)
	}
}

func TestUpsertFactsRejectsUnsupportedSourceConfidence(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	envelope := facts.Envelope{
		FactID:           "fact-1",
		ScopeID:          "scope-123",
		GenerationID:     "generation-456",
		FactKind:         "terraform_state_resource",
		StableFactKey:    "terraform_state_resource:aws_instance.app",
		SchemaVersion:    "1.0.0",
		SourceConfidence: "exact",
		ObservedAt:       time.Date(2026, time.May, 9, 9, 0, 0, 0, time.UTC),
		SourceRef: facts.Ref{
			SourceSystem: "terraform_state",
			FactKey:      "aws_instance.app",
		},
	}

	err := upsertFacts(context.Background(), db, []facts.Envelope{envelope})
	if err == nil {
		t.Fatal("upsertFacts() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "source_confidence") {
		t.Fatalf("upsertFacts() error = %v, want source_confidence context", err)
	}
	if got := len(db.execs); got != 0 {
		t.Fatalf("exec count = %d, want 0", got)
	}
}

func TestMarshalPayloadSanitizesForPostgresJSONB(t *testing.T) {
	t.Parallel()

	t.Run("strips null unicode escapes", func(t *testing.T) {
		t.Parallel()
		payload := map[string]any{"content": "hello\u0000world"}
		data, err := marshalPayload(payload)
		if err != nil {
			t.Fatalf("marshalPayload() error = %v, want nil", err)
		}
		if strings.Contains(string(data), `\u0000`) {
			t.Fatalf("output contains \\u0000: %s", data)
		}
		if !strings.Contains(string(data), "hello") {
			t.Fatalf("missing content: %s", data)
		}
	})

	t.Run("strips raw control bytes", func(t *testing.T) {
		t.Parallel()
		// Embed raw control bytes via a pre-built map that json.Marshal will encode.
		// After marshal + sanitize, no raw control bytes should remain.
		payload := map[string]any{"content": "before\x01\x02\x03after"}
		data, err := marshalPayload(payload)
		if err != nil {
			t.Fatalf("marshalPayload() error = %v, want nil", err)
		}
		for _, b := range data {
			if b < 0x20 && b != '\t' && b != '\n' && b != '\r' {
				t.Fatalf("output contains raw control byte 0x%02x: %s", b, data)
			}
		}
	})

	t.Run("clean payload passes through unchanged", func(t *testing.T) {
		t.Parallel()
		payload := map[string]any{"name": "eshu"}
		data, err := marshalPayload(payload)
		if err != nil {
			t.Fatalf("marshalPayload() error = %v, want nil", err)
		}
		if !strings.Contains(string(data), "eshu") {
			t.Fatalf("missing content: %s", data)
		}
	})

	t.Run("empty payload returns empty object", func(t *testing.T) {
		t.Parallel()
		data, err := marshalPayload(nil)
		if err != nil {
			t.Fatalf("marshalPayload() error = %v, want nil", err)
		}
		if string(data) != "{}" {
			t.Fatalf("got %s, want {}", data)
		}
	})
}

func TestFactStoreListFactsPropagatesQueryErrors(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{{err: errors.New("boom")}},
	}
	store := NewFactStore(db)

	_, err := store.ListFacts(context.Background(), "scope-123", "generation-456")
	if err == nil {
		t.Fatal("ListFacts() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "list facts") {
		t.Fatalf("ListFacts() error = %q, want list facts context", err)
	}
}

func TestFactStoreCountFactsReturnsCount(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{{42}}},
		},
	}
	store := NewFactStore(db)

	count, err := store.CountFacts(context.Background(), "scope-123", "generation-456")
	if err != nil {
		t.Fatalf("CountFacts() error = %v, want nil", err)
	}
	if got, want := count, 42; got != want {
		t.Fatalf("CountFacts() = %d, want %d", got, want)
	}
	if got, want := len(db.queries), 1; got != want {
		t.Fatalf("query count = %d, want %d", got, want)
	}
	if !strings.Contains(db.queries[0].query, "COUNT(*)") {
		t.Fatalf("query = %q, want COUNT(*) query", db.queries[0].query)
	}
}

func TestFactStoreCountFactsPropagatesQueryErrors(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{{err: errors.New("boom")}},
	}
	store := NewFactStore(db)

	_, err := store.CountFacts(context.Background(), "scope-123", "generation-456")
	if err == nil {
		t.Fatal("CountFacts() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "count facts") {
		t.Fatalf("CountFacts() error = %q, want count facts context", err)
	}
}

func TestFactStoreCountFactsNilDB(t *testing.T) {
	t.Parallel()

	store := NewFactStore(nil)

	_, err := store.CountFacts(context.Background(), "scope-123", "generation-456")
	if err == nil {
		t.Fatal("CountFacts() error = nil, want non-nil for nil db")
	}
	if !strings.Contains(err.Error(), "database is required") {
		t.Fatalf("CountFacts() error = %q, want database is required", err)
	}
}
