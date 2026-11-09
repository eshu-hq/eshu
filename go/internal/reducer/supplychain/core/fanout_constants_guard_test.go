// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import "testing"

// TestFanOutChunkAndHolderCapsArePinned guards the fan-out bounding constants
// (#10408 L4): the holder lookup runs one statement per 200 ids and one claim
// reopens at most 256 holders, so a re-key wave stays bounded no matter how
// many members a merge or split names. The origin stamp carries at most one
// chunk, which keeps the stamp comparable by string prefix. Any change to
// these numbers changes the number of holder-read statements, the size of a
// continuation drain, and the stamp shape, so it needs its own proof.
func TestFanOutChunkAndHolderCapsArePinned(t *testing.T) {
	t.Parallel()

	if fanOutCVEChunk != 200 {
		t.Fatalf("fanOutCVEChunk = %d, want 200: a re-key lookup chunk carries at most 200 ids per statement", fanOutCVEChunk)
	}
	if fanOutHolderCap != 256 {
		t.Fatalf("fanOutHolderCap = %d, want 256: one claim reopens at most 256 holders and carries the rest", fanOutHolderCap)
	}
	if FanOutOriginCVEIDCap != 200 {
		t.Fatalf("FanOutOriginCVEIDCap = %d, want 200: the stamp carries at most one lookup chunk", FanOutOriginCVEIDCap)
	}
	if FanOutOriginCVEIDCap != fanOutCVEChunk {
		t.Fatalf("FanOutOriginCVEIDCap = %d, fanOutCVEChunk = %d: the stamp cap must stay one chunk", FanOutOriginCVEIDCap, fanOutCVEChunk)
	}
}
