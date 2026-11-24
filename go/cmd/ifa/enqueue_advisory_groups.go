// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/factwrite"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/replay/cassette"
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
	cassettePath string
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
	fs.StringVar(&o.cassettePath, "cassette", "", "family cassette whose reducer_supply_chain_impact_finding facts are admitted through the impact writer transaction (facts and supports)")
	fs.Var(&groups, "group", "advisory group id to enqueue a projection for (repeatable, at least one required)")
	fs.StringVar(&o.scopeID, "scope", "", "origin scope id recorded on the projection intents (the findings scope); intents ride the synthetic advisory slot")
	fs.StringVar(&o.generationID, "generation", "", "origin generation id recorded on the projection intents; intents ride the synthetic advisory slot")
	fs.StringVar(&o.sourceSystem, "source-system", "", "origin source system mirrored onto the projection intents")
	fs.StringVar(&o.postgresDSN, "postgres-dsn", "", "Postgres DSN (default: ESHU_POSTGRES_DSN/ESHU_FACT_STORE_DSN/ESHU_CONTENT_STORE_DSN)")
	if err := fs.Parse(args); err != nil {
		return enqueueAdvisoryGroupsOptions{}, err //nolint:wrapcheck // flag errors are self-describing.
	}
	if fs.NArg() != 0 {
		return enqueueAdvisoryGroupsOptions{}, fmt.Errorf("ifa enqueue-advisory-groups: positional arguments are not accepted")
	}
	o.groups = groups
	o.cassettePath = strings.TrimSpace(o.cassettePath)
	o.scopeID = strings.TrimSpace(o.scopeID)
	o.generationID = strings.TrimSpace(o.generationID)
	o.sourceSystem = strings.TrimSpace(o.sourceSystem)
	if o.cassettePath == "" {
		return enqueueAdvisoryGroupsOptions{}, fmt.Errorf("ifa enqueue-advisory-groups: -cassette is required; the finding facts are admitted from the committed family cassette, never retyped into flags")
	}
	if len(o.groups) == 0 {
		return enqueueAdvisoryGroupsOptions{}, fmt.Errorf("ifa enqueue-advisory-groups: at least one -group is required; enqueueing nothing would let the cell pass vacuously")
	}
	for _, required := range []struct{ name, value string }{
		{"scope", o.scopeID}, {"generation", o.generationID}, {"source-system", o.sourceSystem},
	} {
		if required.value == "" {
			return enqueueAdvisoryGroupsOptions{}, fmt.Errorf("ifa enqueue-advisory-groups: -%s is required; the enqueuer mirrors the origin intent's source system and records its scope and generation as payload origin exactly as a committed impact pass would", required.name)
		}
	}
	return o, nil
}

// admitCassetteFindings admits the cassette's
// reducer_supply_chain_impact_finding facts for the origin scope and
// generation through the one production write seam (admitFindingRows):
// the conflict-domain lock, a transaction from the production beginner (which
// sets the old-writer fence setting), and the impact writer's own
// supports step, supplychaincore.WriteSupplyChainImpactFindingRows (#11201,
// #10880). Since PR-C2 that step writes the supports table only and no finding
// fact: the reject trigger is armed on the proof database, so a fact write here
// would fail with SQLSTATE 55000. The write stop therefore changes one place.
// Cassette ingestion cannot admit them: a finding payload carries
// reducer_domain=supply_chain_impact, so re-projecting a generation that
// holds them makes the projector's generic per-fact builder enqueue a fresh
// impact pass that retracts them as superseded (proven live on this branch:
// findings_retracted=5, winners=0). The rows carry the very fact ids
// cassette replay would have minted (cassette.DeriveFactID) and the payload
// bytes the committed cassette carries, re-marshaled from its decoded map.
// It returns the facts admitted.
func admitCassetteFindings(ctx context.Context, beginner supplychaincore.SupplyChainImpactBeginner, o enqueueAdvisoryGroupsOptions) (int, error) {
	rows, err := cassetteFindingRows(o)
	if err != nil {
		return 0, err
	}
	if err := admitFindingRows(ctx, beginner, o.scopeID, o.generationID, rows); err != nil {
		return 0, err
	}
	return len(rows), nil
}

