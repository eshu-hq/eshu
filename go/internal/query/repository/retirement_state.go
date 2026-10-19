// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repository

import (
	"context"
	"fmt"
	"strings"

	contractrepository "github.com/eshu-hq/eshu/go/internal/query/querycontract/repository"
)

// Index state values for GET /api/v0/repositories rows. They alias the
// contract derivation so handlers and tests name one set.
const (
	IndexStateIndexed  = contractrepository.IndexStateIndexed
	IndexStateStub     = contractrepository.IndexStateStub
	IndexStateRetiring = contractrepository.IndexStateRetiring
	IndexStateRetired  = contractrepository.IndexStateRetired
)

// PageRetirementReader annotates one repository list page with tombstone
// labels (#7766). Production wiring passes the Postgres retirement store;
// nil disables annotation and every entry derives from its evidence source
// alone.
type PageRetirementReader interface {
	// ReadPageRetirements reads the page's tombstone labels in one
	// bounded statement keyed by repo id.
	ReadPageRetirements(ctx context.Context, repoIDs []string) (map[string]contractrepository.PageRetirement, error)
}

// DeriveRepositoryIndexState derives one list row's bounded index_state in
// the designed order: an open non-complete tombstone reads retiring, a
// complete tombstone reads retired, otherwise the graph evidence source
// decides indexed versus stub. Rows without evidence source (the content
// path) read stub unless a tombstone says otherwise.
func DeriveRepositoryIndexState(tombstone *contractrepository.PageRetirement, evidenceSource string) string {
	if tombstone != nil {
		if tombstone.IsComplete() {
			return IndexStateRetired
		}
		return IndexStateRetiring
	}
	if evidenceSource == contractrepository.CanonicalEvidenceSource {
		return IndexStateIndexed
	}
	return IndexStateStub
}

// retirementLabelToMap renders the bounded retirement object a retiring or
// retired row carries: the tombstone state, its retirement id, and the
// completion timestamp when the row is complete. Nil tombstone renders nil,
// never an empty object.
func retirementLabelToMap(tombstone *contractrepository.PageRetirement) map[string]any {
	if tombstone == nil {
		return nil
	}
	block := map[string]any{
		"state":         tombstone.State,
		"retirement_id": tombstone.RetirementID,
		"retired_at":    nil,
	}
	if tombstone.HasRetiredAt {
		block["retired_at"] = tombstone.RetiredAt.Format("2006-01-02T15:04:05Z07:00")
	}
	return block
}

// annotateListRepositoriesWithRetirement adds index_state plus the bounded
// retirement object to each repo map from one batched tombstone read for
// the page (#7766). Nothing is dropped from the list: retiring and retired
// rows are labelled, never flattened to empty. An annotation read error
// fails the list, mirroring the selection annotation: entries never serve
// with evidence silently missing.
func annotateListRepositoriesWithRetirement(
	ctx context.Context,
	store PageRetirementReader,
	repos []map[string]any,
) ([]map[string]any, error) {
	if store == nil {
		for _, repo := range repos {
			repo["index_state"] = DeriveRepositoryIndexState(nil, repositoryEvidenceSource(repo))
			repo["retirement"] = nil
		}
		return repos, nil
	}
	ids := make([]string, 0, len(repos))
	for _, repo := range repos {
		if id, ok := repo["id"].(string); ok && strings.TrimSpace(id) != "" {
			ids = append(ids, id)
		}
	}
	labels, err := store.ReadPageRetirements(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("annotate repository list with retirement: %w", err)
	}
	for _, repo := range repos {
		var tombstone *contractrepository.PageRetirement
		if id, ok := repo["id"].(string); ok {
			if label, found := labels[id]; found {
				label := label
				tombstone = &label
			}
		}
		repo["index_state"] = DeriveRepositoryIndexState(tombstone, repositoryEvidenceSource(repo))
		// Assign untyped nil for rows without a tombstone: a typed nil
		// map in the interface would read non-nil to callers.
		if tombstone == nil {
			repo["retirement"] = nil
		} else {
			repo["retirement"] = retirementLabelToMap(tombstone)
		}
	}
	return repos, nil
}

// repositoryEvidenceSource reads the row's graph evidence source when the
// page query carried it. Rows without one (the content path) derive stub.
func repositoryEvidenceSource(repo map[string]any) string {
	source, _ := repo["evidence_source"].(string)
	return source
}

// listAnnotationStage names the page annotation that failed, so the
// handler attributes the failure without collapsing two evidences into
// one message.
type listAnnotationStage int

const (
	// listAnnotationSelection failed reading the selection aggregate.
	listAnnotationSelection listAnnotationStage = iota + 1
	// listAnnotationRetirement failed reading the tombstone labels.
	listAnnotationRetirement
)

// failureMessage maps the failed stage to its route failure message.
func (s listAnnotationStage) failureMessage() string {
	if s == listAnnotationRetirement {
		return repositoryListRetirementFailedMessage
	}
	return repositoryListSelectionFailedMessage
}

// annotateListPageWithSelectionAndRetirement annotates one repository list
// page with the selection aggregate then the retirement label, and strips
// the derivation-only evidence_source so the graph and content paths
// answer the same row shape (the delete is a no-op for content rows,
// which carry no evidence source). Entries never serve with evidence
// silently missing: the first failing stage stops the page and reports
// its stage.
func annotateListPageWithSelectionAndRetirement(
	ctx context.Context,
	selections PageSelectionReader,
	retirements PageRetirementReader,
	repos []map[string]any,
) ([]map[string]any, listAnnotationStage, error) {
	annotated, err := annotateListRepositoriesWithSelection(ctx, selections, repos)
	if err != nil {
		return nil, listAnnotationSelection, err
	}
	annotated, err = annotateListRepositoriesWithRetirement(ctx, retirements, annotated)
	if err != nil {
		return nil, listAnnotationRetirement, err
	}
	for _, repo := range annotated {
		delete(repo, "evidence_source")
	}
	return annotated, 0, nil
}
