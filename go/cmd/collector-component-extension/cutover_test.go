// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"errors"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/trace/noop"

	"github.com/eshu-hq/eshu/go/internal/collector/claim/store"
	"github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func testCutoverManifest() component.Manifest {
	return component.Manifest{
		Metadata: component.Metadata{
			ID:        "dev.eshu.examples.scorecard",
			Name:      "Reference Scorecard collector",
			Publisher: "eshu-hq",
			Version:   "0.1.0",
		},
		Spec: component.Spec{CollectorKinds: []string{"scorecard"}},
	}
}

func testCutoverEnv(extra map[string]string) func(string) string {
	return mapEnv(extra)
}

// TestResolveCutoverPlanDefaultsOff proves the cutover gate is default-off:
// no flag means the in-tree path with no error, even when the claim-plane
// URL and producer pins are set (eshu-hq/eshu#8877 leaf 1).
func TestResolveCutoverPlanDefaultsOff(t *testing.T) {
	t.Parallel()

	for name, flag := range map[string]string{
		"unset": "",
		"blank": "   ",
		"false": "false",
		"zero":  "0",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			getenv := testCutoverEnv(map[string]string{
				envCutoverEnabled:         flag,
				store.EnvClaimPlaneURL:    "http://eshu-workflow-claim-plane:8080",
				envCutoverProducerID:      "dev.eshu.examples.scorecard",
				envCutoverProducerVersion: "0.1.0",
			})
			plan, err := resolveCutoverPlan(getenv, testCutoverManifest())
			if err != nil {
				t.Fatalf("resolveCutoverPlan() error = %v, want nil", err)
			}
			if plan.enabled {
				t.Fatal("resolveCutoverPlan().enabled = true, want false with the flag off")
			}
		})
	}
}

// TestResolveCutoverPlanRejectsUnknownFlagValue proves an unrecognized flag
// spelling fails startup closed naming the variable, instead of silently
// staying off or turning on (no silent third option, #8877 leaf 1).
func TestResolveCutoverPlanRejectsUnknownFlagValue(t *testing.T) {
	t.Parallel()

	for _, flag := range []string{"maybe", "yes", "on", "TrUe"} {
		getenv := testCutoverEnv(map[string]string{envCutoverEnabled: flag})
		if _, err := resolveCutoverPlan(getenv, testCutoverManifest()); err == nil {
			t.Fatalf("resolveCutoverPlan(%q) error = nil, want startup refusal", flag)
		} else if !strings.Contains(err.Error(), envCutoverEnabled) {
			t.Fatalf("resolveCutoverPlan(%q) error = %q, want it to name %s", flag, err, envCutoverEnabled)
		}
	}
}

// TestResolveCutoverPlanRequiresClaimPlaneURLWhenEnabled proves the enabled
// gate without a claim-plane URL fails closed naming that variable: the
// remote client, claim store, and commit fence must all point at one plane.
func TestResolveCutoverPlanRequiresClaimPlaneURLWhenEnabled(t *testing.T) {
	t.Parallel()

	getenv := testCutoverEnv(map[string]string{
		envCutoverEnabled:         "true",
		envCutoverProducerID:      "dev.eshu.examples.scorecard",
		envCutoverProducerVersion: "0.1.0",
	})
	_, err := resolveCutoverPlan(getenv, testCutoverManifest())
	if err == nil {
		t.Fatal("resolveCutoverPlan() error = nil, want refusal without the claim-plane URL")
	}
	if !strings.Contains(err.Error(), store.EnvClaimPlaneURL) {
		t.Fatalf("resolveCutoverPlan() error = %q, want it to name %s", err, store.EnvClaimPlaneURL)
	}
}

// TestResolveCutoverPlanRequiresProducerPinWhenEnabled proves the enabled
// gate without a producer pin fails closed: the holder identity must be
// pinned by the operator, never derived silently.
func TestResolveCutoverPlanRequiresProducerPinWhenEnabled(t *testing.T) {
	t.Parallel()

	cases := map[string]map[string]string{
		"missing id": {
			envCutoverEnabled:         "1",
			store.EnvClaimPlaneURL:    "http://eshu-workflow-claim-plane:8080",
			envCutoverProducerVersion: "0.1.0",
		},
		"missing version": {
			envCutoverEnabled:      "1",
			store.EnvClaimPlaneURL: "http://eshu-workflow-claim-plane:8080",
			envCutoverProducerID:   "dev.eshu.examples.scorecard",
		},
	}
	for name, values := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := resolveCutoverPlan(testCutoverEnv(values), testCutoverManifest())
			if err == nil {
				t.Fatalf("resolveCutoverPlan(%s) error = nil, want refusal", name)
			}
			if !strings.Contains(err.Error(), "ESHU_EXTENSIONHOST_CUTOVER_PRODUCER") {
				t.Fatalf("resolveCutoverPlan(%s) error = %q, want it to name the producer pin", name, err)
			}
		})
	}
}

