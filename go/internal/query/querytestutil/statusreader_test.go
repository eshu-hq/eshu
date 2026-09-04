// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querytestutil_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
	"github.com/eshu-hq/eshu/go/internal/status"
)

// TestFakeStatusReaderReturnsSnapshot pins the success path: with no Err set,
// both methods answer with the configured Snapshot.
func TestFakeStatusReaderReturnsSnapshot(t *testing.T) {
	t.Parallel()

	want := status.RawSnapshot{AsOf: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)}
	reader := querytestutil.FakeStatusReader{Snapshot: want}

	got, err := reader.ReadStatusSnapshot(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("ReadStatusSnapshot() error = %v", err)
	}
	if !got.AsOf.Equal(want.AsOf) {
		t.Fatalf("ReadStatusSnapshot() = %+v, want %+v", got, want)
	}
}

// TestFakeStatusReaderReturnsErrWhenSet covers the failure path a handler's
// error-handling tests depend on: Err propagates instead of the zero-value
// error, and Snapshot is not also returned alongside it.
func TestFakeStatusReaderReturnsErrWhenSet(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("status backend unavailable")
	reader := querytestutil.FakeStatusReader{
		Snapshot: status.RawSnapshot{AsOf: time.Now()},
		Err:      sentinel,
	}

	got, err := reader.ReadStatusSnapshot(context.Background(), time.Now())
	if !errors.Is(err, sentinel) {
		t.Fatalf("ReadStatusSnapshot() error = %v, want %v", err, sentinel)
	}
	if !got.AsOf.IsZero() {
		t.Fatalf("ReadStatusSnapshot() = %+v, want the zero snapshot on error", got)
	}
}

// TestFakeStatusReaderFilteredDelegatesToReadStatusSnapshot pins that the
// filtered method is not a second, independently-drifting implementation: it
// ignores the selection and answers exactly as ReadStatusSnapshot would,
// including the error path.
func TestFakeStatusReaderFilteredDelegatesToReadStatusSnapshot(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("status backend unavailable")
	reader := querytestutil.FakeStatusReader{Err: sentinel}

	got, err := reader.ReadStatusSnapshotFiltered(
		context.Background(), time.Now(), status.FullSnapshotSelection())
	if !errors.Is(err, sentinel) {
		t.Fatalf("ReadStatusSnapshotFiltered() error = %v, want %v", err, sentinel)
	}
	if !got.AsOf.IsZero() {
		t.Fatalf("ReadStatusSnapshotFiltered() = %+v, want the zero snapshot on error", got)
	}
}

// TestFakeStatusReaderZeroValueIsUsable matters because a caller can construct
// the fake with no fields set at all, purely to satisfy the status.Reader
// port. The zero value must answer with the zero snapshot and nil error
// rather than panicking.
func TestFakeStatusReaderZeroValueIsUsable(t *testing.T) {
	t.Parallel()

	var reader querytestutil.FakeStatusReader

	got, err := reader.ReadStatusSnapshot(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("ReadStatusSnapshot() error = %v, want nil", err)
	}
	if !got.AsOf.IsZero() {
		t.Fatalf("ReadStatusSnapshot() = %+v, want the zero snapshot", got)
	}
}
