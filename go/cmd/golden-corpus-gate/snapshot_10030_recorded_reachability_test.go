// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	chain "github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
	"github.com/eshu-hq/eshu/go/internal/reducer/packages/correlation"
	"github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
)

// TestGoldenSnapshot10030RecordedReachabilityPinsAreNonVacuous proves the
// #10030 snapshot pins on the two recorded-scanner HTTP query shapes actually
// evaluate reducer output instead of passing on an empty or hand-written
// body. The B-7 live gate replays the cassette but never runs
// EvaluateQueryShape on these HTTP shapes (D13 moved query_shapes out of the
// live path), so without this test a pin could drift from the reducer's real
// verdicts and nothing would fail.
//
// For each shape (CVE-2026-9229 symbol_reachable, CVE-2026-9301 not_called)
// the test builds the response body from REAL reducer output — the recorded
// scanner-worker envelopes in recorded-10030/emitted.json joined with the
// committed OSV advisory facts and the corpus consumption fact — asserts
// EvaluateQueryShape passes, then flips runtime_reachability to the sibling
// advisory's real verdict and asserts the shape rejects it. The mutation
// direction is deliberate: one scan's answers must not collapse across
// advisories, so each shape must fail the other's verdict.
func TestGoldenSnapshot10030RecordedReachabilityPinsAreNonVacuous(t *testing.T) {
	snapshot, err := LoadSnapshot(goldenSnapshotPath())
	if err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}
	findings := build10030RecordedImpactFindings(t)

	cases := []struct {
		shapeKey            string
		cveID               string
		wantReachability    string
		mutatedReachability string
	}{
		{
			shapeKey:            "GET /api/v0/supply-chain/impact/findings?limit=10&cve_id=CVE-2026-9229&profile=comprehensive",
			cveID:               "CVE-2026-9229",
			wantReachability:    "symbol_reachable",
			mutatedReachability: "not_called",
		},
		{
			shapeKey:            "GET /api/v0/supply-chain/impact/findings?limit=10&cve_id=CVE-2026-9301&profile=comprehensive",
			cveID:               "CVE-2026-9301",
			wantReachability:    "not_called",
			mutatedReachability: "symbol_reachable",
		},
	}
	for _, tc := range cases {
		t.Run(tc.cveID, func(t *testing.T) {
			shape, ok := snapshot.QueryShapes.HTTP[tc.shapeKey]
			if !ok {
				t.Fatalf("query_shapes.http missing %q", tc.shapeKey)
			}
			finding, ok := findings[tc.cveID]
			if !ok {
				t.Fatalf("no reducer finding for %s", tc.cveID)
			}
			if finding.RuntimeReachability != tc.wantReachability {
				t.Fatalf("%s RuntimeReachability = %q, want %q (reducer verdict moved)",
					tc.cveID, finding.RuntimeReachability, tc.wantReachability)
			}
			raw, err := json.Marshal(recorded10030FindingsBody(finding))
			if err != nil {
				t.Fatalf("marshal real reducer body: %v", err)
			}
			if result := EvaluateQueryShape(tc.shapeKey, shape, raw); !result.OK {
				t.Fatalf("real reducer output failed %s: %s", tc.shapeKey, result.Detail)
			}

			mutated := recorded10030WireItem(finding)
			mutated["runtime_reachability"] = tc.mutatedReachability
			mutatedRaw, err := json.Marshal(map[string]any{
				"findings": []any{mutated},
				"count":    1,
			})
			if err != nil {
				t.Fatalf("marshal mutated body: %v", err)
			}
			if result := EvaluateQueryShape(tc.shapeKey, shape, mutatedRaw); result.OK {
				t.Fatalf("mutated runtime_reachability=%q passed %s unexpectedly: %s",
					tc.mutatedReachability, tc.shapeKey, result.Detail)
			}
		})
	}
}

