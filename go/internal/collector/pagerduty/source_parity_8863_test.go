// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package pagerduty

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	factsincident "github.com/eshu-hq/eshu/go/internal/facts/incident"
)

// parity8863Reference mirrors one reference block of the parity fixture.
type parity8863Reference struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Summary string `json:"summary"`
}

// parity8863Fixture is the decoded cutover comparison input in
// testdata/8863-parity-pagerduty.json.
type parity8863Fixture struct {
	ScopeID             string `json:"scope_id"`
	GenerationID        string `json:"generation_id"`
	CollectorInstanceID string `json:"collector_instance_id"`
	Incident            struct {
		ID             string                `json:"id"`
		IncidentNumber int64                 `json:"incident_number"`
		Title          string                `json:"title"`
		Status         string                `json:"status"`
		Urgency        string                `json:"urgency"`
		Priority       parity8863Reference   `json:"priority"`
		Service        parity8863Reference   `json:"service"`
		Escalation     parity8863Reference   `json:"escalation"`
		Teams          []parity8863Reference `json:"teams"`
		Assignments    []parity8863Reference `json:"assignments"`
		CreatedAt      string                `json:"created_at"`
		UpdatedAt      string                `json:"updated_at"`
		HTMLURL        string                `json:"html_url"`
	} `json:"incident"`
	LifecycleEvent struct {
		ID         string              `json:"id"`
		IncidentID string              `json:"incident_id"`
		Type       string              `json:"type"`
		Actor      parity8863Reference `json:"actor"`
		Channel    string              `json:"channel"`
		Summary    string              `json:"summary"`
		CreatedAt  string              `json:"created_at"`
		HTMLURL    string              `json:"html_url"`
	} `json:"lifecycle_event"`
	ChangeEvent struct {
		ID       string                `json:"id"`
		Summary  string                `json:"summary"`
		Source   string                `json:"source"`
		Services []parity8863Reference `json:"services"`
		Links    []struct {
			Href string `json:"href"`
			Text string `json:"text"`
		} `json:"links"`
		Timestamp string `json:"timestamp"`
		HTMLURL   string `json:"html_url"`
	} `json:"change_event"`
	ConfigService struct {
		ID              string                `json:"id"`
		Summary         string                `json:"summary"`
		Status          string                `json:"status"`
		AlertCreation   string                `json:"alert_creation"`
		Escalation      parity8863Reference   `json:"escalation"`
		Teams           []parity8863Reference `json:"teams"`
		CreatedAt       string                `json:"created_at"`
		UpdatedAt       string                `json:"updated_at"`
		HTMLURL         string                `json:"html_url"`
		Disabled        bool                  `json:"disabled"`
		Deleted         bool                  `json:"deleted"`
		MatchState      string                `json:"match_state"`
		ManuallyCreated bool                  `json:"manually_created"`
		DriftReason     string                `json:"drift_reason"`
	} `json:"config_service"`
	ConfigIntegration struct {
		ID                 string `json:"id"`
		ServiceID          string `json:"service_id"`
		Summary            string `json:"summary"`
		Type               string `json:"type"`
		VendorID           string `json:"vendor_id"`
		HTMLURL            string `json:"html_url"`
		CreatedAt          string `json:"created_at"`
		UpdatedAt          string `json:"updated_at"`
		Disabled           bool   `json:"disabled"`
		Deleted            bool   `json:"deleted"`
		MatchState         string `json:"match_state"`
		ManuallyCreated    bool   `json:"manually_created"`
		DriftReason        string `json:"drift_reason"`
		RoutingKey         string `json:"routing_key"`
		RoutingKeyRedacted bool   `json:"routing_key_redacted"`
	} `json:"config_integration"`
	ConfigWarning struct {
		ResourceClass string `json:"resource_class"`
		ResourceID    string `json:"resource_id"`
		Reason        string `json:"reason"`
	} `json:"config_warning"`
	Hostile struct {
		BlankIncidentID string `json:"blank_incident_id"`
		BlankEventID    string `json:"blank_event_id"`
		BlankChangeID   string `json:"blank_change_id"`
	} `json:"hostile"`
}

