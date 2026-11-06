// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #9694 disposable PostgreSQL proof that an unchanged vulnerability fetch is skipped without storing a generation, advances the source state's last successful fetch, and keeps the advisory family fresh in readiness while the facts stay 30 days old, and that a source that was not fetched still reports advisory_sources; reads ESHU_POSTGRES_DSN
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	sdkcollector "github.com/eshu-hq/eshu-sdk/collector"
	vulnerabilityv1 "github.com/eshu-hq/eshu-sdk/factschema/vulnerability/v1"
	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/extensionhost"
	hostvulnerability "github.com/eshu-hq/eshu/go/internal/collector/extensionhost/vulnerability"
	"github.com/eshu-hq/eshu/go/internal/facts"
	chain "github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
	projectorruntime "github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/scope"
	storagepostgres "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	vulnerabilitystore "github.com/eshu-hq/eshu/go/internal/storage/postgres/vulnerability"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

const (
	recencyScopeID  = "vuln-intel://nvd/cve"
	recencyInstance = "collector-vulnerability-intelligence"
	recencyCVE      = "CVE-2026-0001"
	recencyEndpoint = "https://nvd.example.test"
)

// recencyCollectorKind is the vulnerability intelligence collector kind the
// envelopes carry. The in-tree collector is deleted (eshu-hq/eshu#9999); the
// kind stays because the satellite emits it through the extension host.
const recencyCollectorKind = "vulnerability_intelligence"

// recencyNVDEnvelopes returns the same NVD answer every time: one complete
// source snapshot plus one CVE record, observed at the clock the claim
// carries, so two claims differ only in time. The payload shape mirrors what
// the retired in-tree NVD envelope builders emitted, built here from the
// core-owned facts contract and the SDK factschema the satellite also emits.
func recencyNVDEnvelopes(generationID string, observedAt time.Time) []facts.Envelope {
	snapshotStableKey := facts.StableID(chain.VulnerabilitySourceSnapshotFactKind, map[string]any{"source": "nvd"})
	snapshot := facts.Envelope{
		FactID:           recencyFactID(chain.VulnerabilitySourceSnapshotFactKind, snapshotStableKey, generationID),
		ScopeID:          recencyScopeID,
		GenerationID:     generationID,
		FactKind:         chain.VulnerabilitySourceSnapshotFactKind,
		StableFactKey:    snapshotStableKey,
		SchemaVersion:    chain.VulnerabilityIntelligenceSchemaVersionV1,
		CollectorKind:    recencyCollectorKind,
		SourceConfidence: facts.SourceConfidenceReported,
		ObservedAt:       observedAt.UTC(),
		Payload: map[string]any{
			"collector_instance_id": recencyInstance,
			"source":                "nvd",
			"query_count":           1,
			"result_count":          1,
			"response_digest":       "",
			"complete":              true,
			"warning_code":          "",
			"warning_message":       "",
			"license":               "LicenseRef-NVD-API-Terms-of-Use",
			"attribution":           "This product uses the NVD API but is not endorsed or certified by the NVD.",
		},
		SourceRef: facts.Ref{
			SourceSystem:   recencyCollectorKind,
			ScopeID:        recencyScopeID,
			GenerationID:   generationID,
			FactKey:        snapshotStableKey,
			SourceURI:      recencyEndpoint,
			SourceRecordID: "nvd#" + generationID,
		},
	}
	// The descriptor binds the snapshot to the configured target, as the live
	// NVD fetch did; without it readiness reads the snapshot as legacy and
	// never as covered.
	snapshot.Payload["coverage_descriptor"] = vulnerabilityv1.NVDCoverageDescriptor{
		Version: "nvd.v1", Source: "nvd", Mode: "cve", CVEIDs: []string{recencyCVE},
		EndpointCandidates: []string{recencyEndpoint}, ExecutedEndpoint: recencyEndpoint,
	}
	cveStableKey := facts.StableID(chain.VulnerabilityCVEFactKind, map[string]any{
		"cve_id": recencyCVE, "source": "nvd",
	})
	record := facts.Envelope{
		FactID:           recencyFactID(chain.VulnerabilityCVEFactKind, cveStableKey, generationID),
		ScopeID:          recencyScopeID,
		GenerationID:     generationID,
		FactKind:         chain.VulnerabilityCVEFactKind,
		StableFactKey:    cveStableKey,
		SchemaVersion:    chain.VulnerabilityIntelligenceSchemaVersionV1,
		CollectorKind:    recencyCollectorKind,
		SourceConfidence: facts.SourceConfidenceReported,
		ObservedAt:       observedAt.UTC(),
		Payload: map[string]any{
			"collector_instance_id": recencyInstance,
			"source":                "nvd",
			"cve_id":                recencyCVE,
			"advisory_id":           recencyCVE,
			"status":                "",
			"published_at":          "",
			"modified_at":           "",
			"source_identifier":     "",
			"descriptions":          []map[string]string{{"lang": "en", "value": "an unchanged advisory"}},
			"cwes":                  nil,
			"cvss_score":            0.0,
			"cvss_vector":           "",
			"severity_label":        "",
			"cvss_metrics":          nil,
			"correlation_anchors":   []string{recencyCVE},
		},
		SourceRef: facts.Ref{
			SourceSystem:   recencyCollectorKind,
			ScopeID:        recencyScopeID,
			GenerationID:   generationID,
			FactKey:        cveStableKey,
			SourceURI:      recencyEndpoint,
			SourceRecordID: recencyCVE,
		},
	}
	return []facts.Envelope{snapshot, record}
}

