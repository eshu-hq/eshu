// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// roundsCutGoldenPath holds the writer input of the seeded round-cut pass as
// the code wrote it before the #11156 diagnostic existed. The diagnostic only
// observes, so the file must keep matching byte for byte.
const roundsCutGoldenPath = "testdata/rounds_cut_11156_write.golden.json"

// roundsCutWriteJSON renders the parts of the writer input a diagnostic could
// disturb: the findings, the partial flag, the stamp, the causes and the token.
func roundsCutWriteJSON(t *testing.T, write SupplyChainImpactWrite) []byte {
	t.Helper()
	body, err := json.MarshalIndent(map[string]any{
		"intent_id":        write.IntentID,
		"scope_id":         write.ScopeID,
		"generation_id":    write.GenerationID,
		"partial_evidence": write.PartialEvidence,
		"scoped_cve_ids":   write.ScopedCVEIDs,
		"partial_causes":   write.PartialCauses,
		"scope_fact_kinds": write.ScopeFactKinds,
		"fencing_token":    write.FencingToken,
		"findings":         write.Findings,
	}, "", "  ")
	if err != nil {
		t.Fatalf("marshal write: %v", err)
	}
	return append(body, '\n')
}

// infoLines returns the INFO log lines of one pass whose message contains msg.
func (h truncationHarness) infoLines(t *testing.T, msg string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(h.logs.String()), "\n") {
		if line == "" {
			continue
		}
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("log line %q is not JSON: %v", line, err)
		}
		if text, _ := entry["msg"].(string); entry["level"] == "INFO" && strings.Contains(text, msg) {
			out = append(out, entry)
		}
	}
	return out
}

// TestRoundsCutReportsUnrequestedFrontier pins #11156 decision 9: a pass cut at
// the round cap reports which arms of the follow-up filter were still
// non-empty, how large each was, and how many envelopes the pass held, so an
// operator can see which arm keeps the walk going. The expanding fixture finds
// one new package per round, so the package arm is the one that never closes.
func TestRoundsCutReportsUnrequestedFrontier(t *testing.T) {
	t.Parallel()

	h := runTruncationPass(t, expandingImpactLoader(), 0)
	if !h.writer.write.PartialEvidence || h.result.SubSignals["evidence_truncated_rounds"] != 1 {
		t.Fatal("fixture did not cut at the round cap")
	}
	signals := h.result.SubSignals
	if got := signals["rounds_cut_frontier_package_ids"]; got != 1 {
		t.Fatalf("rounds_cut_frontier_package_ids = %v, want 1 (one package per round never closes)", got)
	}
	for _, arm := range []string{"cve_ids", "repository_ids", "subject_digests"} {
		if got, ok := signals["rounds_cut_frontier_"+arm]; !ok || got != 0 {
			t.Fatalf("rounds_cut_frontier_%s = %v (present %v), want 0 written beside the open arm", arm, got, ok)
		}
	}
	if got := signals["rounds_cut_rounds"]; got != float64(maxSupplyChainImpactActiveEvidenceLoads) {
		t.Fatalf("rounds_cut_rounds = %v, want %d", got, maxSupplyChainImpactActiveEvidenceLoads)
	}
	cutEnvelopes := signals["rounds_cut_envelopes"]
	if cutEnvelopes < float64(maxSupplyChainImpactActiveEvidenceLoads) {
		t.Fatalf("rounds_cut_envelopes = %v, want at least one envelope per round", cutEnvelopes)
	}

	logged := h.logs.String()
	if warn, info := strings.Index(logged, "findings not retracted"), strings.Index(logged, "cut at the round cap"); warn < 0 || info < warn {
		t.Fatalf("the frontier INFO line must follow the truncation WARN line: %s", logged)
	}
	lines := h.infoLines(t, "evidence expansion cut at the round cap")
	if len(lines) != 1 {
		t.Fatalf("round-cut INFO lines = %d, want 1: %s", len(lines), h.logs.String())
	}
	line := lines[0]
	for key, want := range map[string]any{
		"cause":                          "active_expansion_rounds",
		"rounds":                         float64(maxSupplyChainImpactActiveEvidenceLoads),
		"pass_mode":                      "full",
		"scope_id":                       "vuln-intel://osv/npm/example",
		"generation_id":                  "generation-7154",
		"cut_envelopes":                  cutEnvelopes,
		"frontier_package_ids":           float64(1),
		"frontier_cve_ids":               float64(0),
		"frontier_repository_ids":        float64(0),
		"frontier_file_repository_ids":   float64(0),
		"frontier_seed_product_criteria": float64(0),
	} {
		if line[key] != want {
			t.Fatalf("INFO %s = %v, want %v", key, line[key], want)
		}
	}
	if counter := h.truncatedCounter(t); counter["active_expansion_rounds"] != 1 || len(counter) != 1 {
		t.Fatalf("truncation counter = %v, want the unchanged single active_expansion_rounds", counter)
	}
}