// Frozen in-tree stable keys for the representative fixture at the prep
// commit. The extracted producer must emit byte-identical keys for these
// inputs; any derivation change breaks these assertions by design.
const (
	parity8863IncidentRecordKey  = "9aa5661930815a02cff8127bc0d6094d45b101c8887af3eeec83984d1b177215"
	parity8863LifecycleEventKey  = "1d2f5ebc92165fc3d76c59da6dd0826c026c7e5f15d5794c649bde590c342bfd"
	parity8863ChangeRecordKey    = "1d95d2bcfd3c1d7062973899406d151063999c2f30ae95a8ad2a9052b019c14d"
	parity8863ObservedServiceKey = "11bcc2e79f51d2500454b579cd8357e27915de7ca69af5e1d25fe7a38d92efb5"
	parity8863ObservedIntegKey   = "eaf0fdab51209aee5eca88f15c7a5282114989af6e54cf8468708f680aec4755"
	parity8863CoverageWarningKey = "2e4d6111e0496362ebf610827966cc75262bf758e3e0c73f53e00fa2c3053717"
)

func loadParity8863Fixture(t *testing.T) parity8863Fixture {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "8863-parity-pagerduty.json"))
	if err != nil {
		t.Fatalf("ReadFile(parity fixture) error = %v, want nil", err)
	}
	var fixture parity8863Fixture
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatalf("Unmarshal(parity fixture) error = %v, want nil", err)
	}
	return fixture
}

func parity8863Ref(in parity8863Reference) Reference {
	return Reference{ID: in.ID, Type: in.Type, Summary: in.Summary}
}

func parity8863Refs(in []parity8863Reference) []Reference {
	out := make([]Reference, 0, len(in))
	for _, ref := range in {
		out = append(out, parity8863Ref(ref))
	}
	return out
}

func parity8863Time(t *testing.T, raw string) time.Time {
	t.Helper()
	if raw == "" {
		return time.Time{}
	}
	value, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		t.Fatalf("Parse(%q) error = %v, want nil", raw, err)
	}
	return value
}

func parity8863Context(fixture parity8863Fixture) EnvelopeContext {
	return EnvelopeContext{
		ScopeID:             fixture.ScopeID,
		GenerationID:        fixture.GenerationID,
		CollectorInstanceID: fixture.CollectorInstanceID,
		FencingToken:        7,
		ObservedAt:          time.Date(2026, 10, 20, 13, 0, 0, 0, time.UTC),
		SourceURI:           "https://example.pagerduty.com",
	}
}

// TestParity8863IncidentRecordContract pins the in-tree side of the
// #4047 cutover comparison for incident.record.
func TestParity8863IncidentRecordContract(t *testing.T) {
	t.Parallel()

	fixture := loadParity8863Fixture(t)
	ctx := parity8863Context(fixture)
	in := fixture.Incident
	incident := Incident{
		ID:             in.ID,
		IncidentNumber: in.IncidentNumber,
		Title:          in.Title,
		Status:         in.Status,
		Urgency:        in.Urgency,
		Priority:       parity8863Ref(in.Priority),
		Service:        parity8863Ref(in.Service),
		Escalation:     parity8863Ref(in.Escalation),
		Teams:          parity8863Refs(in.Teams),
		Assignments:    parity8863Refs(in.Assignments),
		CreatedAt:      parity8863Time(t, in.CreatedAt),
		UpdatedAt:      parity8863Time(t, in.UpdatedAt),
		HTMLURL:        in.HTMLURL,
	}

	env, err := NewIncidentRecordEnvelope(ctx, incident)
	if err != nil {
		t.Fatalf("NewIncidentRecordEnvelope() error = %v, want nil", err)
	}
	if got, want := env.FactKind, factsincident.IncidentRecordFactKind; got != want {
		t.Fatalf("FactKind = %q, want %q", got, want)
	}
	if got, want := env.FactKind, "incident.record"; got != want {
		t.Fatalf("FactKind = %q, want %q", got, want)
	}
	if got, want := env.ScopeID, fixture.ScopeID; got != want {
		t.Fatalf("ScopeID = %q, want %q", got, want)
	}
	if got, want := env.GenerationID, fixture.GenerationID; got != want {
		t.Fatalf("GenerationID = %q, want %q", got, want)
	}
	if got, want := env.SchemaVersion, factsincident.IncidentContextSchemaVersionV1; got != want {
		t.Fatalf("SchemaVersion = %q, want %q", got, want)
	}
	if got, want := env.SourceConfidence, facts.SourceConfidenceReported; got != want {
		t.Fatalf("SourceConfidence = %q, want %q", got, want)
	}
	if got, want := env.StableFactKey, parity8863IncidentRecordKey; got != want {
		t.Fatalf("StableFactKey = %q, want frozen %q", got, want)
	}
	if got, want := env.Payload["provider_incident_id"], "PABC123"; got != want {
		t.Fatalf("Payload[provider_incident_id] = %#v, want %#v", got, want)
	}
	if got, want := env.Payload["incident_number"], int64(42); got != want {
		t.Fatalf("Payload[incident_number] = %#v, want %#v", got, want)
	}
	if got := env.SourceRef.SourceURI; strings.Contains(got, "token=") {
		t.Fatalf("SourceURI = %q, want sensitive query stripped", got)
	}

	// Stable identity survives generation rotation; FactID is
	// generation-scoped, so retries and duplicate delivery stay idempotent.
	rotated := ctx
	rotated.GenerationID = "pagerduty:generation-2"
	second, err := NewIncidentRecordEnvelope(rotated, incident)
	if err != nil {
		t.Fatalf("NewIncidentRecordEnvelope(rotated) error = %v, want nil", err)
	}
	if second.StableFactKey != env.StableFactKey {
		t.Fatalf("StableFactKey rotated across generations: %q vs %q", second.StableFactKey, env.StableFactKey)
	}
	if got, want := second.StableFactKey, parity8863IncidentRecordKey; got != want {
		t.Fatalf("StableFactKey(rotated) = %q, want frozen %q", got, want)
	}
	if second.FactID == env.FactID {
		t.Fatal("FactID did not change across generations, want generation-scoped identity")
	}
}

