// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package support holds the pure pieces of the story target-support read that
// link PagerDuty incident-routing facts (#7463) and Jira records and transitions
// (#7464) to a repository: the bounded statements, the sets and predicates the
// source-only count uses, and the Go re-check of a returned row.
//
// A PagerDuty applied-service or observed-service fact carries no repository, so
// it attaches through the reducer's reducer_incident_repository_correlation for
// its provider service id: an exact or derived, non-provenance-only PagerDuty
// decision on an active generation. IncidentRoutingSQL renders the read for one
// repository, RoutingFactCorrelatedTo re-checks in Go that a row's correlation
// names that repository and the fact's own provider service id,
// AdmissibleCorrelationsSQL and LinkedIncidentRoutingPredicate give the
// source-only count its "linked to some repository" half, and IsRoutingFact
// names the two linkable kinds.
//
// A Jira work_item.record or work_item.transition carries no repository either,
// so it attaches through the work_item.external_link of the same issue: same
// scope and active generation, a non-blank provider_work_item_id, and a link
// whose linked_repository_id is the repository. JiraIssueLinkSQL renders the read
// (records first, transitions only while the bound is unfilled, each row stamped
// with the link it joined through and the repository that link names),
// JiraFactLinked re-checks that witness and repository in Go, and LinkedIssuesSQL and LinkedIssuePredicate give the source-only count its
// "issue linked to some repository" half. The second hop is served by migration
// 155, keyed on the kind and the issue id.
//
// The statements are shaped for the existing partial indexes in migration 003
// (the correlation, applied-service and observed-service indexes): each probe is
// fenced with OFFSET 0 and the keys are written exactly as the indexes spell
// them, so a probe is an index condition and not a heap filter. The package
// holds query text and pure functions only. It imports nothing from the query
// root and runs no SQL; the root's ContentReader owns execution, evidence
// shaping and the repository gate.
package support
