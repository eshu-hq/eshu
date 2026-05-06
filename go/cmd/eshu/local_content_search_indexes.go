package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/collector"
	pgstorage "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

const deferredContentSearchIndexPollInterval = 2 * time.Second
const deferredContentSearchIndexBuildTimeout = 10 * time.Minute

type localContentSearchIndexDB interface {
	pgstorage.Executor
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type localContentSearchIndexDrainState struct {
	TotalWork                int
	OpenWork                 int
	CompletedProjectorWork   int
	OpenSharedProjectionWork int
}

var localContentSearchDiscoverRepos = collector.DiscoverFilesystemRepositoryIDs

// startDeferredContentSearchIndexes restores expensive content search indexes
// after the first local-authoritative queue drain for the discovered repo set.
func startDeferredContentSearchIndexes(ctx context.Context, dsn string, expectedProjectors int) (func() error, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open deferred content search index connection: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping deferred content search index connection: %w", err)
	}

	maintainerCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() {
		defer func() { _ = db.Close() }()
		done <- runDeferredContentSearchIndexes(maintainerCtx, db, deferredContentSearchIndexPollInterval, expectedProjectors)
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

func runDeferredContentSearchIndexes(ctx context.Context, db localContentSearchIndexDB, interval time.Duration, expectedProjectors int) error {
	if interval <= 0 {
		interval = deferredContentSearchIndexPollInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		ready, err := localContentSearchIndexesReady(ctx, db, expectedProjectors)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: deferred content search index readiness check failed: %v\n", err)
		}
		if ready {
			start := time.Now()
			buildCtx, cancel := context.WithTimeout(context.Background(), deferredContentSearchIndexBuildTimeout)
			err := pgstorage.EnsureContentSearchIndexes(buildCtx, db)
			cancel()
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "deferred content search indexes ready expected_projectors=%d duration=%s\n", expectedProjectors, time.Since(start).Round(time.Millisecond))
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func localContentSearchIndexExpectedProjectors(workspaceRoot string) (int, error) {
	repos, err := localContentSearchDiscoverRepos(workspaceRoot)
	if err != nil {
		return 0, err
	}
	if len(repos) == 0 {
		return 1, nil
	}
	return len(repos), nil
}

func localContentSearchIndexesReady(ctx context.Context, db localContentSearchIndexDB, expectedProjectors int) (bool, error) {
	state, err := queryLocalContentSearchIndexDrainState(ctx, db)
	if err != nil {
		return false, err
	}
	return localContentSearchIndexesReadyFromState(state, expectedProjectors), nil
}

func queryLocalContentSearchIndexDrainState(ctx context.Context, db localContentSearchIndexDB) (localContentSearchIndexDrainState, error) {
	const query = `
SELECT
  (SELECT count(*) FROM fact_work_items) AS total_work,
  (SELECT count(*) FROM fact_work_items WHERE status <> 'succeeded') AS open_work,
  (SELECT count(*) FROM fact_work_items WHERE stage = 'projector' AND status = 'succeeded') AS completed_projector_work,
  (SELECT count(*) FROM shared_projection_intents WHERE completed_at IS NULL) AS open_shared_work
`
	var state localContentSearchIndexDrainState
	if err := db.QueryRowContext(ctx, query).Scan(
		&state.TotalWork,
		&state.OpenWork,
		&state.CompletedProjectorWork,
		&state.OpenSharedProjectionWork,
	); err != nil {
		return localContentSearchIndexDrainState{}, fmt.Errorf("query local queue drain state: %w", err)
	}
	return state, nil
}

func localContentSearchIndexesReadyFromState(state localContentSearchIndexDrainState, expectedProjectors int) bool {
	if expectedProjectors < 1 {
		expectedProjectors = 1
	}
	return state.TotalWork > 0 &&
		state.OpenWork == 0 &&
		state.CompletedProjectorWork >= expectedProjectors &&
		state.OpenSharedProjectionWork == 0
}
