// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build ifafaultinjection

package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/graphbackpressure"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

type leaseBarrierGraphStub struct {
	calls atomic.Int32
	fail  bool
}

type leaseBarrierExecuteOnlyStub struct{}

func (leaseBarrierExecuteOnlyStub) Execute(context.Context, sourcecypher.Statement) error { return nil }

type leaseBarrierGroupOnlyStub struct{ leaseBarrierExecuteOnlyStub }

func (leaseBarrierGroupOnlyStub) ExecuteGroup(context.Context, []sourcecypher.Statement) error {
	return nil
}

type leaseBarrierProbeOnlyStub struct{ leaseBarrierExecuteOnlyStub }

func (leaseBarrierProbeOnlyStub) ExecuteProbe(context.Context, sourcecypher.Statement) (bool, error) {
	return true, nil
}

// leaseBarrierCountedOnlyStub implements only the counted-write seam: it
// proves wrapGraph advertises counted without requiring Group or Probe.
type leaseBarrierCountedOnlyStub struct{ leaseBarrierExecuteOnlyStub }

func (leaseBarrierCountedOnlyStub) ExecuteCountedWrite(context.Context, sourcecypher.Statement) (int64, error) {
	return 7, nil
}

func (leaseBarrierCountedOnlyStub) ExecuteCountedWriteGroup(_ context.Context, stmts []sourcecypher.Statement) ([]int64, error) {
	out := make([]int64, len(stmts))
	for i := range stmts {
		out[i] = 7
	}
	return out, nil
}

type leaseBarrierGroupCountedStub struct {
	leaseBarrierGroupOnlyStub
	countedCalls atomic.Int32
}

func (s *leaseBarrierGroupCountedStub) ExecuteCountedWrite(context.Context, sourcecypher.Statement) (int64, error) {
	s.countedCalls.Add(1)
	return 7, nil
}

func (s *leaseBarrierGroupCountedStub) ExecuteCountedWriteGroup(_ context.Context, stmts []sourcecypher.Statement) ([]int64, error) {
	s.countedCalls.Add(1)
	out := make([]int64, len(stmts))
	for i := range stmts {
		out[i] = 7
	}
	return out, nil
}

type leaseBarrierProbeCountedStub struct{ leaseBarrierProbeOnlyStub }

func (leaseBarrierProbeCountedStub) ExecuteCountedWrite(context.Context, sourcecypher.Statement) (int64, error) {
	return 7, nil
}

func (leaseBarrierProbeCountedStub) ExecuteCountedWriteGroup(_ context.Context, stmts []sourcecypher.Statement) ([]int64, error) {
	out := make([]int64, len(stmts))
	for i := range stmts {
		out[i] = 7
	}
	return out, nil
}

type leaseBarrierFullCountedStub struct {
	*leaseBarrierGraphStub
	countedCalls atomic.Int32
}

func (s *leaseBarrierFullCountedStub) ExecuteCountedWrite(context.Context, sourcecypher.Statement) (int64, error) {
	s.countedCalls.Add(1)
	if s.leaseBarrierGraphStub.fail {
		return 0, errors.New("graph write failed")
	}
	return 7, nil
}

func (s *leaseBarrierFullCountedStub) ExecuteCountedWriteGroup(_ context.Context, stmts []sourcecypher.Statement) ([]int64, error) {
	s.countedCalls.Add(1)
	if s.leaseBarrierGraphStub.fail {
		return nil, errors.New("graph write failed")
	}
	out := make([]int64, len(stmts))
	for i := range stmts {
		out[i] = 7
	}
	return out, nil
}

func (s *leaseBarrierGraphStub) Execute(context.Context, sourcecypher.Statement) error {
	s.calls.Add(1)
	if s.fail {
		return errors.New("graph write failed")
	}
	return nil
}

func (s *leaseBarrierGraphStub) ExecuteGroup(ctx context.Context, _ []sourcecypher.Statement) error {
	return s.Execute(ctx, sourcecypher.Statement{})
}

func (s *leaseBarrierGraphStub) ExecuteProbe(context.Context, sourcecypher.Statement) (bool, error) {
	return true, nil
}

