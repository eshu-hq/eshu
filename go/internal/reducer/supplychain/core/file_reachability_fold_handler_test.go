// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"math"
	"reflect"
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
)

// foldingStatsLoader is the production store's contract in miniature: when the
// filter carries a fold, file envelopes go into the fold and are not returned.
type foldingStatsLoader struct {
	stubSupplyChainImpactFactLoader
	sawFold       bool
	foldedBytes   int64
	retainedBytes int64
}

func (l *foldingStatsLoader) ListActiveSupplyChainImpactFactsWithStats(
	ctx context.Context,
	filter SupplyChainImpactFactFilter,
) ([]facts.Envelope, SupplyChainImpactActiveEvidenceLoadStats, error) {
	active, truncated, err := l.ListActiveSupplyChainImpactFacts(ctx, filter)
	stats := SupplyChainImpactActiveEvidenceLoadStats{SuppressionTruncated: truncated, RetainedPayloadBytes: l.retainedBytes}
	if filter.FileFold == nil {
		return active, stats, err
	}
	l.sawFold = true
	kept := make([]facts.Envelope, 0, len(active))
	for _, envelope := range active {
		if envelope.FactKind != factload.FactKindFile {
			kept = append(kept, envelope)
			continue
		}
		if filter.FileFold.Add(envelope) {
			stats.FoldedFiles++
			stats.FoldedPayloadBytes += l.foldedBytes
		}
	}
	return kept, stats, err
}

func foldHandlerScopeFacts() []facts.Envelope {
	return []facts.Envelope{
		vulnerabilityCVEFact("cve-js-reach", "CVE-2026-118501", 8.8),
		vulnerabilityAffectedPackageFact("affected-js-reach", "CVE-2026-118501", "pkg:npm/@scope/vulnerable-api", "npm", "@scope/vulnerable-api", "1.2.3", "1.3.0"),
		packageConsumptionFactWithChain("consume-js-reach", "pkg:npm/@scope/vulnerable-api", testImpactRepositoryID, "1.2.3", []string{"@scope/vulnerable-api"}, 1, true),
	}
}

func foldHandlerFiles() []facts.Envelope {
	return []facts.Envelope{
		jsTSPackageAPIFileFact("file-a", testImpactRepositoryID, "src/server.ts", "typescript",
			[]map[string]any{{"name": "default", "alias": "vulnerableAPI", "source": "@scope/vulnerable-api"}},
			[]map[string]any{{"name": "vulnerableAPI", "full_name": "vulnerableAPI.createServer"}}, nil),
		jsTSPackageAPIFileFact("file-b", testImpactRepositoryID, "src/other.ts", "typescript",
			[]map[string]any{{"name": "other", "source": "lodash"}}, nil, nil),
	}
}

func runFoldHandler(t *testing.T, loader factload.FactLoader, writer *recordingSupplyChainImpactWriter, budgetBytes int64) reducercontract.Result {
	t.Helper()
	return runFoldHandlerWithBudgets(t, loader, writer, 0, budgetBytes)
}

func runFoldHandlerWithBudgets(
	t *testing.T,
	loader factload.FactLoader,
	writer *recordingSupplyChainImpactWriter,
	envelopeBudget int,
	budgetBytes int64,
) reducercontract.Result {
	t.Helper()
	handler := SupplyChainImpactHandler{
		FencingTokenIssuer:        newTestImpactFencingTokenIssuer(),
		FactLoader:                loader,
		Writer:                    writer,
		EvidenceBudget:            envelopeBudget,
		EvidencePayloadBytesLimit: budgetBytes,
	}
	result, err := handler.Handle(context.Background(), reducercontract.Intent{
		IntentID: "intent-9781", ScopeID: "vuln-intel://osv/npm/_batch", GenerationID: "generation-9781",
		SourceSystem: "vulnerability_intelligence", Domain: reducercontract.DomainSupplyChainImpact,
		Cause: "vulnerability evidence observed",
	})
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	return result
}

