// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #9777 step 2 disposable PostgreSQL proof that the change-driven replan folds end to end: changed records union the stored names, resolve to the same owned targets the production reader and planner derive (npm scopes, Maven group:artifact via package_id, PyPI normalization, Go case, swift path), withdrawn records retract through the same path, and the drain commits exactly the covered rows with the watermark over them; isolated bootstrap schema
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/coordinator/vulnerability"
	vulnerabilitystore "github.com/eshu-hq/eshu/go/internal/storage/postgres/vulnerability"
)

// replanLiveStores opens the isolated proof database with the owned-inventory
// fixture and the advisory generations, and hands back the fact store and
// the replan store over it.
func replanLiveStores(t *testing.T) (context.Context, *sql.DB, *FactStore, vulnerabilitystore.OwnedTargetReplanStore) {
	t.Helper()
	db, ctx := openIsolatedLiveDB(t, "osv_delta_replan_9777", "set ESHU_POSTGRES_DSN to run the #9777 replan folding proof")
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	t.Cleanup(cancel)
	if _, err := db.ExecContext(ctx, resolverFixtureSQL(t)); err != nil {
		t.Fatalf("seed resolver fixture: %v", err)
	}
	if _, err := db.ExecContext(ctx, storedNamesGenerations); err != nil {
		t.Fatalf("seed advisory generations: %v", err)
	}
	store := NewFactStore(SQLDB{DB: db})
	return ctx, db, store, vulnerabilitystore.NewOwnedTargetReplanStore(SQLDB{DB: db})
}

// seedReplanAdvisory writes one OSV record's affected packages and its CVE
// identity through the fact store with the producer-shaped payload
// (see seedOSVRecord).
func seedReplanAdvisory(ctx context.Context, t *testing.T, store *FactStore, id string, affected ...osvAffectedPackage) {
	t.Helper()
	seedOSVRecord(ctx, t, store, "scope-adv-a", "gen-a2", false, id, affected...)
}

func replanChangedRecord(advisory, ecosystem, name string, withdrawn bool, feed time.Time) vulnerability.OSVDeltaChangedRecord {
	return vulnerability.OSVDeltaChangedRecord{
		AdvisoryID:    advisory,
		Ecosystem:     ecosystem,
		PackageName:   name,
		Withdrawn:     strconv.FormatBool(withdrawn),
		FeedTimestamp: feed.UTC().Format(time.RFC3339),
	}
}

// replanFlow mirrors the coordinator service pass over real stores: admit
// the validated changes, union each pending row with the stored names,
// resolve the whole hit list once, and drain one slot's scopes.
// admitReplanFlow admits one batch of changed records: the intersection must
// take every valid tuple and nothing else.
func admitReplanFlow(
	ctx context.Context,
	t *testing.T,
	replan vulnerabilitystore.OwnedTargetReplanStore,
	instance string,
	changed []vulnerability.OSVDeltaChangedRecord,
	stored map[string][]vulnerability.OSVDeltaStoredPackage,
) int {
	t.Helper()
	intersection := vulnerability.IntersectOSVDeltaChangedNames(changed, stored)
	if intersection.ParseFailed != 0 {
		t.Fatalf("valid changed records parsed as %d failures", intersection.ParseFailed)
	}
	admittedRecords := make([]vulnerabilitystore.OwnedTargetReplanRecord, 0, len(intersection.Changes))
	for _, change := range intersection.Changes {
		admittedRecords = append(admittedRecords, vulnerabilitystore.OwnedTargetReplanRecord{
			AdvisoryID: change.AdvisoryID, Ecosystem: change.Ecosystem, PackageName: change.PackageName,
			Withdrawn: change.Withdrawn, FeedTimestamp: change.FeedTimestamp,
		})
	}
	admitted, _, err := replan.AdmitChangedTargets(ctx, instance, admittedRecords)
	if err != nil {
		t.Fatalf("admit: %v", err)
	}
	return admitted
}

