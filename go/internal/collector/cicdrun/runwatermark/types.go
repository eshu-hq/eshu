// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runwatermark

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrStaleFence reports that a watermark write came from an older claim
// fencing token than the row already stored for that scope+repository.
var ErrStaleFence = errors.New("ci/cd run watermark stale fence")

// Key identifies one polled GitHub Actions target: the ingestion scope and
// repository the ghactionsruntime source fetches a bounded run window for.
// One Key maps to exactly one watermark row; there is no sub-resource
// dimension the way awscloud/checkpoint.Key has ResourceParent, because a
// GitHub Actions target has exactly one runs listing to track.
type Key struct {
	ScopeID    string
	Repository string
}

// Watermark is the persisted marker recording the newest provider run ID a
// claim observed for one Key, fenced by the claim that wrote it.
//
// It exists to DETECT (not resume) a cross-cycle collection gap: each claim
// fetches a bounded, stateless window of the most recent runs, so when more
// than max_runs runs land between two claim cycles, the fetched window's
// oldest run can be newer than Watermark.LastRunID -- meaning every run
// between them was never fetched by either cycle. See ghactionsruntime's
// run_watermark.go for the detection logic that reads this value.
type Watermark struct {
	Key          Key
	LastRunID    string
	GenerationID string
	FencingToken int64
	UpdatedAt    time.Time
}

// Store persists claim-fenced run watermarks for the ghactionsruntime
// source. A Save carrying a fencing token older than the stored row is
// rejected with ErrStaleFence so a superseded claim retry (or an
// out-of-order redelivery) cannot regress the watermark past a newer
// claim's progress. A Save carrying the SAME fencing token as the stored
// row succeeds (idempotent redelivery of an already-applied claim).
type Store interface {
	Load(context.Context, Key) (Watermark, bool, error)
	Save(context.Context, Watermark) error
}

// Validate rejects an incomplete key before it reaches storage.
func (k Key) Validate() error {
	if strings.TrimSpace(k.ScopeID) == "" {
		return fmt.Errorf("ci/cd run watermark scope_id is required")
	}
	if strings.TrimSpace(k.Repository) == "" {
		return fmt.Errorf("ci/cd run watermark repository is required")
	}
	return nil
}

// Validate rejects an incomplete watermark before it reaches storage.
func (w Watermark) Validate() error {
	if err := w.Key.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(w.LastRunID) == "" {
		return fmt.Errorf("ci/cd run watermark last_run_id is required")
	}
	if strings.TrimSpace(w.GenerationID) == "" {
		return fmt.Errorf("ci/cd run watermark generation_id is required")
	}
	if w.FencingToken <= 0 {
		return fmt.Errorf("ci/cd run watermark fencing_token must be positive")
	}
	return nil
}
