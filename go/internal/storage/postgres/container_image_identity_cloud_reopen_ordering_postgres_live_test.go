// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	awsv1 "github.com/eshu-hq/eshu/sdk/go/factschema/aws/v1"
	ociregistryv1 "github.com/eshu-hq/eshu/sdk/go/factschema/ociregistry/v1"
)

const cloudReopenOrderingAccountID = "123456789012"

type cloudReopenOrderingTruth struct {
	Digest           string
	ImageRef         string
	RepositoryID     string
	Outcome          string
	IdentityStrength string
	EvidenceFactIDs  []string
}

// TestContainerImageIdentityCloudReferenceReopenOrderingPostgresLive forces
// both sides of #5699's cross-scope activation order against real Postgres,
// the production fact loader, reducer queue, identity handler, digest-v3
// support writer, and deferred-maintenance reopen.
//
// The cloud-before-OCI case first succeeds with an empty support set while the
// matching OCI generation is pending. Only the production maintenance reopen
// may make that exact work item claimable again after OCI activates. The
// OCI-before-cloud control resolves on its first pass. Both orders must expose
// the same exact-digest truth, and replaying the repaired case once more must
// stay idempotent.
func TestContainerImageIdentityCloudReferenceReopenOrderingPostgresLive(t *testing.T) {
	cloudFirstTruth := proveCloudBeforeOCIReopenOrdering(t)
	ociFirstTruth := proveOCIBeforeCloudReopenOrdering(t)
	if !cloudReopenOrderingTruthEqual(cloudFirstTruth, ociFirstTruth) {
		t.Fatalf(
			"terminal truth differs by activation order: cloud-first=%#v OCI-first=%#v",
			cloudFirstTruth,
			ociFirstTruth,
		)
	}
}

type cloudReopenOrderingHarness struct {
	db      *sql.DB
	ctx     context.Context
	adapter SQLDB
	store   IngestionStore
	handler reducer.ContainerImageIdentityHandler
}

func newCloudReopenOrderingHarness(t *testing.T) cloudReopenOrderingHarness {
	t.Helper()
	db, ctx := openCloudReopenOrderingPostgres(t)
	adapter := SQLDB{DB: db}
	return cloudReopenOrderingHarness{
		db:      db,
		ctx:     ctx,
		adapter: adapter,
		store:   NewIngestionStore(adapter),
		handler: reducer.ContainerImageIdentityHandler{
			FactLoader: NewFactStore(adapter),
			Writer: reducer.PostgresContainerImageIdentitySupportWriter{
				ActivationLookup:  NewContainerImageIdentityScopeStateStore(adapter),
				HeldSupportLoader: NewContainerImageIdentityHeldSupportStore(adapter),
				ClaimedExecer:     ContainerImageIdentityClaimedExecer{DB: adapter},
			},
		},
	}
}

func proveCloudBeforeOCIReopenOrdering(t *testing.T) cloudReopenOrderingTruth {
	t.Helper()
	harness := newCloudReopenOrderingHarness(t)
	fixture := cloudReopenOrderingFixture("order-proof", "1")
	seedCloudReopenOrderingFixture(t, harness.ctx, harness.db, fixture, false)
	queue := cloudReopenOrderingQueue(harness.adapter, "cloud-first-worker")
	intent := enqueueCloudReopenOrderingIntent(t, harness.ctx, &queue, fixture)

	firstResult := handleAndAckCloudReopenOrderingIntent(
		t, harness.ctx, &queue, harness.handler, intent,
	)
	if firstResult.CanonicalWrites != 0 {
		t.Fatalf(
			"cloud-before-OCI first pass CanonicalWrites = %d, want 0 while OCI generation is pending",
			firstResult.CanonicalWrites,
		)
	}
	assertCloudReopenOrderingSupportCount(t, harness.ctx, harness.db, fixture.cloudScopeID, 0)

	activateCloudReopenOrderingOCI(t, harness.ctx, harness.db, fixture)
	if err := harness.store.ReopenSucceededReducerWorkItems(
		harness.ctx, nil, nil, CrossScopeCorrelationReopenDomains(),
	); err != nil {
		t.Fatalf("ReopenSucceededReducerWorkItems() error = %v", err)
	}
	assertCloudReopenOrderingReopened(t, harness.ctx, harness.db, intent.IntentID)
	reopenedIntent := claimCloudReopenOrderingIntent(t, harness.ctx, &queue)
	reopenedResult := handleAndAckCloudReopenOrderingIntent(
		t, harness.ctx, &queue, harness.handler, reopenedIntent,
	)
	if reopenedResult.CanonicalWrites != 1 {
		t.Fatalf(
			"cloud-before-OCI reopened CanonicalWrites = %d, want 1 after OCI activation",
			reopenedResult.CanonicalWrites,
		)
	}
	truth := readCloudReopenOrderingTruth(t, harness.ctx, harness.db, fixture)

	if err := harness.store.ReopenSucceededReducerWorkItems(
		harness.ctx, nil, nil, CrossScopeCorrelationReopenDomains(),
	); err != nil {
		t.Fatalf("second ReopenSucceededReducerWorkItems() error = %v", err)
	}
	idempotentIntent := claimCloudReopenOrderingIntent(t, harness.ctx, &queue)
	handleAndAckCloudReopenOrderingIntent(t, harness.ctx, &queue, harness.handler, idempotentIntent)
	if got := readCloudReopenOrderingTruth(t, harness.ctx, harness.db, fixture); !cloudReopenOrderingTruthEqual(got, truth) {
		t.Fatalf("idempotent replay truth = %#v, want unchanged %#v", got, truth)
	}
	return truth
}

