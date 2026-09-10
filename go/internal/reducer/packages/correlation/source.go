// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package correlation

import (
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/packages/source"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
)

// PackageSourceOutcome names the reducer decision for one package
// registry source hint. Exact and derived outcomes are still candidates; they
// do not authorize package ownership graph writes without stronger build or
// release provenance.
type PackageSourceOutcome string

const (
	// PackageSourceExact means the hint URL exactly matched one
	// active repository remote URL.
	PackageSourceExact PackageSourceOutcome = "exact"
	// PackageSourceDerived means the hint URL matched one active
	// repository after git URL canonicalization such as SSH-to-HTTPS or .git
	// suffix removal.
	PackageSourceDerived PackageSourceOutcome = "derived"
	// PackageSourceAmbiguous means more than one active repository
	// matched the same source hint.
	PackageSourceAmbiguous PackageSourceOutcome = "ambiguous"
	// PackageSourceUnresolved means no repository matched the hint.
	PackageSourceUnresolved PackageSourceOutcome = "unresolved"
	// PackageSourceStale means the hint matched only tombstoned
	// repository facts.
	PackageSourceStale PackageSourceOutcome = "stale"
	// PackageSourceRejected means the hint cannot participate in
	// ownership correlation, such as homepage or generic project metadata.
	PackageSourceRejected PackageSourceOutcome = "rejected"
)

// PackageSourceDecision records the bounded package-source
// correlation result before any canonical package ownership materialization.
type PackageSourceDecision struct {
	PackageID              string
	VersionID              string
	HintKind               string
	SourceURL              string
	RepositoryID           string
	RepositoryName         string
	CandidateRepositoryIDs []string
	Outcome                PackageSourceOutcome
	Reason                 string
	ProvenanceOnly         bool
	CanonicalWrites        int
	EvidenceFactIDs        []string
}

// BuildPackageSourceDecisions classifies package registry
// source_hint facts against repository facts for one reducer input set.
func BuildPackageSourceDecisions(envelopes []facts.Envelope) []PackageSourceDecision {
	hints := extractPackageSourceHints(envelopes)
	repositories := extractPackageSourceRepositories(envelopes)
	decisions := make([]PackageSourceDecision, 0, len(hints))
	for _, hint := range hints {
		decisions = append(decisions, classifyPackageSourceHint(hint, repositories))
	}
	sort.SliceStable(decisions, func(i, j int) bool {
		if decisions[i].PackageID != decisions[j].PackageID {
			return decisions[i].PackageID < decisions[j].PackageID
		}
		if decisions[i].SourceURL != decisions[j].SourceURL {
			return decisions[i].SourceURL < decisions[j].SourceURL
		}
		return decisions[i].HintKind < decisions[j].HintKind
	})
	return decisions
}

func extractPackageSourceHints(envelopes []facts.Envelope) []packageSourceHint {
	hints := make([]packageSourceHint, 0)
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.PackageRegistrySourceHintFactKind {
			continue
		}
		sourceURL := payloadcore.FirstNonBlank(
			payloadcore.PayloadStr(envelope.Payload, "normalized_url"),
			payloadcore.PayloadStr(envelope.Payload, "raw_url"),
		)
		hints = append(hints, packageSourceHint{
			FactID:    envelope.FactID,
			PackageID: payloadcore.PayloadStr(envelope.Payload, "package_id"),
			VersionID: payloadcore.PayloadStr(envelope.Payload, "version_id"),
			HintKind:  strings.ToLower(payloadcore.PayloadStr(envelope.Payload, "hint_kind")),
			SourceURL: sourceURL,
		})
	}
	return hints
}