type leaseBarrierReducerStub struct{ graph sourcecypher.Executor }

func (s leaseBarrierReducerStub) Execute(ctx context.Context, _ reducercontract.Intent) (reducercontract.Result, error) {
	group := s.graph.(sourcecypher.GroupExecutor)
	return reducercontract.Result{}, group.ExecuteGroup(ctx, []sourcecypher.Statement{{Operation: sourcecypher.OperationCanonicalUpsert}})
}

func leaseBarrierIntent(id string, attempt int, epoch time.Time) reducercontract.Intent {
	return reducercontract.Intent{IntentID: id, Domain: reducer.DomainGCPResourceMaterialization, AttemptCount: attempt, ClaimedAt: &epoch}
}

func awaitLeaseBarrierHeld(t *testing.T, path string) ifaLeaseExpiryRecord {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		data, err := os.ReadFile(path + ".held.json")
		if err == nil {
			var record ifaLeaseExpiryRecord
			if err := json.Unmarshal(data, &record); err != nil {
				t.Fatalf("decode held marker: %v", err)
			}
			return record
		}
		if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("read held marker: %v", err)
		}
		select {
		case <-deadline:
			t.Fatal("held marker was not published")
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func TestIfaLeaseExpiryBarrierHoldsOnlyFirstDirectClaimAfterGraphSuccess(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lease")
	barrier, err := newIfaLeaseExpiryBarrier(func(name string) string {
		if name == ifaLeaseExpiryBarrierEnv {
			return path
		}
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
	graph := &leaseBarrierGraphStub{}
	gate := newReducerGraphWriteGate(func(name string) string {
		if name == graphbackpressure.CanonicalMaxInFlightEnv {
			return "1"
		}
		return ""
	}, nil)
	wrapped := barrier.wrapGraph(gate.boundExecutor(graph))
	if _, ok := wrapped.(sourcecypher.GroupExecutor); !ok {
		t.Fatal("group capability lost")
	}
	if _, ok := wrapped.(sourcecypher.ProbeExecutor); !ok {
		t.Fatal("probe capability lost")
	}
	inner := leaseBarrierReducerStub{graph: wrapped}
	runtime := barrier.wrapReducer(inner)
	epoch := time.Now().UTC()
	first := leaseBarrierIntent("target", 1, epoch)
	firstDone := make(chan error, 1)
	go func() { _, err := runtime.Execute(context.Background(), first); firstDone <- err }()
	held := awaitLeaseBarrierHeld(t, path)
	if held.WorkItemID != first.IntentID || held.AttemptCount != 1 || held.ClaimedAt != epoch.Format(time.RFC3339Nano) || held.Domain != string(first.Domain) {
		t.Fatalf("held claim = %+v, want %+v", held, first)
	}
	if got := gate.canonicalGate.InFlight(); got != 0 {
		t.Fatalf("held handler retains %d graph permits", got)
	}
	select {
	case err := <-firstDone:
		t.Fatalf("first handler returned while held: %v", err)
	default:
	}
	second := leaseBarrierIntent("target", 2, epoch.Add(time.Microsecond))
	if _, err := runtime.Execute(context.Background(), second); err != nil {
		t.Fatalf("successor blocked/failed: %v", err)
	}
	if _, err := runtime.Execute(context.Background(), leaseBarrierIntent("unrelated", 1, epoch)); err != nil {
		t.Fatalf("unrelated blocked/failed: %v", err)
	}
	if err := os.WriteFile(path+".release", []byte(`{"work_item_id":"wrong","claimed_at":"wrong"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-firstDone:
		t.Fatalf("wrong release unblocked handler: %v", err)
	default:
	}
	release, _ := json.Marshal(ifaLeaseExpiryRecord{WorkItemID: held.WorkItemID, ClaimedAt: held.ClaimedAt})
	if err := os.WriteFile(path+".release", release, 0o600); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-firstDone:
		if err != nil {
			t.Fatalf("released handler: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("matching release did not finish")
	}
	data, err := os.ReadFile(path + ".finished.json")
	if err != nil {
		t.Fatal(err)
	}
	var finished ifaLeaseExpiryRecord
	if err := json.Unmarshal(data, &finished); err != nil || finished.State != "released" {
		t.Fatalf("finished = %+v, decode error %v", finished, err)
	}
}

func TestIfaLeaseExpiryBarrierRejectsFailedWriteAndCancellation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		fail   bool
		cancel bool
	}{{"failed-write", true, false}, {"cancel", false, true}} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "lease")
			barrier, err := newIfaLeaseExpiryBarrier(func(string) string { return path })
			if err != nil {
				t.Fatal(err)
			}
			graph := &leaseBarrierGraphStub{fail: tc.fail}
			runtime := barrier.wrapReducer(leaseBarrierReducerStub{graph: barrier.wrapGraph(graph)})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			intent := leaseBarrierIntent("target", 1, time.Now().UTC())
			done := make(chan error, 1)
			go func() { _, err := runtime.Execute(ctx, intent); done <- err }()
			if tc.cancel {
				awaitLeaseBarrierHeld(t, path)
				cancel()
			}
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("expected graph failure or cancellation")
				}
			case <-time.After(5 * time.Second):
				t.Fatal("handler did not finish")
			}
			_, err = os.Stat(path + ".held.json")
			if tc.fail && !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("failed write published held marker: %v", err)
			}
			if tc.cancel {
				assertLeaseBarrierTerminal(t, path, intent, "cancelled")
			}
		})
	}
}

func assertLeaseBarrierTerminal(t *testing.T, path string, intent reducercontract.Intent, state string) {
	t.Helper()
	data, err := os.ReadFile(path + ".finished.json")
	if err != nil {
		t.Fatal(err)
	}
	var record ifaLeaseExpiryRecord
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatal(err)
	}
	if record.State != state || record.WorkItemID != intent.IntentID || record.ClaimedAt != intent.ClaimedAt.UTC().Format(time.RFC3339Nano) || record.AttemptCount != intent.AttemptCount || record.Domain != string(intent.Domain) {
		t.Fatalf("terminal record = %+v, want state %q and claim %+v", record, state, intent)
	}
}

func TestIfaLeaseExpiryBarrierDeadlinePublishesTerminalClaim(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lease")
	barrier, err := newIfaLeaseExpiryBarrier(func(string) string { return path })
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	intent := leaseBarrierIntent("deadline", 1, time.Now().UTC())
	runtime := barrier.wrapReducer(leaseBarrierReducerStub{graph: barrier.wrapGraph(&leaseBarrierGraphStub{})})
	_, err = runtime.Execute(ctx, intent)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("handler error = %v, want deadline exceeded", err)
	}
	assertLeaseBarrierTerminal(t, path, intent, "timeout")
}

func TestIfaLeaseExpiryBarrierRejectsInvalidConfiguration(t *testing.T) {
	for _, path := range []string{"relative/path", " ", filepath.Join(t.TempDir(), "missing", "lease")} {
		if _, err := newIfaLeaseExpiryBarrier(func(string) string { return path }); err == nil {
			t.Fatalf("accepted invalid marker path %q", path)
		}
	}
	path := filepath.Join(t.TempDir(), "lease")
	if err := os.WriteFile(path+".held.json", []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := newIfaLeaseExpiryBarrier(func(string) string { return path }); err == nil {
		t.Fatal("accepted stale held marker")
	}
	ownerPath := filepath.Join(t.TempDir(), "lease")
	if err := os.WriteFile(ownerPath+".owner", nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := newIfaLeaseExpiryBarrier(func(string) string { return ownerPath }); err == nil {
		t.Fatal("accepted stale barrier owner")
	}
}

func TestIfaLeaseExpiryBarrierProbeDoesNotHold(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lease")
	barrier, err := newIfaLeaseExpiryBarrier(func(string) string { return path })
	if err != nil {
		t.Fatal(err)
	}
	graph := barrier.wrapGraph(&leaseBarrierGraphStub{})
	if _, ok := graph.(sourcecypher.PhaseGroupExecutor); ok {
		t.Fatal("wrapper advertised an unsupported phase group")
	}
	ctx := context.WithValue(context.Background(), ifaLeaseExpiryContextKey{}, ifaLeaseExpiryRecord{
		WorkItemID: "target", ClaimedAt: time.Now().UTC().Format(time.RFC3339Nano), AttemptCount: 1,
		Domain: string(reducer.DomainGCPResourceMaterialization),
	})
	if found, err := graph.(sourcecypher.ProbeExecutor).ExecuteProbe(ctx, sourcecypher.Statement{}); err != nil || !found {
		t.Fatalf("probe passthrough = %v, %v", found, err)
	}
	if _, err := os.Stat(path + ".held.json"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("probe published held marker: %v", err)
	}
}

func TestIfaLeaseExpiryBarrierPreservesOptionalCapabilities(t *testing.T) {
	barrier, err := newIfaLeaseExpiryBarrier(func(string) string { return filepath.Join(t.TempDir(), "lease") })
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		inner   sourcecypher.Executor
		group   bool
		probe   bool
		counted bool
	}{
		{"execute-only", leaseBarrierExecuteOnlyStub{}, false, false, false},
		{"group-only", leaseBarrierGroupOnlyStub{}, true, false, false},
		{"probe-only", leaseBarrierProbeOnlyStub{}, false, true, false},
		{"group-and-probe", &leaseBarrierGraphStub{}, true, true, false},
		{"counted-only", leaseBarrierCountedOnlyStub{}, false, false, true},
		{"group-and-counted", &leaseBarrierGroupCountedStub{}, true, false, true},
		{"probe-and-counted", leaseBarrierProbeCountedStub{}, false, true, true},
		{"group-probe-and-counted", &leaseBarrierFullCountedStub{leaseBarrierGraphStub: &leaseBarrierGraphStub{}}, true, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wrapped := barrier.wrapGraph(tc.inner)
			_, group := wrapped.(sourcecypher.GroupExecutor)
			_, probe := wrapped.(sourcecypher.ProbeExecutor)
			_, phase := wrapped.(sourcecypher.PhaseGroupExecutor)
			_, counted := wrapped.(sourcecypher.CountedWriteExecutor)
			_, countedGroup := wrapped.(sourcecypher.CountedWriteGroupExecutor)
			if group != tc.group || probe != tc.probe || phase || counted != tc.counted || countedGroup != tc.counted {
				t.Fatalf("capabilities group=%t probe=%t phase=%t counted=%t/%t, want %t %t false %t %t",
					group, probe, phase, counted, countedGroup, tc.group, tc.probe, tc.counted, tc.counted)
			}
		})
	}
}

// TestIfaLeaseExpiryBarrierCountedWriteHoldsOnceAfterInnerSuccess proves the
// counted seam runs the lease hold exactly like ExecuteGroup: one inner call,
// then a hold that resolves only on the matching release, with the counts
// delivered after the release.
func TestIfaLeaseExpiryBarrierCountedWriteHoldsOnceAfterInnerSuccess(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lease")
	barrier, err := newIfaLeaseExpiryBarrier(func(name string) string {
		if name == ifaLeaseExpiryBarrierEnv {
			return path
		}
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
	inner := &leaseBarrierFullCountedStub{leaseBarrierGraphStub: &leaseBarrierGraphStub{}}
	wrapped := barrier.wrapGraph(inner)
	grouped, ok := wrapped.(sourcecypher.CountedWriteGroupExecutor)
	if !ok {
		t.Fatal("full counted inner did not expose CountedWriteGroupExecutor")
	}
	epoch := time.Now().UTC()
	ctx := context.WithValue(context.Background(), ifaLeaseExpiryContextKey{}, ifaLeaseExpiryRecord{
		WorkItemID: "counted-target", ClaimedAt: epoch.Format(time.RFC3339Nano), AttemptCount: 1,
		Domain: string(reducer.DomainGCPResourceMaterialization),
	})
	type result struct {
		counts []int64
		err    error
	}
	done := make(chan result, 1)
	stmts := []sourcecypher.Statement{{Operation: sourcecypher.OperationCanonicalUpsert}}
	go func() { counts, err := grouped.ExecuteCountedWriteGroup(ctx, stmts); done <- result{counts, err} }()
	held := awaitLeaseBarrierHeld(t, path)
	if held.WorkItemID != "counted-target" {
		t.Fatalf("held claim = %+v, want counted-target", held)
	}
	select {
	case r := <-done:
		t.Fatalf("counted handler returned while held: %v", r.err)
	default:
	}
	release, _ := json.Marshal(ifaLeaseExpiryRecord{WorkItemID: held.WorkItemID, ClaimedAt: held.ClaimedAt})
	if err := os.WriteFile(path+".release", release, 0o600); err != nil {
		t.Fatal(err)
	}
	select {
	case r := <-done:
		if r.err != nil {
			t.Fatalf("released counted handler: %v", r.err)
		}
		if len(r.counts) != 1 || r.counts[0] != 7 {
			t.Fatalf("counts = %v, want [7] from the single inner group call", r.counts)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("matching release did not finish the counted call")
	}
	if got := inner.countedCalls.Load(); got != 1 {
		t.Fatalf("inner counted group calls = %d, want exactly 1", got)
	}
}

// TestIfaLeaseExpiryBarrierCountedWriteSkipsHoldOnInnerError proves a failed
// inner write never reaches the hold: the error propagates and no marker is
// published.
func TestIfaLeaseExpiryBarrierCountedWriteSkipsHoldOnInnerError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lease")
	barrier, err := newIfaLeaseExpiryBarrier(func(name string) string {
		if name == ifaLeaseExpiryBarrierEnv {
			return path
		}
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
	inner := &leaseBarrierFullCountedStub{leaseBarrierGraphStub: &leaseBarrierGraphStub{fail: true}}
	wrapped := barrier.wrapGraph(inner)
	grouped, ok := wrapped.(sourcecypher.CountedWriteGroupExecutor)
	if !ok {
		t.Fatal("full counted inner did not expose CountedWriteGroupExecutor")
	}
	epoch := time.Now().UTC()
	ctx := context.WithValue(context.Background(), ifaLeaseExpiryContextKey{}, ifaLeaseExpiryRecord{
		WorkItemID: "counted-fail", ClaimedAt: epoch.Format(time.RFC3339Nano), AttemptCount: 1,
		Domain: string(reducer.DomainGCPResourceMaterialization),
	})
	if _, err := grouped.ExecuteCountedWriteGroup(ctx, []sourcecypher.Statement{{}}); err == nil {
		t.Fatal("counted group over a failing inner returned nil error")
	}
	if _, err := os.Stat(path + ".held.json"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed counted write published a hold marker: %v", err)
	}
}

func TestIfaLeaseExpiryBarrierSingleOwnerAcrossInstances(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lease")
	getenv := func(string) string { return path }
	first, err := newIfaLeaseExpiryBarrier(getenv)
	if err != nil {
		t.Fatal(err)
	}
	second, err := newIfaLeaseExpiryBarrier(getenv)
	if err != nil {
		t.Fatal(err)
	}
	firstExec := first.wrapReducer(leaseBarrierReducerStub{graph: first.wrapGraph(&leaseBarrierGraphStub{})})
	secondExec := second.wrapReducer(leaseBarrierReducerStub{graph: second.wrapGraph(&leaseBarrierGraphStub{})})
	epoch := time.Now().UTC()
	done := make(chan error, 2)
	go func() {
		_, err := firstExec.Execute(context.Background(), leaseBarrierIntent("first", 1, epoch))
		done <- err
	}()
	go func() {
		_, err := secondExec.Execute(context.Background(), leaseBarrierIntent("second", 1, epoch))
		done <- err
	}()
	held := awaitLeaseBarrierHeld(t, path)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("loser returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("loser remained blocked behind another process's barrier")
	}
	select {
	case err := <-done:
		t.Fatalf("winner returned before release: %v", err)
	default:
	}
	release, err := json.Marshal(ifaLeaseExpiryRecord{WorkItemID: held.WorkItemID, ClaimedAt: held.ClaimedAt})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".release", release, 0o600); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("winner release: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("winner did not release")
	}
}
