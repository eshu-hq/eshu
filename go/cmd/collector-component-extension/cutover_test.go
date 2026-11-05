// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel/trace/noop"

	"github.com/eshu-hq/eshu/go/internal/collector/claim/store"
	remoteclaim "github.com/eshu-hq/eshu/go/internal/collector/extensionhost/remote/claim"
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

// recordingGrantObserver captures producer-grant decisions for the
// cutover-observer proof. It implements component.GrantObserver and is safe
// for concurrent use.
type recordingGrantObserver struct {
	mu        sync.Mutex
	decisions []component.GrantDecision
}

func (r *recordingGrantObserver) ObserveGrantDecision(_ context.Context, d component.GrantDecision) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.decisions = append(r.decisions, d)
}

func (r *recordingGrantObserver) snapshot() []component.GrantDecision {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]component.GrantDecision(nil), r.decisions...)
}

const (
	cutoverObserverComponentID = "dev.eshu.examples.granted"
	cutoverObserverVersion     = "0.1.0"
	cutoverObserverCoreKind    = "aws_resource"
	cutoverObserverScope       = "scorecard"
	cutoverObserverSchema      = "1.0.0"
)

// cutoverObserverHome records one live grant for a core-owned kind and
// returns the registry home plus the enabled plan pinned to that holder. The
// plan URL is a dummy: wiring and emission decisions read grants locally and
// make no network calls.
func cutoverObserverHome(t *testing.T) (string, cutoverPlan) {
	t.Helper()

	home := t.TempDir()
	registry := component.NewRegistry(home)
	grant := component.ProducerGrant{
		ProducerID:     cutoverObserverComponentID,
		Version:        cutoverObserverVersion,
		Kind:           cutoverObserverCoreKind,
		SchemaVersions: []string{cutoverObserverSchema},
		Scope:          cutoverObserverScope,
		ExpiresAt:      time.Now().Add(time.Hour).UTC(),
	}
	if err := registry.RecordGrant(grant); err != nil {
		t.Fatalf("RecordGrant() error = %v, want nil", err)
	}
	plan := cutoverPlan{
		enabled:         true,
		remoteURL:       "http://eshu-workflow-claim-plane:8080",
		producerID:      cutoverObserverComponentID,
		producerVersion: cutoverObserverVersion,
		scopes:          []string{cutoverObserverScope},
	}
	return home, plan
}

// TestBuildRemoteCutoverObservesWiringAllow proves the enabled cutover hands
// seam wiring decisions to the worker observer: a live grant authorizes the
// declared core-owned kind and the observer records exactly one activation
// allow naming the pinned holder. It fails while buildRemoteCutover leaves
// the seam Observer nil.
func TestBuildRemoteCutoverObservesWiringAllow(t *testing.T) {
	t.Parallel()

	home, plan := cutoverObserverHome(t)
	observer := &recordingGrantObserver{}
	cutover, err := buildRemoteCutover(plan, home, observer)
	if err != nil {
		t.Fatalf("buildRemoteCutover(enabled) error = %v, want nil", err)
	}
	emissions := []remoteclaim.DeclaredEmission{{Kind: cutoverObserverCoreKind, SchemaVersion: cutoverObserverSchema}}
	if err := cutover.VerifyWiring(context.Background(), emissions); err != nil {
		t.Fatalf("VerifyWiring() error = %v, want nil for the live grant", err)
	}
	got := observer.snapshot()
	if len(got) != 1 {
		t.Fatalf("observed decisions = %d, want exactly 1 wiring allow", len(got))
	}
	decision := got[0]
	if !decision.Allowed {
		t.Errorf("decision.Allowed = false, want true for the live grant")
	}
	if decision.Reason != component.GrantReasonGranted {
		t.Errorf("decision.Reason = %q, want %q", decision.Reason, component.GrantReasonGranted)
	}
	if decision.Stage != component.GrantStageActivation {
		t.Errorf("decision.Stage = %q, want the activation-stage counterpart of seam wiring", decision.Stage)
	}
	if decision.ProducerID != cutoverObserverComponentID || decision.Version != cutoverObserverVersion {
		t.Errorf("decision holder = %q %q, want the pinned holder", decision.ProducerID, decision.Version)
	}
	if decision.Kind != cutoverObserverCoreKind {
		t.Errorf("decision.Kind = %q, want %q", decision.Kind, cutoverObserverCoreKind)
	}
}

