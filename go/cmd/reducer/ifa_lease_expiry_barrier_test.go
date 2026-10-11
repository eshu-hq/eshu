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

func (s leaseBarrierReducerStub) Execute(ctx context.Context, _ reducer.Intent) (reducer.Result, error) {
	group := s.graph.(sourcecypher.GroupExecutor)
	return reducer.Result{}, group.ExecuteGroup(ctx, []sourcecypher.Statement{{Operation: sourcecypher.OperationCanonicalUpsert}})
}

func leaseBarrierIntent(id string, attempt int, epoch time.Time) reducer.Intent {
	return reducer.Intent{IntentID: id, Domain: reducer.DomainGCPResourceMaterialization, AttemptCount: attempt, ClaimedAt: &epoch}
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

func assertLeaseBarrierTerminal(t *testing.T, path string, intent reducer.Intent, state string) {
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
		name  string
		inner sourcecypher.Executor
		group bool
		probe bool
	}{
		{"execute-only", leaseBarrierExecuteOnlyStub{}, false, false},
		{"group-only", leaseBarrierGroupOnlyStub{}, true, false},
		{"probe-only", leaseBarrierProbeOnlyStub{}, false, true},
		{"group-and-probe", &leaseBarrierGraphStub{}, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wrapped := barrier.wrapGraph(tc.inner)
			_, group := wrapped.(sourcecypher.GroupExecutor)
			_, probe := wrapped.(sourcecypher.ProbeExecutor)
			_, phase := wrapped.(sourcecypher.PhaseGroupExecutor)
			if group != tc.group || probe != tc.probe || phase {
				t.Fatalf("capabilities group=%t probe=%t phase=%t, want %t %t false", group, probe, phase, tc.group, tc.probe)
			}
		})
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
