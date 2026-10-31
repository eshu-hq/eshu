// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #9694 disposable PostgreSQL proof that an unchanged vulnerability fetch is skipped without storing a generation, advances the source state's last successful fetch, and keeps the advisory family fresh in readiness while the facts stay 30 days old, and that a source that was not fetched still reports advisory_sources; reads ESHU_POSTGRES_DSN
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	sdkcollector "github.com/eshu-hq/eshu-sdk/collector"
	vulnerabilityv1 "github.com/eshu-hq/eshu-sdk/factschema/vulnerability/v1"
	"github.com/eshu-hq/eshu/go/internal/collector/extensionhost"
	"github.com/eshu-hq/eshu/go/internal/collector/vulnerabilityintelligence"
	"github.com/eshu-hq/eshu/go/internal/collector/vulnerabilityintelligence/checkpoint"
	"github.com/eshu-hq/eshu/go/internal/collector/vulnerabilityintelligence/vulnruntime"
	"github.com/eshu-hq/eshu/go/internal/facts"
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

// nvdProvider returns the same NVD answer every time, observed at the clock the
// claim carries, so two claims differ only in time.
type nvdProvider struct{ observedAt func() time.Time }

func (p nvdProvider) Collect(_ context.Context, target vulnruntime.TargetConfig, boundary vulnruntime.Boundary) ([]facts.Envelope, time.Time, error) {
	observedAt := p.observedAt()
	envCtx := vulnerabilityintelligence.VulnerabilityEnvelopeContext{
		ScopeID: boundary.ScopeID, GenerationID: boundary.GenerationID, CollectorInstanceID: boundary.CollectorInstanceID,
		FencingToken: boundary.FencingToken, ObservedAt: observedAt, SourceURI: recencyEndpoint,
	}
	snapshot, err := vulnerabilityintelligence.NewNVDSourceSnapshotEnvelope(envCtx, vulnerabilityintelligence.VulnerabilitySourceSnapshot{
		QueryCount: 1, ResultCount: 1, Complete: true,
	})
	if err != nil {
		return nil, time.Time{}, err
	}
	// The descriptor binds the snapshot to the configured target, as
	// annotateNVDExecutedCoverage does for a live NVD fetch; without it
	// readiness reads the snapshot as legacy and never as covered.
	snapshot.Payload["coverage_descriptor"] = vulnerabilityv1.NVDCoverageDescriptor{
		Version: "nvd.v1", Source: "nvd", Mode: "cve", CVEIDs: []string{recencyCVE},
		EndpointCandidates: []string{recencyEndpoint}, ExecutedEndpoint: recencyEndpoint,
	}
	record, err := vulnerabilityintelligence.NVDRecordEnvelopes(envCtx, vulnerabilityintelligence.NVDCVE{
		ID: recencyCVE, Descriptions: []vulnerabilityintelligence.NVDDescription{{Lang: "en", Value: "an unchanged advisory"}},
	})
	if err != nil {
		return nil, time.Time{}, err
	}
	return append([]facts.Envelope{snapshot}, record...), observedAt, nil
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

// claim runs one real ClaimedSource claim at observedAt and commits what it
// returns through the production IngestionStore, projecting any stored
// generation.
func (p *recencyProof) claim(observedAt time.Time) {
	p.t.Helper()
	p.gen++
	source, err := vulnruntime.NewClaimedSource(vulnruntime.SourceConfig{
		CollectorInstanceID: recencyInstance,
		Targets: []vulnruntime.TargetConfig{{
			Source: vulnruntime.SourceNVD, ScopeID: recencyScopeID, CVEIDs: []string{recencyCVE}, BaseURL: recencyEndpoint,
		}},
		Provider:   nvdProvider{observedAt: func() time.Time { return observedAt }},
		StateStore: vulnerabilitystore.NewVulnerabilitySourceStateStore(storagepostgres.SQLDB{DB: p.db}),
		Now:        func() time.Time { return observedAt },
	})
	if err != nil {
		p.t.Fatalf("NewClaimedSource() error = %v", err)
	}
	collected, ok, err := source.NextClaimed(context.Background(), workflow.WorkItem{
		CollectorKind: scope.CollectorVulnerabilityIntelligence, CollectorInstanceID: recencyInstance,
		ScopeID: recencyScopeID, GenerationID: "vulnerability_intelligence:generation-" + string(rune('0'+p.gen)), CurrentFencingToken: 1,
	})
	if err != nil || !ok {
		p.t.Fatalf("NextClaimed() = ok %v, error %v", ok, err)
	}
	if err := p.store.CommitScopeGeneration(context.Background(), collected.Scope, collected.Generation, collected.Facts); err != nil {
		p.t.Fatalf("CommitScopeGeneration() error = %v", err)
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
// for an extracted-collector claim after its generation commit returned, with
// the outcome the satellite's result produces: a complete result whose status
// is Partial with a warning count for an unproven fetch (see
// TestClaimOutcomeCarriesAPartialStatusOnACompleteResult and the satellite's
// TestHandleReportsAnUnprovenFetchAsPartial).
func (p *recencyProof) recordExtractedClaim(at time.Time, partial bool) {
	p.t.Helper()
	recorder, err := checkpoint.NewRecorder(checkpoint.Config{
		Store:               vulnerabilitystore.NewVulnerabilitySourceStateStore(storagepostgres.SQLDB{DB: p.db}),
		CollectorInstanceID: recencyInstance,
		Clock:               func() time.Time { return at },
	})
	if err != nil {
		p.t.Fatalf("checkpoint.NewRecorder() error = %v", err)
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
	if err := recorder.RecordClaimCommitted(context.Background(), outcome); err != nil {
		p.t.Fatalf("RecordClaimCommitted() error = %v", err)
	}
}

// refuseShrink commits an empty, unattested successor over the scope's active
// generation, which the destructive-successor guard refuses and rolls back.
func (p *recencyProof) refuseShrink(at time.Time) {
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
	if err := p.store.CommitScopeGeneration(context.Background(), ingestion, generation, stream); err != nil {
		p.t.Fatalf("CommitScopeGeneration() error = %v, want the refusal to return success", err)
	}
	if got := p.count(`SELECT count(*) FROM scope_generations WHERE scope_id = '` + recencyScopeID + `' AND generation_id = 'vulnerability_intelligence:refused'`); got != 0 {
		p.t.Fatal("the destructive successor was stored, want it refused")
	}
}

// TestReadinessKeepsAdvisorySourcesForARefusedShrinkFromTheExtractedCollectorLive
// is the #9694 review G1 probe. The extracted collector's source state is
// written by the host after the commit returns, and a guard refusal returns
// success, so the state is recorded either way. The satellite reports the
// unproven fetch as Partial, so the row is partial and readiness must keep
// reporting advisory_sources for a scope the guard is holding on a stale
// generation. The control shows what a clean status would do: the row reads
// fresh and the freshener hides the held scope, which is why the status matters.
func TestReadinessKeepsAdvisorySourcesForARefusedShrinkFromTheExtractedCollectorLive(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	for _, tc := range []struct {
		name        string
		partial     bool
		wantMissing bool
	}{
		{"unproven fetch reported Partial", true, true},
		{"control: the same refusal under a clean status", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := openRecencyProof(t)
			p.claim(now.Add(-30 * 24 * time.Hour)) // the facts the guard will keep
			p.refuseShrink(now)
			p.recordExtractedClaim(now, tc.partial)
			envelope := p.readiness()
			if got := ReadinessMissingContains(envelope.MissingEvidence, MissingEvidenceAdvisorySources); got != tc.wantMissing {
				t.Fatalf("missing_evidence = %v, advisory_sources reported = %v, want %v", envelope.MissingEvidence, got, tc.wantMissing)
			}
		})
	}
}
