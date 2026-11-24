// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

const (
	liveOrdersScopeID      = "vulnerability_intelligence:orders-api:go-alias"
	liveOrdersGenerationID = "cassette-vulnintel-orders-go-alias-gen1"
	liveOrdersGoScopeID    = "go-reachability://repository:r_ea78e8bb"
	liveOrdersWarningMsg   = "govulncheck scan toolchain_unsupported at legacy/go.mod"
)

// liveScopeFact is one fact row of the orders go-alias scope as the Postgres
// fact store held it, trimmed to the columns the reducer reads.
type liveScopeFact struct {
	FactID           string         `json:"fact_id"`
	ScopeID          string         `json:"scope_id"`
	GenerationID     string         `json:"generation_id"`
	FactKind         string         `json:"fact_kind"`
	StableFactKey    string         `json:"stable_fact_key"`
	SchemaVersion    string         `json:"schema_version"`
	CollectorKind    string         `json:"collector_kind"`
	SourceConfidence string         `json:"source_confidence"`
	FencingToken     int64          `json:"fencing_token"`
	IsTombstone      bool           `json:"is_tombstone"`
	ObservedAt       string         `json:"observed_at"`
	Payload          map[string]any `json:"payload"`
}

// goScanFactKinds are the facts the scanner worker writes into a
// go-reachability scope; every other kind in the recorded file is advisory
// evidence of the vulnerability-intelligence scope.
func goScanFact(row liveScopeFact) bool {
	switch row.FactKind {
	case "vulnerability.go_module_evidence", "vulnerability.go_call_reachability", facts.ScannerWorkerWarningFactKind:
		return true
	default:
		return false
	}
}

// loadLive9401Split reads testdata/live-9401 and splits it the way production
// stores it: the advisories stay in the orders scope, the Go scan facts move
// to the repository's go-reachability scope.
func loadLive9401Split(t *testing.T) (advisories, goScan []facts.Envelope) {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join("testdata", "live-9401", "scope-facts.json"))
	if err != nil {
		t.Fatalf("read live-9401 scope-facts.json: %v", err)
	}
	var rows []liveScopeFact
	if err := json.Unmarshal(raw, &rows); err != nil {
		t.Fatalf("decode live-9401 scope-facts.json: %v", err)
	}
	for _, row := range rows {
		observed, err := time.Parse(time.RFC3339, row.ObservedAt)
		if err != nil {
			t.Fatalf("observed_at %q: %v", row.ObservedAt, err)
		}
		envelope := facts.Envelope{
			FactID:           row.FactID,
			ScopeID:          row.ScopeID,
			GenerationID:     row.GenerationID,
			FactKind:         row.FactKind,
			StableFactKey:    row.StableFactKey,
			SchemaVersion:    row.SchemaVersion,
			CollectorKind:    row.CollectorKind,
			FencingToken:     row.FencingToken,
			SourceConfidence: row.SourceConfidence,
			ObservedAt:       observed,
			Payload:          row.Payload,
			IsTombstone:      row.IsTombstone,
		}
		if goScanFact(row) {
			envelope.ScopeID = liveOrdersGoScopeID
			goScan = append(goScan, envelope)
			continue
		}
		advisories = append(advisories, envelope)
	}
	return advisories, goScan
}

// goStageImpactLoader serves one pass the way the production store does: the
// intent's own scope honors the requested fact kinds, and the Go scan facts
// come only from ListActiveGoReachabilityFacts, the cross-scope Go stage.
type goStageImpactLoader struct {
	*stubSupplyChainImpactFactLoader
	goScan    []facts.Envelope
	requested [][]string
	goFilters []GoReachabilityFactFilter
}

func (l *goStageImpactLoader) ListFactsByKind(_ context.Context, _, _ string, kinds []string) ([]facts.Envelope, error) {
	l.requested = append(l.requested, append([]string(nil), kinds...))
	var out []facts.Envelope
	for _, envelope := range l.scopeFacts {
		if slices.Contains(kinds, envelope.FactKind) {
			out = append(out, envelope)
		}
	}
	return out, nil
}

func (l *goStageImpactLoader) ListActiveGoReachabilityFacts(_ context.Context, filter GoReachabilityFactFilter) ([]facts.Envelope, error) {
	l.goFilters = append(l.goFilters, filter)
	return append([]facts.Envelope(nil), l.goScan...), nil
}