// TestParity8863LifecycleEventContract pins the in-tree side for
// incident.lifecycle_event.
func TestParity8863LifecycleEventContract(t *testing.T) {
	t.Parallel()

	fixture := loadParity8863Fixture(t)
	ctx := parity8863Context(fixture)
	in := fixture.LifecycleEvent
	event := LifecycleEvent{
		ID:         in.ID,
		IncidentID: in.IncidentID,
		Type:       in.Type,
		Actor:      parity8863Ref(in.Actor),
		Channel:    in.Channel,
		Summary:    in.Summary,
		CreatedAt:  parity8863Time(t, in.CreatedAt),
		HTMLURL:    in.HTMLURL,
	}

	env, err := NewLifecycleEventEnvelope(ctx, event)
	if err != nil {
		t.Fatalf("NewLifecycleEventEnvelope() error = %v, want nil", err)
	}
	if got, want := env.FactKind, factsincident.IncidentLifecycleEventFactKind; got != want {
		t.Fatalf("FactKind = %q, want %q", got, want)
	}
	if got, want := env.Payload["provider_event_id"], "LOG1"; got != want {
		t.Fatalf("Payload[provider_event_id] = %#v, want %#v", got, want)
	}
	if got, want := env.Payload["provider_incident_id"], "PABC123"; got != want {
		t.Fatalf("Payload[provider_incident_id] = %#v, want %#v", got, want)
	}
	if got, want := env.StableFactKey, parity8863LifecycleEventKey; got != want {
		t.Fatalf("StableFactKey = %q, want frozen %q", got, want)
	}
}

// TestParity8863ChangeRecordContract pins the in-tree side for
// change.record.
func TestParity8863ChangeRecordContract(t *testing.T) {
	t.Parallel()

	fixture := loadParity8863Fixture(t)
	ctx := parity8863Context(fixture)
	in := fixture.ChangeEvent
	links := make([]Link, 0, len(in.Links))
	for _, link := range in.Links {
		links = append(links, Link{Href: link.Href, Text: link.Text})
	}
	change := ChangeEvent{
		ID:        in.ID,
		Summary:   in.Summary,
		Source:    in.Source,
		Services:  parity8863Refs(in.Services),
		Links:     links,
		Timestamp: parity8863Time(t, in.Timestamp),
		HTMLURL:   in.HTMLURL,
	}

	env, err := NewChangeRecordEnvelope(ctx, change)
	if err != nil {
		t.Fatalf("NewChangeRecordEnvelope() error = %v, want nil", err)
	}
	if got, want := env.FactKind, factsincident.ChangeRecordFactKind; got != want {
		t.Fatalf("FactKind = %q, want %q", got, want)
	}
	if got, want := env.Payload["provider_change_id"], "CHG1"; got != want {
		t.Fatalf("Payload[provider_change_id] = %#v, want %#v", got, want)
	}
	if got, want := env.StableFactKey, parity8863ChangeRecordKey; got != want {
		t.Fatalf("StableFactKey = %q, want frozen %q", got, want)
	}
}

