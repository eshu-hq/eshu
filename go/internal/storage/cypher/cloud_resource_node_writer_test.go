package cypher

import (
	"context"
	"strings"
	"testing"
)

func cloudResourceRows(n int) []map[string]any {
	rows := make([]map[string]any, 0, n)
	for i := 0; i < n; i++ {
		rows = append(rows, map[string]any{
			"uid":                 "uid-" + string(rune('a'+i)),
			"arn":                 "arn:aws:ec2:us-east-1:111122223333:vpc/vpc-1",
			"resource_id":         "vpc-1",
			"resource_type":       "aws_ec2_vpc",
			"name":                "main",
			"state":               "available",
			"account_id":          "111122223333",
			"region":              "us-east-1",
			"service_kind":        "vpc",
			"correlation_anchors": []string{"vpc-1"},
			"source_fact_id":      "fact-1",
			"stable_fact_key":     "key-1",
			"source_system":       "aws",
			"source_record_id":    "rec-1",
			"source_confidence":   "reported",
			"collector_kind":      "aws",
		})
	}
	return rows
}

func TestCloudResourceNodeWriterEmptyRowsIsNoOp(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewCloudResourceNodeWriter(executor, 0)

	if err := writer.WriteCloudResourceNodes(context.Background(), nil, "reducer/aws-resources"); err != nil {
		t.Fatalf("WriteCloudResourceNodes returned error: %v", err)
	}
	if len(executor.calls) != 0 {
		t.Fatalf("len(calls) = %d, want 0 for empty rows", len(executor.calls))
	}
}

func TestCloudResourceNodeWriterMergesOnUID(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewCloudResourceNodeWriter(executor, 0)

	if err := writer.WriteCloudResourceNodes(context.Background(), cloudResourceRows(1), "reducer/aws-resources"); err != nil {
		t.Fatalf("WriteCloudResourceNodes returned error: %v", err)
	}
	if len(executor.calls) != 1 {
		t.Fatalf("len(calls) = %d, want 1", len(executor.calls))
	}
	cypher := executor.calls[0].Cypher
	if !strings.Contains(cypher, "UNWIND $rows AS row") {
		t.Fatalf("cypher missing UNWIND batch shape:\n%s", cypher)
	}
	if !strings.Contains(cypher, "MERGE (r:CloudResource {uid: row.uid})") {
		t.Fatalf("cypher must MERGE on uid identity only:\n%s", cypher)
	}
	if strings.Contains(cypher, "MERGE (r:CloudResource {uid: row.uid, name") {
		t.Fatalf("cypher must not MERGE on a wide mutable map:\n%s", cypher)
	}
	rows, ok := executor.calls[0].Parameters["rows"].([]map[string]any)
	if !ok {
		t.Fatalf("rows parameter type = %T, want []map[string]any", executor.calls[0].Parameters["rows"])
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
}

func TestCloudResourceNodeWriterBatchesRows(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewCloudResourceNodeWriter(executor, 2)

	if err := writer.WriteCloudResourceNodes(context.Background(), cloudResourceRows(5), "reducer/aws-resources"); err != nil {
		t.Fatalf("WriteCloudResourceNodes returned error: %v", err)
	}
	// 5 rows at batch size 2 -> 3 statements.
	if len(executor.calls) != 3 {
		t.Fatalf("len(calls) = %d, want 3 batched statements", len(executor.calls))
	}
}

func TestCloudResourceNodeWriterUsesGroupExecutorAtomically(t *testing.T) {
	t.Parallel()

	executor := &recordingGroupExecutor{}
	writer := NewCloudResourceNodeWriter(executor, 2)

	if err := writer.WriteCloudResourceNodes(context.Background(), cloudResourceRows(5), "reducer/aws-resources"); err != nil {
		t.Fatalf("WriteCloudResourceNodes returned error: %v", err)
	}
	if len(executor.groupCalls) != 1 {
		t.Fatalf("len(groupCalls) = %d, want 1 atomic group", len(executor.groupCalls))
	}
	if len(executor.groupCalls[0]) != 3 {
		t.Fatalf("group statement count = %d, want 3", len(executor.groupCalls[0]))
	}
}

func TestCloudResourceNodeWriterSatisfiesReducerInterface(t *testing.T) {
	t.Parallel()

	// Compile-time guarantee that the cypher writer satisfies the reducer-owned
	// consumer interface. Verified via the reducer wiring assignment below; this
	// test fails to compile if the method set drifts.
	var _ interface {
		WriteCloudResourceNodes(ctx context.Context, rows []map[string]any, evidenceSource string) error
	} = NewCloudResourceNodeWriter(&recordingExecutor{}, 0)
}
