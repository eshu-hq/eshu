// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package searchbenchrun

import (
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/searchbench"
	"github.com/eshu-hq/eshu/go/internal/searchdocs"
)

// EvidenceInput is the live-assembled input for one benchmark evidence record.
// The executor stamps the schema version, derived truth scope, and the complete
// required failure-class contract; callers supply commit, corpus, runs, and the
// recorded decision.
type EvidenceInput struct {
	EshuCommit           string
	SchemaBootstrapState string
	// TruthBasis is the evidence authority basis. It defaults to content_index
	// when empty.
	TruthBasis     searchdocs.TruthBasis
	Corpus         searchbench.CorpusSummary
	Backends       []searchbench.BackendRun
	Recommendation searchbench.Recommendation
}

// AssembleEvidence stamps the evidence schema version, derived truth scope, and
// the complete required failure-class contract, then validates the record so it
// cannot be recorded in the design doc unless it satisfies ValidateEvidence.
func AssembleEvidence(in EvidenceInput) (searchbench.Evidence, error) {
	basis := in.TruthBasis
	if basis == "" {
		basis = searchdocs.TruthBasisContentIndex
	}
	evidence := searchbench.Evidence{
		Version:              searchbench.EvidenceVersion,
		EshuCommit:           in.EshuCommit,
		SchemaBootstrapState: in.SchemaBootstrapState,
		TruthScope:           searchbench.TruthScope{Level: searchdocs.TruthLevelDerived, Basis: basis},
		Corpus:               in.Corpus,
		Backends:             in.Backends,
		FailureClasses:       searchbench.RequiredFailureClasses(),
		Recommendation:       in.Recommendation,
	}
	if err := searchbench.ValidateEvidence(evidence); err != nil {
		return searchbench.Evidence{}, fmt.Errorf("searchbenchrun: assemble evidence: %w", err)
	}
	return evidence, nil
}