// runLive9401Pass runs the handler over the orders scope with the given
// own-scope facts and Go-stage facts and returns the written findings by CVE.
func runLive9401Pass(t *testing.T, ownScope, goScan []facts.Envelope) (map[string]SupplyChainImpactFinding, *goStageImpactLoader) {
	t.Helper()

	for _, module := range []string{"github.com/acme/legacy-lib", "github.com/acme/lib-common"} {
		ownScope = append(ownScope, packageConsumptionFactWithRange(
			"live-9401-consume-"+module, "gomod://proxy.golang.org/"+module, "repository:r_ea78e8bb", "v0.1.0"))
	}
	loader := &goStageImpactLoader{
		stubSupplyChainImpactFactLoader: &stubSupplyChainImpactFactLoader{scopeFacts: ownScope},
		goScan:                          goScan,
	}
	writer := &recordingSupplyChainImpactWriter{}
	handler := SupplyChainImpactHandler{
		FencingTokenIssuer: newTestImpactFencingTokenIssuer(),
		FactLoader:         loader,
		Writer:             writer,
	}
	intent := fanOutIntent(1, nil)
	intent.Domain = reducercontract.DomainSupplyChainImpact
	intent.ScopeID = liveOrdersScopeID
	intent.GenerationID = liveOrdersGenerationID
	if _, err := handler.Handle(context.Background(), intent); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	byCVE := map[string]SupplyChainImpactFinding{}
	for _, finding := range writer.write.Findings {
		byCVE[finding.CVEID] = finding
	}
	return byCVE, loader
}

// TestSupplyChainImpactFactKindsLeaveTheScannerWarningToTheGoStage pins the
// production contract of #10030: the own-scope kind list does not carry
// scanner_worker.warning. The warning is target-only under engaged protection
// for every other analyzer, and Go scan facts reach a pass only through the
// cross-scope Go stage.
func TestSupplyChainImpactFactKindsLeaveTheScannerWarningToTheGoStage(t *testing.T) {
	t.Parallel()

	if slices.Contains(supplyChainImpactFactKinds(), facts.ScannerWorkerWarningFactKind) {
		t.Fatalf("supplyChainImpactFactKinds() lists %s: it must stay out of the own-scope load", facts.ScannerWorkerWarningFactKind)
	}
}

// TestGoScanFactsFromTheGoReachabilityScopeJoinThroughTheGoStage runs the real
// orders facts in the production shape: advisories in the pass's own scope,
// the unfinished-scan warning, module evidence and call reachability in the
// repository's go-reachability scope, served by the Go stage. The three Go
// verdicts the golden gate asserts must come out: GO-2026-9229 symbol reachable,
// GO-2026-9301 not called, CVE-2026-9401 analysis_failed with the scoped
// toolchain_unsupported gap.
func TestGoScanFactsFromTheGoReachabilityScopeJoinThroughTheGoStage(t *testing.T) {
	t.Parallel()

	advisories, goScan := loadLive9401Split(t)
	findings, loader := runLive9401Pass(t, advisories, goScan)

	if len(loader.goFilters) == 0 || !slices.Contains(loader.goFilters[0].RepositoryIDs, "repository:r_ea78e8bb") {
		t.Fatalf("Go stage filters = %#v, want the orders repository id derived from the pass's own evidence", loader.goFilters)
	}
	legacy, ok := findings["CVE-2026-9401"]
	if !ok {
		t.Fatalf("no CVE-2026-9401 finding written: %v", findings)
	}
	if legacy.RuntimeReachability != string(GoVulnReachabilityAnalysisFailed) {
		t.Fatalf("CVE-2026-9401 RuntimeReachability = %q, want analysis_failed", legacy.RuntimeReachability)
	}
	if !containsString(legacy.MissingEvidence, liveOrdersWarningMsg) {
		t.Fatalf("CVE-2026-9401 MissingEvidence = %#v, want %q", legacy.MissingEvidence, liveOrdersWarningMsg)
	}
	if got := findings["CVE-2026-9229"].RuntimeReachability; got != string(GoVulnReachabilitySymbolReachable) {
		t.Fatalf("CVE-2026-9229 RuntimeReachability = %q, want symbol_reachable", got)
	}
	if got := findings["CVE-2026-9301"].RuntimeReachability; got != string(GoVulnReachabilityNotCalled) {
		t.Fatalf("CVE-2026-9301 RuntimeReachability = %q, want not_called", got)
	}
}

// TestAnOwnScopeGoWarningNeverReachesAPass is the twin of the join test: the
// same facts left in the orders scope (the old cassette shape) read
// module_only, because the own-scope load asks for no warning. That shape is
// not production's, which is why the cassette moves the Go scan facts out and
// the kind list does not widen.
func TestAnOwnScopeGoWarningNeverReachesAPass(t *testing.T) {
	t.Parallel()

	advisories, goScan := loadLive9401Split(t)
	ownScope := append([]facts.Envelope{}, advisories...)
	for _, envelope := range goScan {
		envelope.ScopeID = liveOrdersScopeID
		ownScope = append(ownScope, envelope)
	}
	findings, _ := runLive9401Pass(t, ownScope, nil)

	legacy := findings["CVE-2026-9401"]
	if legacy.RuntimeReachability != string(GoVulnReachabilityModuleOnly) {
		t.Fatalf("CVE-2026-9401 RuntimeReachability = %q, want module_only for a warning stored in the pass's own scope", legacy.RuntimeReachability)
	}
}
