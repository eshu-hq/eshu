// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projection

import (
	"context"
	"slices"
	"testing"
	"time"

	codecall "github.com/eshu-hq/eshu/go/internal/reducer/code/call"
	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
)

// TestCodeCallProjectionRunnerFileAndWholeRefreshInterleaveDrains is the
// #10049 interleave proof: a file-scoped (delta) refresh row and a
// whole-scope refresh row pending for the same repository in one acceptance
// unit, driven partition by partition in adversarial order. The two refresh
// shapes hash to different partition keys, so different leased partitions
// can hold them at once; the refresh fence is the only ordering guarantee.
// Both creation orders must satisfy the same contract:
//
//   - a partition attempt on the fenced (later) shape performs no retract,
//     no write, and marks nothing — it cannot jump the fence;
//   - the unfenced (earlier) shape always makes progress, so the pair
//     cannot deadlock;
//   - driving every partition to quiescence marks each row exactly once
//     and writes the file edge exactly once.
//
// Cross-generation supersession (an older generation's rows still pending
// when a newer generation's rows land) is owned by the acceptance and
// superseded-generation machinery the selection runs before this fence, not
// by the fence itself.
func TestCodeCallProjectionRunnerFileAndWholeRefreshInterleaveDrains(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 5, 12, 0, 0, 0, time.UTC)
	partitionCount := 8
	filePartition := codecall.RefreshPartitionKeyForDelta("repo-a", []string{"src/caller.go"})
	wholePartition := codecall.WholeScopePartitionKey("repo-a")
	filePartitionID := mustPartitionForKey(t, filePartition, partitionCount)
	wholePartitionID := mustPartitionForKey(t, wholePartition, partitionCount)
	if filePartitionID == wholePartitionID {
		t.Fatalf("file and whole keys mapped to same partition %d; choose different fixture paths", filePartitionID)
	}

	buildRows := func(wholeFirst bool) (wholeRefresh, fileRefresh, fileEdge sharedintent.Row) {
		wholeAt, fileAt := now, now.Add(time.Millisecond)
		if !wholeFirst {
			wholeAt, fileAt = fileAt, wholeAt
		}
		wholeRefresh = codeCallProjectionWholeScopeRow("whole-refresh", "repo-a", wholeAt)
		fileRefresh = codeCallProjectionFileRefreshRow(
			"file-refresh", filePartition, "repo-a", []string{"/repo/src/caller.go"}, fileAt,
		)
		fileEdge = codeCallProjectionDeltaPartitionRow(
			"caller-edge", filePartition, "repo-a", "/repo/src/caller.go", fileAt.Add(time.Millisecond),
		)
		return wholeRefresh, fileRefresh, fileEdge
	}

	for _, wholeFirst := range []bool{true, false} {
		name := "file refresh created first"
		if wholeFirst {
			name = "whole refresh created first"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			wholeRefresh, fileRefresh, fileEdge := buildRows(wholeFirst)
			rows := []sharedintent.Row{wholeRefresh, fileRefresh, fileEdge}
			if !wholeFirst {
				rows = []sharedintent.Row{fileRefresh, fileEdge, wholeRefresh}
			}
			reader := &fakeCodeCallIntentStore{
				pendingByDomain: rows,
				pendingByAcceptance: map[string][]sharedintent.Row{
					"scope-a|repo-a|run-1": rows,
				},
				leaseGranted: true,
			}
			writer := &recordingCodeCallProjectionEdgeWriter{}
			runner := Runner{
				IntentReader: reader,
				LeaseManager: reader,
				EdgeWriter:   writer,
				AcceptedGen:  acceptedGenerationFixed("gen-1", true),
				Config:       RunnerConfig{BatchLimit: 10, PartitionCount: partitionCount},
			}

			// Adversarial first attempt: the partition holding the later
			// (fenced) shape must not retract, write, or mark anything.
			fencedPartitionID, leadingPartitionID := filePartitionID, wholePartitionID
			if !wholeFirst {
				fencedPartitionID, leadingPartitionID = wholePartitionID, filePartitionID
			}
			blocked, err := runner.processPartitionOnce(context.Background(), now, fencedPartitionID, partitionCount)
			if err != nil {
				t.Fatalf("processPartitionOnce(fenced) error = %v", err)
			}
			if blocked.ProcessedIntents != 0 {
				t.Fatalf("fenced partition ProcessedIntents = %d, want 0 (fence must hold)", blocked.ProcessedIntents)
			}
			if len(reader.marked) != 0 || len(writer.retractCalls) != 0 || len(writer.writeCalls) != 0 {
				t.Fatalf("fenced partition touched state: marked=%v retracts=%d writes=%d, want none",
					reader.marked, len(writer.retractCalls), len(writer.writeCalls))
			}

			// The leading shape always progresses, then the formerly fenced
			// shape drains. Two more attempts (one per partition) suffice in
			// either order: the whole-scope load takes the whole unit, the
			// file load takes its partition.
			for _, partitionID := range []int{leadingPartitionID, fencedPartitionID} {
				if _, err := runner.processPartitionOnce(context.Background(), now, partitionID, partitionCount); err != nil {
					t.Fatalf("processPartitionOnce(%d) error = %v", partitionID, err)
				}
			}

			marked := append([]string(nil), reader.marked...)
			slices.Sort(marked)
			if want := []string{"caller-edge", "file-refresh", "whole-refresh"}; !slices.Equal(marked, want) {
				t.Fatalf("marked = %v, want each row exactly once %v", reader.marked, want)
			}
			writtenEdges := 0
			for _, call := range writer.writeCalls {
				for _, row := range call.rows {
					if row.IntentID == "caller-edge" {
						writtenEdges++
					}
				}
			}
			if writtenEdges != 1 {
				t.Fatalf("caller-edge written %d times, want exactly 1 (writeCalls=%#v)", writtenEdges, writer.writeCalls)
			}
			if len(writer.retractCalls) == 0 {
				t.Fatal("no retract ran; the refresh rows must own a retract before the write")
			}
		})
	}
}