func proveOCIBeforeCloudReopenOrdering(t *testing.T) cloudReopenOrderingTruth {
	t.Helper()
	harness := newCloudReopenOrderingHarness(t)
	fixture := cloudReopenOrderingFixture("order-proof", "1")
	seedCloudReopenOrderingFixture(t, harness.ctx, harness.db, fixture, true)
	queue := cloudReopenOrderingQueue(harness.adapter, "oci-first-worker")
	intent := enqueueCloudReopenOrderingIntent(t, harness.ctx, &queue, fixture)
	ociResult := handleAndAckCloudReopenOrderingIntent(t, harness.ctx, &queue, harness.handler, intent)
	if ociResult.CanonicalWrites != 1 {
		t.Fatalf("OCI-before-cloud CanonicalWrites = %d, want 1 on first pass", ociResult.CanonicalWrites)
	}
	return readCloudReopenOrderingTruth(t, harness.ctx, harness.db, fixture)
}

type cloudReopenOrderingTestFixture struct {
	name              string
	digest            string
	registry          string
	repository        string
	repositoryID      string
	cloudScopeID      string
	cloudGenerationID string
	cloudFactID       string
	ociScopeID        string
	ociGenerationID   string
	ociFactID         string
}

func cloudReopenOrderingFixture(name string, digestSuffix string) cloudReopenOrderingTestFixture {
	digest := "sha256:" + strings.Repeat("0", 63) + digestSuffix
	repository := "reopen-ordering"
	registry := cloudReopenOrderingAccountID + ".dkr.ecr.us-east-1.amazonaws.com"
	repositoryID := "oci-registry://" + registry + "/" + repository
	return cloudReopenOrderingTestFixture{
		name:              name,
		digest:            digest,
		registry:          registry,
		repository:        repository,
		repositoryID:      repositoryID,
		cloudScopeID:      "aws:cloud-reopen:" + name,
		cloudGenerationID: "generation:aws:cloud-reopen:" + name,
		cloudFactID:       "aws-image-reference:cloud-reopen:" + name,
		ociScopeID:        "oci:cloud-reopen:" + name,
		ociGenerationID:   "generation:oci:cloud-reopen:" + name,
		ociFactID:         "oci-image-manifest:cloud-reopen:" + name,
	}
}