// cassetteFindingRows builds the versioned fact rows of the cassette's finding
// facts for the origin scope and generation.
func cassetteFindingRows(o enqueueAdvisoryGroupsOptions) ([]factwrite.VersionedRow, error) {
	file, err := cassette.LoadFile(o.cassettePath)
	if err != nil {
		return nil, fmt.Errorf("load cassette: %w", err)
	}
	now := time.Now().UTC()
	rows := []factwrite.VersionedRow{}
	for _, scopeEntry := range file.Scopes {
		if scopeEntry.ScopeID != o.scopeID || scopeEntry.GenerationID != o.generationID {
			continue
		}
		for _, fact := range scopeEntry.Facts {
			if fact.FactKind != facts.ReducerSupplyChainImpactFindingFactKind {
				continue
			}
			payloadJSON, err := json.Marshal(fact.Payload)
			if err != nil {
				return nil, fmt.Errorf("marshal finding payload %q: %w", fact.StableFactKey, err)
			}
			intentID, _ := fact.Payload["intent_id"].(string)
			collectorKind := fact.CollectorKind
			if collectorKind == "" {
				collectorKind = scopeEntry.CollectorKind
			}
			rows = append(rows, factwrite.VersionedRow{
				FactID:           cassette.DeriveFactID(scopeEntry.ScopeID, scopeEntry.GenerationID, fact.StableFactKey),
				ScopeID:          scopeEntry.ScopeID,
				GenerationID:     scopeEntry.GenerationID,
				FactKind:         facts.ReducerSupplyChainImpactFindingFactKind,
				StableFactKey:    fact.StableFactKey,
				SchemaVersion:    fact.SchemaVersion,
				CollectorKind:    collectorKind,
				SourceConfidence: fact.SourceConfidence,
				SourceSystem:     o.sourceSystem,
				SourceFactKey:    intentID,
				ObservedAt:       now,
				IngestedAt:       now,
				IsTombstone:      fact.IsTombstone,
				Payload:          string(payloadJSON),
				FencingToken:     fact.FencingToken,
			})
		}
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("cassette %s carries no %s facts for scope %q generation %q; admitting nothing would let the cell pass vacuously", o.cassettePath, facts.ReducerSupplyChainImpactFindingFactKind, o.scopeID, o.generationID)
	}
	return rows, nil
}

// admitFindingRows writes finding fact rows through the production impact
// writer's seam: it opens the transaction from the production beginner, takes
// the (scope, generation) conflict-domain lock the writer and the supports
// backfill take, runs the writer's own supports step (no finding fact since PR-C2), and commits.
// The cell does not set the old-writer fence setting itself: the production
// beginner sets it for the transaction, so the admitted rows never mark their
// scope dirty. A transaction without the supports maintainer is refused:
// admitting facts that no support follows would leave the per-support table
// behind the facts.
func admitFindingRows(ctx context.Context, beginner supplychaincore.SupplyChainImpactBeginner, scopeID, generationID string, rows []factwrite.VersionedRow) error {
	tx, err := beginner.BeginSupplyChainImpactTx(ctx)
	if err != nil {
		return fmt.Errorf("begin finding admission transaction: %w", err)
	}
	if err := admitFindingRowsInTx(ctx, tx, scopeID, generationID, rows); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
			return fmt.Errorf("%w; rollback: %v", err, rbErr)
		}
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit finding admission transaction: %w", err)
	}
	return nil
}

func admitFindingRowsInTx(ctx context.Context, tx supplychaincore.SupplyChainImpactTx, scopeID, generationID string, rows []factwrite.VersionedRow) error {
	if _, ok := tx.(supplychaincore.SupplyChainImpactSupportsMaintainer); !ok {
		return fmt.Errorf("finding admission transaction has no supports maintainer; admitted facts would have no supports")
	}
	if _, err := tx.ExecContext(ctx, supplychaincore.SupplyChainImpactConflictDomainLockQuery,
		supplychaincore.SupplyChainImpactConflictDomainKey(scopeID, generationID)); err != nil {
		return fmt.Errorf("lock supply chain impact conflict domain: %w", err)
	}
	// Claim the #11031 first-seen stamps for the replayed rows, so a
	// cassette admitted on a fresh database keeps its original stamps
	// instead of re-stamping the replay time on the next rewrite. A row
	// whose payload already carries its stamp offers it; an unstamped
	// legacy row stamps now. The store is optional, as in the writer.
	if store, ok := tx.(supplychaincore.SupplyChainImpactFirstSeenStore); ok {
		stamps, err := store.ClaimSupplyChainImpactFirstSeen(ctx, supplychaincore.FirstSeenClaimsForRows(rows, nil), time.Now().UTC())
		if err != nil {
			return fmt.Errorf("claim supply chain impact first seen: %w", err)
		}
		if err := supplychaincore.StampPrebuiltFindingPayloads(rows, stamps); err != nil {
			return fmt.Errorf("stamp prebuilt finding payloads: %w", err)
		}
	}
	if err := supplychaincore.WriteSupplyChainImpactFindingRows(ctx, tx, rows, nil); err != nil {
		return fmt.Errorf("admit finding rows: %w", err)
	}
	return nil
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
		result, rebuildErr := store.ResweepWinners(ctx)
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
// winners maintainer's watermark to pass the enqueue. The cassette
// drive admits the collector facts (repositories, evidence) but holds
// the pass's OUTPUT facts back (see admitCassetteFindings for why
// ingestion cannot carry them). This verb performs the production
// steps the drive leaves undone, at their production ports — admit
// the finding rows through the writer supports step, run the
// winners maintainer's full rebuild over them, enqueue through the
// production enqueuer over the real reducer queue, then rebuild again
// so the watermark covers the enqueue the handler defers on. It
// invents no fact kind and no queue shape: the drain that follows
// claims, handles, and acks through the real projector/reducer
// binaries exactly as production does.
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

	admitted, err := admitCassetteFindings(ctx, postgres.SupplyChainImpactBeginner{Beginner: postgres.SQLDB{DB: db}}, o)
	if err != nil {
		return fmt.Errorf("ifa enqueue-advisory-groups: %w", err)
	}
	_, _ = fmt.Fprintf(stdout, "admitted %d finding fact(s) and their supports from cassette through the impact writer's seam\n", admitted)

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
