// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package offlinetier_test

import (
	"context"
	"testing"
	"time"
)

// assertDeltaGitlabNeedsEdgeCount reads back a GitlabJob -> GitlabJob NEEDS
// edge by stable endpoint UIDs and fails if it does not match want.
func assertDeltaGitlabNeedsEdgeCount(
	ctx context.Context,
	t *testing.T,
	exec liveExecutor,
	sourceUID string,
	targetUID string,
	want int64,
	msg string,
) {
	t.Helper()
	var count int64
	var err error
	for attempt := 0; attempt < 20; attempt++ {
		count, err = exec.count(
			ctx,
			`MATCH (a:GitlabJob {uid: $source_uid})-[r:NEEDS]->(b:GitlabJob {uid: $target_uid}) RETURN count(r)`,
			map[string]any{"source_uid": sourceUID, "target_uid": targetUID},
		)
		if err != nil {
			t.Fatalf("count NEEDS edge %q -> %q: %v", sourceUID, targetUID, err)
		}
		if count == want || want != 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if count != want {
		t.Fatalf("%s: NEEDS edge %q -> %q count = %d, want %d", msg, sourceUID, targetUID, count, want)
	}
	t.Logf("NEEDS edge %q -> %q count=%d (want %d) — %s", sourceUID, targetUID, count, want, msg)
}