func seedCloudReopenOrderingFixture(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	fixture cloudReopenOrderingTestFixture,
	activateOCI bool,
) {
	t.Helper()
	now := time.Now().UTC()
	seedCloudReopenOrderingScopeGeneration(
		t, ctx, db, fixture.cloudScopeID, fixture.cloudGenerationID, "aws", true, now,
	)
	seedCloudReopenOrderingScopeGeneration(
		t, ctx, db, fixture.ociScopeID, fixture.ociGenerationID, "oci_registry", activateOCI, now,
	)
	serviceKind := "ecs"
	registryID := cloudReopenOrderingAccountID
	tag := "latest"
	seedCloudReopenOrderingFact(t, ctx, db, fixture.cloudScopeID, fixture.cloudGenerationID,
		fixture.cloudFactID, "aws_image_reference", "aws", awsv1.ImageReference{
			AccountID:          cloudReopenOrderingAccountID,
			Region:             "us-east-1",
			ServiceKind:        &serviceKind,
			RepositoryName:     fixture.repository,
			RegistryID:         &registryID,
			ImageDigest:        fixture.digest,
			ManifestDigest:     fixture.digest,
			Tag:                &tag,
			CorrelationAnchors: []string{fixture.digest, fixture.repository + "@" + fixture.digest},
		}, now)
	seedCloudReopenOrderingFact(t, ctx, db, fixture.ociScopeID, fixture.ociGenerationID,
		fixture.ociFactID, "oci_registry.image_manifest", "oci_registry", ociregistryv1.ImageManifest{
			RepositoryID: fixture.repositoryID,
			Digest:       fixture.digest,
		}, now)
}

func seedCloudReopenOrderingScopeGeneration(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	scopeID string,
	generationID string,
	sourceSystem string,
	active bool,
	now time.Time,
) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, payload
) VALUES ($1, 'repository', $2, $1, $2, $1, $3, $3, 'pending', '{}'::jsonb)
`, scopeID, sourceSystem, now); err != nil {
		t.Fatalf("seed scope %s: %v", scopeID, err)
	}
	status := "pending"
	if active {
		status = "active"
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at
) VALUES (
    $1, $2, 'synthetic', $3::timestamptz, $3::timestamptz, $4::text,
    CASE WHEN $4::text = 'active' THEN $3::timestamptz ELSE NULL END
)
`, generationID, scopeID, now, status); err != nil {
		t.Fatalf("seed generation %s: %v", generationID, err)
	}
	if active {
		if _, err := db.ExecContext(ctx, `
UPDATE ingestion_scopes
SET status = 'active', active_generation_id = $1, ingested_at = $2
WHERE scope_id = $3
`, generationID, now, scopeID); err != nil {
			t.Fatalf("activate scope %s: %v", scopeID, err)
		}
	}
}

func seedCloudReopenOrderingFact(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	scopeID string,
	generationID string,
	factID string,
	factKind string,
	sourceSystem string,
	payload any,
	now time.Time,
) {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal %s payload: %v", factKind, err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records (
    fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    schema_version, collector_kind, fencing_token, source_confidence,
    source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload
) VALUES ($1, $2, $3, $4, $1, '1.0.0', $5, 1, 'observed', $5, $1, $6, $6, false, $7::jsonb)
`, factID, scopeID, generationID, factKind, sourceSystem, now, string(raw)); err != nil {
		t.Fatalf("seed %s fact %s: %v", factKind, factID, err)
	}
}

func activateCloudReopenOrderingOCI(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	fixture cloudReopenOrderingTestFixture,
) {
	t.Helper()
	now := time.Now().UTC()
	if _, err := db.ExecContext(ctx, `
UPDATE scope_generations
SET status = 'active', activated_at = $1
WHERE generation_id = $2
`, now, fixture.ociGenerationID); err != nil {
		t.Fatalf("activate OCI generation: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
UPDATE ingestion_scopes
SET status = 'active', active_generation_id = $1, ingested_at = $2
WHERE scope_id = $3
`, fixture.ociGenerationID, now, fixture.ociScopeID); err != nil {
		t.Fatalf("activate OCI scope: %v", err)
	}
}

func cloudReopenOrderingQueue(adapter SQLDB, leaseOwner string) ReducerQueue {
	queue := NewReducerQueue(adapter, leaseOwner, time.Minute)
	queue.ClaimDomain = reducer.DomainContainerImageIdentity
	return queue
}

func enqueueCloudReopenOrderingIntent(
	t *testing.T,
	ctx context.Context,
	queue *ReducerQueue,
	fixture cloudReopenOrderingTestFixture,
) reducer.Intent {
	t.Helper()
	result, err := queue.Enqueue(ctx, []projector.ReducerIntent{{
		ScopeID:      fixture.cloudScopeID,
		GenerationID: fixture.cloudGenerationID,
		Domain:       reducer.DomainContainerImageIdentity,
		EntityKey:    "container_image_identity:" + fixture.cloudScopeID,
		Reason:       "cloud image reference observed",
		SourceSystem: "aws",
	}})
	if err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}
	if result.Count != 1 {
		t.Fatalf("Enqueue() count = %d, want 1", result.Count)
	}
	return claimCloudReopenOrderingIntent(t, ctx, queue)
}

