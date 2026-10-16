// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package inventory_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"testing/synctest"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/infra/inventory"
)

func TestFenceRaceWorkersJoinAndRetainLateError(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		admissionCtx, stop := context.WithCancel(context.Background())
		operationCtx, cancelOperation := context.WithCancel(context.Background())
		defer cancelOperation()
		workers := &fenceRaceWorkers{}
		entered := make(chan struct{})
		release := make(chan struct{})
		workers.start(admissionCtx, operationCtx, func(ctx context.Context) error {
			close(entered)
			<-release
			if err := ctx.Err(); err != nil {
				return err
			}
			return errors.New("late write failed")
		})
		<-entered
		stop()
		finished := make(chan error, 1)
		go func() { finished <- workers.finish() }()
		synctest.Wait()
		select {
		case <-finished:
			t.Fatal("worker join returned before admitted operation completed")
		default:
		}
		close(release)
		if err := <-finished; err == nil || !strings.Contains(err.Error(), "late write failed") {
			t.Fatalf("joined worker error = %v, want late write failure", err)
		}
	})
}

func TestFenceBatchRejectsPerRepoErrorAndUnreadyFinal(t *testing.T) {
	batch := inventory.ReconcileBatch{
		Ready: true,
		Repos: []inventory.RepoReconcile{{RepoID: "repo-race", Outcome: inventory.ReconcileError, Err: errors.New("repair failed")}},
	}
	if err := fenceBatchError(batch, true); err == nil {
		t.Fatal("nil batch-level error hid a repository repair error")
	}
	if err := fenceBatchError(inventory.ReconcileBatch{}, true); err == nil {
		t.Fatal("unready final cycle passed")
	}
}