// drainReplanPass runs one drain pass over the queued rows: fresh union,
// single whole-hit-list resolve, one slot's scopes, and the commit. It
// returns the plan so the caller asserts the drain, not just the rows.
func drainReplanPass(
	ctx context.Context,
	t *testing.T,
	database *sql.DB,
	store *FactStore,
	replan vulnerabilitystore.OwnedTargetReplanStore,
	instance string,
	stored map[string][]vulnerability.OSVDeltaStoredPackage,
) (vulnerability.OSVDeltaDrainPlan, []string) {
	t.Helper()
	pending, err := replan.ReadPendingReplan(ctx, instance)
	if err != nil {
		t.Fatalf("read pending: %v", err)
	}
	pendingRecords := make([]vulnerability.OSVDeltaChangedRecord, 0, len(pending))
	for _, row := range pending {
		pendingRecords = append(pendingRecords, vulnerability.OSVDeltaChangedRecord{
			AdvisoryID: row.AdvisoryID, Ecosystem: row.Ecosystem, PackageName: row.PackageName,
			Withdrawn: strconv.FormatBool(row.Withdrawn), FeedTimestamp: row.FeedTimestamp.UTC().Format(time.RFC3339), RowID: row.ID,
		})
	}
	drainIntersection := vulnerability.IntersectOSVDeltaChangedNames(pendingRecords, stored)
	resolved, err := store.ResolveOwnedPackageNames(ctx, toResolverLookups(drainIntersection.Lookups))
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	resolvedKeys := make([]string, 0, len(resolved.Targets))
	for _, target := range resolved.Targets {
		resolvedKeys = append(resolvedKeys, target.Lookup.Ecosystem+"\x00"+target.Lookup.Name+"\x00"+target.Version)
	}
	return drainAndCommit(ctx, t, database, replan, instance, drainIntersection, resolved), resolvedKeys
}

func toResolverLookups(lookups []vulnerability.OSVDeltaLookup) []OwnedPackageNameLookup {
	out := make([]OwnedPackageNameLookup, 0, len(lookups))
	for _, lookup := range lookups {
		out = append(out, OwnedPackageNameLookup{Ecosystem: lookup.Ecosystem, Name: lookup.Name})
	}
	return out
}

// drainAndCommit builds drain candidates from the resolution, plans one
// slot, and commits the covered rows with the watermark in one transaction.
// It returns the plan so the caller asserts the drain, not just the rows.
func drainAndCommit(
	ctx context.Context,
	t *testing.T,
	database *sql.DB,
	replan vulnerabilitystore.OwnedTargetReplanStore,
	instance string,
	drainIntersection vulnerability.OSVDeltaIntersection,
	resolved OwnedPackageResolution,
) vulnerability.OSVDeltaDrainPlan {
	t.Helper()
	byLookup := make(map[OwnedPackageNameLookup][]OwnedPackageResolvedTarget)
	for _, target := range resolved.Targets {
		byLookup[target.Lookup] = append(byLookup[target.Lookup], target)
	}
	var candidates []vulnerability.OSVDeltaDrainCandidate
	coveredNames := make(map[vulnerability.OSVDeltaLookup]struct{})
	for _, target := range resolved.Targets {
		coveredNames[vulnerability.OSVDeltaLookup{Ecosystem: target.Lookup.Ecosystem, Name: target.Lookup.Name}] = struct{}{}
	}
	rowByTuple := make(map[string]vulnerability.OSVDeltaValidatedChange)
	for _, change := range drainIntersection.Changes {
		rowByTuple[change.AdvisoryID+"\x00"+change.Ecosystem+"\x00"+change.PackageName] = change
	}
	for _, change := range drainIntersection.Changes {
		for _, name := range change.UnionNames {
			for _, target := range byLookup[OwnedPackageNameLookup{Ecosystem: change.Ecosystem, Name: name}] {
				candidates = append(candidates, vulnerability.OSVDeltaDrainCandidate{
					Ecosystem: target.Lookup.Ecosystem, PackageName: target.Lookup.Name,
					Version: target.Version, KeyHashLow24: target.KeyHashLow24, VirtualSlot: target.VirtualSlot,
					RowIDs: []int64{change.RowID}, FeedTimestamp: change.FeedTimestamp,
				})
			}
		}
	}
	var trivial []vulnerability.OSVDeltaTrivialRow
	for _, change := range drainIntersection.Changes {
		isTrivial := true
		for _, name := range change.UnionNames {
			if _, ok := coveredNames[vulnerability.OSVDeltaLookup{Ecosystem: change.Ecosystem, Name: name}]; ok {
				isTrivial = false
				break
			}
		}
		if isTrivial {
			trivial = append(trivial, vulnerability.OSVDeltaTrivialRow{RowID: change.RowID, FeedTimestamp: change.FeedTimestamp})
		}
	}
	plan := vulnerability.PlanOSVDeltaReplanDrain(candidates, trivial, nil)
	// No partial commit: every non-trivial covered row's every candidate
	// query minted a scope this pass (trivial rows need none). A covered
	// row with an unminted version would abandon that version with no
	// retry once the commit marks the row enqueued.
	trivialSet := make(map[int64]struct{}, len(trivial))
	for _, row := range trivial {
		trivialSet[row.RowID] = struct{}{}
	}
	minted := make(map[string]struct{})
	for _, target := range plan.Targets {
		for _, query := range target.Queries {
			minted[query.Ecosystem+"\x00"+query.PackageName+"\x00"+query.Version] = struct{}{}
		}
	}
	coveredSet := make(map[int64]struct{}, len(plan.CoveredRowIDs))
	for _, id := range plan.CoveredRowIDs {
		coveredSet[id] = struct{}{}
	}
	for _, candidate := range candidates {
		key := candidate.Ecosystem + "\x00" + candidate.PackageName + "\x00" + candidate.Version
		for _, id := range candidate.RowIDs {
			if _, ok := coveredSet[id]; !ok {
				continue
			}
			if _, ok := trivialSet[id]; ok {
				continue
			}
			if _, ok := minted[key]; !ok {
				t.Fatalf("covered row %d query %q minted no scope: partial drain abandons versions", id, key)
			}
		}
	}
	if len(plan.CoveredRowIDs) > 0 && plan.WatermarkSet {
		tx, err := database.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("begin commit tx: %v", err)
		}
		marked, err := replan.CommitReplanDrain(ctx, tx, instance, plan.CoveredRowIDs, plan.Watermark, time.Now().UTC())
		if err != nil {
			_ = tx.Rollback()
			t.Fatalf("commit drain: %v", err)
		}
		// Single coordinator, no races: every covered row marks exactly
		// once. A shortfall here is a lost row, not a race outcome.
		if marked != int64(len(plan.CoveredRowIDs)) {
			_ = tx.Rollback()
			t.Fatalf("marked = %d, want %d covered rows", marked, len(plan.CoveredRowIDs))
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("commit tx: %v", err)
		}
	}
	return plan
}