// TestParity8863ObservedServiceContract pins the in-tree side for
// incident_routing.observed_pagerduty_service.
func TestParity8863ObservedServiceContract(t *testing.T) {
	t.Parallel()

	fixture := loadParity8863Fixture(t)
	ctx := parity8863Context(fixture)
	in := fixture.ConfigService
	service := ConfigService{
		ID:              in.ID,
		Summary:         in.Summary,
		Status:          in.Status,
		AlertCreation:   in.AlertCreation,
		Escalation:      parity8863Ref(in.Escalation),
		Teams:           parity8863Refs(in.Teams),
		CreatedAt:       parity8863Time(t, in.CreatedAt),
		UpdatedAt:       parity8863Time(t, in.UpdatedAt),
		HTMLURL:         in.HTMLURL,
		Disabled:        in.Disabled,
		Deleted:         in.Deleted,
		MatchState:      in.MatchState,
		ManuallyCreated: in.ManuallyCreated,
		DriftReason:     in.DriftReason,
	}

	env, err := NewObservedPagerDutyServiceEnvelope(ctx, service)
	if err != nil {
		t.Fatalf("NewObservedPagerDutyServiceEnvelope() error = %v, want nil", err)
	}
	if got, want := env.FactKind, factsincident.IncidentRoutingObservedPagerDutyServiceFactKind; got != want {
		t.Fatalf("FactKind = %q, want %q", got, want)
	}
	if got, want := env.ScopeID, fixture.ScopeID; got != want {
		t.Fatalf("ScopeID = %q, want %q", got, want)
	}
	if got, want := env.GenerationID, fixture.GenerationID; got != want {
		t.Fatalf("GenerationID = %q, want %q", got, want)
	}
	if got, want := env.SchemaVersion, factsincident.IncidentRoutingSchemaVersionV1; got != want {
		t.Fatalf("SchemaVersion = %q, want %q", got, want)
	}
	if got, want := env.Payload["service_id"], "SVC1"; got != want {
		t.Fatalf("Payload[service_id] = %#v, want %#v", got, want)
	}
	if got, want := env.StableFactKey, parity8863ObservedServiceKey; got != want {
		t.Fatalf("StableFactKey = %q, want frozen %q", got, want)
	}
	if got := env.SourceRef.SourceURI; strings.Contains(got, "token=") {
		t.Fatalf("SourceURI = %q, want sensitive query stripped", got)
	}
}

// TestParity8863ObservedIntegrationContract pins the in-tree side for
// incident_routing.observed_pagerduty_integration.
func TestParity8863ObservedIntegrationContract(t *testing.T) {
	t.Parallel()

	fixture := loadParity8863Fixture(t)
	ctx := parity8863Context(fixture)
	in := fixture.ConfigIntegration
	integration := ConfigIntegration{
		ID:                 in.ID,
		ServiceID:          in.ServiceID,
		Summary:            in.Summary,
		Type:               in.Type,
		VendorID:           in.VendorID,
		HTMLURL:            in.HTMLURL,
		CreatedAt:          parity8863Time(t, in.CreatedAt),
		UpdatedAt:          parity8863Time(t, in.UpdatedAt),
		Disabled:           in.Disabled,
		Deleted:            in.Deleted,
		MatchState:         in.MatchState,
		ManuallyCreated:    in.ManuallyCreated,
		DriftReason:        in.DriftReason,
		RoutingKey:         in.RoutingKey,
		RoutingKeyRedacted: in.RoutingKeyRedacted,
	}

	env, err := NewObservedPagerDutyIntegrationEnvelope(ctx, integration)
	if err != nil {
		t.Fatalf("NewObservedPagerDutyIntegrationEnvelope() error = %v, want nil", err)
	}
	if got, want := env.FactKind, factsincident.IncidentRoutingObservedPagerDutyIntegrationFactKind; got != want {
		t.Fatalf("FactKind = %q, want %q", got, want)
	}
	if got, want := env.Payload["integration_id"], "INT1"; got != want {
		t.Fatalf("Payload[integration_id] = %#v, want %#v", got, want)
	}
	if got, want := env.Payload["service_reference"], "SVC1"; got != want {
		t.Fatalf("Payload[service_reference] = %#v, want %#v", got, want)
	}
	if got, want := env.Payload["routing_key_redacted"], true; got != want {
		t.Fatalf("Payload[routing_key_redacted] = %#v, want %#v", got, want)
	}
	if got, want := env.StableFactKey, parity8863ObservedIntegKey; got != want {
		t.Fatalf("StableFactKey = %q, want frozen %q", got, want)
	}
}