// TestRoundsCutFrontierSilentWithoutRoundCut keeps the new signal off every
// pass the round cap did not stop: a budget cut carries no frontier keys and
// no round-cut INFO line.
func TestRoundsCutFrontierSilentWithoutRoundCut(t *testing.T) {
	t.Parallel()

	budget := runTruncationPass(t, expandingImpactLoader(), 3)
	for key := range budget.result.SubSignals {
		if strings.HasPrefix(key, "rounds_cut_") {
			t.Fatalf("budget-cut pass carries %q", key)
		}
	}
	if lines := budget.infoLines(t, "cut at the round cap"); len(lines) != 0 {
		t.Fatalf("budget-cut pass logged %d round-cut INFO lines", len(lines))
	}
}

// TestRoundsCutDiagnosticChangesNothing proves #11156 decision 9 is
// observability only: a pass cut at round 8 writes the same findings, the same
// partial flag, the same stamp and the same causes (the whole writer input)
// whether or not the diagnostic is read (no logger, no sub-signals), and the
// listed fields equal the bytes the code wrote before the diagnostic existed.
func TestRoundsCutDiagnosticChangesNothing(t *testing.T) {
	t.Parallel()

	with := runTruncationPass(t, expandingImpactLoader(), 0)
	quiet := runTruncationPassWithLogger(t, expandingImpactLoader(), nil)

	if got, want := roundsCutWriteJSON(t, quiet.writer.write), roundsCutWriteJSON(t, with.writer.write); !bytes.Equal(got, want) {
		t.Fatalf("writer input differs with the diagnostic off:\n%s\nvs\n%s", got, want)
	}
	// The whole writer input, not the listed subset: any field a future change
	// lets the diagnostic reach fails here.
	if !reflect.DeepEqual(quiet.writer.write, with.writer.write) {
		t.Fatalf("writer input differs with the diagnostic off:\n%+v\nvs\n%+v", quiet.writer.write, with.writer.write)
	}
	stripped := map[string]float64{}
	for key, value := range with.result.SubSignals {
		if !strings.HasPrefix(key, "rounds_cut_") {
			stripped[key] = value
		}
	}
	for key, value := range quiet.result.SubSignals {
		if strings.HasPrefix(key, "rounds_cut_") {
			continue
		}
		if stripped[key] != value {
			t.Fatalf("sub-signal %q = %v with the diagnostic, %v without", key, stripped[key], value)
		}
	}

	got := roundsCutWriteJSON(t, with.writer.write)
	golden, err := os.ReadFile(filepath.FromSlash(roundsCutGoldenPath))
	if err != nil {
		if os.Getenv("ESHU_WRITE_ROUNDS_CUT_GOLDEN") != "1" {
			t.Fatalf("read golden: %v", err)
		}
		if err := os.WriteFile(filepath.FromSlash(roundsCutGoldenPath), got, 0o600); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		return
	}
	if !bytes.Equal(golden, got) {
		t.Fatalf("writer input drifted from the pre-diagnostic golden:\n%s\nvs\n%s", golden, got)
	}
}

// runTruncationPassWithLogger is runTruncationPass with a caller-chosen logger
// and no instruments, to run the same seeded pass with the diagnostic unread.
func runTruncationPassWithLogger(t *testing.T, loader *stubSupplyChainImpactFactLoader, logger *slog.Logger) truncationHarness {
	t.Helper()
	writer := &recordingSupplyChainImpactWriter{}
	handler := SupplyChainImpactHandler{
		FencingTokenIssuer: newTestImpactFencingTokenIssuer(),
		FactLoader:         loader,
		Writer:             writer,
		Logger:             logger,
	}
	result, err := handler.Handle(context.Background(), reducercontract.Intent{
		IntentID:     "intent-7154",
		ScopeID:      "vuln-intel://osv/npm/example",
		GenerationID: "generation-7154",
		SourceSystem: "vulnerability_intelligence",
		Domain:       reducercontract.DomainSupplyChainImpact,
		Cause:        "vulnerability evidence observed",
	})
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	return truncationHarness{writer: writer, result: result, logs: &bytes.Buffer{}}
}

