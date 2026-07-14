// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build ifarepodependencyproof

package offlinetier_test

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/relationships"
)

func repoDependencyConcurrencyProofEnabled(t *testing.T) bool {
	t.Helper()
	if strings.TrimSpace(os.Getenv(repoDependencyConcurrencyLiveEnv)) != "1" {
		t.Skipf("set %s=1 to run the Ifa repo-dependency concurrency proof", repoDependencyConcurrencyLiveEnv)
		return false
	}
	if !liveTierEnabled() {
		t.Skipf("set %s=1 and real NornicDB connection variables", liveTierEnv)
		return false
	}
	return true
}

type repoDependencyIfaEvidenceLoader struct {
	evidence []relationships.EvidenceFact
}

func (l repoDependencyIfaEvidenceLoader) ListEvidenceFacts(
	_ context.Context,
	_ string,
) ([]relationships.EvidenceFact, error) {
	return append([]relationships.EvidenceFact(nil), l.evidence...), nil
}

type repoDependencyIfaIntentCapture struct {
	rows []reducer.SharedProjectionIntentRow
}

type repoDependencyOverlapWriter struct {
	inner   reducer.SharedProjectionEdgeWriter
	delay   time.Duration
	mu      sync.Mutex
	current int
	max     int
}

func (w *repoDependencyOverlapWriter) RetractEdges(
	ctx context.Context,
	domain string,
	rows []reducer.SharedProjectionIntentRow,
	evidenceSource string,
) error {
	return w.inner.RetractEdges(ctx, domain, rows, evidenceSource)
}

func (w *repoDependencyOverlapWriter) WriteEdges(
	ctx context.Context,
	domain string,
	rows []reducer.SharedProjectionIntentRow,
	evidenceSource string,
) error {
	w.mu.Lock()
	w.current++
	if w.current > w.max {
		w.max = w.current
	}
	w.mu.Unlock()
	defer func() {
		w.mu.Lock()
		w.current--
		w.mu.Unlock()
	}()
	if w.delay > 0 {
		timer := time.NewTimer(w.delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
	}
	return w.inner.WriteEdges(ctx, domain, rows, evidenceSource)
}

func (w *repoDependencyOverlapWriter) maxConcurrent() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.max
}

type repoDependencyIfaReplayRequest struct {
	scopeID      string
	generationID string
	entityKey    string
}

type repoDependencyIfaReplayer struct {
	mu       sync.Mutex
	requests []repoDependencyIfaReplayRequest
}

func (r *repoDependencyIfaReplayer) ReplayWorkloadMaterialization(
	_ context.Context,
	scopeID string,
	generationID string,
	entityKey string,
) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.requests = append(r.requests, repoDependencyIfaReplayRequest{
		scopeID:      scopeID,
		generationID: generationID,
		entityKey:    entityKey,
	})
	return true, nil
}

func (r *repoDependencyIfaReplayer) requestCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.requests)
}

func (r *repoDependencyIfaReplayer) sharedTargetRequestCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	count := 0
	for _, request := range r.requests {
		if request.entityKey == "repo:target-hub" {
			count++
		}
	}
	return count
}

func (c *repoDependencyIfaIntentCapture) UpsertIntents(
	_ context.Context,
	rows []reducer.SharedProjectionIntentRow,
) error {
	c.rows = append(c.rows, rows...)
	return nil
}
