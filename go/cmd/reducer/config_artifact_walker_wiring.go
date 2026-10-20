// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq
//
// Config-artifact walker wiring (#8155 PR3b-2): builds the reducer side of
// the read model repair loop and adapts the storage functions to the
// maintenance leaf's ports, in the pattern of
// infra_inventory_reconcile_wiring.go.

package main

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/content"
	contentartifacts "github.com/eshu-hq/eshu/go/internal/content/artifacts"
	walkartifacts "github.com/eshu-hq/eshu/go/internal/reducer/maintenance/config/artifacts"
	storageartifacts "github.com/eshu-hq/eshu/go/internal/storage/postgres/config/artifacts"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// walkerDisabledEvent is the event_name of the INFO record the reducer logs
// once at start when the config-artifact walker is disabled.
const walkerDisabledEvent = "config_artifacts.walker.disabled"

// walkerNoTransactionEvent is the event_name of the ERROR record the reducer
// logs once at start when the content database does not open transactions.
// The walker stays off; the sibling retention sweeper and graph locker
// degrade the same way over a limited database.
const walkerNoTransactionEvent = "config_artifacts.walker.no_transaction"

// configArtifactWalkerRunnerFor builds the config-artifact walker runner, or
// nil when ESHU_CONFIG_ARTIFACT_WALKER_ENABLED=false. It loads the walker's
// own switch and worker count beside the shared derive deadline, runs the
// process-start encoding and fence checks once, and supplies
// contentartifacts.ExtractorVersion, the guard-pinned extractor version, and
// no other constant.
//
// Unlike the content-writing binaries' NewConfigArtifactWriteDeriver, which
// fails startup over a database without transactions, this builder leaves the
// walker off with an ERROR record instead: the reducer's sibling components
// degrade over a limited database rather than refusing to start, and the
// walker is auxiliary (the Writes derive under their own switch, readers
// stay live for repositories the walker has not derived). In production the
// database is always a pool, so the off path only triggers on a wiring
// regression, which the ERROR record names.
//
// The deriver construction duplicates NewConfigArtifactWriteDeriver on
// purpose: the Write's kill switch does not gate the walker, so the two
// builders read different switches and must stay independent. A refused check
// (non-UTF8 encoding, missing fence) still returns a runner: its derives
// refuse without a statement and the cycle aborts, counted on
// derives_total, until the read model is installed. An invalid walker or
// derive setting, or a database without transactions, fails startup instead
// of building a walker that could never derive.
func configArtifactWalkerRunnerFor(
	ctx context.Context,
	getenv func(string) string,
	database db.ExecQueryer,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
) (*walkartifacts.Runner, error) {
	walkerCfg, err := content.LoadConfigArtifactWalkerConfig(getenv)
	if err != nil {
		return nil, err
	}
	if !walkerCfg.Enabled {
		if logger != nil {
			logger.InfoContext(ctx, "config artifact walker disabled; stored rows fall behind until content Writes derive them",
				"event_name", walkerDisabledEvent)
		}
		return nil, nil
	}
	deriveCfg, err := content.LoadConfigArtifactDeriveConfig(getenv)
	if err != nil {
		return nil, err
	}
	pool, ok := database.(storageartifacts.Database)
	if !ok {
		if logger != nil {
			logger.ErrorContext(ctx, "config artifact walker disabled: content database does not open transactions",
				"event_name", walkerNoTransactionEvent)
		}
		return nil, nil
	}
	encoding := storageartifacts.CheckEncoding(ctx, database, logger)
	fence := storageartifacts.CheckFence(ctx, database, logger)
	deriver, err := storageartifacts.NewDeriver(pool, storageartifacts.DeriverConfig{
		ExtractorVersion: contentartifacts.ExtractorVersion,
		Encoding:         encoding,
		Fence:            fence,
		Timeout:          deriveCfg.Timeout,
		Logger:           logger,
	})
	if err != nil {
		return nil, err
	}
	store := configArtifactWalkerStore{
		database:  database,
		deriver:   deriver,
		skipAfter: walkartifacts.DefaultSkipAfterFailures,
	}
	return &walkartifacts.Runner{
		Lister:  store,
		Claimer: store,
		Deriver: store,
		Marker:  store,
		Config: walkartifacts.Config{
			Workers: walkerCfg.Workers,
		},
		Tracer:      tracer,
		Instruments: instruments,
		Logger:      logger,
	}, nil
}

// configArtifactWalkerStore adapts the storage config-artifact functions to
// the maintenance leaf's Lister, Claimer, Deriver and Marker ports, so the
// leaf never imports storage.
type configArtifactWalkerStore struct {
	database  db.ExecQueryer
	deriver   *storageartifacts.Deriver
	skipAfter int
}

// ListDirtyRepos maps the dirty join to the leaf's work items, carrying the
// reason and the skip flag unchanged.
func (s configArtifactWalkerStore) ListDirtyRepos(ctx context.Context, limit int) ([]walkartifacts.DirtyRepo, error) {
	dirty, err := storageartifacts.ListDirty(ctx, s.database, storageartifacts.DirtyRequest{
		ExtractorVersion:  contentartifacts.ExtractorVersion,
		SkipAfterFailures: s.skipAfter,
		Limit:             limit,
	})
	if err != nil {
		return nil, err
	}
	out := make([]walkartifacts.DirtyRepo, 0, len(dirty))
	for _, item := range dirty {
		out = append(out, walkartifacts.DirtyRepo{
			RepoID:  item.RepoID,
			Reason:  string(item.Reason),
			Skipped: item.Skipped,
		})
	}
	return out, nil
}

// ClaimPage claims the next page of the shared walk.
func (s configArtifactWalkerStore) ClaimPage(ctx context.Context, budget int) ([]string, string, error) {
	return storageartifacts.ClaimPage(ctx, s.database, budget)
}

// BackfillRecorded reports whether the backfill marker exists.
func (s configArtifactWalkerStore) BackfillRecorded(ctx context.Context) (bool, error) {
	return storageartifacts.BackfillComplete(ctx, s.database)
}

// RecordBackfill records the backfill completion marker.
func (s configArtifactWalkerStore) RecordBackfill(ctx context.Context) error {
	return storageartifacts.RecordBackfillMarker(ctx, s.database)
}

// Derive runs the derive protocol for one repository and returns the derive
// outcome verbatim. The production Deriver never returns a transport error:
// failures travel in the outcome (error, panic, timeout) for the runner to
// map onto reconcile_total.
func (s configArtifactWalkerStore) Derive(ctx context.Context, repoID, trigger string) (string, error) {
	result := s.deriver.DeriveRepository(ctx, storageartifacts.Request{
		RepoID:  repoID,
		Trigger: storageartifacts.Trigger(trigger),
	})
	return string(result.Outcome), nil
}