func recencyFactID(factKind, stableFactKey, generationID string) string {
	return facts.StableID("VulnerabilityIntelligenceFact", map[string]any{
		"fact_kind":       factKind,
		"generation_id":   generationID,
		"scope_id":        recencyScopeID,
		"stable_fact_key": stableFactKey,
	})
}

// recencyFreshnessHint digests the envelopes the way the ingestion store's
// duplicate check compares them: same facts, same hint, so the second
// identical claim skips without storing a new generation.
func recencyFreshnessHint(envs []facts.Envelope) string {
	encoded := make([]string, 0, len(envs))
	for _, env := range envs {
		raw, err := json.Marshal(map[string]any{
			"fact_kind":      env.FactKind,
			"payload":        env.Payload,
			"schema_version": env.SchemaVersion,
			"stable_key":     env.StableFactKey,
		})
		if err != nil {
			panic(err)
		}
		encoded = append(encoded, string(raw))
	}
	sort.Strings(encoded)
	return facts.StableID("vulnerability_intelligence.collection.freshness", map[string]any{"envelopes": encoded})
}

type recencyProof struct {
	t     *testing.T
	db    *sql.DB
	store storagepostgres.IngestionStore
	gen   int
}

func openRecencyProof(t *testing.T) *recencyProof {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to a disposable Postgres database to run the #9694 readiness freshness proof")
	}
	db := postgresproof.OpenIsolatedSchema(t, dsn, "eshu_9694_recency", func(ctx context.Context, db *sql.DB) error {
		return storagepostgres.ApplyBootstrap(ctx, storagepostgres.SQLDB{DB: db})
	})
	if _, err := db.ExecContext(context.Background(), `
INSERT INTO collector_instances (instance_id, collector_kind, mode, enabled, configuration, last_observed_at, created_at, updated_at)
VALUES ($1, 'vulnerability_intelligence', 'continuous', TRUE, $2::jsonb, now(), now(), now())`,
		recencyInstance, `{"targets":[{"source":"nvd","scope_id":"`+recencyScopeID+`","base_url":"`+recencyEndpoint+`","cve_ids":["`+recencyCVE+`"]}]}`); err != nil {
		t.Fatalf("seed collector instance: %v", err)
	}
	if err := storagepostgres.BackfillPackageManifestConsumptionKeys(context.Background(), storagepostgres.SQLDB{DB: db}); err != nil {
		t.Fatalf("BackfillPackageManifestConsumptionKeys(): %v", err)
	}
	return &recencyProof{t: t, db: db, store: storagepostgres.NewIngestionStore(storagepostgres.SQLDB{DB: db})}
}

