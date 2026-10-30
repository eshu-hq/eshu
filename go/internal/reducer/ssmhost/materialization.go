// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ssmhost

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts/cloud"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/factdecode"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// ssmHostEvidenceSource tags SSM host inventory graph nodes and edges written
// by this reducer so the supersession retract path scopes its deletes to
// reducer-owned SSM observations, distinct from every other domain's writes.
const ssmHostEvidenceSource = "reducer/ssm-host-inventory"

// MaterializationDomainDefinition returns the additive definition for SSM
// host inventory observation materialization (eshu-hq/eshu#9558). It is
// additive (not part of DefaultDomainDefinitions) because the handler
// requires an explicitly wired HostGraphWriter and fail-closed Bounds;
// registering it without them would silently drop every SSM observation
// before it reached the graph.
func MaterializationDomainDefinition() reducercontract.DomainDefinition {
	return reducercontract.DomainDefinition{
		Domain:  reducercontract.DomainSSMHostInventoryMaterialization,
		Summary: "materialize admitted SSM host inventory observations into canonical observation graph nodes and platform-pairing edges",
		Ownership: reducercontract.OwnershipShape{
			CrossSource:    true,
			CrossScope:     true,
			CanonicalWrite: true,
		},
	}
}

// HostGraphWrite is one batched observation-node and pairing-edge write for a
// superseding host.
type HostGraphWrite struct {
	ScopeID        string
	GenerationID   string
	EvidenceSource string
	Host           cloud.SSMHostTuple
	Apps           []map[string]any
	Platforms      []map[string]any
	Edges          []map[string]any
}

// HostGraphRetract deletes the superseded graph state for one host: the
// application observation nodes of captures older than AppCapture, and the
// platform observation uids whose current-generation evidence conflicts.
// Same-capture rows merge; newer captures are never touched.
type HostGraphRetract struct {
	ScopeID             string
	EvidenceSource      string
	Host                cloud.SSMHostTuple
	AppCaptureOlderThan time.Time
	HasAppCapture       bool
	DeletePlatformUIDs  []string
}

// HostGraphWriter persists SSM host inventory observation nodes,
// platform-pairing edges, and supersession retracts. Implementations MUST be
// idempotent by node and edge uid so reducer retries, duplicate deliveries,
// and concurrent projections converge on one graph truth rather than
// duplicating or fabricating state.
type HostGraphWriter interface {
	// ReadCommittedHosts returns the newest committed application capture
	// and platform evidence times the graph holds for each host, so a
	// delayed or duplicated older capture can never supersede newer truth.
	ReadCommittedHosts(ctx context.Context, scopeID string, hosts []cloud.SSMHostTuple) (map[string]CommittedHost, error)
	// WriteHostNodes merges one host's observation nodes and pairing edges.
	WriteHostNodes(ctx context.Context, write HostGraphWrite) error
	// RetractStaleHostNodes deletes one host's superseded observation state.
	RetractStaleHostNodes(ctx context.Context, retract HostGraphRetract) (int, error)
}

// MaterializationHandler reduces one SSM host inventory materialization
// intent into canonical observation graph nodes and platform-pairing edges.
//
// Only a fresh complete application capture newer than the committed one
// supersedes: mixed-capture, aged, duplicate, and silent generations preserve
// the prior graph truth untouched, with the verdict carried on the result's
// evidence summary. Conflicting platform content for one host and provenance
// stays ambiguous (its prior node retracts, no new node) without blocking the
// coherent application capture. An out-of-skew platform still projects,
// unpaired. No vulnerability or supply-chain finding is ever synthesized
// here: observations project as observations (#9209 owns correlation).
//
// Cross-generation interleaving is fenced by the runtime's generation
// supersession short-circuit (a newer scope generation never runs beside an
// older one); within one scope generation every write and retract is
// uid-idempotent, so duplicate intents and concurrent workers converge.
//
// The convergence boundary is one scope: observation uids carry no scope
// component (they mirror the #9556 stable keys), while each write SETs
// scope-owned properties (scope_id, generation_id, source_fact_id) on the
// shared node. Two scopes projecting the same host last-write-win flap
// those properties, and the scope-exclusive committed-capture read gives
// each scope a different supersession truth. Single-scope-per-host is the
// supported deployment; scoping the uid would fork the collector, reducer,
// and read dedup identity and needs its own design.
type MaterializationHandler struct {
	FactLoader  factload.FactLoader
	GraphWriter HostGraphWriter
	// Bounds carries the fail-closed freshness configuration. Zero bounds
	// fail the intent closed rather than projecting under unbounded rules.
	Bounds Bounds
	// Now supplies the read time; defaults to time.Now.
	Now func() time.Time
	// Instruments records quarantine counters. Nil-safe.
	Instruments *telemetry.Instruments
}