// TestGoldenSnapshot10030RecordedFailedReachabilityPinIsNonVacuous proves the
// #10030 failed-scan snapshot pin (CVE-2026-9401 analysis_failed) evaluates
// real reducer output instead of passing on an empty or hand-written body.
// The recorded scanner-worker envelopes carry no reachability fact for the
// legacy module — only the toolchain_unsupported warning — so the #10043
// mapping must surface runtime_reachability=analysis_failed with
// reachability.state=missing_evidence. The test builds the response body from
// REAL reducer output (the recorded envelopes joined with a synthetic
// GO-2026-9401 advisory mirroring the core recorded-10030 triple and the
// legacy package-consumption fact), asserts EvaluateQueryShape passes, then
// asserts two mutations are rejected: the sibling not_called verdict (one
// scan's answers must not collapse across advisories) and a reachable state
// (the missing-evidence pin must be load-bearing, not decorative).
func TestGoldenSnapshot10030RecordedFailedReachabilityPinIsNonVacuous(t *testing.T) {
	const (
		shapeKey = "GET /api/v0/supply-chain/impact/findings?limit=10&cve_id=CVE-2026-9401&profile=comprehensive"
		cveID    = "CVE-2026-9401"
	)
	snapshot, err := LoadSnapshot(goldenSnapshotPath())
	if err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}
	shape, ok := snapshot.QueryShapes.HTTP[shapeKey]
	if !ok {
		t.Fatalf("query_shapes.http missing %q", shapeKey)
	}
	finding := build10030RecordedFailedImpactFinding(t)
	if finding.RuntimeReachability != string(core.GoVulnReachabilityAnalysisFailed) {
		t.Fatalf("%s RuntimeReachability = %q, want analysis_failed (reducer verdict moved)",
			cveID, finding.RuntimeReachability)
	}
	if finding.Reachability == nil || finding.Reachability.State != core.SupplyChainReachabilityMissingEvidence {
		t.Fatalf("%s reachability = %#v, want missing_evidence (reducer enrichment moved)",
			cveID, finding.Reachability)
	}
	raw, err := json.Marshal(recorded10030FindingsBody(finding))
	if err != nil {
		t.Fatalf("marshal real reducer body: %v", err)
	}
	if result := EvaluateQueryShape(shapeKey, shape, raw); !result.OK {
		t.Fatalf("real reducer output failed %s: %s", shapeKey, result.Detail)
	}

	mutated := recorded10030WireItem(finding)
	mutated["runtime_reachability"] = "not_called"
	mutatedRaw, err := json.Marshal(map[string]any{
		"findings": []any{mutated},
		"count":    1,
	})
	if err != nil {
		t.Fatalf("marshal mutated body: %v", err)
	}
	if result := EvaluateQueryShape(shapeKey, shape, mutatedRaw); result.OK {
		t.Fatalf("mutated runtime_reachability=not_called passed %s unexpectedly: %s",
			shapeKey, result.Detail)
	}

	mutatedState := recorded10030WireItem(finding)
	mutatedState["reachability"] = map[string]any{"state": "reachable"}
	mutatedStateRaw, err := json.Marshal(map[string]any{
		"findings": []any{mutatedState},
		"count":    1,
	})
	if err != nil {
		t.Fatalf("marshal state-mutated body: %v", err)
	}
	if result := EvaluateQueryShape(shapeKey, shape, mutatedStateRaw); result.OK {
		t.Fatalf("mutated reachability.state=reachable passed %s unexpectedly: %s",
			shapeKey, result.Detail)
	}
}

