// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
)

type supplyChainImageIdentity struct {
	factID       string
	digest       string
	imageRef     string
	repositoryID string
	// sourceRepositoryIDs is the full, deliberately broader set of git source
	// repository ids the reducer_container_image_identity decision attributed
	// this image to -- build evidence AND weaker scope/deploy references alike
	// (container_image_identity.go ContainerImageIdentityDecision.SourceRepositoryIDs).
	sourceRepositoryIDs []string
	// buildProvenanceRepositoryIDs is the strong-evidence-only subset: an OCI
	// config source label, a CI run, or verified SLSA provenance (never a mere
	// deploy/scope reference). singleSupplyChainImageSourceRepositoryID ranks
	// this ahead of sourceRepositoryIDs (#5801) so a label-derived repository is
	// not blanked out as ambiguous merely because a weaker scope anchor also
	// names a different repository for the same image.
	buildProvenanceRepositoryIDs []string
	outcome                      string
	canonicalWrites              int
}

func supplyChainImageIdentityFromEnvelope(envelope facts.Envelope) supplyChainImageIdentity {
	return supplyChainImageIdentity{
		factID:   envelope.FactID,
		digest:   payloadStr(envelope.Payload, "digest"),
		imageRef: payloadStr(envelope.Payload, "image_ref"),
		// repositoryID is the OCI/container-registry's OWN repository
		// identifier (e.g. "oci-registry://ghcr.io/org/repo") — see
		// ociRepositoryID in container_image_identity_registry.go. It lives in
		// a disjoint namespace from every git-source Repository entity id
		// (always "repository:..."), so callers that need a repository a
		// workload/service/deployment-lane record can join against MUST use
		// sourceRepositoryIDs below, never this field (issue #5464 STEP 1).
		repositoryID: payloadStr(envelope.Payload, "repository_id"),
		// sourceRepositoryIDs are the git Repository entity ids
		// (decision.SourceRepositoryIDs, container_image_identity.go) the
		// identity decision attributed the image to. This set is deliberately
		// BROADER than genuine build evidence: it collects both an
		// OCI-config-label/CI-run/SLSA-provenance build attribution AND a
		// repository whose Kubernetes/deploy manifest merely REFERENCES the
		// same digest (container_image_identity.go
		// ContainerImageIdentityDecision.SourceRepositoryIDs). This is the
		// only field in this struct that can ever equal a
		// workload/service/deployment-lane repositoryID; the anchor rule
		// (singleSupplyChainImageSourceRepositoryID, #5801/#5813) resolves a
		// single row by preferring its narrower buildProvenanceRepositoryIDs
		// over this broader (possibly two-repository) set when the two
		// disagree, and preferSupplyChainImageIdentity's cross-row tier A >
		// tier B > tier C (#5813) never lets one row's build provenance alone
		// outrank multiple rows that already agree by sourceRepositoryIDs.
		sourceRepositoryIDs:          payloadOrderedStrings(envelope.Payload, "source_repository_ids"),
		buildProvenanceRepositoryIDs: payloadOrderedStrings(envelope.Payload, "build_provenance_repository_ids"),
		outcome:                      payloadStr(envelope.Payload, "outcome"),
		canonicalWrites:              supplyChainInt(envelope.Payload, "canonical_writes"),
	}
}

// singleSupplyChainImageSourceRepositoryID returns image's sole git source
// repository id, or "" when zero or more than one is present. #5464's
// os_package join treats an image identity as an anchor only when it names
// exactly one repository: an image built from (or attributed to) more than
// one source repository cannot be attributed to a single one without
// guessing, matching the #5463 "never invent an anchor" discipline.
//
// buildProvenanceRepositoryIDs (strong evidence only: an OCI config source
// label, a CI run, or verified SLSA provenance) is checked FIRST and wins
// whenever it names exactly one repository -- even when the broader
// sourceRepositoryIDs also carries a different, weaker scope/deploy
// reference. Deduping matchOCIConfigSourceRepository (#5801) made the label
// tier reachable repo-wide, which routinely adds a label-derived repository
// to sourceRepositoryIDs alongside an unrelated repository that merely
// deploys or references the same image; treating that disagreement as
// ambiguity would blank out RepositoryID (and all downstream
// workload/service/environment context) even though the label unambiguously
// named the image's build repository. A label is stronger evidence than a
// scope anchor, so it ranks first. Only when buildProvenanceRepositoryIDs is
// empty or itself ambiguous does the broader sourceRepositoryIDs check apply,
// preserving the original behavior for images with no build evidence. Two
// DISTINCT repositories that are both genuine build evidence (or both
// distinct scope anchors) still resolve to neither: the writer already
// de-duplicates both fields (containerImageIdentityPayload,
// container_image_identity_writer.go), so a length check alone is sufficient
// at each tier.
//
// This is a ROW-LEVEL resolution only: it says nothing about how many
// independent writers agree. A row can satisfy this function by having a
// single build-provenance entry with no other row corroborating it at all.
// See supplyChainImageIdentityAnchorTier's doc comment below for how that
// interacts with the CROSS-ROW winner vote, including the accepted
// limitation where a lone weak deploy-only row can outrank a build-attested
// row.
func singleSupplyChainImageSourceRepositoryID(image supplyChainImageIdentity) string {
	if repositoryID := singleSupplyChainRepositoryID(image.buildProvenanceRepositoryIDs); repositoryID != "" {
		return repositoryID
	}
	return singleSupplyChainRepositoryID(image.sourceRepositoryIDs)
}

