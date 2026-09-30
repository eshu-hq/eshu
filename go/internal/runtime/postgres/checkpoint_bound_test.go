// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCheckpointAcquisitionHasIndependentBound(t *testing.T) {
	access := testAccess(t, "")
	access.writer.SetMaxOpenConns(1)
	held, err := access.writer.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() { _, err := access.ContextWithCheckpoint(ctx); result <- err }()
	select {
	case err := <-result:
		if !errors.Is(err, ErrWriterUnavailable) || !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("checkpoint result=%v", err)
		}
	case <-time.After(access.replayTimeout + 400*time.Millisecond):
		cancel()
		<-result
		t.Fatal("writer pool acquisition exceeded independent checkpoint bound")
	}
	if ctx.Err() != nil {
		t.Fatal("checkpoint deadline canceled caller")
	}
	if err := held.Close(); err != nil {
		t.Fatal(err)
	}
	checkpointCtx, err := access.ContextWithCheckpoint(ctx)
	if err != nil {
		t.Fatal(err)
	}
	original, _ := ctx.Deadline()
	actual, _ := checkpointCtx.Deadline()
	if !actual.Equal(original) {
		t.Fatal("business context inherited checkpoint deadline")
	}
}
