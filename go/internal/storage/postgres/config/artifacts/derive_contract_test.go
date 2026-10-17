// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts

import (
	"context"
	"errors"
	"testing"

	contentartifacts "github.com/eshu-hq/eshu/go/internal/content/artifacts"
)

// TestDeriveMapsEveryStartupCheckResult feeds the real results of
// CheckEncoding and CheckFence into a Deriver and pins the outcome of each:
// every unsupported encoding result is encoding_unsupported, and every
// unsupported fence result, the failed catalog read (fence_check_failed)
// included, is fence_missing (decision record section 3.1, item 7). Neither
// sends a derive statement.
func TestDeriveMapsEveryStartupCheckResult(t *testing.T) {
	ctx := context.Background()
	utf8 := CheckEncoding(ctx, &encodingQueryer{value: "UTF8"}, nil)
	allFired := CheckFence(ctx, &fenceQueryer{rows: allFenceRows()}, nil)
	cases := []struct {
		name       string
		encoding   Encoding
		fence      Fence
		wantReason string
		want       Outcome
	}{
		{"encoding read failed", CheckEncoding(ctx, &encodingQueryer{err: errors.New("down")}, nil), allFired, ReasonEncodingCheckFailed, OutcomeEncodingUnsupported},
		{"encoding not UTF8", CheckEncoding(ctx, &encodingQueryer{value: "LATIN1"}, nil), allFired, ReasonEncodingUnsupported, OutcomeEncodingUnsupported},
		{"fence catalog read failed", utf8, CheckFence(ctx, &fenceQueryer{err: errors.New("down")}, nil), ReasonFenceCheckFailed, OutcomeFenceMissing},
		{"fence rows unreadable", utf8, CheckFence(ctx, &fenceQueryer{rows: allFenceRows(), rowsErr: errors.New("reset")}, nil), ReasonFenceCheckFailed, OutcomeFenceMissing},
		{"fence trigger absent", utf8, CheckFence(ctx, &fenceQueryer{rows: without("content_files_version_fence_delete")}, nil), ReasonVersionFenceMissing, OutcomeFenceMissing},
		{"fence trigger disabled", utf8, CheckFence(ctx, &fenceQueryer{rows: withEnabled("config_artifact_fence_pending_commit", "D")}, nil), ReasonVersionFenceMissing, OutcomeFenceMissing},
		{"fence trigger replica only", utf8, CheckFence(ctx, &fenceQueryer{rows: withEnabled("content_files_version_fence_insert", "R")}, nil), ReasonVersionFenceMissing, OutcomeFenceMissing},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if reason := tc.encoding.Reason + tc.fence.Reason; reason != tc.wantReason {
				t.Fatalf("check reasons = %q, want %q", reason, tc.wantReason)
			}
			fake := &deriveFakeDB{version: int64Ptr(1)}
			config := supportedConfig()
			config.Encoding, config.Fence = tc.encoding, tc.fence
			result := newTestDeriver(t, fake, config, rowsLoad(composeRow())).DeriveRepository(ctx, Request{RepoID: "repo-a", Trigger: TriggerWrite})
			if result.Outcome != tc.want || len(fake.statements()) != 0 {
				t.Fatalf("Outcome = %q with statements %v, want %q and none", result.Outcome, fake.statements(), tc.want)
			}
		})
	}
	fake := &deriveFakeDB{version: int64Ptr(1)}
	config := supportedConfig()
	config.Encoding, config.Fence = utf8, allFired
	if result := newTestDeriver(t, fake, config, rowsLoad(composeRow())).DeriveRepository(ctx, Request{RepoID: "repo-a", Trigger: TriggerWrite}); result.Outcome != OutcomeOK {
		t.Fatalf("supported checks: Outcome = %q, want ok", result.Outcome)
	}
}

// lockCalls returns the arguments of every repository-lock statement sent.
func lockCalls(t *testing.T, fake *deriveFakeDB) [][]any {
	t.Helper()
	fake.mu.Lock()
	defer fake.mu.Unlock()
	var out [][]any
	for _, call := range fake.calls {
		if call.sql == repoLockSQL {
			if !call.inTx {
				t.Fatal("repository lock taken outside a transaction")
			}
			out = append(out, call.args)
		}
	}
	return out
}

// TestDeriveWriteAndFailureRecordShareOneRepositoryLock: the successful write
// and the failure-only update take the repository lock through one helper,
// with one key string, 'config_artifact_paths:' || repo_id, so they contend
// on the same advisory lock.
func TestDeriveWriteAndFailureRecordShareOneRepositoryLock(t *testing.T) {
	const wantKey = "config_artifact_paths:repo-a"
	success := &deriveFakeDB{version: int64Ptr(2)}
	if result := newTestDeriver(t, success, supportedConfig(), rowsLoad(composeRow())).DeriveRepository(context.Background(), Request{RepoID: "repo-a", Trigger: TriggerWrite}); result.Outcome != OutcomeOK {
		t.Fatalf("success derive = %+v", result)
	}
	failure := &deriveFakeDB{version: int64Ptr(2)}
	failing := func(context.Context, contentartifacts.Reader, contentartifacts.Repository, []contentartifacts.File, bool, int) ([]map[string]any, contentartifacts.ReadStats, error) {
		return nil, contentartifacts.ReadStats{}, errFakeLoad
	}
	if result := newTestDeriver(t, failure, supportedConfig(), failing).DeriveRepository(context.Background(), Request{RepoID: "repo-a", Trigger: TriggerWrite}); !result.FailureRecorded {
		t.Fatalf("failing derive = %+v, want the failure recorded", result)
	}
	writeLocks, failureLocks := lockCalls(t, success), lockCalls(t, failure)
	if len(writeLocks) != 1 || len(failureLocks) != 1 {
		t.Fatalf("lock calls: write %v, failure %v; want one each", writeLocks, failureLocks)
	}
	if len(writeLocks[0]) != 1 || writeLocks[0][0] != wantKey {
		t.Fatalf("write lock args = %v, want [%q]", writeLocks[0], wantKey)
	}
	if len(failureLocks[0]) != 1 || failureLocks[0][0] != writeLocks[0][0] {
		t.Fatalf("failure lock args = %v, want the write's key %v", failureLocks[0], writeLocks[0])
	}
	if got := repoLockKey("repo-a"); got != wantKey {
		t.Fatalf("repoLockKey() = %q, want %q", got, wantKey)
	}
}
