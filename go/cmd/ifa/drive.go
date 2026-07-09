// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"

	_ "github.com/jackc/pgx/v5/stdlib" // pgx stdlib driver for database/sql

	"github.com/eshu-hq/eshu/go/internal/replay/cassette"
	"github.com/eshu-hq/eshu/go/internal/replay/concurrentreplay"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// driveDefaultWorkers is the "ifa drive" -workers default: a single
// sequential worker, the mode issue #4395's acceptance clause names
// explicitly ("same Odù drains ... at N=1").
const driveDefaultWorkers = 1

// drivePostgresDSNEnvKeys mirrors runtime.LoadPostgresConfig's own lookup
// order (ESHU_FACT_STORE_DSN, ESHU_CONTENT_STORE_DSN, ESHU_POSTGRES_DSN); an
// explicit -postgres-dsn flag overrides all three so an ambient env value in
// a lower-precedence key cannot win over it.
var drivePostgresDSNEnvKeys = []string{"ESHU_FACT_STORE_DSN", "ESHU_CONTENT_STORE_DSN", "ESHU_POSTGRES_DSN"}

// driveOptions holds the parsed command-line inputs for one "ifa drive" run.
type driveOptions struct {
	cassette    string
	workers     int
	postgresDSN string
}

func parseDriveFlags(args []string, stderr io.Writer) (driveOptions, error) {
	fs := flag.NewFlagSet("ifa drive", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var o driveOptions
	fs.StringVar(&o.cassette, "cassette", "", "path to a replay/cassette JSON file (required)")
	fs.IntVar(&o.workers, "workers", driveDefaultWorkers, "number of concurrent Driver workers draining the cassette")
	fs.StringVar(&o.postgresDSN, "postgres-dsn", "", "Postgres DSN to commit into (default: ESHU_POSTGRES_DSN/ESHU_FACT_STORE_DSN/ESHU_CONTENT_STORE_DSN)")
	if err := fs.Parse(args); err != nil {
		return driveOptions{}, err //nolint:wrapcheck // flag errors are self-describing.
	}
	if o.cassette == "" {
		return driveOptions{}, errors.New("ifa drive: -cassette is required")
	}
	return o, nil
}

// runDriveCommand implements `ifa drive`: the Ifá P2 concurrent replay driver
// verb (design doc 4102, issue #4395, parent epic #4389). It loads the
// cassette at -cassette via the production cassette.Source seam, wraps it in
// concurrentreplay.NewSource so it is safe to drain with more than one
// worker, opens Postgres, and runs concurrentreplay.Driver{Workers: -workers}
// against a postgres.IngestionStore Committer — the same durable commit
// boundary a live cassette collector uses.
//
// The cassette is loaded before Postgres is opened: a bad -cassette path (or
// a missing flag) fails fast without requiring a database connection, so a
// hermetic caller can exercise that path without Docker or Postgres running.
//
// This verb deliberately does not apply schema DDL, and does not run the
// projector or reducer itself: draining the fact_work_items rows it enqueues
// to the acceptance clause's residual_max:0 requires those to run separately
// against the same database, exactly as scripts/verify-ifa-replay-drive.sh
// orchestrates.
func runDriveCommand(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	o, err := parseDriveFlags(args, stderr)
	if err != nil {
		return err
	}

	src, err := cassette.NewSource(o.cassette)
	if err != nil {
		return fmt.Errorf("ifa drive: load cassette %s: %w", o.cassette, err)
	}

	db, err := driveOpenPostgres(ctx, o.postgresDSN)
	if err != nil {
		return fmt.Errorf("ifa drive: open postgres: %w", err)
	}
	defer func() {
		_ = db.Close()
	}()

	logger := slog.New(slog.NewJSONHandler(stderr, nil))
	committer := postgres.NewIngestionStore(postgres.SQLDB{DB: db})
	committer.Logger = logger

	driver := concurrentreplay.Driver{
		Source:    concurrentreplay.NewSource(src),
		Committer: committer,
		Workers:   o.workers,
		Logger:    logger,
	}

	report, err := driver.Run(ctx)
	if err != nil {
		return fmt.Errorf("ifa drive: drive %s: %w", o.cassette, err)
	}
	return printDriveReport(stdout, o, report)
}

// printDriveReport renders the Driver's Report as one human-readable summary
// line so a caller (or a script polling the drain SQL afterward) can confirm
// the requested worker count actually committed every recorded generation.
func printDriveReport(w io.Writer, o driveOptions, report concurrentreplay.Report) error {
	_, err := fmt.Fprintf(w, "ifa drive: cassette=%s workers=%d generations_committed=%d duration=%s\n",
		o.cassette, report.Workers, report.GenerationsCommitted, report.Duration)
	if err != nil {
		return fmt.Errorf("ifa drive: write report: %w", err)
	}
	return nil
}

// driveOpenPostgres opens a Postgres connection using dsn when set, otherwise
// the same ESHU_POSTGRES_DSN/ESHU_FACT_STORE_DSN/ESHU_CONTENT_STORE_DSN
// environment variables every other host binary reads through
// runtime.OpenPostgres/LoadPostgresConfig.
func driveOpenPostgres(ctx context.Context, dsn string) (*sql.DB, error) {
	getenv := os.Getenv
	if dsn != "" {
		getenv = func(key string) string {
			for _, k := range drivePostgresDSNEnvKeys {
				if key == k {
					return dsn
				}
			}
			return os.Getenv(key)
		}
	}
	return runtimecfg.OpenPostgres(ctx, getenv)
}
