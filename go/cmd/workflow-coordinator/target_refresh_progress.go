// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	vulnerabilitystore "github.com/eshu-hq/eshu/go/internal/storage/postgres/vulnerability"
)

// targetRefreshProgressStore serves the coordinator's per-target refresh
// probe and post-admission record from the progress table (eshu-hq/eshu#9879).
type targetRefreshProgressStore struct {
	database db.ExecQueryer
}

func (s targetRefreshProgressStore) ReadTargetRefreshProgress(ctx context.Context, collectorInstanceID string) (map[string]time.Time, error) {
	return vulnerabilitystore.NewTargetRefreshProgressStore(s.database).ReadTargetRefreshProgress(ctx, collectorInstanceID)
}

func (s targetRefreshProgressStore) RecordTargetsRefreshPlanned(ctx context.Context, collectorInstanceID string, scopeIDs []string, plannedAt time.Time) error {
	return vulnerabilitystore.NewTargetRefreshProgressStore(s.database).RecordTargetsRefreshPlanned(ctx, collectorInstanceID, scopeIDs, plannedAt)
}