// claim commits one NVD answer observed at observedAt through the production
// IngestionStore and records the source-state rows the extension host writes
// for an extracted-collector claim, projecting any stored generation. The
// envelopes are built from the core-owned facts contract and the SDK
// factschema the satellite emits; the in-tree collector is deleted
// (eshu-hq/eshu#9999), so nothing here imports it.
func (p *recencyProof) claim(observedAt time.Time) {
	p.t.Helper()
	p.gen++
	generationID := "vulnerability_intelligence:generation-" + string(rune('0'+p.gen))
	envs := recencyNVDEnvelopes(generationID, observedAt)
	recorder, err := hostvulnerability.NewRecorder(hostvulnerability.Config{
		Store:               vulnerabilitystore.NewVulnerabilitySourceStateStore(storagepostgres.SQLDB{DB: p.db}),
		CollectorInstanceID: recencyInstance,
		Clock:               func() time.Time { return observedAt },
	})
	if err != nil {
		p.t.Fatalf("hostvulnerability.NewRecorder() error = %v", err)
	}
	outcome := extensionhost.ClaimOutcome{
		Item: workflow.WorkItem{
			CollectorKind: scope.CollectorVulnerabilityIntelligence, CollectorInstanceID: recencyInstance,
			ScopeID: recencyScopeID,
		},
		State: sdkcollector.ResultComplete, FactCount: len(envs),
		AttemptedAt: observedAt, ObservedAt: observedAt,
	}
	ctx := context.Background()
	if err := recorder.RecordClaimOutcome(ctx, outcome); err != nil {
		p.t.Fatalf("RecordClaimOutcome() error = %v", err)
	}
	collected := collector.FactsFromSlice(
		scope.IngestionScope{
			ScopeID: recencyScopeID, SourceSystem: string(scope.CollectorVulnerabilityIntelligence),
			ScopeKind: scope.KindVulnerabilityIntelligence, CollectorKind: scope.CollectorVulnerabilityIntelligence,
			PartitionKey: "nvd", Metadata: map[string]string{"source": "nvd"},
		},
		scope.ScopeGeneration{
			GenerationID: generationID,
			ScopeID:      recencyScopeID,
			ObservedAt:   observedAt.UTC(),
			IngestedAt:   observedAt.UTC(),
			Status:       scope.GenerationStatusPending,
			TriggerKind:  scope.TriggerKindSnapshot,
			// The hint is the duplicate check's identity: an unchanged answer
			// carries the same hint, so the second claim skips without
			// storing a new generation (#9694). Nothing here attests a whole
			// fetch, so the destructive-successor guard stays armed.
			FreshnessHint: recencyFreshnessHint(envs),
		},
		envs,
	)
	if err := p.store.CommitScopeGeneration(ctx, collected.Scope, collected.Generation, collected.Facts); err != nil {
		p.t.Fatalf("CommitScopeGeneration() error = %v", err)
	}
	if err := recorder.RecordClaimCommitted(ctx, outcome); err != nil {
		p.t.Fatalf("RecordClaimCommitted() error = %v", err)
	}
	queue := storagepostgres.NewProjectorQueue(storagepostgres.SQLDB{DB: p.db}, "recency-proof", time.Minute)
	for {
		work, found, err := queue.Claim(context.Background())
		if err != nil {
			p.t.Fatalf("project claim: %v", err)
		}
		if !found {
			return
		}
		if err := queue.Ack(context.Background(), work, projectorruntime.Result{}); err != nil {
			p.t.Fatalf("project ack: %v", err)
		}
	}
}

