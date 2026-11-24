// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/reducer/factwrite"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	pgstore "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// winnersSourcesPerGroup is the number of source facts seeded per canonical
// key: every group rebuilds to one winner with source_count > 1, so the
// legacy read-time dedup the winners leg replaces has real work to do.
const winnersSourcesPerGroup = 5

// winnersSeedFactKind is the reducer-derived fact kind backing the impact
// finding read model. It must stay identical to the query read model's
// FindingFactKind: the rebuild's active_facts CTE filters on this literal,
// so a drift would seed facts the rebuild never sees and the winners leg
// would measure an empty table while claiming a populated one.
// TestWinnersSeedFactKindMatchesReadModel pins the string.
const winnersSeedFactKind = "reducer_supply_chain_impact_finding"

// winnersFactAnchor is one scope generation that carries finding facts. The
// seed anchors on the plan's active generations only: the rebuild's
// active_facts CTE joins fact_records to ingestion_scopes on
// scope.active_generation_id, so facts on a superseded generation would be
// silently dropped from winners while still counting in fact_records.
type winnersFactAnchor struct {
	ScopeID      string
	GenerationID string
}

// winnersFindingFact is one synthetic impact finding fact. Facts sharing a
// canonical-key group collapse to a single winner at rebuild time; the
// distinct fact IDs within a group exercise the legacy read-time dedup the
// winners leg is compared against.
type winnersFindingFact struct {
	FactID       string
	ScopeID      string
	GenerationID string
	Payload      map[string]any
}

// BuildWinnersFindingFacts deterministically builds total synthetic finding
// facts in groups of sourcesPerGroup, round-robined over anchors. Pure:
// same inputs always produce the same facts, so the winners table is
// reproducible run to run. Empty anchors or a non-positive total yield no
// facts rather than facts no active-facts join could ever match.
func BuildWinnersFindingFacts(anchors []winnersFactAnchor, total, sourcesPerGroup int) []winnersFindingFact {
	if len(anchors) == 0 || total <= 0 || sourcesPerGroup <= 0 {
		return nil
	}
	facts := make([]winnersFindingFact, 0, total)
	for i := 0; i < total; i++ {
		group := i / sourcesPerGroup
		source := i % sourcesPerGroup
		anchor := anchors[i%len(anchors)]
		facts = append(facts, winnersFindingFact{
			FactID:       fmt.Sprintf("winners-seed-fact-%07d", i),
			ScopeID:      anchor.ScopeID,
			GenerationID: anchor.GenerationID,
			Payload:      winnersFindingPayload(group, source),
		})
	}
	return facts
}

// winnersFindingPayload builds the deterministic payload for one source fact
// in a canonical-key group. Every field the rebuild's canonical-key SQL
// reads (cve/advisory/package/purl/product/match/version/range/status/
// repo/digest) is constant within a group and distinct across groups, so one
// group rebuilds to exactly one winner; fact_id and priority_score vary per
// source so the winner pick is deterministic. service/workload/environment
// arrays are non-empty so the grouped-inventory dimensions the winners flag
// enables answer with rows, not just non-503 empties.
func winnersFindingPayload(group, source int) map[string]any {
	cve := fmt.Sprintf("CVE-2026-%05d", group)
	advisory := fmt.Sprintf("GHSA-seed-%04d", group%2000)
	pkg := fmt.Sprintf("pkg:npm/seed-%04d@1.0.0", group%1000)
	status := "affected_exact"
	if group%3 == 2 {
		status = "affected_transitive"
	}
	score := (group*7 + source) % 100
	bucket := "low"
	if score >= 70 {
		bucket = "high"
	} else if score >= 40 {
		bucket = "medium"
	}
	return map[string]any{
		"finding_id":        fmt.Sprintf("finding:winners-seed-%05d", group),
		"cve_id":            cve,
		"advisory_id":       advisory,
		"package_id":        pkg,
		"purl":              pkg,
		"product_criteria":  fmt.Sprintf("seed-product-%04d", group%1000),
		"match_criteria_id": fmt.Sprintf("seed-match-%04d", group%1000),
		"observed_version":  "1.0.0",
		"requested_range":   "^1.0.0",
		"impact_status":     status,
		"repository_id":     fmt.Sprintf("repository:seed-%02d", group%25),
		"subject_digest":    fmt.Sprintf("sha256:%064d", group),
		"ecosystem":         "npm",
		"cvss_score":        4.0 + float64((group*13)%60)/10.0,
		"priority_score":    score,
		"priority_bucket":   bucket,
		"detection_profile": "precise",
		"match_reason":      "npm_semver_affected_range",
		"suppression_state": "active",
		"service_ids":       []string{fmt.Sprintf("seed-svc-%02d", group%12)},
		"workload_ids":      []string{fmt.Sprintf("seed-wl-%02d", group%8)},
		"environments":      []string{fmt.Sprintf("seed-env-%d", group%3)},
		"image_ref":         "",
	}
}

