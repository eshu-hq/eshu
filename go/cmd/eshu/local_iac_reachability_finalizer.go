package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	pgstorage "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

const localIaCReachabilityFinalizerPollInterval = 2 * time.Second
const localIaCReachabilityMaterializationTimeout = 10 * time.Minute

var queryLocalIaCReachabilityDrainState = queryLocalContentSearchIndexDrainState

// startLocalIaCReachabilityFinalizer materializes dead-IaC reachability rows
// after the first local-authoritative corpus drain.
func startLocalIaCReachabilityFinalizer(ctx context.Context, dsn string, expectedProjectors int) (func() error, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open IaC reachability finalizer connection: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping IaC reachability finalizer connection: %w", err)
	}

	finalizerCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() {
		defer func() { _ = db.Close() }()
		done <- runLocalIaCReachabilityFinalizer(
			finalizerCtx,
			db,
			expectedProjectors,
			localIaCReachabilityFinalizerPollInterval,
			func(ctx context.Context) error {
				store := pgstorage.NewIngestionStore(pgstorage.SQLDB{DB: db})
				return store.MaterializeIaCReachability(ctx, nil, nil)
			},
		)
	}()

	return func() error {
		cancel()
		err := <-done
		if err == nil || err == context.Canceled {
			return nil
		}
		return err
	}, nil
}

// runLocalIaCReachabilityFinalizer waits for the local-authoritative queue to
// drain once, then invokes the reachability materializer exactly once.
func runLocalIaCReachabilityFinalizer(
	ctx context.Context,
	db localContentSearchIndexDB,
	expectedProjectors int,
	interval time.Duration,
	materialize func(context.Context) error,
) error {
	if interval <= 0 {
		interval = localIaCReachabilityFinalizerPollInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		ready, err := localIaCReachabilityFinalizerReady(ctx, db, expectedProjectors)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: IaC reachability finalizer readiness check failed: %v\n", err)
		}
		if ready {
			start := time.Now()
			materializeCtx, cancel := context.WithTimeout(ctx, localIaCReachabilityMaterializationTimeout)
			err := materialize(materializeCtx)
			cancel()
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "IaC reachability rows materialized expected_projectors=%d duration=%s\n", expectedProjectors, time.Since(start).Round(time.Millisecond))
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// localIaCReachabilityFinalizerReady reads the same drain signal used by
// deferred content indexes so reachability is materialized only after reducers
// and shared projections have quiesced.
func localIaCReachabilityFinalizerReady(ctx context.Context, db localContentSearchIndexDB, expectedProjectors int) (bool, error) {
	state, err := queryLocalIaCReachabilityDrainState(ctx, db)
	if err != nil {
		return false, err
	}
	return localIaCReachabilityFinalizerReadyFromState(state, expectedProjectors), nil
}

// localIaCReachabilityFinalizerReadyFromState keeps readiness tests detached
// from a live Postgres instance.
func localIaCReachabilityFinalizerReadyFromState(state localContentSearchIndexDrainState, expectedProjectors int) bool {
	return localContentSearchIndexesReadyFromState(state, expectedProjectors)
}