func (p *recencyProof) count(query string) int {
	p.t.Helper()
	var n int
	if err := p.db.QueryRowContext(context.Background(), query).Scan(&n); err != nil {
		p.t.Fatalf("%s: %v", query, err)
	}
	return n
}

func (p *recencyProof) lastSuccess() time.Time {
	p.t.Helper()
	var at time.Time
	if err := p.db.QueryRowContext(context.Background(),
		`SELECT last_success_at FROM vulnerability_source_states WHERE scope_id = $1`, recencyScopeID).Scan(&at); err != nil {
		p.t.Fatalf("read source state: %v", err)
	}
	return at
}

func (p *recencyProof) readiness() ReadinessEnvelope {
	p.t.Helper()
	snapshot, err := NewPostgresReadinessStore(p.db).ReadSupplyChainImpactReadiness(context.Background(), ReadinessQuery{CVEID: recencyCVE})
	if err != nil {
		p.t.Fatalf("ReadSupplyChainImpactReadiness() error = %v", err)
	}
	return BuildReadiness(TargetScope{CVEID: recencyCVE}, nil, false, snapshot)
}

// TestReadinessAdvisoryFreshAfterSkippedUnchangedFetchLive is the #9694 F1
// repro against real tables: the answer is unchanged for 30 days, the second
// claim is skipped without a new generation, the facts stay 30 days old, and
// readiness must still call the fetched source fresh.
func TestReadinessAdvisoryFreshAfterSkippedUnchangedFetchLive(t *testing.T) {
	p := openRecencyProof(t)
	now := time.Now().UTC().Truncate(time.Second)
	first := now.Add(-30 * 24 * time.Hour)
	p.claim(first)
	p.claim(now) // same answer, 30 days later

	if got := p.count(`SELECT count(*) FROM scope_generations WHERE scope_id = '` + recencyScopeID + `'`); got != 1 {
		t.Fatalf("generations = %d, want 1: the unchanged fetch must be skipped, not stored", got)
	}
	if got := p.lastSuccess(); got.Before(now.Add(-time.Minute)) {
		t.Fatalf("source state last_success_at = %s, want advanced to the skipped fetch at %s", got, now)
	}
	if got := p.count(`SELECT count(*) FROM fact_records WHERE scope_id = '` + recencyScopeID + `' AND observed_at > now() - interval '14 days'`); got != 0 {
		t.Fatalf("%d facts were re-observed inside the window, want 0: the skip stores nothing", got)
	}
	envelope := p.readiness()
	if ReadinessMissingContains(envelope.MissingEvidence, MissingEvidenceAdvisorySources) {
		t.Fatalf("missing_evidence = %v, want no %s for a source fetched just now", envelope.MissingEvidence, MissingEvidenceAdvisorySources)
	}
	if ReadinessMissingContains(envelope.MissingEvidence, MissingEvidenceTargetCollection) {
		t.Fatalf("missing_evidence = %v, want no %s: the snapshot's source was fetched just now", envelope.MissingEvidence, MissingEvidenceTargetCollection)
	}
	for _, family := range envelope.EvidenceSources {
		if family.Family == EvidenceFamilyVulnerabilityAdvisory && family.Freshness != FreshnessLabelFresh {
			t.Fatalf("advisory freshness = %q, want fresh", family.Freshness)
		}
	}
}

// TestReadinessAdvisoryStaleWhenNeverRefetchedLive is the control: a source
// whose only fetch was 30 days ago is genuinely stale and still reports
// advisory_sources.
func TestReadinessAdvisoryStaleWhenNeverRefetchedLive(t *testing.T) {
	p := openRecencyProof(t)
	p.claim(time.Now().UTC().Add(-30 * 24 * time.Hour))
	envelope := p.readiness()
	if !ReadinessMissingContains(envelope.MissingEvidence, MissingEvidenceAdvisorySources) {
		t.Fatalf("missing_evidence = %v, want %s for a source not fetched in 30 days", envelope.MissingEvidence, MissingEvidenceAdvisorySources)
	}
}