// canonicalKeyFields projects the payload subset the rebuild's canonical-key
// SQL concatenates. The test uses it to prove the group structure; the row
// shaper keeps the full payload because the winner body select reads beyond
// the key (status, ecosystem, severity, service dimensions).
func canonicalKeyFields(payload map[string]any) map[string]any {
	fields := []string{
		"cve_id", "advisory_id", "package_id", "purl",
		"product_criteria", "match_criteria_id", "observed_version",
		"requested_range", "impact_status", "repository_id",
		"subject_digest",
	}
	out := make(map[string]any, len(fields))
	for _, key := range fields {
		out[key] = payload[key]
	}
	return out
}

// winnersFindingVersionedRows shapes the seeded findings into the rows the impact
// writer hands its supports step (supplychaincore.WriteSupplyChainImpactFindingRows):
// the same VersionedRow the production writer builds, carrying the finding payload
// verbatim. Split from the write so it stays unit-testable without a live Postgres.
func winnersFindingVersionedRows(facts []winnersFindingFact, now time.Time) ([]factwrite.VersionedRow, error) {
	rows := make([]factwrite.VersionedRow, 0, len(facts))
	for _, f := range facts {
		payload, err := json.Marshal(f.Payload)
		if err != nil {
			return nil, fmt.Errorf("marshal winners payload for fact %s: %w", f.FactID, err)
		}
		rows = append(rows, factwrite.VersionedRow{
			FactID:           f.FactID,
			ScopeID:          f.ScopeID,
			GenerationID:     f.GenerationID,
			FactKind:         winnersSeedFactKind,
			StableFactKey:    f.FactID,
			SchemaVersion:    "1.0.0",
			SourceConfidence: "inferred",
			SourceSystem:     "synthetic",
			SourceFactKey:    f.FactID,
			ObservedAt:       now,
			IngestedAt:       now,
			Payload:          string(payload),
		})
	}
	return rows, nil
}

// SeedWinnersSupports writes the seeded findings through the production writer's own
// supports step in one transaction (#10880 PR-C2 o). The finding kind is no longer
// written to fact_records (a reject trigger refuses it), so the corpus the winners leg
// sweeps is the per-support table the maintainer's full resweep reads. The winners table
// itself is still derived by RebuildWinnersReadModel through the production rebuild
// port, never hand-written, so the leg cannot drift from the maintainer's derivation.
func SeedWinnersSupports(ctx context.Context, db *sql.DB, facts []winnersFindingFact, now time.Time) error {
	rows, err := winnersFindingVersionedRows(facts, now)
	if err != nil {
		return err
	}
	tx, err := (pgstore.SupplyChainImpactBeginner{Beginner: pgstore.SQLDB{DB: db}}).BeginSupplyChainImpactTx(ctx)
	if err != nil {
		return fmt.Errorf("begin winners supports seed: %w", err)
	}
	if err := supplychaincore.WriteSupplyChainImpactFindingRows(ctx, tx, rows); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("seed winners supports: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit winners supports seed: %w", err)
	}
	return nil
}

// seedWinnersSupportsFromDSN opens its own handle for SeedWinnersSupports, as
// rebuildSeededWinners does for the rebuild.
func seedWinnersSupportsFromDSN(ctx context.Context, postgresDSN string, facts []winnersFindingFact) error {
	db, err := sql.Open("pgx", postgresDSN)
	if err != nil {
		return fmt.Errorf("open postgres for winners supports seed: %w", err)
	}
	defer func() { _ = db.Close() }()
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping postgres for winners supports seed: %w", err)
	}
	return SeedWinnersSupports(ctx, db, facts, time.Now().UTC())
}