// TestSupplyChainImpactHandlerFoldsActiveEvidenceFilesWithoutChangingFindings
// is the #9781 end-to-end parity: the same facts produce the same findings
// whether the loader returns the file envelopes or folds them, and a folding
// loader sees a fold on the until-stable load.
func TestSupplyChainImpactHandlerFoldsActiveEvidenceFilesWithoutChangingFindings(t *testing.T) {
	t.Parallel()

	plain := &stubSupplyChainImpactFactLoader{scopeFacts: foldHandlerScopeFacts(), active: foldHandlerFiles()}
	plainWriter := &recordingSupplyChainImpactWriter{}
	plainRunResult := runFoldHandler(t, plain, plainWriter, 0)

	folding := &foldingStatsLoader{stubSupplyChainImpactFactLoader: stubSupplyChainImpactFactLoader{
		scopeFacts: foldHandlerScopeFacts(), active: foldHandlerFiles(),
	}, foldedBytes: 1000}
	foldingWriter := &recordingSupplyChainImpactWriter{}
	result := runFoldHandler(t, folding, foldingWriter, 0)

	if !folding.sawFold {
		t.Fatal("loader never saw a FileFold, want one on the until-stable active-evidence load")
	}
	if len(plainWriter.write.Findings) == 0 {
		t.Fatal("plain run wrote no findings, want the JS/TS reachability finding")
	}
	if !reflect.DeepEqual(foldingWriter.write.Findings, plainWriter.write.Findings) {
		t.Fatalf("findings differ:\n folded %#v\n plain  %#v", foldingWriter.write.Findings, plainWriter.write.Findings)
	}
	if plainResult, foldedResult := plainRunResult.SubSignals["evidence_envelopes"], result.SubSignals["evidence_envelopes"]; plainResult != foldedResult {
		t.Fatalf("SubSignals[evidence_envelopes] = %v folded, %v plain, want the fold to count its files as envelopes", foldedResult, plainResult)
	}
	if got := result.SubSignals["active_evidence_files_folded"]; got < 2 {
		t.Fatalf("SubSignals[active_evidence_files_folded] = %v, want at least the 2 folded files", got)
	}
	if got := result.SubSignals["active_evidence_payload_bytes_folded"]; got < 2000 {
		t.Fatalf("SubSignals[active_evidence_payload_bytes_folded] = %v, want at least 2000", got)
	}
}

// TestSupplyChainImpactHandlerKeepsFilesWhenLoaderCannotFold pins the
// fallback: a loader without the fold seam returns file envelopes and the pass
// reads them as before.
func TestSupplyChainImpactHandlerKeepsFilesWhenLoaderCannotFold(t *testing.T) {
	t.Parallel()

	plain := &stubSupplyChainImpactFactLoader{scopeFacts: foldHandlerScopeFacts(), active: foldHandlerFiles()}
	writer := &recordingSupplyChainImpactWriter{}
	runFoldHandler(t, plain, writer, 0)
	if len(plain.filters) == 0 || plain.filters[0].FileFold == nil {
		t.Fatalf("filters = %#v, want the until-stable filter to carry a fold even for a loader that ignores it", plain.filters)
	}
	if len(writer.write.Findings) == 0 || writer.write.Findings[0].RuntimeReachability != jsTSPackageAPICallEvidence {
		t.Fatalf("findings = %#v, want JS/TS call evidence from the returned file envelopes", writer.write.Findings)
	}
}

// TestSupplyChainImpactEvidenceBudgetChargesRetainedPayloadBytes pins the
// byte-aware budget: a load that retains more payload bytes than the limit
// marks the pass partial under the existing evidence_budget cause, and folded
// bytes are never charged because the fold does not keep them.
func TestSupplyChainImpactEvidenceBudgetChargesRetainedPayloadBytes(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name         string
		retained     int64
		folded       int64
		limit        int64
		wantPartial  float64
		wantRatioMin float64
	}{
		{name: "retained bytes over the limit", retained: 3000, limit: 2000, wantPartial: 1, wantRatioMin: 1.4},
		{name: "retained bytes under the limit", retained: 1000, limit: 2000, wantPartial: 0, wantRatioMin: 0.4},
		{name: "retained bytes exactly at the limit", retained: 2000, limit: 2000, wantPartial: 0, wantRatioMin: 1},
		{name: "folded bytes are not charged", retained: 0, folded: 1 << 30, limit: 2000, wantPartial: 0, wantRatioMin: 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			loader := &foldingStatsLoader{
				stubSupplyChainImpactFactLoader: stubSupplyChainImpactFactLoader{scopeFacts: foldHandlerScopeFacts(), active: foldHandlerFiles()},
				foldedBytes:                     test.folded, retainedBytes: test.retained,
			}
			result := runFoldHandler(t, loader, &recordingSupplyChainImpactWriter{}, test.limit)
			if got := result.SubSignals["evidence_partial"]; got != test.wantPartial {
				t.Fatalf("SubSignals[evidence_partial] = %v, want %v", got, test.wantPartial)
			}
			if got := result.SubSignals["evidence_payload_bytes_ratio"]; math.IsNaN(got) || got < test.wantRatioMin {
				t.Fatalf("SubSignals[evidence_payload_bytes_ratio] = %v, want at least %v", got, test.wantRatioMin)
			}
		})
	}
}

