// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"errors"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// assertCommittedSupersededClaim checks the dropped claim identity and the
// surviving ACK outcome, which the caller also verifies in PostgreSQL.
func assertCommittedSupersededClaim(t *testing.T, err error, stale, latest reducercontract.Intent) {
	t.Helper()
	if !errors.Is(err, reducer.ErrExecutionClaimRejected) {
		t.Fatalf("AckBatch error = %v, want canonical claim rejection", err)
	}
	var superseded *reducer.BatchAckSupersededError
	if !errors.As(err, &superseded) {
		t.Fatalf("AckBatch error = %T, want BatchAckSupersededError", err)
	}
	if len(superseded.Superseded) != 1 || !reflect.DeepEqual(superseded.Superseded[0], stale) {
		t.Fatalf("dropped claims = %+v, want exactly %+v", superseded.Superseded, stale)
	}
	if !superseded.KeptCommitted || !superseded.Supersedes(stale) || superseded.Supersedes(latest) {
		t.Fatalf("supersession = %+v, want stale dropped and latest committed", superseded)
	}
}