// singleSupplyChainRepositoryID returns the sole entry of repositoryIDs, or ""
// when it is empty or carries more than one entry.
func singleSupplyChainRepositoryID(repositoryIDs []string) string {
	if len(repositoryIDs) != 1 {
		return ""
	}
	return repositoryIDs[0]
}

// preferSupplyChainImageIdentity picks a deterministic winner between two
// reducer_container_image_identity rows observed for the SAME digest (issue
// #5464 layer 2). The producer writes one canonical decision fact per
// triggering scope/ref with no per-digest canonicalization, so one digest can
// carry many rows -- this corpus alone has 16 for one digest -- and unlike
// the scannerAnalyses last-write-wins case in supply_chain_impact_index_build.go
// (safe because a content-addressed digest is identical across every writer,
// so any winner is equally correct), these rows can DISAGREE on
// source_repository_ids: fifteen rows here carry exactly one repository, one
// carries two (ambiguous). An unconditional last-write-wins assignment would
// make the winning RepositoryID/workload/service/environment join outcome
// depend on envelope iteration order -- an accident, not a proof -- so a
// defined tie-break is REQUIRED here, unlike the scannerAnalyses case.
//
// Preference: supplyChainImageIdentityAnchorTier ranks each row into tier A
// (source-repository consensus), tier B (resolvable only via this row's own
// build provenance), or tier C (unresolvable), and a lower tier always beats
// a higher one -- see that function's doc for why tier A must never lose to
// tier B (#5813: the row-level provenance-first ranking
// singleSupplyChainImageSourceRepositoryID applies to ONE row must not also
// let that row win the CROSS-ROW vote merely for having build evidence, when
// other rows agree by source alone). Between two rows in the SAME tier, the
// lexicographically smaller factID wins -- arbitrary, and order-independent
// (folding the same set of rows in any order reaches the same minimum), but
// NOT stable run to run: factID is a SHA-256 over an identity that includes
// generation_id (facts.StableID, containerImageIdentityIdentity), and a row
// derived from a live git snapshot gets a fresh generation_id every
// collector run (git_source_processing.go's sourceRunID embeds the run's
// wall-clock observed_at). Two same-tier rows that disagree on repository
// can therefore pick a DIFFERENT winner on every run even though nothing
// about the underlying evidence changed -- see issue #5887, where this
// bare function's tie-break was exactly that failure mode.
//
// preferSupplyChainImageIdentityConsensus (supply_chain_impact_anchor_consensus.go)
// is the run-stable replacement bestSupplyChainImageIdentitiesByDigest and
// addSupplyChainImpactIndexEntry actually use: it defers to this function
// for the tier check and for any same-repository or unresolved-repository
// comparison (behavior identical to here), but breaks a same-tier,
// different-repository disagreement by corroboration count instead of
// factID. This bare function is kept for direct row-pair tier tests and as
// the final fallback once repository identity and corroboration count both
// tie -- at that point which specific row wins cannot change the resolved
// repository, so factID's per-run instability no longer matters.
func preferSupplyChainImageIdentity(existing, candidate supplyChainImageIdentity) supplyChainImageIdentity {
	existingTier := supplyChainImageIdentityAnchorTier(existing)
	candidateTier := supplyChainImageIdentityAnchorTier(candidate)
	if existingTier != candidateTier {
		if candidateTier < existingTier {
			return candidate
		}
		return existing
	}
	if candidate.factID < existing.factID {
		return candidate
	}
	return existing
}

