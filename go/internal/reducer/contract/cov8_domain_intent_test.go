// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contract

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/truth"
)

// cov8ValidOwnership is the minimal ownership shape that passes Validate:
// cross-source, cross-scope, and canonical truth via CanonicalWrite.
func cov8ValidOwnership() OwnershipShape {
	return OwnershipShape{CrossSource: true, CrossScope: true, CanonicalWrite: true}
}

// TestCov8OwnershipShapeValidate pins the reducer boundary: every arm must
// hold, and CounterEmit alone also satisfies the truth half.
func TestCov8OwnershipShapeValidate(t *testing.T) {
	t.Parallel()

	if err := cov8ValidOwnership().Validate(); err != nil {
		t.Errorf("Validate() = %v, want nil", err)
	}
	if err := (OwnershipShape{CrossSource: true, CrossScope: true, CounterEmit: true}).Validate(); err != nil {
		t.Errorf("Validate() with CounterEmit = %v, want nil", err)
	}
	for name, shape := range map[string]OwnershipShape{
		"not cross-source": {},
		"not cross-scope":  {CrossSource: true},
		"no truth":         {CrossSource: true, CrossScope: true},
	} {
		if err := shape.Validate(); err == nil {
			t.Errorf("Validate(%s) = nil, want error", name)
		}
	}
}

// TestCov8CrossScopeDependencyValidate pins dependency registration: an
// empty declaration and an unknown producer fail the build loudly.
func TestCov8CrossScopeDependencyValidate(t *testing.T) {
	t.Parallel()

	valid := CrossScopeDependency{ProducerDomains: []Domain{DomainWorkloadIdentity}}
	if err := valid.Validate(); err != nil {
		t.Errorf("Validate() = %v, want nil", err)
	}
	if err := (CrossScopeDependency{}).Validate(); err == nil {
		t.Error("Validate() with no producers = nil, want error")
	}
	bad := CrossScopeDependency{ProducerDomains: []Domain{"no-such-domain"}}
	if err := bad.Validate(); err == nil {
		t.Error("Validate() with unknown producer = nil, want error")
	}
}

// TestCov8DomainDefinitionValidate pins registration checks layer by
// layer: domain, summary, ownership, truth contract, then dependencies.
func TestCov8DomainDefinitionValidate(t *testing.T) {
	t.Parallel()

	valid := DomainDefinition{
		Domain:    DomainWorkloadIdentity,
		Summary:   "code call edges",
		Ownership: cov8ValidOwnership(),
		TruthContract: truth.Contract{
			CanonicalKind: "code_call_edge",
			SourceLayers:  []truth.Layer{truth.LayerSourceDeclaration},
		},
		CrossScopeDependencies: []CrossScopeDependency{
			{ProducerDomains: []Domain{DomainWorkloadMaterialization}},
		},
	}
	if err := valid.Validate(); err != nil {
		t.Errorf("Validate() = %v, want nil", err)
	}

	cases := []struct {
		name   string
		mutate func(*DomainDefinition)
	}{
		{"bad domain", func(d *DomainDefinition) { d.Domain = "no-such-domain" }},
		{"blank summary", func(d *DomainDefinition) { d.Summary = "  " }},
		{"bad ownership", func(d *DomainDefinition) { d.Ownership = OwnershipShape{} }},
		{"bad truth", func(d *DomainDefinition) { d.TruthContract = truth.Contract{} }},
		{"bad dependency", func(d *DomainDefinition) {
			d.CrossScopeDependencies = []CrossScopeDependency{{}}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			def := valid
			tc.mutate(&def)
			if err := def.Validate(); err == nil {
				t.Errorf("Validate(%s) = nil, want error", tc.name)
			}
		})
	}
}

// TestCov8HandlerFuncHandle pins the function adapter: it forwards ctx
// and intent and returns the wrapped result.
func TestCov8HandlerFuncHandle(t *testing.T) {
	t.Parallel()

	want := Result{}
	var gotIntent Intent
	handler := HandlerFunc(func(_ context.Context, intent Intent) (Result, error) {
		gotIntent = intent
		return want, nil
	})
	intent := Intent{IntentID: "intent-1"}
	if _, err := handler.Handle(context.Background(), intent); err != nil {
		t.Fatalf("Handle() = %v, want nil", err)
	}
	if gotIntent.IntentID != "intent-1" {
		t.Errorf("forwarded intent = %+v, want IntentID intent-1", gotIntent)
	}

	sentinel := errors.New("boom")
	failing := HandlerFunc(func(context.Context, Intent) (Result, error) { return Result{}, sentinel })
	if _, err := failing.Handle(context.Background(), intent); !errors.Is(err, sentinel) {
		t.Errorf("Handle() = %v, want %v", err, sentinel)
	}
}