// build10030RecordedFailedImpactFinding replays the reducer inputs for the
// #10030 failed-scan tuple: the five RECORDED scanner-worker envelopes (the
// legacy module evidence and the toolchain_unsupported warning are the two
// that matter here), a synthetic GO-2026-9401/CVE-2026-9401 advisory pair
// mirroring the core recorded-10030 triple (the committed cassette carries
// no 9401 advisory facts), and the legacy-lib package-consumption fact. It
// requires exactly the one failed tuple, so a duplicate, a collapse into a
// sibling advisory, or a dropped finding fails here.
func build10030RecordedFailedImpactFinding(t *testing.T) core.SupplyChainImpactFinding {
	t.Helper()

	envelopes := load10030RecordedScannerEnvelopes(t)
	envelopes = append(envelopes, recorded10030FailedAdvisoryEnvelopes()...)
	envelopes = append(envelopes, facts.Envelope{
		FactID:   "orders-legacy-lib-consumption",
		FactKind: correlation.PackageConsumptionFactKind,
		Payload: map[string]any{
			"package_id":        "gomod://proxy.golang.org/github.com/acme/legacy-lib",
			"relationship_kind": "consumption",
			"repository_id":     "repository:r_ea78e8bb",
			"dependency_range":  "v0.1.0",
			"canonical_writes":  1,
			"evidence_fact_ids": []any{"manifest-lock-1"},
		},
	})

	findings := core.BuildSupplyChainImpactFindings(envelopes)
	if len(findings) != 1 {
		t.Fatalf("recorded failed findings = %d, want exactly the 9401 tuple: %#v", len(findings), findings)
	}
	finding := findings[0]
	if finding.CVEID != "CVE-2026-9401" {
		t.Fatalf("recorded failed finding CVE = %q, want CVE-2026-9401: %#v", finding.CVEID, finding)
	}
	return finding
}

// recorded10030FailedAdvisoryEnvelopes returns the synthetic advisory-side
// facts (vulnerability.cve plus vulnerability.affected_package with the
// affected range the core recorded-10030 triple pins) for the failed
// GO-2026-9401/CVE-2026-9401 tuple. The committed vulnerability-intelligence
// cassette carries no 9401 advisory facts, so the pin body builds them the
// same way the core classifier test does; the scanner envelopes above stay
// the single recorded-scanner truth input.
func recorded10030FailedAdvisoryEnvelopes() []facts.Envelope {
	const osvID = "GO-2026-9401"
	const cveID = "CVE-2026-9401"
	const modulePath = "github.com/acme/legacy-lib"
	return []facts.Envelope{
		{
			FactID:   "recorded-10030-cve-" + osvID,
			FactKind: chain.VulnerabilityCVEFactKind,
			Payload: map[string]any{
				"cve_id":      cveID,
				"advisory_id": osvID,
				"cvss_score":  7.5,
				"aliases":     []any{osvID, cveID},
			},
		},
		{
			FactID:   "recorded-10030-affected-" + osvID,
			FactKind: chain.VulnerabilityAffectedPackageFactKind,
			Payload: map[string]any{
				"advisory_id":  osvID,
				"cve_id":       cveID,
				"ecosystem":    "go",
				"package_name": modulePath,
				"module_path":  modulePath,
				"package_id":   "gomod://proxy.golang.org/" + modulePath,
				"affected_ranges": []any{map[string]any{
					"type": "SEMVER",
					"events": []any{
						map[string]any{"introduced": "0"},
						map[string]any{"fixed": "0.1.1"},
					},
				}},
				"fixed_versions": []any{"0.1.1"},
			},
		},
	}
}

