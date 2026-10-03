// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package code

// CrossRepoDeadCodeConsumerReads says how one cross-repo consumer-evidence
// lookup is bounded. The handler builds it; the reader does what it says.
//
// PageRepositoryIDs is the consumer-repository list the evidence page binds
// in SQL ahead of its LIMIT: the request's own consumer selector when it
// named one, otherwise the caller's grant, and empty only for an unscoped
// caller who named neither -- the single case where an unbounded page is the
// right answer. Which list goes here decides where the row cap falls.
// Binding the grant while the request named one consumer let a thousand rows
// from another granted repository fill the page and push the requested
// consumer off it.
//
// SignalGrant is the caller's grant the ungranted-consumer probe tests each
// consumer repository against, and empty means no probe runs. The probe
// answers, per producer entity, whether a consumer outside that grant
// exists; it is the half of the question the grant-bound page cannot see,
// and losing it would mark a live symbol dead. A request that named a
// consumer selector leaves this empty, because the only consumers the probe
// could report are ones that request excluded.
//
// The implementation moved from root's content_reader_dead_code_cross_repo.go
// for #6060 so a handler-family subpackage can build the same read plan
// without importing root.
type CrossRepoDeadCodeConsumerReads struct {
	PageRepositoryIDs []string
	SignalGrant       []string
}

// CrossRepoDeadCodeHiddenConsumers is the set of producer entity ids the
// ungranted-consumer probe proved have at least one active-generation
// consumer in a repository outside the caller's grant.
//
// It is a set of PRODUCER entity ids -- every one of which the caller is
// already reading -- and carries nothing about the consumer: not its
// repository, not its entity, not a count. The route only ever needed the
// yes/no, and answering only the yes/no is what lets the probe stop at the
// first HIDDEN row -- ungranted and live -- instead of enumerating the group.
// The implementation moved from root's content_reader_dead_code_cross_repo.go
// for #6060 so a handler-family subpackage can build the same set without
// importing root.
type CrossRepoDeadCodeHiddenConsumers map[string]struct{}

// Has reports whether the probe found an out-of-grant consumer for this
// producer entity.
func (h CrossRepoDeadCodeHiddenConsumers) Has(entityID string) bool {
	_, ok := h[entityID]
	return ok
}

// CrossRepoDeadCodeCoverageRequest says which consumer repositories the
// per-request coverage check must prove complete before a producer symbol may
// be called dead. The handler builds it from the same read plan as the evidence
// page; the reader does what it says in one statement, never one per candidate.
//
// A consumer repository is a gap when one of its repository scopes has an active
// generation with code_calls or inheritance_edges work and no code_reachability_
// repository_watermarks row for that generation, a truncated one, or one whose
// verdict_schema_epoch is below the current epoch (#7547).
// Absence of a consumer row only means "not called" for a repository that is not
// a gap. The check does not detect a stale or partly drained snapshot whose
// watermark says truncated = false, nor a zero-root snapshot before the writer
// stamps it truncated.
//
// RepositoryIDs is the consumer list when AllRepositories is false: the
// request's own consumer selector, or the caller's grant. AllRepositories
// checks every repository scope with an active generation, and is only used for
// an unscoped caller who named no consumer. RequireActiveScope makes a listed
// repository with no active repository scope at all count as incomplete; the
// handler sets it for a consumer the request named, because the caller asked
// for that repository's evidence by name, and leaves it off for the grant
// list, where a granted repository nobody has ingested is not a consumer.
type CrossRepoDeadCodeCoverageRequest struct {
	RepositoryIDs      []string
	AllRepositories    bool
	RequireActiveScope bool
}

// CrossRepoDeadCodeCoverage is the coverage check's answer: the consumer
// repositories that are not proven complete.
//
// IncompleteRepositoryIDs is sorted and capped; IncompleteTruncated says the
// cap cut the list. A repository id here is one the request named, one inside
// the caller's grant, or -- for an unscoped caller -- any repository, so it
// never discloses a repository the caller may not see.
type CrossRepoDeadCodeCoverage struct {
	IncompleteRepositoryIDs []string
	IncompleteTruncated     bool
}

// Complete reports whether every consumer repository the request covers has a
// complete watermark for its active generation.
func (c CrossRepoDeadCodeCoverage) Complete() bool {
	return len(c.IncompleteRepositoryIDs) == 0 && !c.IncompleteTruncated
}
