// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build ifafaultinjection

package cypher

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/replay/faultreplay"
)

type restartGroupRecordingExecutor struct {
	*faultRecordingExecutor
}

func (e *restartGroupRecordingExecutor) ExecutePhaseGroup(
	ctx context.Context,
	statements []Statement,
) error {
	return e.ExecuteGroup(ctx, statements)
}

func TestFaultingExecutorRestartRecordsTriggerGroupBeforeSentinel(t *testing.T) {
	t.Parallel()

	inner := &restartGroupRecordingExecutor{
		faultRecordingExecutor: &faultRecordingExecutor{supportsGrp: true},
	}
	sentinel := filepath.Join(t.TempDir(), "restart.sentinel")
	script := faultreplay.Script{
		Version: faultreplay.CurrentVersion,
		Faults: []faultreplay.FaultOp{{
			Kind:    faultreplay.KindRestartBackendBetweenPhaseGroups,
			Trigger: faultreplay.Trigger{AfterPhaseGroups: intPtr(2)},
		}},
	}
	fe := mustFaultingExecutor(t, inner, script, sentinel)
	firstGroup := []Statement{{
		Operation:  OperationCanonicalUpsert,
		Cypher:     "MERGE (n {uid: 'first-group'})",
		Parameters: map[string]any{"scope_id": "scope-a"},
	}}
	statements := []Statement{{
		Operation:  OperationCanonicalUpsert,
		Cypher:     "UNWIND $rows AS row MERGE (n {uid: row.uid})",
		Parameters: map[string]any{"scope_id": "scope-a", "rows": []any{map[string]any{"uid": "node-1"}}},
		Drain:      true,
		DrainVar:   "n",
	}}

	if err := fe.ExecuteGroup(context.Background(), firstGroup); err != nil {
		t.Fatalf("ExecuteGroup before restart boundary: %v", err)
	}
	recordPath := sentinel + restartTriggerRecordSuffix
	for _, path := range []string{sentinel, recordPath} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("restart artifact %s exists before group 2: %v", path, err)
		}
	}

	done := make(chan error, 1)
	go func() {
		done <- fe.ExecutePhaseGroup(context.Background(), statements)
	}()

	waitForFaultFile(t, sentinel)
	recordBytes, err := os.ReadFile(recordPath)
	if err != nil {
		t.Fatalf("read restart trigger record after sentinel became visible: %v", err)
	}
	var record restartTriggerRecord
	if err := json.Unmarshal(recordBytes, &record); err != nil {
		t.Fatalf("decode restart trigger record: %v", err)
	}
	if record.GroupOrdinal != 2 || record.Surface != restartSurfaceExecutePhase {
		t.Fatalf("trigger identity = ordinal %d surface %q, want 2 and %q", record.GroupOrdinal, record.Surface, restartSurfaceExecutePhase)
	}
	if len(record.Statements) != 1 {
		t.Fatalf("trigger statements = %d, want 1", len(record.Statements))
	}
	got := record.Statements[0]
	if got.Operation != OperationCanonicalUpsert || got.Cypher != statements[0].Cypher || !got.Drain || got.DrainVar != "n" {
		t.Fatalf("trigger statement metadata = %+v, want %+v", got, statements[0])
	}
	rows, ok := got.Parameters["rows"].([]any)
	if !ok || len(rows) != 1 {
		t.Fatalf("trigger statement rows = %#v, want one exact prepared row", got.Parameters["rows"])
	}

	if err := os.Remove(sentinel); err != nil {
		t.Fatalf("remove sentinel: %v", err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ExecutePhaseGroup after sentinel removal: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for ExecutePhaseGroup to unblock")
	}

	if err := fe.ExecuteGroup(context.Background(), firstGroup); err != nil {
		t.Fatalf("ExecuteGroup after restart fired: %v", err)
	}
	afterRefire, err := os.ReadFile(recordPath)
	if err != nil {
		t.Fatalf("read trigger record after later group: %v", err)
	}
	if !bytes.Equal(afterRefire, recordBytes) {
		t.Fatal("later group overwrote the one trigger record")
	}
}

func TestFaultingExecutorRestartTriggerRecordIsBounded(t *testing.T) {
	t.Parallel()

	fe := &FaultingExecutor{sentinelPath: filepath.Join(t.TempDir(), "restart.sentinel")}
	tooMany := make([]Statement, maxRestartTriggerStatements+1)
	if err := fe.writeRestartTriggerRecord(1, restartSurfaceExecuteGroup, tooMany); err == nil {
		t.Fatal("expected an oversized statement group to fail closed")
	}
	tooLarge := []Statement{{
		Cypher:     "RETURN $payload",
		Parameters: map[string]any{"payload": strings.Repeat("x", maxRestartTriggerRecordBytes)},
	}}
	if err := fe.writeRestartTriggerRecord(1, restartSurfaceExecuteGroup, tooLarge); err == nil {
		t.Fatal("expected an oversized trigger record to fail closed")
	}
	if _, err := os.Stat(fe.sentinelPath + restartTriggerRecordSuffix); !os.IsNotExist(err) {
		t.Fatalf("boundedness failure published a trigger record: %v", err)
	}
}

func waitForFaultFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(path); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", path)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
