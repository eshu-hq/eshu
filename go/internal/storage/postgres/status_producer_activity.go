// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
)

func readProducerActivitySnapshot(
	ctx context.Context,
	queryer Queryer,
	asOf time.Time,
) (statuspkg.ProducerActivitySnapshot, error) {
	rows, err := queryer.QueryContext(ctx, producerActivityQuery, asOf)
	if err != nil {
		return statuspkg.ProducerActivitySnapshot{}, fmt.Errorf("read producer activity: %w", err)
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return statuspkg.ProducerActivitySnapshot{}, fmt.Errorf("read producer activity: %w", err)
		}
		return statuspkg.ProducerActivitySnapshot{}, nil
	}

	var hasActiveOrPendingGeneration bool
	var latestGenerationAgeSeconds sql.NullFloat64
	if scanErr := rows.Scan(&hasActiveOrPendingGeneration, &latestGenerationAgeSeconds); scanErr != nil {
		return statuspkg.ProducerActivitySnapshot{}, fmt.Errorf("read producer activity: %w", scanErr)
	}
	if err := rows.Err(); err != nil {
		return statuspkg.ProducerActivitySnapshot{}, fmt.Errorf("read producer activity: %w", err)
	}

	snapshot := statuspkg.ProducerActivitySnapshot{
		HasActiveOrPendingGeneration: hasActiveOrPendingGeneration,
	}
	if latestGenerationAgeSeconds.Valid {
		snapshot.LatestGenerationAge = durationFromSeconds(latestGenerationAgeSeconds.Float64)
	}
	return snapshot, nil
}
