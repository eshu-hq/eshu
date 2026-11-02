// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"errors"
	"testing"

	"go.opentelemetry.io/otel/trace/noop"

	"github.com/eshu-hq/eshu/go/internal/collector/claim/store"
	"github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// TestBuildClaimedServiceRefusesClaimPlane proves the component-extension
// collector keeps its core Postgres claim store when
// ESHU_WORKFLOW_CLAIM_PLANE_URL is unset and refuses to start when it is set:
// its slot-scope resolver reads workflow_runs rows that only core Postgres
// holds (#8875).
func TestBuildClaimedServiceRefusesClaimPlane(t *testing.T) {
	t.Parallel()
	componentHome := t.TempDir()
	installEnabledComponent(t, componentHome, "process", writeComponentExtensionConfig(t))
	base := map[string]string{
		envComponentHome:            componentHome,
		envComponentTrustMode:       component.TrustModeAllowlist,
		envComponentAllowIDs:        "dev.eshu.examples.scorecard",
		envComponentAllowPublishers: "eshu-hq",
		envComponentCoreVersion:     "dev",
		envCollectorOwnerID:         "component-owner-a",
	}
	tracer := noop.NewTracerProvider().Tracer("test")

	service, err := buildClaimedService(&fakeExecQueryer{}, mapEnv(base), tracer, nil, nil)
	if err != nil {
		t.Fatalf("buildClaimedService(unset) error = %v, want nil", err)
	}
	if _, ok := service.ControlStore.(*postgres.WorkflowControlStore); !ok {
		t.Fatalf("unset ControlStore = %T, want *postgres.WorkflowControlStore", service.ControlStore)
	}
	if fenced, ok := service.Committer.(interface{ ClaimPlaneFenced() bool }); ok && fenced.ClaimPlaneFenced() {
		t.Fatalf("core Committer = %T fences on the claim plane, want the in-tree fence (#9376)", service.Committer)
	}

	withURL := map[string]string{store.EnvClaimPlaneURL: "http://eshu-workflow-claim-plane:8080"}
	for key, value := range base {
		withURL[key] = value
	}
	_, err = buildClaimedService(&fakeExecQueryer{}, mapEnv(withURL), tracer, nil, nil)
	if !errors.Is(err, store.ErrClaimPlaneUnsupported) {
		t.Fatalf("buildClaimedService(claim plane) error = %v, want ErrClaimPlaneUnsupported", err)
	}
}
