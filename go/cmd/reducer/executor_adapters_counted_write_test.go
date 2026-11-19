// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"sync"
	"testing"

	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// countedFakeSession implements cypherRunner plus cypherCountedWriteRunner
// with scripted per-statement counts, so tests can prove the counted-write
// seam survives the production adapter chain end to end. It deliberately
// does not embed *fakeNeo4jSession: promotion would silently satisfy
// cypherProber and defeat the seam-absence negative case below.
type countedFakeSession struct {
	mu          sync.Mutex
	calls       []fakeCypherCall
	groupCalls  int
	counts      []int64
	singleCalls int
	err         error
}

func (s *countedFakeSession) Execute(ctx context.Context, stmt sourcecypher.Statement) error {
	return s.RunCypher(ctx, stmt.Cypher, stmt.Parameters)
}

func (s *countedFakeSession) ExecuteCountedWrite(ctx context.Context, stmt sourcecypher.Statement) (int64, error) {
	return s.RunCypherCountedWrite(ctx, stmt.Cypher, stmt.Parameters)
}

func (s *countedFakeSession) ExecuteCountedWriteGroup(ctx context.Context, stmts []sourcecypher.Statement) ([]int64, error) {
	return s.RunCypherCountedWriteGroup(ctx, stmts)
}

func (s *countedFakeSession) RunCypher(_ context.Context, cypher string, params map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, fakeCypherCall{Cypher: cypher, Parameters: params})
	return s.err
}

func (s *countedFakeSession) RunCypherGroup(ctx context.Context, stmts []sourcecypher.Statement) error {
	for _, stmt := range stmts {
		if err := s.RunCypher(ctx, stmt.Cypher, stmt.Parameters); err != nil {
			return err
		}
	}
	return nil
}

func (s *countedFakeSession) RunCypherCountedWrite(_ context.Context, _ string, _ map[string]any) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.singleCalls++
	if s.err != nil {
		return 0, s.err
	}
	if len(s.counts) == 0 {
		return 1, nil
	}
	c := s.counts[0]
	s.counts = s.counts[1:]
	return c, nil
}

func (s *countedFakeSession) RunCypherCountedWriteGroup(_ context.Context, stmts []sourcecypher.Statement) ([]int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.groupCalls++
	if s.err != nil {
		return nil, s.err
	}
	out := make([]int64, len(stmts))
	for i := range stmts {
		if len(s.counts) == 0 {
			out[i] = 1
			continue
		}
		out[i] = s.counts[0]
		s.counts = s.counts[1:]
	}
	return out, nil
}

// countedWriteStmt builds a minimal counted-write statement: the seam reports
// the RETURNed matched count, so the text only needs to be stable.
func countedWriteStmt(cypher string) sourcecypher.Statement {
	return sourcecypher.Statement{Cypher: cypher, Parameters: map[string]any{}}
}

// productionCountedChain builds the production canonical write path the
// code-call EdgeWriter uses: root adapter, instrumented wrapper, gate-bound
// executor, then the (identity, untagged-build) lease-expiry barrier wrap.
// It mirrors ifa_lease_expiry_chain.go's canonical construction exactly.
func productionCountedChain(t *testing.T, session *countedFakeSession, maxInFlight string) sourcecypher.Executor {
	t.Helper()
	root := newReducerNeo4jExecutor(session, nil)
	instrumented := &sourcecypher.InstrumentedExecutor{Inner: root}
	getenv := func(key string) string {
		if key == "ESHU_GRAPH_WRITE_MAX_IN_FLIGHT" {
			return maxInFlight
		}
		return ""
	}
	gate := newReducerGraphWriteGate(getenv, nil)
	var barrier *ifaLeaseExpiryBarrier
	return barrier.wrapGraph(gate.boundExecutor(instrumented))
}