// Handle executes one SSM host inventory materialization intent.
func (h MaterializationHandler) Handle(
	ctx context.Context,
	intent reducercontract.Intent,
) (reducercontract.Result, error) {
	totalStart := time.Now()
	if intent.Domain != reducercontract.DomainSSMHostInventoryMaterialization {
		return reducercontract.Result{}, fmt.Errorf(
			"ssm host inventory materialization handler does not accept domain %q",
			intent.Domain,
		)
	}
	if err := h.Bounds.Validate(); err != nil {
		return reducercontract.Result{}, err
	}
	if h.FactLoader == nil {
		return reducercontract.Result{}, fmt.Errorf("ssm host inventory materialization fact loader is required")
	}
	if h.GraphWriter == nil {
		return reducercontract.Result{}, fmt.Errorf("ssm host inventory materialization graph writer is required")
	}
	now := time.Now
	if h.Now != nil {
		now = h.Now
	}

	loadStart := time.Now()
	envelopes, err := factload.LoadFactsForKinds(
		ctx,
		h.FactLoader,
		intent.ScopeID,
		intent.GenerationID,
		[]string{cloud.SSMHostApplicationFactKind, cloud.SSMHostPlatformFactKind},
	)
	if err != nil {
		return reducercontract.Result{}, fmt.Errorf("load facts for ssm host inventory materialization: %w", err)
	}
	loadDuration := time.Since(loadStart)

	extractStart := time.Now()
	apps, platforms, quarantined, err := ExtractObservations(envelopes, envelopes)
	if err != nil {
		return reducercontract.Result{}, err
	}
	inputInvalidCount := factdecode.RecordQuarantinedFacts(ctx, h.Instruments, reducercontract.DomainSSMHostInventoryMaterialization, intent.ScopeID, intent.GenerationID, quarantined)
	extractDuration := time.Since(extractStart)

	assessStart := time.Now()
	hosts := distinctHosts(apps, platforms)
	var committed map[string]CommittedHost
	if len(hosts) > 0 {
		committed, err = h.GraphWriter.ReadCommittedHosts(ctx, intent.ScopeID, hosts)
		if err != nil {
			return reducercontract.Result{}, fmt.Errorf("read committed ssm host captures: %w", err)
		}
	}
	projections, tally := ProjectHosts(apps, platforms, committed, h.Bounds, now())
	assessDuration := time.Since(assessStart)

	var writeDuration, retractDuration time.Duration
	writtenNodes := 0
	retractedNodes := 0
	for _, projection := range projections {
		if !projection.Supersede {
			continue
		}
		writeStart := time.Now()
		err := h.GraphWriter.WriteHostNodes(ctx, HostGraphWrite{
			ScopeID:        intent.ScopeID,
			GenerationID:   intent.GenerationID,
			EvidenceSource: ssmHostEvidenceSource,
			Host:           projection.Host,
			Apps:           projection.AppRows,
			Platforms:      projection.PlatformRows,
			Edges:          projection.EdgeRows,
		})
		writeDuration += time.Since(writeStart)
		if err != nil {
			return reducercontract.Result{}, fmt.Errorf("write ssm host observation nodes: %w", err)
		}
		writtenNodes += len(projection.AppRows) + len(projection.PlatformRows) + len(projection.EdgeRows)

		retractStart := time.Now()
		retracted, err := h.GraphWriter.RetractStaleHostNodes(ctx, HostGraphRetract{
			ScopeID:             intent.ScopeID,
			EvidenceSource:      ssmHostEvidenceSource,
			Host:                projection.Host,
			AppCaptureOlderThan: projection.AppVerdict.CaptureTime,
			HasAppCapture:       true,
			DeletePlatformUIDs:  conflictedPlatformUIDs(projection),
		})
		retractDuration += time.Since(retractStart)
		if err != nil {
			return reducercontract.Result{}, fmt.Errorf("retract superseded ssm host observations: %w", err)
		}
		retractedNodes += retracted
	}

	logMaterializationCompleted(ctx, intent, len(envelopes), tally, writtenNodes, retractedNodes,
		loadDuration, extractDuration, assessDuration, writeDuration, retractDuration, time.Since(totalStart))

	return reducercontract.Result{
		IntentID: intent.IntentID,
		Domain:   reducercontract.DomainSSMHostInventoryMaterialization,
		Status:   reducercontract.ResultStatusSucceeded,
		EvidenceSummary: fmt.Sprintf(
			"ssm host inventory: %d host(s), %d superseded, %d preserved, %d node(s)/edge(s) written, %d stale node(s) retracted, %d input_invalid fact(s) quarantined",
			tally.Hosts,
			tally.Superseded,
			tally.Preserved,
			writtenNodes,
			retractedNodes,
			inputInvalidCount,
		),
		SubSignals:      factdecode.InputInvalidSubSignals(inputInvalidCount),
		CanonicalWrites: writtenNodes,
	}, nil
}

