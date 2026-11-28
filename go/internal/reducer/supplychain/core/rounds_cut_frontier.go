// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"log/slog"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// roundsCutFrontierArms names the arms of the until-stable follow-up filter,
// in the order frontierArmSizes lists them. The set is closed on purpose: each
// name is a sub-signal key and a log key, never a value an id could reach. A
// new arm on SupplyChainImpactFactFilter needs a name here (the arm test fails
// until it has one).
var roundsCutFrontierArms = [...]string{
	"package_ids",
	"seed_package_ids",
	"purls",
	"seed_purls",
	"cve_ids",
	"upstream_cve_ids",
	"alias_ids",
	"advisory_ids",
	"subject_digests",
	"document_ids",
	"product_criteria",
	"seed_product_criteria",
	"repository_ids",
	"file_repository_ids",
	"image_refs",
}

// frontierArmSizes counts the values each arm of a follow-up filter still
// holds, in roundsCutFrontierArms order.
func frontierArmSizes(f SupplyChainImpactFactFilter) [len(roundsCutFrontierArms)]int {
	return [len(roundsCutFrontierArms)]int{
		len(f.PackageIDs),
		len(f.SeedPackageIDs),
		len(f.PURLs),
		len(f.SeedPURLs),
		len(f.CVEIDs),
		len(f.UpstreamCVEIDs),
		len(f.AliasIDs),
		len(f.AdvisoryIDs),
		len(f.SubjectDigests),
		len(f.DocumentIDs),
		len(f.ProductCriteria),
		len(f.SeedProductCriteria),
		len(f.RepositoryIDs),
		len(f.FileRepositoryIDs),
		len(f.ImageRefs),
	}
}

// roundsCutFrontier records what the until-stable loop had not yet asked for
// when the round cap stopped it (#11156): the per-arm size of the follow-up
// filter that was still non-empty, the round cap, and the envelopes the pass
// held at the cut. It only observes. Nothing reads it to decide what the pass
// loads, writes or retracts. The zero value means the round cap did not stop
// the pass.
type roundsCutFrontier struct {
	rounds    int
	envelopes int
	sizes     [len(roundsCutFrontierArms)]int
}

// newRoundsCutFrontier captures the frontier at the cut: next is the
// follow-up filter after the provenance bound and the scope restrict, so it is
// exactly what the next round would have requested.
func newRoundsCutFrontier(next SupplyChainImpactFactFilter, rounds, envelopes int) roundsCutFrontier {
	return roundsCutFrontier{rounds: rounds, envelopes: envelopes, sizes: frontierArmSizes(next)}
}

// cut reports whether the round cap stopped the pass.
func (c roundsCutFrontier) cut() bool {
	return c.rounds > 0
}

// mergeSubSignals adds the round-cut accounting to the success line's
// sub-signals: rounds_cut_rounds, rounds_cut_envelopes and one
// rounds_cut_frontier_<arm> per arm, zeros included so an operator reads the
// whole frontier. A pass the round cap did not stop adds nothing.
func (c roundsCutFrontier) mergeSubSignals(subSignals map[string]float64) {
	if !c.cut() {
		return
	}
	subSignals["rounds_cut_rounds"] = float64(c.rounds)
	subSignals["rounds_cut_envelopes"] = float64(c.envelopes)
	for i, arm := range roundsCutFrontierArms {
		subSignals["rounds_cut_frontier_"+arm] = float64(c.sizes[i])
	}
}

// emitRoundsCutFrontier writes the INFO line that names the arms still open at
// the round cap (#11156), so an operator can see which arm keeps the walk going
// and how large the frontier is. It follows the WARN line the truncation
// counter already carries and adds no instrument: the closed arm names are log
// and sub-signal keys, and no id is a label. scope_id and generation_id are
// log fields only, like the WARN line's.
func (h SupplyChainImpactHandler) emitRoundsCutFrontier(
	ctx context.Context,
	intent reducercontract.Intent,
	loaded supplyChainImpactLoadedEvidence,
) {
	frontier := loaded.truncation.roundsCut
	if h.Logger == nil || !frontier.cut() {
		return
	}
	attrs := []any{
		slog.String("domain", string(reducercontract.DomainSupplyChainImpact)),
		slog.String("scope_id", intent.ScopeID),
		slog.String("generation_id", intent.GenerationID),
		slog.String("intent_id", intent.IntentID),
		slog.String("pass_mode", supplyChainImpactPassMode(intent, loaded)),
		slog.String("cause", telemetry.SupplyChainImpactPartialCauseRounds),
		slog.Int("rounds", frontier.rounds),
		slog.Int("cut_envelopes", frontier.envelopes),
	}
	for i, arm := range roundsCutFrontierArms {
		attrs = append(attrs, slog.Int("frontier_"+arm, frontier.sizes[i]))
	}
	h.Logger.InfoContext(ctx, "supply chain impact evidence expansion cut at the round cap", attrs...)
}
