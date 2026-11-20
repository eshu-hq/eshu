// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Tests in this file feed the reducer the RECORDED scanner-worker output for
// eshu-hq/eshu#10030 (testdata/recorded-10030/emitted.json, see its README)
// instead of hand-written payloads, so a wrong reading of the real emitter
// wire shape fails here. The three (repository, module, advisory) tuples the
// recording carries must classify distinctly: the scanned orders-api module
// proves symbol reachability for GO-2026-9229 and not_called for
// GO-2026-9301, while the unscanned legacy module carries only module
// evidence plus the toolchain_unsupported warning for GO-2026-9401.

const (
	recorded10030RepositoryID = "repository:r_ea78e8bb"
	recorded10030CommitSHA    = "bbca297156091bebc27cd79940f9f9b6017914bb"
	recorded10030Module       = "github.com/acme/lib-common"
	recorded10030LegacyModule = "github.com/acme/legacy-lib"
)

// loadRecorded10030Envelopes returns the five facts the real analyzer
// emitted, in emission order: root module evidence, legacy module evidence,
// the symbol reachability fact, the not_called fact, and the warning.
func loadRecorded10030Envelopes(t *testing.T) []facts.Envelope {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join("testdata", "recorded-10030", "emitted.json"))
	if err != nil {
		t.Fatalf("read recorded-10030 emitted.json: %v", err)
	}
	var envelopes []facts.Envelope
	if err := json.Unmarshal(raw, &envelopes); err != nil {
		t.Fatalf("decode recorded-10030 emitted.json: %v", err)
	}
	return envelopes
}

// TestRecorded10030EmissionShape pins the recording the cassette
// transcribes: five facts in emission order with the wire values the gate
// replays. A re-record that changes the emitter output fails here first.
func TestRecorded10030EmissionShape(t *testing.T) {
	t.Parallel()

	envelopes := loadRecorded10030Envelopes(t)
	if len(envelopes) != 5 {
		t.Fatalf("recorded envelopes = %d, want 5", len(envelopes))
	}
	wantKinds := []string{
		"vulnerability.go_module_evidence",
		"vulnerability.go_module_evidence",
		"vulnerability.go_call_reachability",
		"vulnerability.go_call_reachability",
		"scanner_worker.warning",
	}
	for i, want := range wantKinds {
		if envelopes[i].FactKind != want {
			t.Fatalf("envelope[%d].FactKind = %q, want %q", i, envelopes[i].FactKind, want)
		}
		if envelopes[i].CollectorKind != "scanner_worker" {
			t.Fatalf("envelope[%d].CollectorKind = %q, want scanner_worker", i, envelopes[i].CollectorKind)
		}
	}
	symbol := envelopes[2].Payload
	if symbol["osv_id"] != "GO-2026-9229" || symbol["reachability_level"] != "symbol" {
		t.Fatalf("symbol fact = %v, want GO-2026-9229/symbol", symbol)
	}
	if symbol["deepest_symbol"] != "Identity[string]" {
		t.Fatalf("deepest_symbol = %v, want the instantiated generic the scanner reports", symbol["deepest_symbol"])
	}
	if symbol["source_commit_sha"] != recorded10030CommitSHA {
		t.Fatalf("source_commit_sha = %v, want the staged HEAD", symbol["source_commit_sha"])
	}
	notCalled := envelopes[3].Payload
	if notCalled["osv_id"] != "GO-2026-9301" || notCalled["reachability_level"] != "not_called" {
		t.Fatalf("not_called fact = %v, want GO-2026-9301/not_called", notCalled)
	}
	warning := envelopes[4].Payload
	if warning["reason"] != "toolchain_unsupported" || warning["relative_path"] != "legacy/go.mod" {
		t.Fatalf("warning = %v, want toolchain_unsupported for legacy/go.mod", warning)
	}
}

// recorded10030Advisories builds the advisory-side facts (cve plus
// affected_package with the affected range the DB pins) for the three
// recorded tuples. The cassette carries the same facts from the
// vulnerability-intelligence collector; the scanner never emits them.
func recorded10030Advisories(t *testing.T) []facts.Envelope {
	t.Helper()

	out := []facts.Envelope{}
	add := func(osvID, cveID, modulePath, fixed string) {
		advisory := vulnerabilityCVEFact("recorded-10030-cve-"+osvID, cveID, 7.5)
		advisory.Payload["advisory_id"] = osvID
		advisory.Payload["aliases"] = []any{osvID, cveID}
		affected := goAffectedPackageEnvelopeWithRange(
			t, osvID, modulePath,
			[]map[string]any{
				{
					"type": "SEMVER",
					"events": []map[string]any{
						{"introduced": "0"},
						{"fixed": fixed},
					},
				},
			},
			[]string{fixed},
		)
		affected.Payload["advisory_id"] = osvID
		affected.Payload["cve_id"] = cveID
		affected.Payload["package_id"] = "gomod://proxy.golang.org/" + modulePath
		out = append(out, advisory, affected)
	}
	add("GO-2026-9229", "CVE-2026-9229", recorded10030Module, "1.0.1")
	add("GO-2026-9301", "CVE-2026-9301", recorded10030Module, "1.0.1")
	add("GO-2026-9401", "CVE-2026-9401", recorded10030LegacyModule, "0.1.1")
	return out
}