func classifyPackageSourceHint(
	hint packageSourceHint,
	repositories []packageSourceRepository,
) PackageSourceDecision {
	decision := PackageSourceDecision{
		PackageID:       hint.PackageID,
		VersionID:       hint.VersionID,
		HintKind:        hint.HintKind,
		SourceURL:       hint.SourceURL,
		ProvenanceOnly:  true,
		CanonicalWrites: 0,
		EvidenceFactIDs: compactStringSlice(hint.FactID),
	}
	if hint.PackageID == "" || hint.SourceURL == "" {
		decision.Outcome = PackageSourceRejected
		decision.Reason = "source hint is missing package identity or URL"
		return decision
	}
	if hint.HintKind != "repository" {
		decision.Outcome = PackageSourceRejected
		decision.Reason = "hint kind " + hint.HintKind + " is provenance-only and cannot prove repository ownership"
		return decision
	}

	activeMatches, staleMatches := matchPackageSourceRepositories(hint, repositories)
	switch len(activeMatches) {
	case 0:
		if len(staleMatches) > 0 {
			decision.Outcome = PackageSourceStale
			decision.CandidateRepositoryIDs = packageSourceRepositoryIDs(staleMatches)
			decision.Reason = "source hint matched only tombstoned repository facts"
			return decision
		}
		decision.Outcome = PackageSourceUnresolved
		decision.Reason = "source hint did not match any repository remote"
		return decision
	case 1:
		match := activeMatches[0]
		decision.RepositoryID = match.RepositoryID
		decision.RepositoryName = match.RepositoryName
		if exactPackageSourceURLMatch(hint.SourceURL, match.RemoteURL) {
			decision.Outcome = PackageSourceExact
			decision.Reason = "source hint matches repository remote exactly"
			return decision
		}
		decision.Outcome = PackageSourceDerived
		decision.Reason = "source hint matches repository remote after git URL canonicalization"
		return decision
	default:
		decision.Outcome = PackageSourceAmbiguous
		decision.CandidateRepositoryIDs = packageSourceRepositoryIDs(activeMatches)
		decision.Reason = "source hint matches multiple active repository remotes"
		return decision
	}
}

func packageSourceRepositoryIDs(repositories []packageSourceRepository) []string {
	ids := make([]string, 0, len(repositories))
	for _, repository := range repositories {
		ids = append(ids, repository.RepositoryID)
	}
	sort.Strings(ids)
	return ids
}

// exactPackageSourceURLMatch forwards to [source.ExactURLMatch].
// The [servicecatalog] family's ServiceCatalogCorrelationHandler classifier
// calls the same comparison directly as source.ExactURLMatch;
// this root spelling stays only for this file's own caller (issue #6061).
func exactPackageSourceURLMatch(left string, right string) bool {
	return source.ExactURLMatch(left, right)
}

// compactStringSlice forwards to [payloadcore.CompactStringSlice].
func compactStringSlice(values ...string) []string {
	return payloadcore.CompactStringSlice(values...)
}

// The remainder of this file is the transitional compatibility surface for
// the package-source hint/repository shapes and matching helpers that moved
// to [source] (issue #6379, epic #6061). Root call sites keep
// their current spelling; each entry is deleted once its last caller has
// moved into a family subpackage.

// packageSourceHint is one package registry source_hint fact.
type packageSourceHint = source.Hint

// packageSourceRepository is one repository fact matched against source
// hints.
type packageSourceRepository = source.Repository

// extractPackageSourceRepositories forwards to
// [source.ExtractRepositories].
func extractPackageSourceRepositories(envelopes []facts.Envelope) []packageSourceRepository {
	return source.ExtractRepositories(envelopes)
}

// matchPackageSourceRepositories forwards to
// [source.MatchRepositories].
func matchPackageSourceRepositories(
	hint packageSourceHint,
	repositories []packageSourceRepository,
) ([]packageSourceRepository, []packageSourceRepository) {
	return source.MatchRepositories(hint, repositories)
}

// canonicalPackageSourceURLKey forwards to
// [source.CanonicalURLKey].
func canonicalPackageSourceURLKey(raw string) string {
	return source.CanonicalURLKey(raw)
}
