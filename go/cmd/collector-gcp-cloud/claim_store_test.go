// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/trace/noop"
	"golang.org/x/oauth2"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/claim/store"
	"github.com/eshu-hq/eshu/go/internal/collector/gcpcloud/gcpruntime"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// TestBuildClaimedServiceSelectsClaimStoreFromEnv proves the collector wires
// its claim store through store.New: unset keeps the core Postgres
// store, a valid ESHU_WORKFLOW_CLAIM_PLANE_URL selects the claim-plane
// client, and an invalid one fails startup naming the variable (#8875).
//
// buildClaimedService builds the live cloud provider before the claim store,
// and the production factory resolves real cloud credentials. The test stubs
// the newGCPADCLiveClient package seam so it needs no credentials or network. It
// swaps a package variable, so it does not run in parallel.
func TestBuildClaimedServiceSelectsClaimStoreFromEnv(t *testing.T) {
	oldFactory := newGCPADCLiveClient
	t.Cleanup(func() { newGCPADCLiveClient = oldFactory })
	newGCPADCLiveClient = func(_ context.Context, credentialRef string, quotaProjectID string) (gcpruntime.LiveClient, error) {
		return gcpruntime.LiveClient{
			CredentialRef:  credentialRef,
			QuotaProjectID: quotaProjectID,
			TokenSource:    oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "test-token"}),
		}, nil
	}
	build := func(raw string) (collector.ClaimedService, error) {
		return buildClaimedService(
			context.Background(),
			postgres.SQLDB{},
			smokeRedactionKey(t),
			func(key string) string {
				if key == store.EnvClaimPlaneURL {
					return raw
				}
				return claimedRuntimeEnv(key)
			},
			noop.NewTracerProvider().Tracer("test"),
			metricnoop.NewMeterProvider().Meter("test"),
			nil,
			slog.Default(),
		)
	}

	service, err := build("")
	if err != nil {
		t.Fatalf("buildClaimedService(unset) error = %v, want nil", err)
	}
	if _, ok := service.ControlStore.(*postgres.WorkflowControlStore); !ok {
		t.Fatalf("unset ControlStore = %T, want *postgres.WorkflowControlStore", service.ControlStore)
	}
	if fenced, ok := service.Committer.(interface{ ClaimPlaneFenced() bool }); ok && fenced.ClaimPlaneFenced() {
		t.Fatalf("core Committer = %T fences on the claim plane, want the in-tree fence (#9376)", service.Committer)
	}

	service, err = build("http://eshu-workflow-claim-plane:8080")
	if err != nil {
		t.Fatalf("buildClaimedService(claim plane) error = %v, want nil", err)
	}
	if _, ok := service.ControlStore.(*workflow.ClaimPlaneClient); !ok {
		t.Fatalf("claim-plane ControlStore = %T, want *workflow.ClaimPlaneClient", service.ControlStore)
	}
	if fenced, ok := service.Committer.(interface{ ClaimPlaneFenced() bool }); !ok || !fenced.ClaimPlaneFenced() {
		t.Fatalf("claim-plane Committer = %T, want one that fences commits on the claim plane (#9376)", service.Committer)
	}

	_, err = build("ftp://claim-plane")
	if err == nil || !strings.Contains(err.Error(), store.EnvClaimPlaneURL) {
		t.Fatalf("buildClaimedService(invalid) error = %v, want one naming %s", err, store.EnvClaimPlaneURL)
	}
}