// TestRecorded10030TuplesClassifyDistinctly proves the classifier keeps the
// three recorded (repository, module, advisory) tuples apart: the scanned
// tuple carries call proof, the unscanned legacy tuple carries only the
// warning, and the failed tuple cannot borrow the scanned tuple's proof.
func TestRecorded10030TuplesClassifyDistinctly(t *testing.T) {
	t.Parallel()

	recorded := loadRecorded10030Envelopes(t)
	envelopes := append(append([]facts.Envelope{}, recorded...), recorded10030Advisories(t)...)

	findings, quarantined, err := classifyGoVulnerabilityReachabilityWithQuarantine(envelopes)
	if err != nil {
		t.Fatalf("classify recorded-10030: %v", err)
	}
	if len(quarantined) != 0 {
		t.Fatalf("quarantined = %d, want 0 for recorded emitter output: %#v", len(quarantined), quarantined)
	}
	if len(findings) != 3 {
		t.Fatalf("findings = %d, want 3 distinct tuples: %#v", len(findings), findings)
	}
	byTuple := map[string]GoVulnerabilityFinding{}
	for _, finding := range findings {
		byTuple[finding.RepositoryID+"\x00"+finding.ModulePath+"\x00"+finding.OSVID] = finding
	}
	scanned := byTuple[recorded10030RepositoryID+"\x00"+recorded10030Module+"\x00GO-2026-9229"]
	if scanned.Reachability != GoVulnReachabilitySymbolReachable {
		t.Fatalf("scanned tuple reachability = %q, want symbol_reachable", scanned.Reachability)
	}
	if scanned.Symbol != "Identity[string]" {
		t.Fatalf("scanned tuple symbol = %q, want the recorded instantiated generic", scanned.Symbol)
	}
	if containsString(scanned.MissingEvidence, "govulncheck call-graph evidence missing") {
		t.Fatalf("scanned tuple must not miss call-graph proof: %#v", scanned.MissingEvidence)
	}
	uncalled := byTuple[recorded10030RepositoryID+"\x00"+recorded10030Module+"\x00GO-2026-9301"]
	if uncalled.Reachability != GoVulnReachabilityNotCalled {
		t.Fatalf("uncalled tuple reachability = %q, want not_called", uncalled.Reachability)
	}
	if containsString(uncalled.MissingEvidence, "govulncheck call-graph evidence missing") {
		t.Fatalf("not_called tuple must not miss call-graph proof: %#v", uncalled.MissingEvidence)
	}
	failed := byTuple[recorded10030RepositoryID+"\x00"+recorded10030LegacyModule+"\x00GO-2026-9401"]
	if failed.Reachability != GoVulnReachabilityModuleOnly {
		t.Fatalf("failed tuple reachability = %q, want module_only", failed.Reachability)
	}
	if !containsString(failed.MissingEvidence, "govulncheck call-graph evidence missing") {
		t.Fatalf("failed tuple MissingEvidence = %#v, want the missing call-proof gap", failed.MissingEvidence)
	}
	wantWarning := "govulncheck scan toolchain_unsupported at legacy/go.mod"
	if !containsString(failed.MissingEvidence, wantWarning) {
		t.Fatalf("failed tuple MissingEvidence = %#v, want the scoped warning %q", failed.MissingEvidence, wantWarning)
	}
	if containsString(scanned.MissingEvidence, wantWarning) || containsString(uncalled.MissingEvidence, wantWarning) {
		t.Fatalf("module-scoped warning leaked into the scanned module: %#v / %#v",
			scanned.MissingEvidence, uncalled.MissingEvidence)
	}
}

// TestRecorded10030ImpactFindingsKeepLiveVerdicts feeds the recorded
// envelopes through the same impact path the live gate replays, proving the
// CVE-2026-9229 symbol_reachable identity holds and the second advisory
// surfaces as not_called before the cassette carries either recorded fact.
func TestRecorded10030ImpactFindingsKeepLiveVerdicts(t *testing.T) {
	t.Parallel()

	recorded := loadRecorded10030Envelopes(t)
	packageID := "gomod://proxy.golang.org/" + recorded10030Module
	envelopes := append(append([]facts.Envelope{}, recorded...),
		packageConsumptionFactWithRange("recorded-10030-consume", packageID, recorded10030RepositoryID, "v1.0.0"))
	for _, advisory := range recorded10030Advisories(t) {
		if advisory.FactKind != "vulnerability.cve" && advisory.FactKind != "vulnerability.affected_package" {
			continue
		}
		if payload, ok := advisory.Payload["cve_id"]; ok && payload != "CVE-2026-9401" {
			envelopes = append(envelopes, advisory)
		}
	}

	findings := supplyChainImpactFindingsByCVE(BuildSupplyChainImpactFindings(envelopes))

	called := findings["CVE-2026-9229"]
	assertSupplyChainImpactStatus(t, called, SupplyChainImpactAffectedExact)
	if called.RuntimeReachability != string(GoVulnReachabilitySymbolReachable) {
		t.Fatalf("CVE-2026-9229 RuntimeReachability = %q, want symbol_reachable (identity must not move)",
			called.RuntimeReachability)
	}
	if containsString(called.MissingEvidence, "govulncheck call-graph evidence missing") {
		t.Fatalf("CVE-2026-9229 must keep its recorded call-path proof: %#v", called.MissingEvidence)
	}

	quiet := findings["CVE-2026-9301"]
	if quiet.Reachability == nil {
		t.Fatal("CVE-2026-9301 has no Reachability")
	}
	if quiet.Reachability.State != SupplyChainReachabilityNotCalled ||
		quiet.RuntimeReachability != string(GoVulnReachabilityNotCalled) {
		t.Fatalf("CVE-2026-9301 state=%q runtime=%q, want not_called/not_called",
			quiet.Reachability.State, quiet.RuntimeReachability)
	}
}