// build10030RecordedImpactFindings replays the reducer inputs the #10030 live
// gate replays: the five RECORDED scanner-worker envelopes (two go module
// evidence, one symbol and one not_called call-reachability fact, plus the
// toolchain warning), the committed OSV advisory facts for both advisories,
// and the package-consumption fact the corpus reads from the orders-api go.mod
// fixture. It returns the findings keyed by CVE id and requires exactly the
// two recorded tuples, so a duplicate, a collapse across advisories, or a
// dropped tuple fails here rather than slipping through a per-shape body.
func build10030RecordedImpactFindings(t *testing.T) map[string]core.SupplyChainImpactFinding {
	t.Helper()

	envelopes := load10030RecordedScannerEnvelopes(t)
	envelopes = append(envelopes, load10030CassetteAdvisoryEnvelopes(t)...)
	envelopes = append(envelopes, facts.Envelope{
		FactID:   "orders-api-lib-common-consumption",
		FactKind: correlation.PackageConsumptionFactKind,
		Payload: map[string]any{
			"package_id":        "gomod://proxy.golang.org/github.com/acme/lib-common",
			"relationship_kind": "consumption",
			"repository_id":     "repository:r_ea78e8bb",
			"dependency_range":  "v1.0.0",
			"canonical_writes":  1,
			"evidence_fact_ids": []any{"manifest-lock-1"},
		},
	})

	findings := core.BuildSupplyChainImpactFindings(envelopes)
	byCVE := make(map[string]core.SupplyChainImpactFinding, len(findings))
	for _, finding := range findings {
		byCVE[finding.CVEID] = finding
	}
	for _, cveID := range []string{"CVE-2026-9229", "CVE-2026-9301"} {
		if _, ok := byCVE[cveID]; !ok {
			t.Fatalf("no finding for %s among %d findings: %#v", cveID, len(findings), findings)
		}
	}
	if len(byCVE) != 2 {
		t.Fatalf("recorded findings = %d CVEs, want exactly the 2 recorded tuples: %#v", len(byCVE), byCVE)
	}
	return byCVE
}

// load10030RecordedScannerEnvelopes reads the real analyzer output recorded
// for eshu-hq/eshu#10030: five facts in emission order. A re-record that
// changes the emitter wire shape fails the reducer's own emission-shape test
// first; reading the file here (instead of copying its values) keeps this
// gate body live against the same bytes.
func load10030RecordedScannerEnvelopes(t *testing.T) []facts.Envelope {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join("..", "..", "..",
		"go", "internal", "reducer", "supplychain", "core",
		"testdata", "recorded-10030", "emitted.json"))
	if err != nil {
		t.Fatalf("read recorded-10030 emitted.json: %v", err)
	}
	var envelopes []facts.Envelope
	if err := json.Unmarshal(raw, &envelopes); err != nil {
		t.Fatalf("decode recorded-10030 emitted.json: %v", err)
	}
	return envelopes
}