func distinctHosts(apps []ApplicationObservation, platforms []PlatformObservation) []cloud.SSMHostTuple {
	seen := map[string]cloud.SSMHostTuple{}
	for _, app := range apps {
		host := app.HostTuple()
		seen[HostKey(host)] = host
	}
	for _, platform := range platforms {
		host := platform.HostTuple()
		seen[HostKey(host)] = host
	}
	hosts := make([]cloud.SSMHostTuple, 0, len(seen))
	for _, host := range seen {
		hosts = append(hosts, host)
	}
	return hosts
}

// conflictedPlatformUIDs returns the platform observation uids the current
// generation renders ambiguous: a conflicting source retracts its prior node
// and projects no replacement, so stale agreed truth never stands beside an
// unresolved conflict. The uid is host plus source type, so it is derivable
// without a replacement row.
func conflictedPlatformUIDs(projection HostProjection) []string {
	var uids []string
	for _, source := range projection.ConflictedPlatformSources {
		uids = append(uids, PlatformUID(projection.Host, source))
	}
	return uids
}

func logMaterializationCompleted(
	ctx context.Context,
	intent reducercontract.Intent,
	factCount int,
	tally ProjectionTally,
	writtenNodes int,
	retractedNodes int,
	loadDuration, extractDuration, assessDuration, writeDuration, retractDuration, totalDuration time.Duration,
) {
	slog.InfoContext(
		ctx, "ssm host inventory materialization completed",
		log.ScopeID(intent.ScopeID),
		log.GenerationID(intent.GenerationID),
		log.Domain(string(intent.Domain)),
		slog.Int("fact_count", factCount),
		slog.Int("host_count", tally.Hosts),
		slog.Int("hosts_superseded", tally.Superseded),
		slog.Int("hosts_preserved", tally.Preserved),
		slog.Int("nodes_written", writtenNodes),
		slog.Int("nodes_retracted", retractedNodes),
		slog.Float64("load_facts_duration_seconds", loadDuration.Seconds()),
		slog.Float64("extract_duration_seconds", extractDuration.Seconds()),
		slog.Float64("assess_duration_seconds", assessDuration.Seconds()),
		slog.Float64("graph_write_duration_seconds", writeDuration.Seconds()),
		slog.Float64("retract_duration_seconds", retractDuration.Seconds()),
		slog.Float64("total_duration_seconds", totalDuration.Seconds()),
	)
}
