// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"sync"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// concurrentCloudResourceEdgeWriter records calls under a mutex so the race
// detector can prove that concurrent reducer workers projecting the same scope
// generation do not corrupt shared writer state. The edge MERGE identity is the
// real concurrency-safety device (idempotent on source/type/target); this test
// proves the Eshu-side handler has no data race when run with -race.
type concurrentCloudResourceEdgeWriter struct {
	mu          sync.Mutex
	writeCalls  int
	writtenRows int
}

func (w *concurrentCloudResourceEdgeWriter) WriteCloudResourceEdges(
	_ context.Context,
	rows []map[string]any,
	_ string,
	_ string,
	_ string,
) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.writeCalls++
	w.writtenRows += len(rows)
	return nil
}

func (w *concurrentCloudResourceEdgeWriter) RetractCloudResourceEdges(
	_ context.Context,
	_ []string,
	_ string,
	_ string,
) error {
	return nil
}

// immutableFactLoader returns the same bounded fact slice to every caller with
// no shared mutable state, so concurrent reducer workers reading one
// generation's facts do not race on the loader itself. (The package-shared
// stubFactLoader counts calls and is intentionally not used here.)
type immutableFactLoader struct {
	envelopes []facts.Envelope
}

func (l immutableFactLoader) ListFacts(_ context.Context, _, _ string) ([]facts.Envelope, error) {
	return l.envelopes, nil
}

func TestAWSRelationshipMaterializationConcurrentReprojectionIsRaceFree(t *testing.T) {
	t.Parallel()

	source := resourceEnvelope("111122223333", "us-east-1", "aws_lambda_function",
		"arn:aws:lambda:us-east-1:111122223333:function:fn", "arn:aws:lambda:us-east-1:111122223333:function:fn")
	target := resourceEnvelope("111122223333", "us-east-1", "aws_kms_key",
		"arn:aws:kms:us-east-1:111122223333:key/abc", "arn:aws:kms:us-east-1:111122223333:key/abc")
	rel := awsRelationshipEnvelope(map[string]any{
		"account_id":         "111122223333",
		"region":             "us-east-1",
		"relationship_type":  "USES_KMS_KEY",
		"source_resource_id": "arn:aws:lambda:us-east-1:111122223333:function:fn",
		"source_arn":         "arn:aws:lambda:us-east-1:111122223333:function:fn",
		"target_resource_id": "arn:aws:kms:us-east-1:111122223333:key/abc",
		"target_arn":         "arn:aws:kms:us-east-1:111122223333:key/abc",
		"target_type":        "aws_kms_key",
	})

	writer := &concurrentCloudResourceEdgeWriter{}
	handler := AWSRelationshipMaterializationHandler{
		// A shared, immutable fact loader stands in for one generation's facts;
		// every worker reads the same bounded set and resolves in memory.
		FactLoader:           immutableFactLoader{envelopes: []facts.Envelope{source, target, rel}},
		EdgeWriter:           writer,
		ReadinessLookup:      readyLookup(true, true),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
	}

	const workers = 8
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			if _, err := handler.Handle(context.Background(), awsRelationshipIntent()); err != nil {
				t.Errorf("concurrent Handle returned error: %v", err)
			}
		}()
	}
	wg.Wait()

	// Each worker produces the same single idempotent edge row; the MERGE on
	// (source, type, target) collapses them in the graph. We assert only that
	// every worker attempted exactly one resolved row, proving deterministic,
	// race-free resolution.
	if writer.writeCalls != workers {
		t.Fatalf("writeCalls = %d, want %d", writer.writeCalls, workers)
	}
	if writer.writtenRows != workers {
		t.Fatalf("writtenRows = %d, want %d (one idempotent edge per worker)", writer.writtenRows, workers)
	}
}