// load10030CassetteAdvisoryEnvelopes returns the committed OSV advisory facts
// (vulnerability.cve plus vulnerability.affected_package) for the recorded
// orders-api scope, which carry the source-attributed severity the scanner
// never emits: the CVSS vector and label the #9229 provenance and
// cvss_v3_high pins derive from. Only the advisory kinds are taken; the
// scope's transcribed scanner facts stay out so the recorded emitted.json
// above remains the single scanner-truth input.
func load10030CassetteAdvisoryEnvelopes(t *testing.T) []facts.Envelope {
	t.Helper()

	const scopeID = "vulnerability_intelligence:orders-api:go-alias"
	raw, err := os.ReadFile(filepath.Join("..", "..", "..",
		"testdata", "cassettes", "vulnerabilityintelligence", "supply-chain-demo.json"))
	if err != nil {
		t.Fatalf("read vulnerability intelligence cassette: %v", err)
	}
	var file struct {
		Scopes []struct {
			ScopeID      string    `json:"scope_id"`
			GenerationID string    `json:"generation_id"`
			ObservedAt   time.Time `json:"observed_at"`
			Facts        []struct {
				FactKind      string         `json:"fact_kind"`
				StableFactKey string         `json:"stable_fact_key"`
				Payload       map[string]any `json:"payload"`
			} `json:"facts"`
		} `json:"scopes"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatalf("parse vulnerability intelligence cassette: %v", err)
	}
	for _, scope := range file.Scopes {
		if scope.ScopeID != scopeID {
			continue
		}
		var envelopes []facts.Envelope
		for _, fact := range scope.Facts {
			switch fact.FactKind {
			case "vulnerability.cve", "vulnerability.affected_package":
				envelopes = append(envelopes, facts.Envelope{
					FactID:        fact.StableFactKey,
					ScopeID:       scope.ScopeID,
					GenerationID:  scope.GenerationID,
					FactKind:      fact.FactKind,
					StableFactKey: fact.StableFactKey,
					ObservedAt:    scope.ObservedAt,
					Payload:       fact.Payload,
				})
			}
		}
		if len(envelopes) == 0 {
			t.Fatalf("cassette scope %q has no advisory facts", scopeID)
		}
		return envelopes
	}
	t.Fatalf("cassette has no scope %q", scopeID)
	return nil
}

// recorded10030FindingsBody wraps one real finding in the single-result list
// body the pinned shapes assert: results_field "findings" with
// minimum_results and maximum_results both 1, plus the sibling count field
// the required_response_fields list names.
func recorded10030FindingsBody(finding core.SupplyChainImpactFinding) map[string]any {
	return map[string]any{
		"findings": []any{recorded10030WireItem(finding)},
		"count":    1,
	}
}

// recorded10030WireItem projects one REAL reducer finding onto the HTTP wire
// object the findings list route serves. The keys are the wire names the
// query side decodes (impact/findings_decode.go DecodeFindingRow reads
// cve_id, advisory_id, package_id, repository_id, observed_version,
// runtime_reachability, reachability, evidence_path, cvss_score,
// priority_reason_codes and the provenance selected_* block); every value
// comes straight from the reducer struct, so no hand-written verdict can
// satisfy the pins.
func recorded10030WireItem(finding core.SupplyChainImpactFinding) map[string]any {
	item := map[string]any{
		"cve_id":                finding.CVEID,
		"advisory_id":           finding.AdvisoryID,
		"package_id":            finding.PackageID,
		"repository_id":         finding.RepositoryID,
		"observed_version":      finding.ObservedVersion,
		"runtime_reachability":  finding.RuntimeReachability,
		"evidence_path":         stringsToAny10030(finding.EvidencePath),
		"cvss_score":            finding.CVSSScore,
		"priority_reason_codes": stringsToAny10030(finding.PriorityReasonCodes),
		"provenance": map[string]any{
			"selected_severity_source":       finding.SeveritySource,
			"selected_severity_score":        finding.CVSSScore,
			"selected_severity_score_source": finding.SeverityScoreSource,
			"selected_severity_vector":       finding.SeverityVector,
			"selected_severity_label":        finding.SeverityLabel,
		},
	}
	if block := recorded10030ReachabilityBlock(finding); block != nil {
		item["reachability"] = block
	}
	return item
}

// recorded10030ReachabilityBlock projects the finding's REAL reachability
// enrichment onto the wire object the findings list route serves
// (querycontract/packet ReachabilityResult: state plus the missing_evidence
// array among others). It mirrors the reducer's own payload encoder
// (supplyChainReachabilityPayload), so the failed-scan pin asserts the same
// state and missing-evidence entries the live finding persists.
func recorded10030ReachabilityBlock(finding core.SupplyChainImpactFinding) map[string]any {
	reachability := finding.Reachability
	if reachability == nil || reachability.State == "" {
		return nil
	}
	block := map[string]any{
		"state":             string(reachability.State),
		"confidence":        reachability.Confidence,
		"source":            reachability.Source,
		"evidence":          reachability.Evidence,
		"reason":            reachability.Reason,
		"language_maturity": reachability.LanguageMaturity,
	}
	if len(reachability.MissingEvidence) > 0 {
		block["missing_evidence"] = stringsToAny10030(reachability.MissingEvidence)
	}
	if reachability.ScanLevel != "" {
		block["scan_level"] = reachability.ScanLevel
	}
	if reachability.ScanMode != "" {
		block["scan_mode"] = reachability.ScanMode
	}
	if reachability.ScannerVersion != "" {
		block["scanner_version"] = reachability.ScannerVersion
	}
	if reachability.TraceCount != nil {
		block["trace_count"] = *reachability.TraceCount
	}
	return block
}

func stringsToAny10030(values []string) []any {
	out := make([]any, 0, len(values))
	for _, value := range values {
		out = append(out, value)
	}
	return out
}