// RebuildWinnersReadModel reconciles the winners table to the seeded facts
// through the maintainer's own full resweep
// (SupplyChainImpactWinnersStore.ResweepWinners), the same port the ifa
// advisory-group drive uses for backfills and proofs. It returns the
// rebuild counts. Skipped (another resweep in flight) is an error here: no
// reducer runs beside this gate, so a skip would mean an unexpected writer
// and an unreconciled table.
func RebuildWinnersReadModel(ctx context.Context, db *sql.DB) (inserted, updated, deleted, unchanged int64, err error) {
	store := pgstore.NewSupplyChainImpactWinnersStore(pgstore.SQLDB{DB: db})
	result, err := store.ResweepWinners(ctx)
	if err != nil {
		return 0, 0, 0, 0, fmt.Errorf("rebuild supply chain impact winners: %w", err)
	}
	if result.Skipped {
		return 0, 0, 0, 0, fmt.Errorf("rebuild supply chain impact winners skipped: another resweep holds the lock")
	}
	return result.Inserted, result.Updated, result.Deleted, result.Unchanged, nil
}

// winnersAnchorsFromPlan projects every plan scope onto its active
// generation, in plan order. Anchoring anywhere else would seed facts the
// rebuild's active-generation join drops.
func winnersAnchorsFromPlan(plan SeedPlan) []winnersFactAnchor {
	anchors := make([]winnersFactAnchor, 0, len(plan.Scopes))
	for _, s := range plan.Scopes {
		anchors = append(anchors, winnersFactAnchor{
			ScopeID:      s.ScopeID,
			GenerationID: s.ActiveGenerationID,
		})
	}
	return anchors
}

// rebuildSeededWinners opens the seeded Postgres, runs the winners rebuild
// through the production port, and proves the table is populated and
// watermarked. The handle is separate from the seed pool because the winners
// store takes database/sql, not pgxpool; it closes before the graph seed so
// no extra connection lingers through the sweep.
func rebuildSeededWinners(ctx context.Context, postgresDSN string, wantWinners int64) (inserted, updated, deleted, unchanged int64, err error) {
	db, err := sql.Open("pgx", postgresDSN)
	if err != nil {
		return 0, 0, 0, 0, fmt.Errorf("open postgres for winners rebuild: %w", err)
	}
	defer func() { _ = db.Close() }()
	if err := db.PingContext(ctx); err != nil {
		return 0, 0, 0, 0, fmt.Errorf("ping postgres for winners rebuild: %w", err)
	}
	inserted, updated, deleted, unchanged, err = RebuildWinnersReadModel(ctx, db)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	if err := VerifyWinnersSeeded(ctx, db, wantWinners); err != nil {
		return 0, 0, 0, 0, err
	}
	return inserted, updated, deleted, unchanged, nil
}

// expectedWinnerGroups is the number of winners the seeded facts must rebuild
// to: one canonical-key group rebuilds to exactly one winner, and
// BuildWinnersFindingFacts emits ceil(total / sourcesPerGroup) groups.
func expectedWinnerGroups(total, sourcesPerGroup int) int64 {
	if total <= 0 || sourcesPerGroup <= 0 {
		return 0
	}
	return int64((total + sourcesPerGroup - 1) / sourcesPerGroup)
}

// checkWinnersCount fails unless the winners table holds exactly the winners the
// seeded groups rebuild to. A count above zero is not enough: a partial rebuild
// would leave the winners leg measuring a smaller read model than the legacy
// leg reads.
func checkWinnersCount(got, want int64) error {
	if got != want {
		return fmt.Errorf("supply_chain_impact_canonical_winners holds %d rows, want %d (one per seeded canonical-key group)", got, want)
	}
	return nil
}

// VerifyWinnersSeeded fails unless the winners table holds exactly wantWinners rows and the
// resweep stamped its watermark. Both must hold for the winners leg to prove
// anything: rows without a watermark (or vice versa) mean a partial rebuild
// the freshness probe would refuse to serve from.
func VerifyWinnersSeeded(ctx context.Context, db *sql.DB, wantWinners int64) error {
	var winners int64
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM supply_chain_impact_canonical_winners`).Scan(&winners); err != nil {
		return fmt.Errorf("count supply chain impact canonical winners: %w", err)
	}
	if winners == 0 {
		return fmt.Errorf("supply_chain_impact_canonical_winners holds no rows: the winners leg would sweep an empty read model")
	}
	if err := checkWinnersCount(winners, wantWinners); err != nil {
		return err
	}
	var watermark time.Time
	if err := db.QueryRowContext(ctx, `SELECT materialized_at FROM supply_chain_impact_winners_materialization WHERE singleton = 1`).Scan(&watermark); err != nil {
		return fmt.Errorf("read supply chain impact winners watermark: %w", err)
	}
	if watermark.IsZero() {
		return fmt.Errorf("supply chain impact winners watermark is zero: rebuild did not stamp")
	}
	return nil
}
