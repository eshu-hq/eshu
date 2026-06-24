package postgres

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/relationships"
)

// TestUpsertEvidenceFactsBatchesInserts is the #3704 write-throughput gate. The
// pre-#3704 path issued ONE INSERT round-trip per evidence fact; at corpus scale
// that per-row round-trip is the serial client-side cost that made the backfill
// the long pole. UpsertEvidenceFacts must now group facts into multi-row INSERT
// statements (evidenceInsertBatchRows per statement), so the number of
// ExecContext calls is ceil(N/batchRows), not N.
func TestUpsertEvidenceFactsBatchesInserts(t *testing.T) {
	t.Parallel()

	const factCount = evidenceInsertBatchRows + 7 // forces two batches
	facts := make([]relationships.EvidenceFact, 0, factCount)
	for i := 0; i < factCount; i++ {
		facts = append(facts, relationships.EvidenceFact{
			EvidenceKind:     relationships.EvidenceKind("terraform_module"),
			RelationshipType: relationships.RelationshipType("depends_on"),
			SourceRepoID:     "repo-source",
			TargetRepoID:     "repo-target-" + string(rune('a'+i%26)) + itoa(i),
			Confidence:       0.9,
			Rationale:        "module reference",
		})
	}

	fake := &fakeExecQueryer{}
	store := NewRelationshipStore(fake)
	if err := store.UpsertEvidenceFacts(context.Background(), "gen-1", facts); err != nil {
		t.Fatalf("UpsertEvidenceFacts() error = %v, want nil", err)
	}

	wantBatches := (factCount + evidenceInsertBatchRows - 1) / evidenceInsertBatchRows
	if len(fake.execs) != wantBatches {
		t.Fatalf("UpsertEvidenceFacts issued %d ExecContext calls, want %d batched calls for %d facts (batch rows = %d)",
			len(fake.execs), wantBatches, factCount, evidenceInsertBatchRows)
	}

	// Correctness: every batch statement is an ON CONFLICT DO NOTHING insert into
	// relationship_evidence_facts, and the total bound parameters equal
	// factCount * evidenceInsertColumns (no row dropped).
	totalArgs := 0
	for _, call := range fake.execs {
		if !strings.Contains(call.query, "INSERT INTO relationship_evidence_facts") {
			t.Fatalf("batched statement is not an evidence insert:\n%s", call.query)
		}
		if !strings.Contains(call.query, "ON CONFLICT (evidence_id) DO NOTHING") {
			t.Fatalf("batched evidence insert lost ON CONFLICT idempotency:\n%s", call.query)
		}
		totalArgs += len(call.args)
	}
	if totalArgs != factCount*evidenceInsertColumns {
		t.Fatalf("batched inserts bound %d args, want %d (%d facts x %d columns)",
			totalArgs, factCount*evidenceInsertColumns, factCount, evidenceInsertColumns)
	}
}

// itoa is a tiny dependency-free int-to-string for test fixture uniqueness.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