// TestCov8DomainVocabulary pins the domain registry surface: parsing,
// validation, and the deterministic known-domain inventory.
func TestCov8DomainVocabulary(t *testing.T) {
	t.Parallel()

	domain, err := ParseDomain("  " + string(DomainWorkloadIdentity) + " ")
	if err != nil || domain != DomainWorkloadIdentity {
		t.Errorf("ParseDomain() = %q, %v; want %q, nil", domain, err, DomainWorkloadIdentity)
	}
	if _, err := ParseDomain(""); err == nil {
		t.Error("ParseDomain(blank) = nil error, want error")
	}
	if _, err := ParseDomain("no-such-domain"); err == nil {
		t.Error("ParseDomain(unknown) = nil error, want error")
	}
	if err := Domain("").Validate(); err == nil {
		t.Error("Validate(blank domain) = nil, want error")
	}

	known := KnownDomains()
	if len(known) == 0 {
		t.Fatal("KnownDomains() is empty")
	}
	for i := 1; i < len(known); i++ {
		if known[i-1] >= known[i] {
			t.Fatalf("KnownDomains() not sorted at %d: %q >= %q", i, known[i-1], known[i])
		}
	}
	found := false
	for _, d := range known {
		if d == DomainWorkloadIdentity {
			found = true
		}
		if err := d.Validate(); err != nil {
			t.Errorf("Validate(%q) = %v, want nil", d, err)
		}
	}
	if !found {
		t.Errorf("KnownDomains() misses %q", DomainWorkloadIdentity)
	}

	// ProjectionDomains is the capability inventory, a separate registry
	// from the validatable knownDomains set: a projection domain such as
	// code_calls does NOT pass Validate (pinned here). See the handoff
	// note on this registry split.
	projection := ProjectionDomains()
	if len(projection) == 0 {
		t.Fatal("ProjectionDomains() is empty")
	}
	projectionSeen := map[Domain]bool{}
	for _, d := range projection {
		if projectionSeen[d] {
			t.Errorf("ProjectionDomains() duplicates %q", d)
		}
		projectionSeen[d] = true
	}
	if err := Domain(DomainCodeCalls).Validate(); err == nil {
		t.Error("Validate(code_calls projection domain) = nil, want unknown-domain error")
	}
}

// cov8ValidIntent returns an Intent that passes Validate end to end.
func cov8ValidIntent() Intent {
	now := time.Now().UTC()
	return Intent{
		IntentID:        "intent-1",
		ScopeID:         "scope-1",
		GenerationID:    "gen-1",
		SourceSystem:    "test",
		Domain:          DomainWorkloadIdentity,
		Cause:           "coverage",
		EnqueuedAt:      now,
		AvailableAt:     now,
		RelatedScopeIDs: []string{"scope-1"},
		Status:          IntentStatusPending,
	}
}

// TestCov8IntentValidate pins the durable intent contract field by field.
func TestCov8IntentValidate(t *testing.T) {
	t.Parallel()

	if err := cov8ValidIntent().Validate(); err != nil {
		t.Errorf("Validate() = %v, want nil", err)
	}
	cases := []struct {
		name   string
		mutate func(*Intent)
	}{
		{"blank intent id", func(i *Intent) { i.IntentID = "" }},
		{"blank scope", func(i *Intent) { i.ScopeID = " " }},
		{"blank generation", func(i *Intent) { i.GenerationID = "" }},
		{"blank source", func(i *Intent) { i.SourceSystem = "" }},
		{"bad domain", func(i *Intent) { i.Domain = "nope" }},
		{"blank cause", func(i *Intent) { i.Cause = "" }},
		{"zero enqueued", func(i *Intent) { i.EnqueuedAt = time.Time{} }},
		{"zero available", func(i *Intent) { i.AvailableAt = time.Time{} }},
		{"no related scopes", func(i *Intent) { i.RelatedScopeIDs = nil }},
		{"bad status", func(i *Intent) { i.Status = "nope" }},
		{"blank entity key", func(i *Intent) { i.EntityKeys = []string{"ok", " "} }},
		{"blank related scope", func(i *Intent) { i.RelatedScopeIDs = []string{" "} }},
		{"duplicate related scope", func(i *Intent) { i.RelatedScopeIDs = []string{"a", "a"} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			intent := cov8ValidIntent()
			tc.mutate(&intent)
			if err := intent.Validate(); err == nil {
				t.Errorf("Validate(%s) = nil, want error", tc.name)
			}
		})
	}

	if got := cov8ValidIntent().ScopeGenerationKey(); got != "scope-1:gen-1" {
		t.Errorf("ScopeGenerationKey() = %q, want scope-1:gen-1", got)
	}
}

