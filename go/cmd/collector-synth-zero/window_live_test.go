// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build !windows

package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	sdkcollector "github.com/eshu-hq/eshu-sdk/collector"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/extensionhost"
	"github.com/eshu-hq/eshu/go/internal/component"
	activation "github.com/eshu-hq/eshu/go/internal/coordinator/component/activation"
	extplanner "github.com/eshu-hq/eshu/go/internal/coordinator/planner/component/extension"
	"github.com/eshu-hq/eshu/go/internal/eshulocal"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// captureRecorder keeps the status rows the host records for one run.
type captureRecorder struct {
	records []extensionhost.StatusRecord
}

func (c *captureRecorder) RecordExtensionStatus(_ context.Context, record extensionhost.StatusRecord) error {
	c.records = append(c.records, record)
	return nil
}

// buildEmitter compiles the lane binary under a temp dir and returns its
// path. The end-to-end spawns this exact binary through the host process
// runner, so the test proves the shipped artifact, not an in-process call.
func buildEmitter(t *testing.T) string {
	t.Helper()

	bin := filepath.Join(t.TempDir(), "eshu-collector-synth-zero")
	cmd := exec.Command("go", "build", "-trimpath", "-o", bin, ".")
	cmd.Dir = "."
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build emitter: %v\n%s", err, out)
	}
	return bin
}

func tableCount(t *testing.T, db *sql.DB, table string) int64 {
	t.Helper()

	var count int64
	if err := db.QueryRow(`SELECT count(*) FROM ` + table).Scan(&count); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return count
}

func itemStatus(t *testing.T, db *sql.DB, workItemID string) string {
	t.Helper()

	var status string
	if err := db.QueryRow(
		`SELECT status FROM workflow_work_items WHERE work_item_id = $1`, workItemID,
	).Scan(&status); err != nil {
		t.Fatalf("read work item status: %v", err)
	}
	return status
}

