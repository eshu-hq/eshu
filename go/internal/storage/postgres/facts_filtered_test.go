package postgres

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestFactStoreListFactsByKindFiltersFactKinds(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{
				rows: [][]any{{
					"fact-1",
					"scope-123",
					"generation-456",
					"content_entity",
					"content_entity:repo-1:entity-1",
					"1.0.0",
					"git",
					int64(0),
					"unknown",
					"git",
					"fact-key",
					"file:///repo/path/main.go",
					"record-123",
					time.Date(2026, time.April, 28, 8, 0, 0, 0, time.UTC),
					false,
					[]byte(`{"repo_id":"repo-1","entity_id":"entity-1"}`),
				}},
			},
		},
	}
	store := NewFactStore(db)

	loaded, err := store.ListFactsByKind(
		context.Background(),
		"scope-123",
		"generation-456",
		[]string{"repository", "content_entity"},
	)
	if err != nil {
		t.Fatalf("ListFactsByKind() error = %v, want nil", err)
	}
	if got, want := len(loaded), 1; got != want {
		t.Fatalf("ListFactsByKind() len = %d, want %d", got, want)
	}
	if got, want := loaded[0].FactKind, "content_entity"; got != want {
		t.Fatalf("ListFactsByKind()[0].FactKind = %q, want %q", got, want)
	}
	if got, want := len(db.queries), 1; got != want {
		t.Fatalf("query count = %d, want %d", got, want)
	}
	query := db.queries[0].query
	if !strings.Contains(query, "fact_kind = ANY($3::text[])") {
		t.Fatalf("query = %q, want fact_kind ANY filter", query)
	}
	if !strings.Contains(query, "ORDER BY observed_at ASC, fact_id ASC") {
		t.Fatalf("query = %q, want stable fact ordering", query)
	}
	kinds, ok := db.queries[0].args[2].([]string)
	if !ok {
		t.Fatalf("third query arg type = %T, want []string", db.queries[0].args[2])
	}
	if got, want := strings.Join(kinds, ","), "repository,content_entity"; got != want {
		t.Fatalf("fact kind arg = %q, want %q", got, want)
	}
}

func TestFactStoreListFactsByKindUsesFactBatchSizedPages(t *testing.T) {
	t.Parallel()

	firstPage := makeFactRowsForListFactsByKind(factBatchSize, 0)
	secondPage := makeFactRowsForListFactsByKind(1, factBatchSize)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: firstPage},
			{rows: secondPage},
		},
	}
	store := NewFactStore(db)

	loaded, err := store.ListFactsByKind(
		context.Background(),
		"scope-123",
		"generation-456",
		[]string{"file"},
	)
	if err != nil {
		t.Fatalf("ListFactsByKind() error = %v, want nil", err)
	}
	if got, want := len(loaded), factBatchSize+1; got != want {
		t.Fatalf("ListFactsByKind() len = %d, want %d", got, want)
	}
	if got, want := len(db.queries), 2; got != want {
		t.Fatalf("query count = %d, want %d", got, want)
	}
	for _, call := range db.queries {
		if !strings.Contains(call.query, "LIMIT $6") {
			t.Fatalf("query missing page limit:\n%s", call.query)
		}
		if !strings.Contains(call.query, "(observed_at, fact_id) > ($4::timestamptz, $5::text)") {
			t.Fatalf("query missing stable cursor:\n%s", call.query)
		}
		if got, want := call.args[5], factBatchSize; got != want {
			t.Fatalf("page size arg = %v, want %d", got, want)
		}
	}
	if got, want := db.queries[0].args[3], any(nil); got != want {
		t.Fatalf("first page cursor timestamp = %v, want nil", got)
	}
	if got, want := db.queries[1].args[3], loaded[factBatchSize-1].ObservedAt; got != want {
		t.Fatalf("second page cursor timestamp = %v, want %v", got, want)
	}
	if got, want := db.queries[1].args[4], loaded[factBatchSize-1].FactID; got != want {
		t.Fatalf("second page cursor fact ID = %v, want %v", got, want)
	}
}

func TestFactStoreListFactsByKindAndPayloadValueFiltersTopLevelPayloadValues(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{
				rows: [][]any{{
					"fact-1",
					"scope-123",
					"generation-456",
					"content_entity",
					"content_entity:repo-1:entity-1",
					"git",
					"fact-key",
					"file:///repo/path/main.go",
					"record-123",
					time.Date(2026, time.April, 28, 8, 0, 0, 0, time.UTC),
					false,
					[]byte(`{"repo_id":"repo-1","entity_id":"entity-1","entity_type":"Class"}`),
				}},
			},
		},
	}
	store := NewFactStore(db)

	loaded, err := store.ListFactsByKindAndPayloadValue(
		context.Background(),
		"scope-123",
		"generation-456",
		"content_entity",
		"entity_type",
		[]string{"Class", "Function"},
	)
	if err != nil {
		t.Fatalf("ListFactsByKindAndPayloadValue() error = %v, want nil", err)
	}
	if got, want := len(loaded), 1; got != want {
		t.Fatalf("ListFactsByKindAndPayloadValue() len = %d, want %d", got, want)
	}
	query := db.queries[0].query
	if !strings.Contains(query, "fact_kind = $3") {
		t.Fatalf("query = %q, want single fact kind filter", query)
	}
	if !strings.Contains(query, "payload ->> $4 = ANY($5::text[])") {
		t.Fatalf("query = %q, want payload value allowlist", query)
	}
	if got, want := db.queries[0].args[3], "entity_type"; got != want {
		t.Fatalf("payload key arg = %v, want %q", got, want)
	}
	values, ok := db.queries[0].args[4].([]string)
	if !ok {
		t.Fatalf("payload values arg type = %T, want []string", db.queries[0].args[4])
	}
	if got, want := strings.Join(values, ","), "Class,Function"; got != want {
		t.Fatalf("payload values arg = %q, want %q", got, want)
	}
}

func makeFactRowsForListFactsByKind(count int, offset int) [][]any {
	rows := make([][]any, 0, count)
	base := time.Date(2026, time.April, 28, 8, 0, 0, 0, time.UTC)
	for i := 0; i < count; i++ {
		n := offset + i
		rows = append(rows, []any{
			"fact-" + time.Unix(int64(n), 0).UTC().Format("150405"),
			"scope-123",
			"generation-456",
			"file",
			"file:repo-1:path",
			"git",
			"fact-key",
			"file:///repo/path/main.go",
			"record-123",
			base.Add(time.Duration(n) * time.Second),
			false,
			[]byte(`{"repo_id":"repo-1","relative_path":"main.go"}`),
		})
	}
	return rows
}