// TestProductionChainPreservesCountedWriteSeam proves the counted-write seam
// survives the whole production canonical chain: the chain exposes both
// counted interfaces, a group write returns the runner's aligned counts with
// exactly one underlying group call, and a single write returns its count.
func TestProductionChainPreservesCountedWriteSeam(t *testing.T) {
	t.Parallel()

	session := &countedFakeSession{counts: []int64{2, 2}}
	executor := productionCountedChain(t, session, "")

	grouped, ok := executor.(sourcecypher.CountedWriteGroupExecutor)
	if !ok {
		t.Fatalf("production chain %T does not implement CountedWriteGroupExecutor: the seam is stripped before EdgeWriter", executor)
	}
	single, ok := executor.(sourcecypher.CountedWriteExecutor)
	if !ok {
		t.Fatalf("production chain %T does not implement CountedWriteExecutor: the seam is stripped before EdgeWriter", executor)
	}

	stmts := []sourcecypher.Statement{countedWriteStmt("UNWIND $rows AS row MATCH (a) WHERE a.id = row.id MERGE (a)-[r:CALLS]->(b) RETURN count(r)"), countedWriteStmt("UNWIND $rows AS row MATCH (a) WHERE a.id = row.id MERGE (a)-[r:CALLS]->(b) RETURN count(r)")}
	counts, err := grouped.ExecuteCountedWriteGroup(context.Background(), stmts)
	if err != nil {
		t.Fatalf("ExecuteCountedWriteGroup() error = %v, want nil", err)
	}
	if len(counts) != 2 || counts[0] != 2 || counts[1] != 2 {
		t.Fatalf("ExecuteCountedWriteGroup() = %v, want [2 2] aligned with the two statements", counts)
	}
	session.mu.Lock()
	groupCalls := session.groupCalls
	session.mu.Unlock()
	if groupCalls != 1 {
		t.Fatalf("underlying group calls = %d, want exactly 1 (one logical group call, retries owned below)", groupCalls)
	}

	count, err := single.ExecuteCountedWrite(context.Background(), countedWriteStmt("UNWIND $rows AS row MATCH (a) WHERE a.id = row.id MERGE (a)-[r:CALLS]->(b) RETURN count(r)"))
	if err != nil {
		t.Fatalf("ExecuteCountedWrite() error = %v, want nil", err)
	}
	if count != 1 {
		t.Fatalf("ExecuteCountedWrite() = %d, want the runner's scripted default 1", count)
	}
}

// TestProductionChainCountedWriteFailsClosedWithoutSeam proves the fail-closed
// direction at the production chain: a runner without the counted capability
// surfaces ErrCountedWriteUnsupported with zero counts, so the writer parks
// instead of completing silently. The wrappers advertise the seam (their
// types forward), but every layer bottoms out at the root and fails.
func TestProductionChainCountedWriteFailsClosedWithoutSeam(t *testing.T) {
	t.Parallel()

	plain := &runCypherOnlySession{}
	root := newReducerNeo4jExecutor(plain, nil)
	instrumented := &sourcecypher.InstrumentedExecutor{Inner: root}
	gate := newReducerGraphWriteGate(func(string) string { return "" }, nil)
	var barrier *ifaLeaseExpiryBarrier
	executor := barrier.wrapGraph(gate.boundExecutor(instrumented))

	grouped, ok := executor.(sourcecypher.CountedWriteGroupExecutor)
	if !ok {
		t.Fatal("chain over a seam-less runner must still expose CountedWriteGroupExecutor (advertise-but-fail-closed)")
	}
	if _, err := grouped.ExecuteCountedWriteGroup(context.Background(), []sourcecypher.Statement{countedWriteStmt("RETURN 1")}); !errors.Is(err, sourcecypher.ErrCountedWriteUnsupported) {
		t.Fatalf("counted group over a seam-less chain err = %v, want ErrCountedWriteUnsupported", err)
	}
	single, ok := executor.(sourcecypher.CountedWriteExecutor)
	if !ok {
		t.Fatal("chain over a seam-less runner must still expose CountedWriteExecutor (advertise-but-fail-closed)")
	}
	if _, err := single.ExecuteCountedWrite(context.Background(), countedWriteStmt("RETURN 1")); !errors.Is(err, sourcecypher.ErrCountedWriteUnsupported) {
		t.Fatalf("counted write over a seam-less chain err = %v, want ErrCountedWriteUnsupported", err)
	}
}
