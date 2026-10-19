// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build ifafaultinjection

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

const ifaLeaseExpiryBarrierEnv = "ESHU_IFA_LEASE_EXPIRY_BARRIER"

type ifaLeaseExpiryContextKey struct{}

// ifaLeaseExpiryRecord is the bounded, exact-claim handshake with the shell cell.
type ifaLeaseExpiryRecord struct {
	WorkItemID   string `json:"work_item_id"`
	ClaimedAt    string `json:"claimed_at"`
	AttemptCount int    `json:"attempt_count,omitempty"`
	Domain       string `json:"domain,omitempty"`
	State        string `json:"state,omitempty"`
}

// ifaLeaseExpiryBarrier pauses one direct GCP handler after a committed graph
// call has returned. It never holds a graph permit, driver transaction or queue
// lock while waiting for the test driver's exact release record.
type ifaLeaseExpiryBarrier struct {
	path  string
	fired atomic.Bool
}

// newIfaLeaseExpiryBarrier validates the optional test-only marker path. A bad
// configured path is a startup error instead of silently disabling the cell.
func newIfaLeaseExpiryBarrier(getenv func(string) string) (*ifaLeaseExpiryBarrier, error) {
	raw := getenv(ifaLeaseExpiryBarrierEnv)
	if raw == "" {
		return nil, nil
	}
	path := strings.TrimSpace(raw)
	if path != raw || path == "" {
		return nil, fmt.Errorf("%s must have no surrounding whitespace", ifaLeaseExpiryBarrierEnv)
	}
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, fmt.Errorf("%s must be a clean absolute file path", ifaLeaseExpiryBarrierEnv)
	}
	for _, suffix := range []string{".owner", ".held.json", ".release", ".finished.json"} {
		if _, err := os.Lstat(path + suffix); err == nil {
			return nil, fmt.Errorf("%s: stale barrier file %q", ifaLeaseExpiryBarrierEnv, path+suffix)
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%s: inspect barrier path: %w", ifaLeaseExpiryBarrierEnv, err)
		}
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".ifa-lease-check-*")
	if err != nil {
		return nil, fmt.Errorf("%s: marker directory is not writable: %w", ifaLeaseExpiryBarrierEnv, err)
	}
	_ = os.Remove(file.Name())
	if err := file.Close(); err != nil {
		return nil, fmt.Errorf("%s: close marker check: %w", ifaLeaseExpiryBarrierEnv, err)
	}
	return &ifaLeaseExpiryBarrier{path: path}, nil
}

// wrapReducer attaches only an eligible claimed intent's immutable identity to
// its handler context. The first successful graph operation decides the hold.
func (b *ifaLeaseExpiryBarrier) wrapReducer(inner reducer.Executor) reducer.Executor {
	if b == nil {
		return inner
	}
	return ifaLeaseExpiryReducerExecutor{inner: inner}
}

type ifaLeaseExpiryReducerExecutor struct {
	inner reducer.Executor
}

func (e ifaLeaseExpiryReducerExecutor) Execute(ctx context.Context, intent reducercontract.Intent) (reducer.Result, error) {
	if intent.Domain != reducer.DomainGCPResourceMaterialization || intent.IntentID == "" || intent.ClaimedAt == nil || intent.ClaimedAt.IsZero() || intent.AttemptCount < 1 {
		return e.inner.Execute(ctx, intent)
	}
	record := ifaLeaseExpiryRecord{
		WorkItemID: intent.IntentID, ClaimedAt: intent.ClaimedAt.UTC().Format(time.RFC3339Nano),
		AttemptCount: intent.AttemptCount, Domain: string(intent.Domain),
	}
	return e.inner.Execute(context.WithValue(ctx, ifaLeaseExpiryContextKey{}, record), intent)
}

// wrapGraph keeps the source executor's advertised Group/Probe capabilities.
// Production's grouped/probe-capable canonical chain uses the combined adapter.
func (b *ifaLeaseExpiryBarrier) wrapGraph(inner sourcecypher.Executor) sourcecypher.Executor {
	if b == nil {
		return inner
	}
	base := ifaLeaseExpiryGraphExecutor{inner: inner, barrier: b}
	_, group := inner.(sourcecypher.GroupExecutor)
	_, probe := inner.(sourcecypher.ProbeExecutor)
	switch {
	case group && probe:
		return ifaLeaseExpiryGraphGroupProbeExecutor{ifaLeaseExpiryGraphGroupExecutor{base}}
	case group:
		return ifaLeaseExpiryGraphGroupExecutor{base}
	case probe:
		return ifaLeaseExpiryGraphProbeExecutor{base}
	default:
		return base
	}
}

