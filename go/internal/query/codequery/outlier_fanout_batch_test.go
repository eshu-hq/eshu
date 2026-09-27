// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"context"
	"testing"
)

// TestChunkOutlierCalleeEdgeKeysUsesDedicatedBatchSize pins the
// convention-outlier CALLS-fanout chunker to its own batch size
// (outlierCalleeEdgeBatchSize, 250, #7325), decoupled from the
// wrapper-bypass evidence track's wrapperEvidenceKeyBatchSize (50): the same
// input id set must chunk differently under each chunker, and
// chunkOutlierCalleeEdgeKeys must never exceed 250 keys per chunk.
func TestChunkOutlierCalleeEdgeKeysUsesDedicatedBatchSize(t *testing.T) {
	if got := chunkOutlierCalleeEdgeKeys(nil); len(got) != 0 {
		t.Fatalf("chunks(nil) = %d, want 0", len(got))
	}

	ids := make([]string, 0, 2*outlierCalleeEdgeBatchSize+1)
	for i := 0; i < 2*outlierCalleeEdgeBatchSize+1; i++ {
		ids = append(ids, string(rune('a'+i%26))+string(rune('0'+(i/26)%10))+string(rune('A'+(i/260)%26)))
	}

	outlierChunks := chunkOutlierCalleeEdgeKeys(ids)
	if len(outlierChunks) != 3 {
		t.Fatalf("chunkOutlierCalleeEdgeKeys(%d ids) = %d chunks, want 3 (ceil(%d/%d))",
			len(ids), len(outlierChunks), len(ids), outlierCalleeEdgeBatchSize)
	}
	if len(outlierChunks[0]) != outlierCalleeEdgeBatchSize || len(outlierChunks[1]) != outlierCalleeEdgeBatchSize || len(outlierChunks[2]) != 1 {
		t.Fatalf("outlier chunk sizes = %d/%d/%d, want %d/%d/1",
			len(outlierChunks[0]), len(outlierChunks[1]), len(outlierChunks[2]),
			outlierCalleeEdgeBatchSize, outlierCalleeEdgeBatchSize)
	}

	// The wrapper-bypass evidence track's chunker, unchanged, still splits
	// the identical id set at 50 keys per chunk: the two batch sizes are
	// independently configured, not aliases of one shared constant.
	wrapperChunks := chunkWrapperEvidenceKeys(ids)
	wantWrapperChunks := (len(ids) + wrapperEvidenceKeyBatchSize - 1) / wrapperEvidenceKeyBatchSize
	if len(wrapperChunks) != wantWrapperChunks {
		t.Fatalf("chunkWrapperEvidenceKeys(%d ids) = %d chunks, want %d (ceil(%d/%d)); wrapper-bypass batch size must stay independent of the outlier fan-out's",
			len(ids), len(wrapperChunks), wantWrapperChunks, len(ids), wrapperEvidenceKeyBatchSize)
	}
	if len(wrapperChunks) == len(outlierChunks) {
		t.Fatalf("wrapper and outlier chunk counts both = %d over %d ids; the two batch sizes (%d vs %d) should produce different chunk counts here",
			len(wrapperChunks), len(ids), wrapperEvidenceKeyBatchSize, outlierCalleeEdgeBatchSize)
	}

	flat := []string{}
	for _, chunk := range outlierChunks {
		flat = append(flat, chunk...)
	}
	for i := range ids {
		if flat[i] != ids[i] {
			t.Fatalf("outlier chunk round trip order breaks at %d: %q != %q", i, flat[i], ids[i])
		}
	}
}