// TestWindowZeroFactEndToEnd runs the window vehicle on a disposable stack:
// plan (real generic planner) → claim → execute (real emitter binary through
// the real host source) → complete → ack, against embedded Postgres torn
// down at the end. It asserts the item completes and fact, generation, and
// projection-queue tables hold zero new rows.
//
// The run uses the default-off in-tree claim path (no claim-plane URL): the
// emission truth the window proves — an unchanged result carrying no facts —
// is identical on either backend, because only claim acquisition and commit
// fencing differ. The cutover gate itself is pinned by
// TestWindowDeploymentCarriesCutoverPin against the same manifest.
func TestWindowZeroFactEndToEnd(t *testing.T) {
	// Gated behind ESHU_SYNTH_ZERO_LIVE: the test boots a real embedded
	// Postgres (downloads a distribution on first run, binds a port) and
	// builds the emitter binary. Skipping is the correct default, matching
	// the embedded-postgres live convention; the lane proof runs it
	// explicitly and records the command in the README.
	if strings.TrimSpace(os.Getenv("ESHU_SYNTH_ZERO_LIVE")) == "" {
		t.Skip("set ESHU_SYNTH_ZERO_LIVE=1 to run the disposable-venue end-to-end")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()

	root := t.TempDir()
	managed, err := eshulocal.StartEmbeddedPostgres(ctx, eshulocal.Layout{
		WorkspaceID: "synth-zero-window",
		PostgresDir: filepath.Join(root, "postgres"),
		CacheDir:    filepath.Join(root, "cache"),
		LogsDir:     filepath.Join(root, "logs"),
	})
	if err != nil {
		t.Fatalf("start embedded postgres: %v", err)
	}
	// Teardown is part of the proof: the venue holds no shared store, so
	// stopping it must succeed and temp dirs vanish with the test.
	defer func() {
		if err := managed.Close(); err != nil {
			t.Fatalf("stop embedded postgres: %v", err)
		}
	}()

	database, err := sql.Open("pgx", managed.DSN)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	defer func() { _ = database.Close() }()
	if err := database.PingContext(ctx); err != nil {
		t.Fatalf("ping postgres: %v", err)
	}
	if err := postgres.ApplyBootstrap(ctx, postgres.SQLDB{DB: database}); err != nil {
		t.Fatalf("ApplyBootstrap(): %v", err)
	}

	emitterBin := buildEmitter(t)
	home := seedRegistry(t)
	manifest, err := component.NewRegistry(home).LoadInstalledManifest(synthComponentID, synthVersion)
	if err != nil {
		t.Fatalf("LoadInstalledManifest() error = %v", err)
	}
	manifestBytes, err := os.ReadFile("manifest.yaml")
	if err != nil {
		t.Fatalf("read manifest.yaml: %v", err)
	}
	sum := sha256.Sum256(manifestBytes)
	activationDoc, err := json.Marshal(map[string]any{
		"schema_version":    "eshu.component.instance.v1",
		"component_id":      synthComponentID,
		"component_version": synthVersion,
		"publisher":         "eshu-hq",
		"manifest_digest":   hex.EncodeToString(sum[:]),
		"config_handle":     "synth-window-handle",
		"runtime": map[string]any{
			"sdk_protocol": "collector-sdk/v1alpha1",
			"adapter":      "process",
		},
	})
	if err != nil {
		t.Fatalf("marshal activation document: %v", err)
	}
	now := time.Now().UTC()
	instance := workflow.CollectorInstance{
		InstanceID:     synthInstanceID,
		CollectorKind:  scope.CollectorKind(synthKind),
		Mode:           workflow.CollectorModeScheduled,
		Enabled:        true,
		ClaimsEnabled:  true,
		DisplayName:    "Synthetic zero-fact window instance",
		Configuration:  string(activationDoc),
		LastObservedAt: now,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if _, _, err := activation.ParseConfig(instance.Configuration); err != nil {
		t.Fatalf("ParseConfig(activation document) error = %v", err)
	}

	// Plan through the real generic component planner: the dedicated kind
	// must plan exactly one pending item.
	planner := extplanner.WorkPlanner{}
	run, items, err := planner.PlanComponentExtensionWork(ctx, extplanner.PlanRequest{
		Instance:   instance,
		ObservedAt: now,
		PlanKey:    "synth-window-1",
	})
	if err != nil {
		t.Fatalf("PlanComponentExtensionWork() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("planned items = %d, want 1", len(items))
	}
	if string(items[0].CollectorKind) != synthKind || items[0].CollectorInstanceID != synthInstanceID {
		t.Fatalf("planned item targets %s/%s, want the synth kind and instance",
			items[0].CollectorKind, items[0].CollectorInstanceID)
	}

	control := postgres.NewWorkflowControlStore(postgres.SQLDB{DB: database})
	if err := control.CreateRun(ctx, run); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	if err := control.EnqueueWorkItems(ctx, items); err != nil {
		t.Fatalf("EnqueueWorkItems() error = %v", err)
	}

	beforeFacts := tableCount(t, database, "fact_records")
	beforeGenerations := tableCount(t, database, "scope_generations")
	beforeQueue := tableCount(t, database, "fact_work_items")

	recorder := &captureRecorder{}
	source, err := extensionhost.NewSource(extensionhost.Config{
		Manifest:            manifest,
		CollectorInstanceID: synthInstanceID,
		ScopeKind:           "component",
		ConfigHandle:        "synth-window-handle",
		Runner:              extensionhost.ProcessRunner{Command: emitterBin},
		StatusRecorder:      recorder,
		Clock:               time.Now,
	})
	if err != nil {
		t.Fatalf("NewSource() error = %v", err)
	}
	service := collector.ClaimedService{
		ControlStore:        control,
		Source:              source,
		Committer:           postgres.NewIngestionStore(postgres.SQLDB{DB: database}),
		CollectorKind:       scope.CollectorKind(synthKind),
		CollectorInstanceID: synthInstanceID,
		OwnerID:             "synth-window-owner",
		ClaimIDFunc:         func() string { return fmt.Sprintf("synth-window-claim-%d", time.Now().UTC().UnixNano()) },
		PollInterval:        100 * time.Millisecond,
		ClaimLeaseTTL:       time.Minute,
		HeartbeatInterval:   5 * time.Second,
		MaxAttempts:         3,
		Clock:               time.Now,
	}

	runCtx, stopService := context.WithCancel(ctx)
	defer stopService()
	runErr := make(chan error, 1)
	go func() { runErr <- service.Run(runCtx) }()

	workItemID := items[0].WorkItemID
	deadline := time.Now().Add(2 * time.Minute)
	for {
		if status := itemStatus(t, database, workItemID); status == string(workflow.WorkItemStatusCompleted) {
			break
		}
		if time.Now().After(deadline) {
			stopService()
			t.Fatalf("work item %s not completed before deadline (status %s)",
				workItemID, itemStatus(t, database, workItemID))
		}
		time.Sleep(200 * time.Millisecond)
	}
	stopService()
	if err := <-runErr; err != nil {
		t.Fatalf("service Run() error = %v, want nil after clean stop", err)
	}

	if got := itemStatus(t, database, workItemID); got != string(workflow.WorkItemStatusCompleted) {
		t.Fatalf("final status = %s, want completed", got)
	}
	if got := tableCount(t, database, "fact_records"); got != beforeFacts {
		t.Fatalf("fact_records delta = %d, want 0", got-beforeFacts)
	}
	if got := tableCount(t, database, "scope_generations"); got != beforeGenerations {
		t.Fatalf("scope_generations delta = %d, want 0", got-beforeGenerations)
	}
	if got := tableCount(t, database, "fact_work_items"); got != beforeQueue {
		t.Fatalf("fact_work_items delta = %d, want 0", got-beforeQueue)
	}
	if len(recorder.records) != 1 {
		t.Fatalf("recorded statuses = %d, want exactly 1", len(recorder.records))
	}
	recorded := recorder.records[0]
	if recorded.State != sdkcollector.ResultUnchanged {
		t.Fatalf("recorded state = %q, want unchanged", recorded.State)
	}
	if recorded.Class != sdkcollector.StatusComplete || recorded.FactCount != 0 {
		t.Fatalf("recorded status = %+v, want {complete, fact_count 0}", recorded)
	}
	if recorded.GenerationID != items[0].GenerationID {
		t.Fatalf("recorded generation %q, want the claimed generation %q",
			recorded.GenerationID, items[0].GenerationID)
	}
}
