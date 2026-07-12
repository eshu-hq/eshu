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

	"github.com/eshu-hq/eshu/go/internal/collector"
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
	cassette       string
	workers        int
	postgresDSN    string
	fromFacts      bool
	factsSourceDSN string
}

func parseDriveFlags(args []string, stderr io.Writer) (driveOptions, error) {
	fs := flag.NewFlagSet("ifa drive", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var o driveOptions
	fs.StringVar(&o.cassette, "cassette", "", "path to a replay/cassette JSON file (required unless -from-facts)")
	fs.IntVar(&o.workers, "workers", driveDefaultWorkers, "number of concurrent Driver workers draining the source")
	fs.StringVar(&o.postgresDSN, "postgres-dsn", "", "Postgres DSN to commit into (default: ESHU_POSTGRES_DSN/ESHU_FACT_STORE_DSN/ESHU_CONTENT_STORE_DSN)")
	fs.BoolVar(&o.fromFacts, "from-facts", false, "re-drain persisted fact_records (from -facts-source-dsn) instead of a cassette (B-12 determinism composition, issue #5008)")
	fs.StringVar(&o.factsSourceDSN, "facts-source-dsn", "", "with -from-facts: Postgres DSN to read fact_records from; MUST differ from -postgres-dsn")
	if err := fs.Parse(args); err != nil {
		return driveOptions{}, err //nolint:wrapcheck // flag errors are self-describing.
	}
	if err := o.validate(); err != nil {
		return driveOptions{}, err
	}
	return o, nil
}

// validate enforces that a run names exactly one source. In -from-facts mode the
// source and commit databases MUST be two distinct DSNs: FactSliceSource
// re-drains the same fact_records the target committer upserts, and upserting
// them back into the source database is a no-op (the ON CONFLICT (fact_id) and
// ON CONFLICT (work_item_id) clauses collide), so the target must be a fresh
// graph-feeding database. The explicit -postgres-dsn requirement blocks an
// env-derived target from silently coinciding with the source.
func (o driveOptions) validate() error {
	if o.fromFacts {
		if o.cassette != "" {
			return errors.New("ifa drive: -cassette and -from-facts are mutually exclusive; a run names exactly one source")
		}
		if o.factsSourceDSN == "" {
			return errors.New("ifa drive: -from-facts requires -facts-source-dsn (the fact_records source database)")
		}
		if o.postgresDSN == "" {
			return errors.New("ifa drive: -from-facts requires an explicit -postgres-dsn commit target distinct from -facts-source-dsn")
		}
		if o.factsSourceDSN == o.postgresDSN {
			return errors.New("ifa drive: -facts-source-dsn and -postgres-dsn must differ; re-draining fact_records into the same database is a no-op")
		}
		return nil
	}
	if o.cassette == "" {
		return errors.New("ifa drive: -cassette is required (or pass -from-facts with -facts-source-dsn)")
	}
	return nil
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

	delegate, sourceLabel, closeSource, err := driveOpenSource(ctx, o)
	if err != nil {
		return err
	}
	defer closeSource()

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
		Source:    concurrentreplay.NewSource(delegate),
		Committer: committer,
		Workers:   o.workers,
		Logger:    logger,
	}

	report, err := driver.Run(ctx)
	if err != nil {
		return fmt.Errorf("ifa drive: drive %s: %w", sourceLabel, err)
	}
	return printDriveReport(stdout, sourceLabel, report)
}

// driveOpenSource builds the concurrentreplay delegate source for the run and
// returns it with a human-readable label and a cleanup closure. In cassette
// mode the cassette is loaded first (before any Postgres connection) so a bad
// -cassette path fails fast without a database, exactly as the hermetic
// missing/nonexistent-cassette tests require. In -from-facts mode it opens the
// fact_records source database, enumerates every persisted scope generation via
// FactStore.ListScopeGenerationWork, and hands both to
// concurrentreplay.NewFactSliceSource so the recorded facts re-drain into the
// (distinct) commit target — the B-12 determinism composition of issue #5008.
func driveOpenSource(ctx context.Context, o driveOptions) (collector.Source, string, func(), error) {
	if !o.fromFacts {
		src, err := cassette.NewSource(o.cassette)
		if err != nil {
			return nil, "", nil, fmt.Errorf("ifa drive: load cassette %s: %w", o.cassette, err)
		}
		return src, o.cassette, func() {}, nil
	}

	srcDB, err := driveOpenPostgres(ctx, o.factsSourceDSN)
	if err != nil {
		return nil, "", nil, fmt.Errorf("ifa drive: open facts source postgres: %w", err)
	}
	factStore := postgres.NewFactStore(postgres.SQLDB{DB: srcDB})
	slices, err := factStore.ListScopeGenerationWork(ctx)
	if err != nil {
		_ = srcDB.Close()
		return nil, "", nil, fmt.Errorf("ifa drive: enumerate fact_records scope generations: %w", err)
	}
	return concurrentreplay.NewFactSliceSource(factStore, slices),
		"fact_records",
		func() { _ = srcDB.Close() },
		nil
}

// printDriveReport renders the Driver's Report as one human-readable summary
// line so a caller (or a script polling the drain SQL afterward) can confirm
// the requested worker count actually committed every recorded generation. The
// source DSNs are deliberately not printed — they may embed credentials — so
// only the source label ("fact_records" or the cassette path) is emitted.
func printDriveReport(w io.Writer, sourceLabel string, report concurrentreplay.Report) error {
	_, err := fmt.Fprintf(w, "ifa drive: source=%s workers=%d generations_committed=%d duration=%s\n",
		sourceLabel, report.Workers, report.GenerationsCommitted, report.Duration)
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