// TestReadOutlierCalleeEdgesSendsFullBatchSizeChunks pins
// readOutlierCalleeEdges to the outlier fan-out's own 250-key batch, not
// the wrapper-bypass evidence track's 50-key one (#7325). A fake graph
// reader records the member_ids length of every statement it receives; 501
// member ids must produce exactly 3 statements (250, 250, 1) -- a
// regression back to the shared 50-key chunker would send 11 statements
// (50 x 10 + 1) instead, so this test is RED against that shape and GREEN
// only at the dedicated 250 batch.
func TestReadOutlierCalleeEdgesSendsFullBatchSizeChunks(t *testing.T) {
	const memberCount = 2*outlierCalleeEdgeBatchSize + 1 // 501 at batch 250

	ids := make([]string, 0, memberCount)
	for i := 0; i < memberCount; i++ {
		ids = append(ids, string(rune('a'+i%26))+string(rune('0'+(i/26)%10))+string(rune('A'+(i/260)%26)))
	}

	var chunkSizes []int
	handler := &CodeHandler{
		Profile:      ProfileLocalAuthoritative,
		GraphBackend: GraphBackendNeo4j,
		Neo4j: fakeGraphReader{run: func(_ context.Context, _ string, params map[string]any) ([]map[string]any, error) {
			memberIDs, _ := params["member_ids"].([]string)
			chunkSizes = append(chunkSizes, len(memberIDs))
			return nil, nil
		}},
	}

	if _, _, err := handler.readOutlierCalleeEdges(context.Background(), ids, "repo-7325"); err != nil {
		t.Fatalf("readOutlierCalleeEdges() error = %v", err)
	}

	wantChunks := (memberCount + outlierCalleeEdgeBatchSize - 1) / outlierCalleeEdgeBatchSize
	if len(chunkSizes) != wantChunks {
		t.Fatalf("statement count = %d, want %d (ceil(%d/%d)); got chunk sizes %v",
			len(chunkSizes), wantChunks, memberCount, outlierCalleeEdgeBatchSize, chunkSizes)
	}
	for i, size := range chunkSizes {
		if i < len(chunkSizes)-1 {
			if size != outlierCalleeEdgeBatchSize {
				t.Fatalf("chunk %d size = %d, want the full batch size %d", i, size, outlierCalleeEdgeBatchSize)
			}
			continue
		}
		wantLast := memberCount - outlierCalleeEdgeBatchSize*(wantChunks-1)
		if size != wantLast {
			t.Fatalf("last chunk size = %d, want remainder %d", size, wantLast)
		}
	}
}

// TestCollectWrapperGraphEvidenceStaysAtWrapperBatchSize proves the
// wrapper-bypass evidence track's three batched reads
// (collectWrapperGraphEvidence) still chunk at wrapperEvidenceKeyBatchSize
// (50), unaffected by the outlier fan-out's dedicated 250-key batch
// (#7325): #7325's measurement covered only the outlier CALLS-fanout read,
// so this track's batch size must not have moved.
func TestCollectWrapperGraphEvidenceStaysAtWrapperBatchSize(t *testing.T) {
	const wrapperCount = 2*wrapperEvidenceKeyBatchSize + 1 // 101 at batch 50

	wrapperIDs := make([]string, 0, wrapperCount)
	for i := 0; i < wrapperCount; i++ {
		wrapperIDs = append(wrapperIDs, string(rune('a'+i%26))+string(rune('0'+(i/26)%10))+string(rune('A'+(i/260)%26)))
	}

	var calleeChunkSizes []int
	handler := &CodeHandler{
		Profile:      ProfileLocalAuthoritative,
		GraphBackend: GraphBackendNeo4j,
		Neo4j: fakeGraphReader{run: func(_ context.Context, _ string, params map[string]any) ([]map[string]any, error) {
			sourceIDs, _ := params["source_ids"].([]string)
			if sourceIDs != nil {
				calleeChunkSizes = append(calleeChunkSizes, len(sourceIDs))
			}
			return nil, nil
		}},
	}

	if _, err := handler.collectWrapperGraphEvidence(context.Background(), "repo-7325", wrapperIDs); err != nil {
		t.Fatalf("collectWrapperGraphEvidence() error = %v", err)
	}

	wantChunks := (wrapperCount + wrapperEvidenceKeyBatchSize - 1) / wrapperEvidenceKeyBatchSize
	if len(calleeChunkSizes) != wantChunks {
		t.Fatalf("wrapper delegation-callee statement count = %d, want %d (ceil(%d/%d)); got chunk sizes %v",
			len(calleeChunkSizes), wantChunks, wrapperCount, wrapperEvidenceKeyBatchSize, calleeChunkSizes)
	}
	for i, size := range calleeChunkSizes {
		if i < len(calleeChunkSizes)-1 && size != wrapperEvidenceKeyBatchSize {
			t.Fatalf("chunk %d size = %d, want the full wrapper batch size %d", i, size, wrapperEvidenceKeyBatchSize)
		}
	}
}