// TestCov8IntentStatusLifecycle pins Terminal and the WithStatus
// timestamp rules: claim/run stamp ClaimedAt, finish stamps CompletedAt,
// and only succeeded/failed are terminal.
func TestCov8IntentStatusLifecycle(t *testing.T) {
	t.Parallel()

	for _, status := range []IntentStatus{
		IntentStatusPending, IntentStatusClaimed, IntentStatusRunning,
		IntentStatusSucceeded, IntentStatusFailed,
	} {
		if err := status.Validate(); err != nil {
			t.Errorf("Validate(%q) = %v, want nil", status, err)
		}
	}
	if err := IntentStatus("nope").Validate(); err == nil {
		t.Error("Validate(unknown) = nil, want error")
	}
	if IntentStatusSucceeded.Terminal() != true || IntentStatusFailed.Terminal() != true {
		t.Error("succeeded/failed must be terminal")
	}
	for _, status := range []IntentStatus{IntentStatusPending, IntentStatusClaimed, IntentStatusRunning} {
		if status.Terminal() {
			t.Errorf("%q must not be terminal", status)
		}
	}

	at := time.Now().UTC()
	base := cov8ValidIntent()
	base.EntityKeys = []string{"e1"}
	base.Payload = map[string]any{"k": "v"}
	claimedAt := at.Add(-time.Hour)
	completedAt := at.Add(-time.Minute)
	base.ClaimedAt = &claimedAt
	base.CompletedAt = &completedAt
	failure := FailureRecord{FailureClass: "test", Message: "nope"}

	claimed := base.WithStatus(IntentStatusClaimed, at)
	if claimed.Status != IntentStatusClaimed || claimed.ClaimedAt == nil || !claimed.ClaimedAt.Equal(at) {
		t.Error("WithStatus(claimed) must stamp ClaimedAt")
	}
	if claimed.CompletedAt == nil || !claimed.CompletedAt.Equal(completedAt) {
		t.Error("WithStatus(claimed) must keep CompletedAt")
	}
	// Clone detaches slices and pointers: mutating the clone must not
	// touch the base.
	claimed.EntityKeys[0] = "mutated"
	*claimed.ClaimedAt = at.Add(time.Hour)
	if base.EntityKeys[0] != "e1" || !base.ClaimedAt.Equal(claimedAt) {
		t.Error("WithStatus must detach slices and timestamps from the base")
	}

	done := base.WithStatus(IntentStatusSucceeded, at)
	if done.CompletedAt == nil || !done.CompletedAt.Equal(at) {
		t.Error("WithStatus(succeeded) must stamp CompletedAt")
	}
	pending := base.WithStatus(IntentStatusPending, at)
	if pending.Status != IntentStatusPending {
		t.Error("WithStatus(pending) must keep pending status")
	}

	withFailure := base
	withFailure.Failure = &failure
	if withFailure.Clone().Failure == nil {
		t.Error("Clone() must carry the failure record")
	}
	nilPayload := cov8ValidIntent()
	if got := nilPayload.Clone(); got.Payload != nil {
		t.Errorf("Clone() of nil payload = %v, want nil", got.Payload)
	}
}

// TestCov8MaterializationDiagnosticSignals pins the operator signal shape:
// input readiness as 0/1 plus the written-row count.
func TestCov8MaterializationDiagnosticSignals(t *testing.T) {
	t.Parallel()

	ready := MaterializationDiagnosticSignals(true, 7)
	if ready[DiagnosticSignalInputReady] != 1.0 || ready[DiagnosticSignalWrittenRows] != 7.0 {
		t.Errorf("ready signals = %v, want input_ready=1 written=7", ready)
	}
	stalled := MaterializationDiagnosticSignals(false, 0)
	if stalled[DiagnosticSignalInputReady] != 0.0 || stalled[DiagnosticSignalWrittenRows] != 0.0 {
		t.Errorf("stalled signals = %v, want zeros", stalled)
	}
}