// TestRoundsCutFrontierArmsCoverTheFilter keeps the closed arm list equal to
// the follow-up filter: every list-bearing field of SupplyChainImpactFactFilter
// reads into exactly one named arm, so an arm added to the filter without a
// name here fails instead of hiding from the operator.
func TestRoundsCutFrontierArmsCoverTheFilter(t *testing.T) {
	t.Parallel()

	armOf := map[string]string{
		"PackageIDs":          "package_ids",
		"SeedPackageIDs":      "seed_package_ids",
		"PURLs":               "purls",
		"SeedPURLs":           "seed_purls",
		"CVEIDs":              "cve_ids",
		"UpstreamCVEIDs":      "upstream_cve_ids",
		"AliasIDs":            "alias_ids",
		"AdvisoryIDs":         "advisory_ids",
		"SubjectDigests":      "subject_digests",
		"DocumentIDs":         "document_ids",
		"ProductCriteria":     "product_criteria",
		"SeedProductCriteria": "seed_product_criteria",
		"RepositoryIDs":       "repository_ids",
		"FileRepositoryIDs":   "file_repository_ids",
		"ImageRefs":           "image_refs",
	}
	typ := reflect.TypeOf(SupplyChainImpactFactFilter{})
	seen := map[string]bool{}
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if field.Type != reflect.TypeOf([]string(nil)) {
			continue
		}
		arm, ok := armOf[field.Name]
		if !ok {
			t.Fatalf("filter arm %s has no frontier arm name", field.Name)
		}
		// Fill only this arm; every other arm must read zero.
		var filter SupplyChainImpactFactFilter
		reflect.ValueOf(&filter).Elem().Field(i).Set(reflect.ValueOf(make([]string, 3)))
		sizes := frontierArmSizes(filter)
		for k, name := range roundsCutFrontierArms {
			want := 0
			if name == arm {
				want = 3
				seen[name] = true
			}
			if sizes[k] != want {
				t.Fatalf("arm %s: size[%s] = %d, want %d", field.Name, name, sizes[k], want)
			}
		}
	}
	if len(seen) != len(roundsCutFrontierArms) {
		t.Fatalf("filter reaches %d of %d frontier arms", len(seen), len(roundsCutFrontierArms))
	}
}

// TestRoundsCutFrontierSubSignalsNameEveryArm checks the sub-signal shape for a
// frontier with several open arms: one closed key per arm, the rounds and the
// envelope count, and nothing for a pass the round cap did not stop.
func TestRoundsCutFrontierSubSignalsNameEveryArm(t *testing.T) {
	t.Parallel()

	frontier := newRoundsCutFrontier(SupplyChainImpactFactFilter{
		PackageIDs:     []string{"p1", "p2"},
		CVEIDs:         []string{"c1"},
		RepositoryIDs:  []string{"r1", "r2", "r3"},
		SubjectDigests: []string{"d1"},
	}, 8, 41)
	signals := map[string]float64{}
	frontier.mergeSubSignals(signals)
	want := map[string]float64{
		"rounds_cut_rounds":                   8,
		"rounds_cut_envelopes":                41,
		"rounds_cut_frontier_package_ids":     2,
		"rounds_cut_frontier_cve_ids":         1,
		"rounds_cut_frontier_repository_ids":  3,
		"rounds_cut_frontier_subject_digests": 1,
	}
	if len(signals) != 2+len(roundsCutFrontierArms) {
		t.Fatalf("sub-signals = %d keys, want rounds, envelopes and %d arms: %v", len(signals), len(roundsCutFrontierArms), signals)
	}
	for key, value := range want {
		if signals[key] != value {
			t.Fatalf("%s = %v, want %v", key, signals[key], value)
		}
	}

	quiet := map[string]float64{}
	roundsCutFrontier{}.mergeSubSignals(quiet)
	if len(quiet) != 0 {
		t.Fatalf("a pass the round cap did not stop carries %v", quiet)
	}
}
