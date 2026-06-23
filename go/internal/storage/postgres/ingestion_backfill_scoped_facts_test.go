package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/lib/pq"
)

// contentFactRow builds the 16-column fact_records projection row that
// scanFactEnvelope expects, with the supplied payload JSON bytes. The column
// order mirrors listOnboardedRepoScopedRelationshipFactRecordsQuery and
// listLatestRelationshipFactRecordsQuery so the scoped loader can reuse the
// shared scanner.
func contentFactRow(factID, scopeID, generationID, factKind string, payload string) []any {
	return []any{
		factID,                // fact_id
		scopeID,               // scope_id
		generationID,          // generation_id
		factKind,              // fact_kind
		"stable-" + factID,    // stable_fact_key
		"v1",                  // schema_version
		"git",                 // collector_kind
		int64(1),              // fencing_token
		"high",                // source_confidence
		"git",                 // source_system
		"src-" + factID,       // source_fact_key
		"",                    // source_uri
		"",                    // source_record_id
		time.Unix(0, 0).UTC(), // observed_at
		false,                 // is_tombstone
		[]byte(payload),       // payload
	}
}

func TestLoadOnboardedRepoScopedRelationshipFactsPassesAnchorsAndScans(t *testing.T) {
	t.Parallel()

	payload := `{"content_path":"main.tf","content_body":"module \"x\" { source = \"./payments-service\" }"}`
	queryer := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{contentFactRow("fact-1", "scope-1", "gen-1", "content", payload)}},
		},
	}

	anchors := []string{"payments-service"}
	loaded, err := loadOnboardedRepoScopedRelationshipFacts(context.Background(), queryer, anchors)
	if err != nil {
		t.Fatalf("loadOnboardedRepoScopedRelationshipFacts returned error: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("loaded %d facts, want 1", len(loaded))
	}
	if loaded[0].FactKind != "content" {
		t.Fatalf("loaded fact kind = %q, want content", loaded[0].FactKind)
	}

	if len(queryer.queries) != 1 {
		t.Fatalf("issued %d queries, want 1", len(queryer.queries))
	}
	call := queryer.queries[0]
	if call.query != listOnboardedRepoScopedRelationshipFactRecordsQuery {
		t.Fatalf("unexpected query:\n%s", call.query)
	}
	if len(call.args) != 1 {
		t.Fatalf("query args = %d, want 1", len(call.args))
	}
	likeArgs, ok := call.args[0].(pq.StringArray)
	if !ok {
		t.Fatalf("query arg type = %T, want pq.StringArray", call.args[0])
	}
	if len(likeArgs) != 1 || likeArgs[0] != "%payments-service%" {
		t.Fatalf("LIKE args = %v, want [%%payments-service%%]", []string(likeArgs))
	}
}

func TestLoadOnboardedRepoScopedRelationshipFactsEmptyAnchorsShortCircuits(t *testing.T) {
	t.Parallel()

	queryer := &fakeExecQueryer{}
	loaded, err := loadOnboardedRepoScopedRelationshipFacts(context.Background(), queryer, nil)
	if err != nil {
		t.Fatalf("expected nil error for empty anchors, got %v", err)
	}
	if loaded != nil {
		t.Fatalf("expected nil facts for empty anchors, got %v", loaded)
	}
	if len(queryer.queries) != 0 {
		t.Fatalf("expected no queries for empty anchors, got %d", len(queryer.queries))
	}
}

func TestScopedRelationshipFactQueryShape(t *testing.T) {
	t.Parallel()

	query := listOnboardedRepoScopedRelationshipFactRecordsQuery
	for _, want := range []string{
		"fact.fact_kind IN ('content', 'file', 'gcp_cloud_relationship')",
		"lower(fact.payload::text) LIKE ANY($1)",
		"latest_generations",
		"latest.generation_id IS NOT NULL",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("scoped query missing %q", want)
		}
	}
}

func TestBuildPayloadAnchorLikeTermsEscapesWildcards(t *testing.T) {
	t.Parallel()

	got := buildPayloadAnchorLikeTerms([]string{`a_b%c\d`, "plain"})
	want := []string{`%a\_b\%c\\d%`, "%plain%"}
	if len(got) != len(want) {
		t.Fatalf("terms = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("term[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