type ifaLeaseExpiryGraphExecutor struct {
	inner   sourcecypher.Executor
	barrier *ifaLeaseExpiryBarrier
}

func (e ifaLeaseExpiryGraphExecutor) Execute(ctx context.Context, stmt sourcecypher.Statement) error {
	if err := e.inner.Execute(ctx, stmt); err != nil {
		return err
	}
	return e.barrier.afterSuccess(ctx)
}

type ifaLeaseExpiryGraphGroupExecutor struct{ ifaLeaseExpiryGraphExecutor }

func (e ifaLeaseExpiryGraphGroupExecutor) ExecuteGroup(ctx context.Context, stmts []sourcecypher.Statement) error {
	if err := e.inner.(sourcecypher.GroupExecutor).ExecuteGroup(ctx, stmts); err != nil {
		return err
	}
	return e.barrier.afterSuccess(ctx)
}

type ifaLeaseExpiryGraphProbeExecutor struct{ ifaLeaseExpiryGraphExecutor }

func (e ifaLeaseExpiryGraphProbeExecutor) ExecuteProbe(ctx context.Context, stmt sourcecypher.Statement) (bool, error) {
	return e.inner.(sourcecypher.ProbeExecutor).ExecuteProbe(ctx, stmt)
}

type ifaLeaseExpiryGraphGroupProbeExecutor struct {
	ifaLeaseExpiryGraphGroupExecutor
}

func (e ifaLeaseExpiryGraphGroupProbeExecutor) ExecuteGroup(ctx context.Context, stmts []sourcecypher.Statement) error {
	return e.ifaLeaseExpiryGraphGroupExecutor.ExecuteGroup(ctx, stmts)
}

func (e ifaLeaseExpiryGraphGroupProbeExecutor) ExecuteProbe(ctx context.Context, stmt sourcecypher.Statement) (bool, error) {
	return e.inner.(sourcecypher.ProbeExecutor).ExecuteProbe(ctx, stmt)
}

// afterSuccess is a one-shot rendezvous; the CAS occurs after the real graph
// write returns and no mutex is held while waiting for the shell release.
func (b *ifaLeaseExpiryBarrier) afterSuccess(ctx context.Context) error {
	record, ok := ctx.Value(ifaLeaseExpiryContextKey{}).(ifaLeaseExpiryRecord)
	if !ok || !b.fired.CompareAndSwap(false, true) {
		return nil
	}
	// More than one reducer process can share the cell's path. The exclusive
	// owner file extends the one-shot CAS across processes without holding an
	// OS lock while the handler waits. A crashed winner leaves the cell failed
	// closed (no held marker or terminal proof), never a second paused owner.
	owner, err := os.OpenFile(b.path+".owner", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("claim ifa lease-expiry barrier: %w", err)
	}
	if err := owner.Close(); err != nil {
		return fmt.Errorf("close ifa lease-expiry owner: %w", err)
	}
	if err := writeIfaLeaseExpiryRecord(b.path+".held.json", record); err != nil {
		return fmt.Errorf("publish ifa lease-expiry hold: %w", err)
	}
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			record.State = "cancelled"
			if errors.Is(err, context.DeadlineExceeded) {
				record.State = "timeout"
			}
			if finishErr := writeIfaLeaseExpiryRecord(b.path+".finished.json", record); finishErr != nil {
				return fmt.Errorf("ifa lease-expiry cancelled: %w; publish terminal state: %v", err, finishErr)
			}
			return err
		}
		release, err := os.ReadFile(b.path + ".release")
		if err == nil {
			var matching ifaLeaseExpiryRecord
			if err := json.Unmarshal(release, &matching); err != nil {
				record.State = "invalid_release"
				if finishErr := writeIfaLeaseExpiryRecord(b.path+".finished.json", record); finishErr != nil {
					return fmt.Errorf("invalid ifa lease-expiry release: %w; publish terminal state: %v", err, finishErr)
				}
				return fmt.Errorf("invalid ifa lease-expiry release: %w", err)
			}
			if matching.WorkItemID == record.WorkItemID && matching.ClaimedAt == record.ClaimedAt {
				if err := ctx.Err(); err != nil {
					continue
				}
				record.State = "released"
				if err := writeIfaLeaseExpiryRecord(b.path+".finished.json", record); err != nil {
					return fmt.Errorf("publish ifa lease-expiry release: %w", err)
				}
				return nil
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("read ifa lease-expiry release: %w", err)
		}
		select {
		case <-ctx.Done():
		case <-ticker.C:
		}
	}
}

// writeIfaLeaseExpiryRecord uses rename so the shell never sees partial JSON.
func writeIfaLeaseExpiryRecord(path string, record ifaLeaseExpiryRecord) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".ifa-lease-record-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if err := json.NewEncoder(file).Encode(record); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}