// TestParity8863CoverageWarningContract pins the in-tree side for
// incident_routing.coverage_warning.
func TestParity8863CoverageWarningContract(t *testing.T) {
	t.Parallel()

	fixture := loadParity8863Fixture(t)
	ctx := parity8863Context(fixture)
	in := fixture.ConfigWarning
	warning := ConfigWarning{
		ResourceClass: in.ResourceClass,
		ResourceID:    in.ResourceID,
		Reason:        in.Reason,
	}

	env, err := NewPagerDutyConfigCoverageWarningEnvelope(ctx, warning)
	if err != nil {
		t.Fatalf("NewPagerDutyConfigCoverageWarningEnvelope() error = %v, want nil", err)
	}
	if got, want := env.FactKind, factsincident.IncidentRoutingCoverageWarningFactKind; got != want {
		t.Fatalf("FactKind = %q, want %q", got, want)
	}
	if got, want := env.Payload["reason"], "permission_hidden"; got != want {
		t.Fatalf("Payload[reason] = %#v, want %#v", got, want)
	}
	if got, want := env.StableFactKey, parity8863CoverageWarningKey; got != want {
		t.Fatalf("StableFactKey = %q, want frozen %q", got, want)
	}
}

// TestParity8863HostileInputsFailsClosed proves blank provider identity
// and blank envelope context are rejected, never emitted. Every rejection
// branch the constructors document gets its own case, driven by the
// fixture hostile block.
func TestParity8863HostileInputsFailsClosed(t *testing.T) {
	t.Parallel()

	fixture := loadParity8863Fixture(t)
	ctx := parity8863Context(fixture)
	hostile := fixture.Hostile

	if _, err := NewIncidentRecordEnvelope(ctx, Incident{ID: hostile.BlankIncidentID}); err == nil {
		t.Fatal("NewIncidentRecordEnvelope(blank id) error = nil, want rejection")
	}
	if _, err := NewLifecycleEventEnvelope(ctx, LifecycleEvent{ID: hostile.BlankEventID}); err == nil {
		t.Fatal("NewLifecycleEventEnvelope(blank id) error = nil, want rejection")
	}
	if _, err := NewLifecycleEventEnvelope(ctx, LifecycleEvent{ID: "LOG1"}); err == nil {
		t.Fatal("NewLifecycleEventEnvelope(blank incident id) error = nil, want rejection")
	}
	if _, err := NewChangeRecordEnvelope(ctx, ChangeEvent{ID: hostile.BlankChangeID}); err == nil {
		t.Fatal("NewChangeRecordEnvelope(blank id) error = nil, want rejection")
	}
	if _, err := NewObservedPagerDutyServiceEnvelope(ctx, ConfigService{}); err == nil {
		t.Fatal("NewObservedPagerDutyServiceEnvelope(blank id) error = nil, want rejection")
	}
	if _, err := NewObservedPagerDutyIntegrationEnvelope(ctx, ConfigIntegration{}); err == nil {
		t.Fatal("NewObservedPagerDutyIntegrationEnvelope(blank id) error = nil, want rejection")
	}
	for name, bad := range map[string]EnvelopeContext{
		"blank scope":      {GenerationID: ctx.GenerationID, CollectorInstanceID: ctx.CollectorInstanceID},
		"blank generation": {ScopeID: ctx.ScopeID, CollectorInstanceID: ctx.CollectorInstanceID},
		"blank instance":   {ScopeID: ctx.ScopeID, GenerationID: ctx.GenerationID},
	} {
		if _, err := NewIncidentRecordEnvelope(bad, Incident{ID: "PABC123"}); err == nil {
			t.Fatalf("NewIncidentRecordEnvelope(%s) error = nil, want rejection", name)
		}
		if _, err := NewLifecycleEventEnvelope(bad, LifecycleEvent{ID: "LOG1", IncidentID: "PABC123"}); err == nil {
			t.Fatalf("NewLifecycleEventEnvelope(%s) error = nil, want rejection", name)
		}
		if _, err := NewChangeRecordEnvelope(bad, ChangeEvent{ID: "CHG1"}); err == nil {
			t.Fatalf("NewChangeRecordEnvelope(%s) error = nil, want rejection", name)
		}
	}
}