// TestSupplyChainImpactFoldedFilesStopCountingAgainstTheEnvelopeBudget pins the
// disclosed #9781 behavior change: a scope that went partial only because its
// file facts pushed the envelope count over the cap now completes, because
// folded files are not kept. The findings are the same either way.
func TestSupplyChainImpactFoldedFilesStopCountingAgainstTheEnvelopeBudget(t *testing.T) {
	t.Parallel()

	const envelopeBudget = 1 // two file facts exceed it; folding charges none
	plain := &stubSupplyChainImpactFactLoader{scopeFacts: foldHandlerScopeFacts(), active: foldHandlerFiles()}
	plainWriter := &recordingSupplyChainImpactWriter{}
	plainResult := runFoldHandlerWithBudgets(t, plain, plainWriter, envelopeBudget, 0)

	folding := &foldingStatsLoader{stubSupplyChainImpactFactLoader: stubSupplyChainImpactFactLoader{
		scopeFacts: foldHandlerScopeFacts(), active: foldHandlerFiles(),
	}}
	foldingWriter := &recordingSupplyChainImpactWriter{}
	foldingResult := runFoldHandlerWithBudgets(t, folding, foldingWriter, envelopeBudget, 0)

	if got := plainResult.SubSignals["evidence_partial"]; got != 1 {
		t.Fatalf("plain run evidence_partial = %v, want 1 (file envelopes over the cap)", got)
	}
	if got := foldingResult.SubSignals["evidence_partial"]; got != 0 {
		t.Fatalf("folding run evidence_partial = %v, want 0 (folded files are not charged)", got)
	}
	const limitNote = "active supply-chain evidence expansion limit reached"
	if len(plainWriter.write.Findings) != 1 || len(foldingWriter.write.Findings) != 1 {
		t.Fatalf("findings = %d plain, %d folded, want one each", len(plainWriter.write.Findings), len(foldingWriter.write.Findings))
	}
	plainFinding, foldedFinding := plainWriter.write.Findings[0], foldingWriter.write.Findings[0]
	if !slices.Contains(plainFinding.MissingEvidence, limitNote) || slices.Contains(foldedFinding.MissingEvidence, limitNote) {
		t.Fatalf("limit note: plain %v, folded %v, want it on the partial pass only", plainFinding.MissingEvidence, foldedFinding.MissingEvidence)
	}
	// The verdict is the same; only the partial-pass note differs.
	for _, check := range []struct {
		name         string
		plain, fixed any
	}{
		{"Status", plainFinding.Status, foldedFinding.Status},
		{"Confidence", plainFinding.Confidence, foldedFinding.Confidence},
		{"RuntimeReachability", plainFinding.RuntimeReachability, foldedFinding.RuntimeReachability},
		{"PriorityScore", plainFinding.PriorityScore, foldedFinding.PriorityScore},
		{"EvidenceFactIDs", plainFinding.EvidenceFactIDs, foldedFinding.EvidenceFactIDs},
		{"RepositoryID", plainFinding.RepositoryID, foldedFinding.RepositoryID},
	} {
		if !reflect.DeepEqual(check.plain, check.fixed) {
			t.Fatalf("%s differs: plain %v, folded %v", check.name, check.plain, check.fixed)
		}
	}
}

// TestSupplyChainImpactHandlerBoundsEachLoadByTheRemainingByteBudget pins that
// the byte budget is handed to the loader, so the first round is bounded page
// by page during the load and not only after it returns (#9781).
func TestSupplyChainImpactHandlerBoundsEachLoadByTheRemainingByteBudget(t *testing.T) {
	t.Parallel()

	loader := &foldingStatsLoader{
		stubSupplyChainImpactFactLoader: stubSupplyChainImpactFactLoader{scopeFacts: foldHandlerScopeFacts(), active: foldHandlerFiles()},
	}
	runFoldHandler(t, loader, &recordingSupplyChainImpactWriter{}, 5000)
	if len(loader.filters) == 0 {
		t.Fatal("loader saw no active-evidence filter")
	}
	for i, filter := range loader.filters {
		if filter.PayloadBytesLimit <= 0 || filter.PayloadBytesLimit > 5000 {
			t.Fatalf("filters[%d].PayloadBytesLimit = %d, want what is left of the 5000-byte budget", i, filter.PayloadBytesLimit)
		}
	}

	defaulted := &foldingStatsLoader{
		stubSupplyChainImpactFactLoader: stubSupplyChainImpactFactLoader{scopeFacts: foldHandlerScopeFacts(), active: foldHandlerFiles()},
	}
	runFoldHandler(t, defaulted, &recordingSupplyChainImpactWriter{}, 0)
	if len(defaulted.filters) == 0 || defaulted.filters[0].PayloadBytesLimit != maxSupplyChainImpactEvidencePayloadBytesPerIntent {
		t.Fatalf("first-round PayloadBytesLimit = %v, want the package default %d", defaulted.filters, maxSupplyChainImpactEvidencePayloadBytesPerIntent)
	}
}

