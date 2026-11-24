// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build replayseed && !windows

package main

// Hosted-family seeder for arm T2 of the #10882 built-binary replay
// (scripts/dev/10882-replay). It prepares what the built
// eshu-collector-component-extension binary needs to run offline against local
// PostgreSQL on the in-tree claim path: an on-disk component registry with the
// synthetic zero-fact component installed, granted and enabled, and N pending
// workflow items planted by the real generic planner, whose source_system is the
// component id. The binary then claims them through its real dispatcher, which
// consults per-source backpressure before each grant.
//
// The synthetic emitter commits no facts, so T2 proves claim pacing (pending
// items stay pending, no attempts spent, each completes after release) and
// nothing about hosted fact commit.

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/component"
	activation "github.com/eshu-hq/eshu/go/internal/coordinator/component/activation"
	extplanner "github.com/eshu-hq/eshu/go/internal/coordinator/planner/component/extension"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// TestReplaySeedHostedFamily installs the synthetic component into
// ESHU_REPLAY_COMPONENT_HOME (runtime command ESHU_REPLAY_SYNTH_BIN) and plants
// ESHU_REPLAY_HOSTED_ITEMS pending work items in ESHU_REPLAY_DSN.
func TestReplaySeedHostedFamily(t *testing.T) {
	dsn := os.Getenv("ESHU_REPLAY_DSN")
	home := os.Getenv("ESHU_REPLAY_COMPONENT_HOME")
	synthBin := os.Getenv("ESHU_REPLAY_SYNTH_BIN")
	if dsn == "" || home == "" || synthBin == "" {
		t.Skip("set ESHU_REPLAY_DSN, ESHU_REPLAY_COMPONENT_HOME and ESHU_REPLAY_SYNTH_BIN to run the hosted-family seeder")
	}
	items := 24
	if raw := os.Getenv("ESHU_REPLAY_HOSTED_ITEMS"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			t.Fatalf("ESHU_REPLAY_HOSTED_ITEMS = %q: %v", raw, err)
		}
		items = parsed
	}
	ctx := context.Background()

	if err := os.MkdirAll(home, 0o750); err != nil {
		t.Fatalf("create component home: %v", err)
	}
	registry := component.NewRegistry(home)
	manifestPath, err := filepath.Abs("manifest.yaml")
	if err != nil {
		t.Fatalf("abs manifest: %v", err)
	}
	if _, err := registry.Install(manifestPath, component.VerificationResult{
		Allowed: true, Mode: component.TrustModeAllowlist, Component: synthComponentID,
		Publisher: "eshu-hq", Version: synthVersion,
	}); err != nil {
		t.Fatalf("Install(manifest.yaml): %v", err)
	}
	grantRaw, err := os.ReadFile("trust-grant.json")
	if err != nil {
		t.Fatalf("read trust-grant.json: %v", err)
	}
	var grant component.ProducerGrant
	if err := json.Unmarshal(grantRaw, &grant); err != nil {
		t.Fatalf("decode trust-grant.json: %v", err)
	}
	if err := registry.RecordGrant(grant); err != nil {
		t.Fatalf("RecordGrant: %v", err)
	}
	configPath := filepath.Join(home, "replay-runtime-config.yaml")
	runtimeConfig := fmt.Sprintf("host: {}\nprocess:\n  command: %s\n  args: []\n  env: []\n  dir: \"\"\n"+
		"  stdout_limit_bytes: 4194304\n  stderr_limit_bytes: 65536\noci: {}\nconfig: {}\n", synthBin)
	if err := os.WriteFile(configPath, []byte(runtimeConfig), 0o600); err != nil {
		t.Fatalf("write runtime config: %v", err)
	}
	if _, err := registry.Enable(synthComponentID, component.Activation{
		InstanceID: synthInstanceID, Mode: "scheduled", ClaimsEnabled: true, ConfigPath: configPath,
	}); err != nil {
		t.Fatalf("Enable: %v", err)
	}

	manifestBytes, err := os.ReadFile("manifest.yaml")
	if err != nil {
		t.Fatalf("read manifest.yaml: %v", err)
	}
	sum := sha256.Sum256(manifestBytes)
	activationDoc, err := json.Marshal(map[string]any{
		"schema_version": "eshu.component.instance.v1", "component_id": synthComponentID,
		"component_version": synthVersion, "publisher": "eshu-hq", "manifest_digest": hex.EncodeToString(sum[:]),
		"config_handle": "synth-window-handle",
		"runtime":       map[string]any{"sdk_protocol": "collector-sdk/v1alpha1", "adapter": "process"},
	})
	if err != nil {
		t.Fatalf("marshal activation document: %v", err)
	}
	now := time.Now().UTC()
	instance := workflow.CollectorInstance{
		InstanceID: synthInstanceID, CollectorKind: scope.CollectorKind(synthKind), Mode: workflow.CollectorModeScheduled,
		Enabled: true, ClaimsEnabled: true, DisplayName: "replay hosted family", Configuration: string(activationDoc),
		LastObservedAt: now, CreatedAt: now, UpdatedAt: now,
	}
	database, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	defer func() { _ = database.Close() }()
	control := postgres.NewWorkflowControlStore(postgres.SQLDB{DB: database})
	planner := extplanner.WorkPlanner{}
	for i := 1; i <= items; i++ {
		run, planned, err := planner.PlanComponentExtensionWork(ctx, extplanner.PlanRequest{
			Instance: instance, ObservedAt: now, PlanKey: fmt.Sprintf("replay-hosted-%04d", i),
		})
		if err != nil {
			t.Fatalf("plan item %d: %v", i, err)
		}
		if len(planned) != 1 || planned[0].SourceSystem != synthComponentID {
			t.Fatalf("plan %d: %d items, source %q, want one item attributed to %q", i, len(planned), planned[0].SourceSystem, synthComponentID)
		}
		if err := control.CreateRun(ctx, run); err != nil {
			t.Fatalf("CreateRun %d: %v", i, err)
		}
		if err := control.EnqueueWorkItems(ctx, planned); err != nil {
			t.Fatalf("EnqueueWorkItems %d: %v", i, err)
		}
	}
	if _, _, err := activation.ParseConfig(instance.Configuration); err != nil {
		t.Fatalf("ParseConfig(activation document): %v", err)
	}
	t.Logf("replay hosted family: %d pending work items for %s/%s", items, synthKind, synthInstanceID)
}
