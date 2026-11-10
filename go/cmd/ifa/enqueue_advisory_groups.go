// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// advisoryGroupListFlag collects repeated -group flags.
type advisoryGroupListFlag []string

func (f *advisoryGroupListFlag) String() string { return strings.Join(*f, ",") }

// Set appends one -group value, rejecting blanks so a shell slip cannot
// silently shrink the enqueue set the cell meant to prove.
func (f *advisoryGroupListFlag) Set(value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("ifa enqueue-advisory-groups: -group must not be blank")
	}
	*f = append(*f, value)
	return nil
}

// enqueueAdvisoryGroupsOptions holds the parsed inputs for one
// "ifa enqueue-advisory-groups" run.
type enqueueAdvisoryGroupsOptions struct {
	groups       []string
	scopeID      string
	generationID string
	sourceSystem string
	postgresDSN  string
}

func parseEnqueueAdvisoryGroupsFlags(args []string, stderr io.Writer) (enqueueAdvisoryGroupsOptions, error) {
	fs := flag.NewFlagSet("ifa enqueue-advisory-groups", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var o enqueueAdvisoryGroupsOptions
	var groups advisoryGroupListFlag
	fs.Var(&groups, "group", "advisory group id to enqueue a projection for (repeatable, at least one required)")
	fs.StringVar(&o.scopeID, "scope", "", "origin scope id the projection intents ride (the findings scope)")
	fs.StringVar(&o.generationID, "generation", "", "origin generation id the projection intents ride")
	fs.StringVar(&o.sourceSystem, "source-system", "", "origin source system mirrored onto the projection intents")
	fs.StringVar(&o.postgresDSN, "postgres-dsn", "", "Postgres DSN (default: ESHU_POSTGRES_DSN/ESHU_FACT_STORE_DSN/ESHU_CONTENT_STORE_DSN)")
	if err := fs.Parse(args); err != nil {
		return enqueueAdvisoryGroupsOptions{}, err //nolint:wrapcheck // flag errors are self-describing.
	}
	if fs.NArg() != 0 {
		return enqueueAdvisoryGroupsOptions{}, fmt.Errorf("ifa enqueue-advisory-groups: positional arguments are not accepted")
	}
	o.groups = groups
	o.scopeID = strings.TrimSpace(o.scopeID)
	o.generationID = strings.TrimSpace(o.generationID)
	o.sourceSystem = strings.TrimSpace(o.sourceSystem)
	if len(o.groups) == 0 {
		return enqueueAdvisoryGroupsOptions{}, fmt.Errorf("ifa enqueue-advisory-groups: at least one -group is required; enqueueing nothing would let the cell pass vacuously")
	}
	for _, required := range []struct{ name, value string }{
		{"scope", o.scopeID}, {"generation", o.generationID}, {"source-system", o.sourceSystem},
	} {
		if required.value == "" {
			return enqueueAdvisoryGroupsOptions{}, fmt.Errorf("ifa enqueue-advisory-groups: -%s is required; the enqueuer rides the origin intent's scope, generation, and source system exactly as a committed impact pass would", required.name)
		}
	}
	return o, nil
}

// rebuildWinnersOnce runs one full winners resweep — the rebuild the
// SupplyChainImpactWinnersMaintainer runs on its cadence, through the
// same store port — retrying only the single-flight skip: another
// resweep in flight (the reducer's own maintainer tick) writes nothing
// and stamps nothing, so the verb waits it out on a bounded budget
// instead of enqueueing against a watermark that never moved.
func rebuildWinnersOnce(ctx context.Context, store postgres.SupplyChainImpactWinnersStore) (inserted, updated, deleted, unchanged int64, err error) {
	deadline := time.Now().Add(60 * time.Second)
	for {
		result, rebuildErr := store.ResweepWinners(ctx, time.Now().UTC())
		if rebuildErr != nil {
			return 0, 0, 0, 0, rebuildErr
		}
		if !result.Skipped {
			return result.Inserted, result.Updated, result.Deleted, result.Unchanged, nil
		}
		if time.Now().After(deadline) {
			return 0, 0, 0, 0, fmt.Errorf("winners resweep stayed single-flight-skipped for 60s; another resweep never released the lock")
		}
		select {
		case <-ctx.Done():
			return 0, 0, 0, 0, ctx.Err() //nolint:wrapcheck // cancellation is the caller's own signal.
		case <-time.After(500 * time.Millisecond):
		}
	}
}

// runEnqueueAdvisoryGroupsCommand implements `ifa enqueue-advisory-groups`:
// the mid-drive step of the advisory_group Ifá live cells (#10466).
//
// Production admits an advisory_group_projection intent only from a
// committed supply_chain_impact pass, through
// AdvisoryGroupProjectionEnqueuer, and the handler then waits for the
// winners maintainer's watermark to pass the enqueue. A cassette drive
// admits the pass's OUTPUT facts (reducer_supply_chain_impact_finding)
// directly, so no pass runs and nothing enqueues. This verb performs
// the two production steps the drive leaves undone, at their production
// ports — the winners maintainer's full rebuild over the admitted
// finding facts, then the production enqueuer over the real reducer
// queue, then a second rebuild so the watermark covers the enqueue the
// handler defers on. It invents no fact kind and no queue shape: the
// drain that follows claims, handles, and acks through the real
// projector/reducer binaries exactly as production does.
//
// The group ids are explicit flags, mirroring the enqueuer's own
// contract (a pass hands it TouchedGroupIDs). Deriving them here would
// reimplement the impact pass's fan-out, which is that family's claim,
// not this one's.
func runEnqueueAdvisoryGroupsCommand(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	o, err := parseEnqueueAdvisoryGroupsFlags(args, stderr)
	if err != nil {
		return err
	}

	db, err := driveOpenPostgres(ctx, o.postgresDSN)
	if err != nil {
		return fmt.Errorf("ifa enqueue-advisory-groups: open postgres: %w", err)
	}
	defer func() { _ = db.Close() }()

	store := postgres.NewSupplyChainImpactWinnersStore(postgres.SQLDB{DB: db})
	inserted, updated, deleted, unchanged, err := rebuildWinnersOnce(ctx, store)
	if err != nil {
		return fmt.Errorf("ifa enqueue-advisory-groups: rebuild winners before enqueue: %w", err)
	}
	_, _ = fmt.Fprintf(stdout, "winners rebuild before enqueue: inserted=%d updated=%d deleted=%d unchanged=%d\n", inserted, updated, deleted, unchanged)

	queue := postgres.NewReducerQueue(postgres.SQLDB{DB: db}, "ifa-enqueue-advisory-groups", time.Minute)
	enqueuer := postgres.NewAdvisoryGroupProjectionEnqueuer(queue)
	origin := reducercontract.Intent{
		ScopeID:      o.scopeID,
		GenerationID: o.generationID,
		SourceSystem: o.sourceSystem,
	}
	if err := enqueuer.EnqueueAdvisoryGroupProjections(ctx, origin, o.groups); err != nil {
		return fmt.Errorf("ifa enqueue-advisory-groups: %w", err)
	}
	_, _ = fmt.Fprintf(stdout, "enqueued advisory_group_projection for %d group(s): %s\n", len(o.groups), strings.Join(o.groups, ","))

	inserted, updated, deleted, unchanged, err = rebuildWinnersOnce(ctx, store)
	if err != nil {
		return fmt.Errorf("ifa enqueue-advisory-groups: rebuild winners after enqueue: %w", err)
	}
	_, _ = fmt.Fprintf(stdout, "winners rebuild after enqueue: inserted=%d updated=%d deleted=%d unchanged=%d\n", inserted, updated, deleted, unchanged)
	return nil
}