// TestResolveCutoverPlanRefusesProducerMismatch proves a producer pin that
// disagrees with the verified manifest fails closed naming only the
// variable: a misconfigured host must never prove as another holder, and
// the refused value stays out of the error.
func TestResolveCutoverPlanRefusesProducerMismatch(t *testing.T) {
	t.Parallel()

	const impostor = "dev.eshu.examples.impostor"
	getenv := testCutoverEnv(map[string]string{
		envCutoverEnabled:         "true",
		store.EnvClaimPlaneURL:    "http://eshu-workflow-claim-plane:8080",
		envCutoverProducerID:      impostor,
		envCutoverProducerVersion: "0.1.0",
	})
	_, err := resolveCutoverPlan(getenv, testCutoverManifest())
	if err == nil {
		t.Fatal("resolveCutoverPlan() error = nil, want refusal on producer mismatch")
	}
	if !strings.Contains(err.Error(), envCutoverProducerID) {
		t.Fatalf("resolveCutoverPlan() error = %q, want it to name %s", err, envCutoverProducerID)
	}
	if strings.Contains(err.Error(), impostor) {
		t.Fatalf("resolveCutoverPlan() error = %q, want it to withhold the refused value", err)
	}
}

// TestResolveCutoverPlanBindsManifestIdentity proves the enabled plan binds
// the verified manifest's identity and collector kinds as the remote
// client's holder: every grant decision evaluates under exactly this
// identity, never one taken from the wire.
func TestResolveCutoverPlanBindsManifestIdentity(t *testing.T) {
	t.Parallel()

	getenv := testCutoverEnv(map[string]string{
		envCutoverEnabled:         "true",
		store.EnvClaimPlaneURL:    "http://eshu-workflow-claim-plane:8080",
		envCutoverProducerID:      "dev.eshu.examples.scorecard",
		envCutoverProducerVersion: "0.1.0",
	})
	plan, err := resolveCutoverPlan(getenv, testCutoverManifest())
	if err != nil {
		t.Fatalf("resolveCutoverPlan() error = %v, want nil", err)
	}
	if !plan.enabled {
		t.Fatal("resolveCutoverPlan().enabled = false, want true")
	}
	if plan.producerID != "dev.eshu.examples.scorecard" || plan.producerVersion != "0.1.0" {
		t.Fatalf("resolveCutoverPlan() identity = %q %q, want the manifest identity",
			plan.producerID, plan.producerVersion)
	}
	if len(plan.scopes) != 1 || plan.scopes[0] != "scorecard" {
		t.Fatalf("resolveCutoverPlan().scopes = %q, want [scorecard]", plan.scopes)
	}
}

// TestBuildClaimedServiceKeepsRefusalWhenCutoverOff proves the
// URL-set-refuses-startup law still holds with the flag explicitly off:
// lifting it is reserved for the designed enabled path.
func TestBuildClaimedServiceKeepsRefusalWhenCutoverOff(t *testing.T) {
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
		envCutoverEnabled:           "false",
		store.EnvClaimPlaneURL:      "http://eshu-workflow-claim-plane:8080",
	}
	tracer := noop.NewTracerProvider().Tracer("test")

	_, err := buildClaimedService(&fakeExecQueryer{}, mapEnv(base), tracer, nil, nil)
	if !errors.Is(err, store.ErrClaimPlaneUnsupported) {
		t.Fatalf("buildClaimedService(cutover off) error = %v, want ErrClaimPlaneUnsupported", err)
	}
}

// TestBuildClaimedServiceRefusesEnabledCutoverWithoutProducerPin proves the
// enabled flag without the producer pin fails startup before any claim: the
// CoreOnlyReason lift never opens an unbound holder.
func TestBuildClaimedServiceRefusesEnabledCutoverWithoutProducerPin(t *testing.T) {
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
		envCutoverEnabled:           "true",
		store.EnvClaimPlaneURL:      "http://eshu-workflow-claim-plane:8080",
	}
	tracer := noop.NewTracerProvider().Tracer("test")

	_, err := buildClaimedService(&fakeExecQueryer{}, mapEnv(base), tracer, nil, nil)
	if err == nil {
		t.Fatal("buildClaimedService(enabled, no pin) error = nil, want startup refusal")
	}
	if !strings.Contains(err.Error(), "ESHU_EXTENSIONHOST_CUTOVER_PRODUCER") {
		t.Fatalf("buildClaimedService(enabled, no pin) error = %q, want it to name the producer pin", err)
	}
}

// TestBuildClaimedServiceRoutesEnabledFlow proves the designed path: with
// the flag on, the claim-plane URL set, and the producer pin matching the
// verified manifest, claims flow through the claim-plane client and commits
// fence on its pin. Construction makes no network calls.
func TestBuildClaimedServiceRoutesEnabledFlow(t *testing.T) {
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
		envCutoverEnabled:           "true",
		store.EnvClaimPlaneURL:      "http://eshu-workflow-claim-plane:8080",
		envCutoverProducerID:        "dev.eshu.examples.scorecard",
		envCutoverProducerVersion:   "0.1.0",
	}
	tracer := noop.NewTracerProvider().Tracer("test")

	service, err := buildClaimedService(&fakeExecQueryer{}, mapEnv(base), tracer, nil, nil)
	if err != nil {
		t.Fatalf("buildClaimedService(enabled) error = %v, want nil", err)
	}
	if _, ok := service.ControlStore.(*workflow.ClaimPlaneClient); !ok {
		t.Fatalf("ControlStore = %T, want *workflow.ClaimPlaneClient", service.ControlStore)
	}
	fenced, ok := service.Committer.(interface{ ClaimPlaneFenced() bool })
	if !ok {
		t.Fatalf("Committer = %T, want it to report claim-plane fencing", service.Committer)
	}
	if !fenced.ClaimPlaneFenced() {
		t.Fatal("Committer.ClaimPlaneFenced() = false, want the plane commit fence on the enabled path")
	}
}
