// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build ifafaultinjection

package main

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

type leaseBarrierForwardingStub struct {
	leaseBarrierExecuteOnlyStub
	groupCalls int
	probeCalls int
	err        error
}

func (s *leaseBarrierForwardingStub) ExecuteGroup(_ context.Context, stmts []sourcecypher.Statement) error {
	s.groupCalls += len(stmts)
	return s.err
}

func (s *leaseBarrierForwardingStub) ExecuteProbe(context.Context, sourcecypher.Statement) (bool, error) {
	s.probeCalls++
	return s.err == nil, s.err
}

func TestIfaLeaseExpiryBarrierCombinedForwardsGroupAndProbe(t *testing.T) {
	for _, fail := range []bool{false, true} {
		inner := &leaseBarrierForwardingStub{}
		if fail {
			inner.err = errors.New("backend failure")
		}
		barrier := &ifaLeaseExpiryBarrier{path: filepath.Join(t.TempDir(), "lease")}
		wrapped := barrier.wrapGraph(inner)
		group, ok := wrapped.(sourcecypher.GroupExecutor)
		if !ok {
			t.Fatal("combined input lost Group")
		}
		probe, ok := wrapped.(sourcecypher.ProbeExecutor)
		if !ok {
			t.Fatal("combined input lost Probe")
		}
		if err := group.ExecuteGroup(context.Background(), []sourcecypher.Statement{{}, {}}); !errors.Is(err, inner.err) {
			t.Fatalf("group error = %v, want %v", err, inner.err)
		}
		found, err := probe.ExecuteProbe(context.Background(), sourcecypher.Statement{})
		if !errors.Is(err, inner.err) || found != !fail || inner.groupCalls != 2 || inner.probeCalls != 1 {
			t.Fatalf("dispatch found=%t error=%v group calls=%d probe calls=%d", found, err, inner.groupCalls, inner.probeCalls)
		}
	}
}
