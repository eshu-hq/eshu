// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"time"
)

func (s Service) recordReconcile(ctx context.Context, observation ReconcileObservation) {
	if s.Metrics == nil {
		return
	}
	s.Metrics.RecordReconcile(ctx, observation)
}

func (s Service) recordReap(ctx context.Context, observation ReapObservation) {
	if s.Metrics == nil {
		return
	}
	s.Metrics.RecordReap(ctx, observation)
}

func (s Service) recordRunReconciliation(ctx context.Context, observation RunReconciliationObservation) {
	if s.Metrics == nil {
		return
	}
	s.Metrics.RecordRunReconciliation(ctx, observation)
}

func tickerChan(ticker *time.Ticker) <-chan time.Time {
	if ticker == nil {
		return nil
	}
	return ticker.C
}