// TestBuildRemoteCutoverObservesEmissionDenyAfterRevoke is the trust-boundary
// proof for the enabled path: after the grant is revoked, the next emission
// fails closed AND the denial is observed with the revoked reason, so a
// revoked producer cannot emit silently on the extracted path. It fails while
// the seam Observer stays nil.
func TestBuildRemoteCutoverObservesEmissionDenyAfterRevoke(t *testing.T) {
	t.Parallel()

	home, plan := cutoverObserverHome(t)
	registry := component.NewRegistry(home)
	if err := registry.RevokeGrant(cutoverObserverComponentID, cutoverObserverVersion, cutoverObserverCoreKind, cutoverObserverScope); err != nil {
		t.Fatalf("RevokeGrant() error = %v, want nil", err)
	}
	observer := &recordingGrantObserver{}
	cutover, err := buildRemoteCutover(plan, home, observer)
	if err != nil {
		t.Fatalf("buildRemoteCutover(enabled) error = %v, want nil", err)
	}
	err = cutover.AuthorizeEmission(context.Background(), cutoverObserverCoreKind, cutoverObserverSchema)
	deny, ok := err.(*remoteclaim.DenyError)
	if !ok {
		t.Fatalf("AuthorizeEmission() error = %v (%T), want *DenyError after revoke", err, err)
	}
	if deny.Reason != component.GrantReasonRevoked {
		t.Fatalf("DenyError.Reason = %q, want %q", deny.Reason, component.GrantReasonRevoked)
	}
	got := observer.snapshot()
	if len(got) != 1 {
		t.Fatalf("observed decisions = %d, want exactly 1 emission deny", len(got))
	}
	decision := got[0]
	if decision.Allowed {
		t.Errorf("decision.Allowed = true, want false after revoke")
	}
	if decision.Reason != component.GrantReasonRevoked {
		t.Errorf("decision.Reason = %q, want %q", decision.Reason, component.GrantReasonRevoked)
	}
	if decision.Stage != component.GrantStageEmission {
		t.Errorf("decision.Stage = %q, want %q", decision.Stage, component.GrantStageEmission)
	}
	if decision.ProducerID != cutoverObserverComponentID || decision.Version != cutoverObserverVersion {
		t.Errorf("decision holder = %q %q, want the pinned holder, never a wire value", decision.ProducerID, decision.Version)
	}
}

// TestBuildRemoteCutoverNilObserverKeepsOutcome proves observation never
// changes the allow/deny outcome: with a nil observer the enabled cutover
// still authorizes the live grant. The nil path stays the disabled-observation
// default, never a silent behavior change.
func TestBuildRemoteCutoverNilObserverKeepsOutcome(t *testing.T) {
	t.Parallel()

	home, plan := cutoverObserverHome(t)
	cutover, err := buildRemoteCutover(plan, home, nil)
	if err != nil {
		t.Fatalf("buildRemoteCutover(enabled, nil observer) error = %v, want nil", err)
	}
	emissions := []remoteclaim.DeclaredEmission{{Kind: cutoverObserverCoreKind, SchemaVersion: cutoverObserverSchema}}
	if err := cutover.VerifyWiring(context.Background(), emissions); err != nil {
		t.Fatalf("VerifyWiring(nil observer) error = %v, want nil for the live grant", err)
	}
}

// TestBuildRemoteCutoverDisabledBuildsNothingWithObserver proves the
// default-off gate ignores the observer: a disabled plan builds no cutover,
// so no observation is possible and the in-tree path stays byte-identical.
func TestBuildRemoteCutoverDisabledBuildsNothingWithObserver(t *testing.T) {
	t.Parallel()

	observer := &recordingGrantObserver{}
	cutover, err := buildRemoteCutover(cutoverPlan{}, t.TempDir(), observer)
	if err != nil {
		t.Fatalf("buildRemoteCutover(disabled) error = %v, want nil", err)
	}
	if cutover != nil {
		t.Fatal("buildRemoteCutover(disabled) = non-nil, want nil so the in-tree path stays exact")
	}
	if got := observer.snapshot(); len(got) != 0 {
		t.Fatalf("observed decisions = %d, want 0 on the disabled path", len(got))
	}
}