// recordExtractedClaim writes the source-state row the extension host writes
// for an extracted-collector claim whose generation the guard refused (#10067):
// the commit returned nil but stored nothing, so the host records the refusal
// the guard reported on the commit's disposition box instead of a commit. The
// row is partial and names the refusal, which keeps the advisory family
// unfreshened while the guard holds the scope on its stale generation. The
// status half still comes from the satellite's result: a complete result whose
// status is Partial with a warning count for an unproven fetch (see
// TestClaimOutcomeCarriesAPartialStatusOnACompleteResult and the satellite's
// TestHandleReportsAnUnprovenFetchAsPartial).
func (p *recencyProof) recordExtractedClaim(at time.Time, partial bool, refusal collector.CommitRefusal) {
	p.t.Helper()
	recorder, err := hostvulnerability.NewRecorder(hostvulnerability.Config{
		Store:               vulnerabilitystore.NewVulnerabilitySourceStateStore(storagepostgres.SQLDB{DB: p.db}),
		CollectorInstanceID: recencyInstance,
		Clock:               func() time.Time { return at },
	})
	if err != nil {
		p.t.Fatalf("hostvulnerability.NewRecorder() error = %v", err)
	}
	outcome := extensionhost.ClaimOutcome{
		Item: workflow.WorkItem{
			CollectorKind: scope.CollectorVulnerabilityIntelligence, CollectorInstanceID: recencyInstance, ScopeID: recencyScopeID,
		},
		State: sdkcollector.ResultComplete, Partial: partial, FactCount: 1,
		AttemptedAt: at, ObservedAt: at,
	}
	if partial {
		outcome.WarningCount = 1
	}
	// The guard refused the shrink above, so the host records the refusal the
	// guard reported on the commit's disposition box, never a commit.
	if err := recorder.RecordClaimRefused(context.Background(), outcome, refusal); err != nil {
		p.t.Fatalf("RecordClaimRefused() error = %v", err)
	}
}

// refuseShrink commits an empty, unattested successor over the scope's active
// generation, which the destructive-successor guard refuses and rolls back.
// It returns the refusal the guard reported on the commit's disposition box,
// so the caller records this commit's disposition instead of a hardcoded one.
func (p *recencyProof) refuseShrink(at time.Time) collector.CommitRefusal {
	p.t.Helper()
	stream := make(chan facts.Envelope)
	close(stream)
	generation := scope.ScopeGeneration{
		GenerationID: "vulnerability_intelligence:refused", ScopeID: recencyScopeID, ObservedAt: at, IngestedAt: at,
		Status: scope.GenerationStatusPending, TriggerKind: scope.TriggerKindSnapshot, FreshnessHint: "truncated-answer",
	}
	ingestion := scope.IngestionScope{
		ScopeID: recencyScopeID, SourceSystem: string(scope.CollectorVulnerabilityIntelligence),
		ScopeKind: scope.KindVulnerabilityIntelligence, CollectorKind: scope.CollectorVulnerabilityIntelligence, PartitionKey: "nvd",
	}
	commitCtx, box := collector.WithCommitDisposition(context.Background())
	if err := p.store.CommitScopeGeneration(commitCtx, ingestion, generation, stream); err != nil {
		p.t.Fatalf("CommitScopeGeneration() error = %v, want the refusal to return success", err)
	}
	if got := p.count(`SELECT count(*) FROM scope_generations WHERE scope_id = '` + recencyScopeID + `' AND generation_id = 'vulnerability_intelligence:refused'`); got != 0 {
		p.t.Fatal("the destructive successor was stored, want it refused")
	}
	refusal, refused := box.Refusal()
	if !refused {
		p.t.Fatalf("disposition = %q, want the guard to refuse the empty successor", box.Kind())
	}
	return refusal
}

