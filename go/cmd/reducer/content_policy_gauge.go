// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/telemetry/snapshot"
)

const (
	// gaugeContentPolicyRawRows is the eshu_dp_gauge_snapshot_* label of the
	// #8833 exposure gauge's snapshot source.
	gaugeContentPolicyRawRows = "reducer_content_policy_raw_rows"
	contentPolicyRawRowsKey   = "raw_rows"
)

// cachedContentPolicyRawRowsObserver serves eshu_dp_content_policy_raw_rows
// from a snapshot. Before the first successful read it errors rather than
// report a zero, because zero is the cleanup runbook's exit signal.
type cachedContentPolicyRawRowsObserver struct{ source *snapshot.Source }

func (c cachedContentPolicyRawRowsObserver) ContentPolicyRawRows(ctx context.Context) (int64, error) {
	counts, err := c.source.Counts(ctx)
	if err != nil {
		return 0, err
	}
	rows, ok := counts[contentPolicyRawRowsKey]
	if !ok {
		return 0, errPostgresGaugeSnapshotNotReady
	}
	return rows, nil
}

// registerContentPolicyRawRowsGauge feeds eshu_dp_content_policy_raw_rows
// (#8833) from the Postgres gauge refresher, so the side-table count runs on
// the refresher's goroutine and a scrape serves the last snapshot. A nil
// observer (a registry that governs no path) registers nothing.
func registerContentPolicyRawRowsGauge(
	refresher *snapshot.Refresher,
	instruments *telemetry.Instruments,
	meter metric.Meter,
	observer telemetry.ContentPolicyRawRowsObserver,
) error {
	if observer == nil {
		return nil
	}
	if refresher == nil {
		return errors.New("the content policy raw rows gauge needs the postgres gauge refresher")
	}
	source, err := refresher.Register(gaugeContentPolicyRawRows, func(ctx context.Context) (map[string]int64, error) {
		rows, err := observer.ContentPolicyRawRows(ctx)
		if err != nil {
			return nil, err
		}
		return map[string]int64{contentPolicyRawRowsKey: rows}, nil
	})
	if err != nil {
		return fmt.Errorf("register content policy raw rows snapshot source: %w", err)
	}
	if err := telemetry.RegisterContentPolicyRawRowsObservableGauge(instruments, meter, cachedContentPolicyRawRowsObserver{source: source}); err != nil {
		return fmt.Errorf("register content policy raw rows observable gauge: %w", err)
	}
	return nil
}

// contentPolicyRawRowsObserverFor returns the eshu_dp_content_policy_raw_rows
// store for the default registry's governed paths (#8833), or nil when the
// registry governs none.
func contentPolicyRawRowsObserverFor(database *sql.DB) telemetry.ContentPolicyRawRowsObserver {
	pattern := parser.DefaultRegistry().ContentStorePathPattern()
	if pattern == "" {
		return nil
	}
	return postgres.NewContentPolicyRawRowsStore(postgres.SQLQueryer{DB: database}, pattern)
}