// supplyChainImageIdentityAnchorTier ranks how confidently a single
// reducer_container_image_identity row anchors a repository, for
// preferSupplyChainImageIdentity's cross-row winner selection (#5813, a
// follow-up to #5801's row-level provenance-first ranking in
// singleSupplyChainImageSourceRepositoryID). Three tiers, checked in this
// strict order:
//
//   - Tier A (0): singleSupplyChainRepositoryID(row.sourceRepositoryIDs)
//     alone is non-empty -- the row's full, broader source-repository set
//     already names exactly one repository. IMPORTANT: this means "this row
//     alone resolves unambiguously by source_repository_ids", NOT multi-writer
//     consensus -- despite the "cross-evidence consensus" framing below, tier
//     A does not require corroboration from more than one writer. A single
//     row whose sourceRepositoryIDs happens to carry exactly one entry
//     qualifies for tier A even when that entry is nothing more than a lone,
//     uncorroborated deploy/scope reference (the weakest evidence kind
//     sourceRepositoryIDs can hold). In the corpus shape this most often
//     represents, multiple writers HAVE contributed to sourceRepositoryIDs
//     for the winning row and they agree; but the tier check itself cannot
//     tell that shape apart from a single weak reference, and does not try
//     to.
//   - Tier B (1): sourceRepositoryIDs alone is ambiguous or empty, but
//     singleSupplyChainImageSourceRepositoryID(row) still resolves because
//     buildProvenanceRepositoryIDs names exactly one repository. This
//     recovers a row whose broader sourceRepositoryIDs disagrees (its own
//     build repository plus an unrelated deploy/scope reference) via that
//     row's stronger internal evidence, but it is NOT the same strength as
//     tier A: tier A is multiple pieces of evidence already agreeing, tier B
//     is one row breaking its own internal tie.
//   - Tier C (2): neither resolves -- this row cannot anchor anything.
//
// Tier A must never be displaced by tier B. #5813's golden-corpus regression
// was exactly that failure mode: persisting buildProvenanceRepositoryIDs and
// preferring it row-locally promoted a row that is ambiguous by
// sourceRepositoryIDs (naming both the true deploying repository and its own
// building repository) into the same "unambiguous" class as rows that
// genuinely agree, where it could then beat the fifteen agreeing rows purely
// because its factID happened to sort smaller. Tier B must still beat tier C,
// though, so an all-ambiguous digest whose one build-provenance-bearing row
// has a larger factID than an otherwise-unresolvable row does not blank the
// anchor entirely.
//
// ACCEPTED LIMITATION (tier A's lack of multi-writer corroboration): because
// tier A only checks that THIS row's own sourceRepositoryIDs is a singleton,
// a lone row carrying nothing but a single weak deploy reference (tier A)
// outranks a row with genuine build provenance whose own sourceRepositoryIDs
// also carries a second, stale reference (tier B) -- even though the build
// row's evidence is individually stronger. This is a PRE-EXISTING limitation,
// not one this tier fix introduces: pre-#5813 (and pre-#5801)
// `singleSupplyChainImageSourceRepositoryID` used the equivalent of
// `len(sourceRepositoryIDs) == 1` as its sole "unambiguous" criterion, which
// produces the identical outcome for that shape. The tier fix deliberately
// preserves it, because tier B must never displace tier A: the B-12 golden
// pin (fifteen independently agreeing deploy rows beating one row that is
// ambiguous by sourceRepositoryIDs but resolvable via its own build
// provenance) depends on tier A's priority holding even when tier A's own
// evidence is thin. See
// TestPreferSupplyChainImageIdentityAcceptedLimitationLoneDeployRowBeatsBuildProvenanceRow
// in supply_chain_impact_index_build_test.go for the pinned regression.
// Changing tier A to require multi-writer corroboration (rather than mere
// singleton-ness of sourceRepositoryIDs) is a deliberate semantic decision
// that needs owner sign-off before it changes behavior here -- it is not an
// incidental refactor.
func supplyChainImageIdentityAnchorTier(row supplyChainImageIdentity) int {
	const (
		supplyChainImageIdentityAnchorTierSourceConsensus = 0
		supplyChainImageIdentityAnchorTierBuildProvenance = 1
		supplyChainImageIdentityAnchorTierUnresolved      = 2
	)
	if singleSupplyChainRepositoryID(row.sourceRepositoryIDs) != "" {
		return supplyChainImageIdentityAnchorTierSourceConsensus
	}
	if singleSupplyChainImageSourceRepositoryID(row) != "" {
		return supplyChainImageIdentityAnchorTierBuildProvenance
	}
	return supplyChainImageIdentityAnchorTierUnresolved
}

// bestSupplyChainImageIdentitiesByDigest folds every
// reducer_container_image_identity envelope in envelopes into one
// deterministic winner per digest via preferSupplyChainImageIdentityConsensus
// (#5887; see that function's doc for why the bare factID tie-break alone is
// not run-stable). Exposed as its own batch helper (rather than inlined into
// addSupplyChainImpactIndexEntry's per-envelope case in
// supply_chain_impact_index_build.go) so any future consumer that needs the
// same digest-to-repository resolution over a whole envelope batch can reuse
// this exact tie-break instead of re-deriving it.
func bestSupplyChainImageIdentitiesByDigest(envelopes []facts.Envelope) map[string]supplyChainImageIdentity {
	consensus := buildSupplyChainImageIdentityConsensus(envelopes)
	winners := make(map[string]supplyChainImageIdentity)
	for _, envelope := range envelopes {
		if envelope.FactKind != containerImageIdentityFactKind {
			continue
		}
		image := supplyChainImageIdentityFromEnvelope(envelope)
		if image.digest == "" {
			continue
		}
		if existing, ok := winners[image.digest]; ok {
			image = preferSupplyChainImageIdentityConsensus(existing, image, consensus)
		}
		winners[image.digest] = image
	}
	return winners
}
