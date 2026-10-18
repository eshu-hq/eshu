// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package iamcan

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestIAMCanPerformCompletionLogReasonWhenCommitAlreadyDone pins the
// completion-log retract pair on the path where the wait decision does not
// commit (#8167 review F1). A same-fingerprint evaluation whose ledger row is
// not poll-eligible (its stored key list is incomplete) takes the full path
// but the decision finds this generation, queue cycle, and missing set already
// committed, so commitEdges never runs. The log must still carry a closed
// reason, never skip_retract=true with an empty one. The test replaces the
// process default logger, so it must not run in parallel.
func TestIAMCanPerformCompletionLogReasonWhenCommitAlreadyDone(t *testing.T) {
	clock := waitTestNow()
	ledger := newMemoryWaitLedger()
	loader := &fakeCrossScopeTargets{snapshot: waitSnapshot(false)}
	handler, _ := waitHandler(ledger, loader, &clock)
	intent := waitIntent("iam-gen-1", clock)

	_, err := handler.Handle(context.Background(), intent)
	requireTargetNotReady(t, err)

	// An incomplete key list disables the cheap poll path; the fingerprint
	// still matches, so the decision reports the commit as already done.
	key := crossScopeIAMScope + "|" + string(reducercontract.DomainIAMCanPerformMaterialization)
	ledger.mu.Lock()
	row := ledger.rows[key]
	row.MissingKeys = nil
	ledger.rows[key] = row
	ledger.mu.Unlock()

	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	clock = clock.Add(30 * time.Second)
	next := &recordingIAMCanPerformWriter{}
	handler.Writer = next
	_, err = handler.Handle(context.Background(), intent)
	requireTargetNotReady(t, err)
	if next.retractCalls != 0 || next.edgeCalls != 0 {
		t.Fatalf("already-committed evaluation touched the graph: retracts %d edges %d, want 0/0", next.retractCalls, next.edgeCalls)
	}

	var completed map[string]any
	dec := json.NewDecoder(&buf)
	for dec.More() {
		var rec map[string]any
		if err := dec.Decode(&rec); err != nil {
			t.Fatalf("decode log record: %v", err)
		}
		if rec["msg"] == "iam can_perform materialization completed" {
			completed = rec
		}
	}
	if completed == nil {
		t.Fatalf("no completion log emitted; got %s", buf.String())
	}
	if got := completed[telemetry.LogKeySkipRetract]; got != true {
		t.Fatalf("%s = %v, want true (the retract did not run)", telemetry.LogKeySkipRetract, got)
	}
	if got := completed[telemetry.LogKeySkipRetractReason]; got != reducercontract.RetractReasonAlreadyCommitted {
		t.Fatalf("%s = %v, want %q", telemetry.LogKeySkipRetractReason, got, reducercontract.RetractReasonAlreadyCommitted)
	}
}