// TestSupplyChainImpactHandlerSpentByteBudgetNeverReadsAsUnlimited pins the
// edge the loader depends on: when earlier rounds retained exactly the byte
// limit the pass is not over budget and may load again, but that load must be
// bounded to one page. A remaining budget of zero would reach the loader as
// "no limit" (#9855).
func TestSupplyChainImpactHandlerSpentByteBudgetNeverReadsAsUnlimited(t *testing.T) {
	t.Parallel()

	const limit = 4000
	for _, test := range []struct {
		name string
		used int64
		want int64
	}{
		{name: "nothing spent", used: 0, want: limit},
		{name: "partly spent", used: 1000, want: limit - 1000},
		{name: "exactly spent", used: limit, want: 1},
		{name: "over spent", used: limit + 500, want: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			loader := &foldingStatsLoader{stubSupplyChainImpactFactLoader: stubSupplyChainImpactFactLoader{
				scopeFacts: foldHandlerScopeFacts(), active: foldHandlerFiles(),
			}}
			handler := SupplyChainImpactHandler{FactLoader: loader}
			budget := newSupplyChainImpactEvidenceBudget(0)
			budget.bytesLimit, budget.bytesUsed = limit, test.used
			if _, _, err := handler.loadActiveSupplyChainImpactFacts(
				context.Background(), SupplyChainImpactFactFilter{CVEIDs: []string{"CVE-2026-118501"}}, budget,
			); err != nil {
				t.Fatalf("loadActiveSupplyChainImpactFacts() error = %v", err)
			}
			if len(loader.filters) != 1 {
				t.Fatalf("loader saw %d filters, want 1", len(loader.filters))
			}
			if got := loader.filters[0].PayloadBytesLimit; got != test.want {
				t.Fatalf("PayloadBytesLimit = %d, want %d (0 would mean unlimited)", got, test.want)
			}
		})
	}
}

// TestSupplyChainImpactHandlerPlacesTheDigestAfterThePassFiles pins where the
// digest goes. The JS/TS reader keeps the first usage of the best evidence
// priority, so files the pass already held (here the scope load's file) must
// reach the builder before the folded ones, as they would without the fold
// (#9855).
func TestSupplyChainImpactHandlerPlacesTheDigestAfterThePassFiles(t *testing.T) {
	t.Parallel()

	files := foldHandlerFiles()
	// Two files cite the same package with equal evidence priority, so the
	// earlier one in pass order must win the finding's evidence.
	early := jsTSPackageAPIFileFact("file-early", testImpactRepositoryID, "src/early.ts", "typescript",
		[]map[string]any{{"name": "default", "alias": "vulnerableAPI", "source": "@scope/vulnerable-api"}},
		[]map[string]any{{"name": "vulnerableAPI", "full_name": "vulnerableAPI.createServer"}}, nil)
	scope := append(foldHandlerScopeFacts(), early)

	plain := &stubSupplyChainImpactFactLoader{scopeFacts: scope, active: files}
	plainWriter := &recordingSupplyChainImpactWriter{}
	runFoldHandler(t, plain, plainWriter, 0)

	folding := &foldingStatsLoader{stubSupplyChainImpactFactLoader: stubSupplyChainImpactFactLoader{scopeFacts: scope, active: files}}
	foldingWriter := &recordingSupplyChainImpactWriter{}
	runFoldHandler(t, folding, foldingWriter, 0)

	if len(plainWriter.write.Findings) == 0 || len(foldingWriter.write.Findings) == 0 {
		t.Fatal("no findings written, want the JS/TS reachability finding on both runs")
	}
	plainIDs := plainWriter.write.Findings[0].EvidenceFactIDs
	foldedIDs := foldingWriter.write.Findings[0].EvidenceFactIDs
	if !slices.Contains(plainIDs, "file-early") {
		t.Fatalf("plain evidence = %v, want the earlier file to win the tie", plainIDs)
	}
	if !reflect.DeepEqual(plainIDs, foldedIDs) {
		t.Fatalf("evidence ids differ: plain %v, folded %v (digest placed before the pass's earlier files?)", plainIDs, foldedIDs)
	}
}