func claimCloudReopenOrderingIntent(
	t *testing.T,
	ctx context.Context,
	queue *ReducerQueue,
) reducer.Intent {
	t.Helper()
	intent, claimed, err := queue.Claim(ctx)
	if err != nil {
		t.Fatalf("Claim() error = %v", err)
	}
	if !claimed {
		t.Fatal("Claim() claimed = false, want true")
	}
	return intent
}

func handleAndAckCloudReopenOrderingIntent(
	t *testing.T,
	ctx context.Context,
	queue *ReducerQueue,
	handler reducer.ContainerImageIdentityHandler,
	intent reducer.Intent,
) reducer.Result {
	t.Helper()
	result, err := handler.Handle(ctx, intent)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if err := queue.Ack(ctx, intent, result); err != nil {
		t.Fatalf("Ack() error = %v", err)
	}
	return result
}

func assertCloudReopenOrderingSupportCount(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	scopeID string,
	want int,
) {
	t.Helper()
	var got int
	if err := db.QueryRowContext(ctx, `
SELECT count(*)
FROM container_image_identity_current_supports
WHERE scope_id = $1
`, scopeID).Scan(&got); err != nil {
		t.Fatalf("count current supports: %v", err)
	}
	if got != want {
		t.Fatalf("current supports for %s = %d, want %d", scopeID, got, want)
	}
}

func assertCloudReopenOrderingReopened(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	workItemID string,
) {
	t.Helper()
	var status string
	var reopenedAt sql.NullTime
	if err := db.QueryRowContext(ctx, `
SELECT status, reopened_at
FROM fact_work_items
WHERE work_item_id = $1
`, workItemID).Scan(&status, &reopenedAt); err != nil {
		t.Fatalf("read reopened work item: %v", err)
	}
	if status != "pending" || !reopenedAt.Valid {
		t.Fatalf("reopened work item status/reopened_at = %q/%v, want pending/non-null", status, reopenedAt)
	}
}

func readCloudReopenOrderingTruth(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	fixture cloudReopenOrderingTestFixture,
) cloudReopenOrderingTruth {
	t.Helper()
	var truth cloudReopenOrderingTruth
	var evidenceJSON []byte
	if err := db.QueryRowContext(ctx, `
SELECT digest, image_ref, repository_id, outcome, identity_strength,
       to_json(evidence_fact_ids)
FROM container_image_identity_current_supports
WHERE scope_id = $1
`, fixture.cloudScopeID).Scan(
		&truth.Digest,
		&truth.ImageRef,
		&truth.RepositoryID,
		&truth.Outcome,
		&truth.IdentityStrength,
		&evidenceJSON,
	); err != nil {
		t.Fatalf("read current support for %s: %v", fixture.name, err)
	}
	if err := json.Unmarshal(evidenceJSON, &truth.EvidenceFactIDs); err != nil {
		t.Fatalf("decode evidence fact ids for %s: %v", fixture.name, err)
	}
	slices.Sort(truth.EvidenceFactIDs)
	wantEvidence := []string{fixture.cloudFactID, fixture.ociFactID}
	slices.Sort(wantEvidence)
	if truth.Digest != fixture.digest ||
		truth.ImageRef != fixture.registry+"/"+fixture.repository+"@"+fixture.digest ||
		truth.RepositoryID != fixture.repositoryID ||
		truth.Outcome != "exact_digest" ||
		truth.IdentityStrength != "explicit_digest" ||
		!slices.Equal(truth.EvidenceFactIDs, wantEvidence) {
		t.Fatalf("current support for %s = %#v, want exact typed AWS+OCI identity", fixture.name, truth)
	}
	return truth
}

func cloudReopenOrderingTruthEqual(left cloudReopenOrderingTruth, right cloudReopenOrderingTruth) bool {
	return left.Digest == right.Digest &&
		left.ImageRef == right.ImageRef &&
		left.RepositoryID == right.RepositoryID &&
		left.Outcome == right.Outcome &&
		left.IdentityStrength == right.IdentityStrength &&
		slices.Equal(left.EvidenceFactIDs, right.EvidenceFactIDs)
}