// TestReadinessKeepsAdvisorySourcesForARefusedShrinkFromTheExtractedCollectorLive
// is the #9694 review G1 probe, repointed at the refused-commit row (#10067).
// The extracted collector's source state is written by the host after the
// commit returns, and a guard refusal returns success, so the host records the
// refusal. The row is partial and readiness must keep reporting
// advisory_sources for a scope the guard is holding on a stale generation. The
// second row pins that the satellite's Partial status is not what holds the
// row partial: a refused commit under a clean status stays partial too,
// because nothing landed.
func TestReadinessKeepsAdvisorySourcesForARefusedShrinkFromTheExtractedCollectorLive(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	for _, tc := range []struct {
		name        string
		partial     bool
		wantMissing bool
	}{
		{"unproven fetch reported Partial", true, true},
		{"refused commit under a clean status stays partial", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := openRecencyProof(t)
			p.claim(now.Add(-30 * 24 * time.Hour)) // the facts the guard will keep
			refusal := p.refuseShrink(now)
			p.recordExtractedClaim(now, tc.partial, refusal)
			envelope := p.readiness()
			if got := ReadinessMissingContains(envelope.MissingEvidence, MissingEvidenceAdvisorySources); got != tc.wantMissing {
				t.Fatalf("missing_evidence = %v, advisory_sources reported = %v, want %v", envelope.MissingEvidence, got, tc.wantMissing)
			}
			for _, family := range envelope.EvidenceSources {
				if family.Family == EvidenceFamilyVulnerabilityAdvisory && family.Freshness == FreshnessLabelFresh {
					t.Fatalf("advisory freshness = %q, want anything but fresh for a refused row", family.Freshness)
				}
			}
		})
	}
}

// TestReadinessGlobalLayoutWithoutLocalClaimColumnsUsesLegacyLive pins the
// #9544 shard-2/6 failure deterministically: a database-global authority
// layout (left behind here, as a shard mate's full bootstrap does) with an
// isolated schema that skipped the public-only migrations must read the
// legacy statement, not fail on the missing 241a claim columns.
func TestReadinessGlobalLayoutWithoutLocalClaimColumnsUsesLegacyLive(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to a disposable Postgres database to run the mixed-layout fallback proof")
	}
	ctx := context.Background()
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open admin database: %v", err)
	}
	defer func() { _ = admin.Close() }()
	// Install only what a foreign full bootstrap would leave globally, and
	// remove only what this test creates: a pre-existing real layout stays.
	var layoutExists bool
	if err := admin.QueryRowContext(ctx, `SELECT to_regclass('eshu_security.role_policy_state') IS NOT NULL`).Scan(&layoutExists); err != nil {
		t.Fatalf("probe global layout: %v", err)
	}
	if !layoutExists {
		if _, err := admin.ExecContext(ctx, `CREATE SCHEMA IF NOT EXISTS eshu_security`); err != nil {
			t.Fatalf("create eshu_security schema: %v", err)
		}
		if _, err := admin.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS eshu_security.role_policy_state (probe_stub BOOLEAN NOT NULL DEFAULT TRUE)`); err != nil {
			t.Fatalf("create role_policy_state stub: %v", err)
		}
		t.Cleanup(func() {
			_, _ = admin.ExecContext(context.Background(), `DROP TABLE IF EXISTS eshu_security.role_policy_state`)
		})
	}
	p := openRecencyProof(t)
	p.claim(time.Now().UTC().Add(-30 * 24 * time.Hour))
	envelope := p.readiness()
	if !ReadinessMissingContains(envelope.MissingEvidence, MissingEvidenceAdvisorySources) {
		t.Fatalf("missing_evidence = %v, want %s for a source not fetched in 30 days", envelope.MissingEvidence, MissingEvidenceAdvisorySources)
	}
}