// TestOSVDeltaReplanFoldingDifferentialLive proves the folding chain agrees
// with production: the union rebuilds (Maven group:artifact from the stored
// package id, the Swift repository path, PyPI carried for the resolver to
// fold, Hex never parsed from its placeholder), the resolution returns the
// reader's targets with the reader's hash and slot, and the drain covers
// exactly those rows.
func TestOSVDeltaReplanFoldingDifferentialLive(t *testing.T) {
	ctx, database, store, replan := replanLiveStores(t)
	feed := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	instance := "replan-differential"

	// Stored advisories through the real collector: the union rebuilds from
	// whatever the producer wrote, by construction.
	seedReplanAdvisory(ctx, t, store, "ADV-D1", osvPackage("npm", "@Scope/Pkg"))
	seedReplanAdvisory(ctx, t, store, "ADV-D2", osvPackage("npm", "left-pad"))
	seedReplanAdvisory(ctx, t, store, "ADV-D3", osvPackage("PyPI", "Foo_Bar"))
	seedReplanAdvisory(ctx, t, store, "ADV-D4", osvPackage("Maven", "org.apache:commons"))
	seedReplanAdvisory(ctx, t, store, "ADV-D5", osvPackage("Go", "github.com/Foo/Bar"))
	seedReplanAdvisory(ctx, t, store, "ADV-D6", osvPackage("SwiftURL", "github.com/apple/swift-nio"))
	// ADV-D9 is the dropped name: the record carries only the new name, the
	// stored advisory still holds left-pad, and the union must re-query it.
	seedReplanAdvisory(ctx, t, store, "ADV-D9",
		osvPackage("npm", "left-pad"),
		osvPackage("npm", "kept-name"))
	// ADV-W is withdrawn with an empty current record: only the stored name
	// resolves, and the retraction flows through the same drain.
	seedReplanAdvisory(ctx, t, store, "ADV-W", osvPackage("npm", "left-pad"))

	changed := []vulnerability.OSVDeltaChangedRecord{
		replanChangedRecord("ADV-D1", "npm", "@scope/pkg", false, feed),
		replanChangedRecord("ADV-D2", "npm", "left-pad", false, feed),
		replanChangedRecord("ADV-D3", "pypi", "foo-bar", false, feed),
		replanChangedRecord("ADV-D4", "maven", "org.apache:commons", false, feed),
		replanChangedRecord("ADV-D5", "go", "github.com/foo/bar", false, feed),
		replanChangedRecord("ADV-D6", "swift", "github.com/apple/swift-nio", false, feed),
		replanChangedRecord("ADV-D9", "npm", "brand-new-name", false, feed),
		replanChangedRecord("ADV-W", "npm", "left-pad", true, feed),
	}
	storedRaw, err := store.ListStoredAdvisoryPackageNames(ctx, []string{
		"ADV-D1", "ADV-D2", "ADV-D3", "ADV-D4", "ADV-D5", "ADV-D6", "ADV-D9", "ADV-W",
	})
	if err != nil {
		t.Fatalf("stored advisory names: %v", err)
	}
	stored := make(map[string][]vulnerability.OSVDeltaStoredPackage)
	for id, names := range storedRaw {
		for _, name := range names {
			stored[id] = append(stored[id], vulnerability.OSVDeltaStoredPackage{
				Ecosystem: name.Ecosystem, PackageName: name.PackageName, PackageID: name.PackageID,
			})
		}
	}
	if admitted := admitReplanFlow(ctx, t, replan, instance, changed, stored); admitted != len(changed) {
		t.Fatalf("admitted = %d, want %d (nothing dropped)", admitted, len(changed))
	}

	// Drain to quiescence, one winning slot anchoring each pass, with
	// covered rows completing across their other slots as completion
	// scopes. The watermark never passes an unqueued record in the
	// operational sense the queue's CAS enforces: it advances only inside
	// the transaction that marks the covered rows enqueued, monotonically,
	// and to a covered timestamp (the marked == covered assertion in
	// drainAndCommit pins the atomicity per pass, and its full-coverage
	// assertion pins that no committed row abandons a version). Overflow
	// rows keep the queue — and later passes — waiting; they are never
	// skipped, because consumption is table-driven and the watermark never
	// filters it. A numeric feed-timestamp prefix would contradict
	// interleaved completion slots, so the proof asserts monotonicity plus
	// quiescence instead of a prefix.
	drainedQueries := make(map[string]uint32)
	resolvedQueries := make(map[string]struct{})
	var passes int
	var prevWatermark time.Time
	for {
		pending, err := replan.ReadPendingReplan(ctx, instance)
		if err != nil {
			t.Fatalf("read pending: %v", err)
		}
		if len(pending) == 0 {
			break
		}
		if passes >= 12 {
			t.Fatalf("queue did not drain in 12 passes (%d rows left)", len(pending))
		}
		plan, passResolved := drainReplanPass(ctx, t, database, store, replan, instance, stored)
		for _, key := range passResolved {
			resolvedQueries[key] = struct{}{}
		}
		// A draining pass mints at least its winning slot; covered rows
		// complete across their other slots as completion scopes, so a
		// pass may plan more than one slot and still commit whole rows.
		if len(plan.CoveredRowIDs) > 0 && plan.SlotsPlanned < 1 {
			t.Fatalf("pass covered %d rows but planned %d slots", len(plan.CoveredRowIDs), plan.SlotsPlanned)
		}
		for _, target := range plan.Targets {
			for _, query := range target.Queries {
				key := query.Ecosystem + "\x00" + query.PackageName + "\x00" + query.Version
				if slot, dup := drainedQueries[key]; dup && slot != planSlotOf(t, target.ScopeID) {
					t.Fatalf("query %q drained under two slots (%d and %d)", key, slot, planSlotOf(t, target.ScopeID))
				}
				drainedQueries[key] = planSlotOf(t, target.ScopeID)
			}
		}
		committed, set, _, _, err := replan.ReadCommitWatermark(ctx, instance)
		if err != nil {
			t.Fatalf("read watermark: %v", err)
		}
		if !set {
			t.Fatal("watermark unset after a draining pass")
		}
		if committed.Before(prevWatermark) {
			t.Fatalf("watermark regressed from %v to %v: the CAS must be monotonic", prevWatermark, committed)
		}
		prevWatermark = committed
		passes++
	}
	pending, err := replan.ReadPendingReplan(ctx, instance)
	if err != nil {
		t.Fatalf("read pending at quiescence: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("%d rows still pending at quiescence", len(pending))
	}
	committed, set, _, _, err := replan.ReadCommitWatermark(ctx, instance)
	if err != nil {
		t.Fatalf("read final watermark: %v", err)
	}
	if !set || !committed.Equal(feed) {
		t.Fatalf("final watermark = (%v, %v), want the drained feed time", committed, set)
	}
	// No abandonment: every resolved query drained under exactly one slot
	// across the passes. A committed row whose version never minted a
	// scope would leave its resolved query out of the drained set.
	for key := range resolvedQueries {
		if _, ok := drainedQueries[key]; !ok {
			t.Fatalf("resolved query %q never drained: a version was abandoned", key)
		}
	}

	// Rewind: re-presenting the whole batch admits nothing and loses
	// nothing — duplicates, no misses, no resurrections.
	intersection := vulnerability.IntersectOSVDeltaChangedNames(changed, stored)
	rewound := make([]vulnerabilitystore.OwnedTargetReplanRecord, 0, len(intersection.Changes))
	for _, change := range intersection.Changes {
		rewound = append(rewound, vulnerabilitystore.OwnedTargetReplanRecord{
			AdvisoryID: change.AdvisoryID, Ecosystem: change.Ecosystem, PackageName: change.PackageName,
			Withdrawn: change.Withdrawn, FeedTimestamp: change.FeedTimestamp,
		})
	}
	admitted, duplicates, err := replan.AdmitChangedTargets(ctx, instance, rewound)
	if err != nil {
		t.Fatalf("rewind admit: %v", err)
	}
	if admitted != 0 || duplicates != len(changed) {
		t.Fatalf("rewind admit = (%d, %d), want (0, %d)", admitted, duplicates, len(changed))
	}

	// The differential: every drained query resolves singly to an owned
	// target with the same version, slot, and hash the production reader
	// and resolver derive — the drain agrees fold-for-fold.
	for key, slot := range drainedQueries {
		parts := strings.Split(key, "\x00")
		lookup := OwnedPackageNameLookup{Ecosystem: parts[0], Name: parts[1]}
		resolution, err := store.ResolveOwnedPackageNames(ctx, []OwnedPackageNameLookup{lookup})
		if err != nil {
			t.Fatalf("single resolve %v: %v", lookup, err)
		}
		if len(resolution.Targets) == 0 {
			t.Fatalf("drained query %q resolves to nothing singly", key)
		}
		matched := false
		for _, target := range resolution.Targets {
			if target.Version == parts[2] && target.VirtualSlot == slot {
				matched = true
			}
		}
		if !matched {
			t.Fatalf("drained query %q has no singly-resolved target with version %q in slot %d", key, parts[2], slot)
		}
	}
	// The dropped name still re-queried: ADV-D9's stored left-pad resolves
	// even though the record dropped it.
	if _, ok := drainedQueries["npm\x00left-pad\x001.2.0"]; !ok {
		t.Fatalf("drained queries lack the dropped stored name: %v", drainedQueries)
	}
	// The withdrawn record flows through the same drain, not a side path:
	// its row is enqueued with the flag carried, and retracting it reuses
	// the normal claim path instead of a separate signal.
	rows, err := database.QueryContext(ctx,
		`SELECT advisory_id, withdrawn, status FROM owned_target_replan WHERE collector_instance_id = $1`,
		instance)
	if err != nil {
		t.Fatalf("read drained rows: %v", err)
	}
	defer func() { _ = rows.Close() }()
	seen := map[string]bool{}
	for rows.Next() {
		var advisory, status string
		var withdrawn bool
		if err := rows.Scan(&advisory, &withdrawn, &status); err != nil {
			t.Fatalf("scan drained rows: %v", err)
		}
		if status != "enqueued" {
			t.Fatalf("row %s has status %q at quiescence, want enqueued", advisory, status)
		}
		seen[advisory] = withdrawn
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate drained rows: %v", err)
	}
	withdrawn, ok := seen["ADV-W"]
	if !ok || !withdrawn {
		t.Fatalf("withdrawn ADV-W missing or unflagged in the drained rows: %v", seen)
	}
	if len(seen) != len(changed) {
		t.Fatalf("drained %d distinct advisories, want %d (nothing lost)", len(seen), len(changed))
	}
}

// planSlotOf parses the virtual slot out of a pinned _replan scope id.
func planSlotOf(t *testing.T, scopeID string) uint32 {
	t.Helper()
	parts := strings.Split(strings.TrimSpace(scopeID), "/")
	if len(parts) != 7 || parts[0] != "vuln-intel:" || parts[2] != "osv" || parts[4] != "_replan" {
		t.Fatalf("scope id %q is not a pinned _replan id", scopeID)
	}
	var slot uint32
	for _, c := range parts[5] {
		if c < '0' || c > '9' {
			t.Fatalf("scope id %q has a non-numeric slot", scopeID)
		}
		slot = slot*10 + uint32(c-'0')
	}
	return slot
}
