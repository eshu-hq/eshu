// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repository

import (
	"errors"
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/selector"
	"github.com/eshu-hq/eshu/go/internal/query/tracing"
)

// Fixed client-facing messages for repository-family read failures (#7674).
// Each route step that answers a server failure has its own constant, so an
// operator can tell from the body alone which read failed. The response body
// is exactly that text: a backend error quotes SQL, Cypher, hosts, and
// credentials, so err.Error() never reaches a 4xx or 5xx body. The cause is
// recorded on the request span instead.
const (
	repositoryListContentFailedMessage = "repository list content catalog read failed"
	repositoryListCountFailedMessage   = "repository list count query failed"
	repositoryListPageFailedMessage    = "repository list page query failed"
	// repositoryListSelectionFailedMessage covers the #8454 selection
	// annotation read: entries never serve with evidence silently missing.
	repositoryListSelectionFailedMessage = "repository list selection annotation failed"

	catalogContentRepositoriesFailedMessage = "catalog content repository read failed"
	catalogRepositoriesFailedMessage        = "catalog repository query failed"
	catalogWorkloadsFailedMessage           = "catalog workload query failed"

	repositoryContextLookupFailedMessage             = "repository context lookup failed"
	repositoryContextCoverageFailedMessage           = "repository context content coverage read failed"
	repositoryContextCountsFailedMessage             = "repository context summary count query failed"
	repositoryContextDeploymentEvidenceFailedMessage = "repository context deployment evidence read failed"

	repositoryStoryLookupFailedMessage              = "repository story lookup failed"
	repositoryStoryCoverageFailedMessage            = "repository story content coverage read failed"
	repositoryStoryGraphSummaryFailedMessage        = "repository story graph summary query failed"
	repositoryStorySemanticOverviewFailedMessage    = "repository story semantic overview read failed"
	repositoryStoryNarrativeFilesFailedMessage      = "repository story narrative file read failed"
	repositoryStoryDeploymentEvidenceFailedMessage  = "repository story deployment evidence read failed"
	repositoryStoryCICDEvidenceFailedMessage        = "repository story ci/cd evidence read failed"
	repositoryStoryTargetDocumentationFailedMessage = "repository story target documentation read failed"
	repositoryStoryTargetSupportFailedMessage       = "repository story target support read failed"

	repositoryStatsCoverageFailedMessage = "repository stats content coverage read failed"

	repositoryTreeLookupFailedMessage        = "repository tree lookup failed"
	repositoryTreePathContextFailedMessage   = "repository tree path context read failed"
	repositoryTreeLanguageFilesFailedMessage = "repository tree language-filtered file list failed"
	repositoryTreePathFilesFailedMessage     = "repository tree path-scoped file list failed"
	repositoryTreeFilesFailedMessage         = "repository tree file list failed"
	repositoryTreeRefsFailedMessage          = "repository tree ref validation failed"

	repositoryContentLookupFailedMessage = "repository content lookup failed"
	repositoryContentReadFailedMessage   = "repository file content read failed"
	repositoryContentRefsFailedMessage   = "repository content ref validation failed"

	repositoryBranchesLookupFailedMessage = "repository branches lookup failed"
	repositoryBranchesRefsFailedMessage   = "repository branches ref read failed"

	repositoryFreshnessReadFailedMessage   = "repository freshness read failed"
	repositoryFreshnessLookupFailedMessage = "repository freshness lookup failed"

	repositoriesByLanguageReadFailedMessage  = "repositories by language read failed"
	repositoriesByLanguageCountFailedMessage = "repositories by language count failed"
	repositoryLanguageInventoryFailedMessage = "repository language inventory read failed"
)

// coverageReadFailedLastError is the fixed coverage.last_error (stats) and
// coverage_summary.last_error (story) value when the content-store coverage
// read failed for a reason that is neither the route timeout nor a verdict
// the route answers itself (#7674). The field is a closed set of fixed
// strings: this one, the timeout text derived from repositoryStatsReadTimeout,
// "content store not configured", and "content store coverage unavailable".
const coverageReadFailedLastError = "content store coverage read failed"

// coverageCapability is the capability a coverage-read verdict names; stats
// and story both answer under the context-overview capability.
const coverageCapability = "platform_impact.context_overview"

// writeReadFailure answers a failed read step of a repository route.
// querycontract.WriteGraphReadError runs first, so a reader fence answers the
// retryable 503 with Retry-After and a graph verdict its 503 or 504. Anything
// else answers message through tracing.WriteServerFailure: 500 with err
// recorded on the request span, or 499 with only the client-cancel event when
// the caller canceled the request. err's text never reaches the body.
func writeReadFailure(w http.ResponseWriter, r *http.Request, err error, capability, message string) {
	if querycontract.WriteGraphReadError(w, r, err, capability) {
		return
	}
	tracing.WriteServerFailure(w, r, err, http.StatusInternalServerError, message)
}

// writeCoverageVerdict answers the two coverage-read failures that stats and
// story must not degrade into a 200 partial (#7674 Fork 1), and reports
// whether it wrote a response. A reader fence or graph verdict answers through
// querycontract.WriteGraphReadError (fence: 503 with Retry-After). A client
// cancel of the request answers 499 with message through
// tracing.WriteServerFailure. Any other error, and a nil err, writes nothing:
// the route degrades to its 200 partial with the fixed coverage last_error.
func writeCoverageVerdict(w http.ResponseWriter, r *http.Request, err error, message string) bool {
	if err == nil {
		return false
	}
	if querycontract.WriteGraphReadError(w, r, err, coverageCapability) {
		return true
	}
	if tracing.ClientCanceled(r.Context(), err) {
		tracing.WriteServerFailure(w, r, err, http.StatusInternalServerError, message)
		return true
	}
	return false
}

// writeSelectorAnswer answers a repository selector error that
// querycontract.WriteGraphReadError and selector.WriteLookupFailure did not
// claim. An unmatched selector answers 404 and an ambiguous one 400, each with
// the typed error's own text, which quotes only the caller's selector and
// candidate repository ids; a wrapper around it can quote backend detail, so
// the wrapper's text never reaches the body. Any other error is a server
// fault and answers selector.LookupFailureMessage through
// tracing.WriteServerFailure (500, or 499 for a client cancel), matching
// selector.ResolveForRequestWithAccess.
func writeSelectorAnswer(w http.ResponseWriter, r *http.Request, err error) {
	var notFound selector.NotFoundError
	if errors.As(err, &notFound) {
		querycontract.WriteError(w, http.StatusNotFound, notFound.Error())
		return
	}
	var ambiguous selector.AmbiguousError
	if errors.As(err, &ambiguous) {
		querycontract.WriteError(w, http.StatusBadRequest, ambiguous.Error())
		return
	}
	tracing.WriteServerFailure(w, r, err, http.StatusInternalServerError, selector.LookupFailureMessage)
}

// writeRefVerdict answers a non-zero readmodel.ValidateSelectedRepositoryRef
// verdict with its fixed message and a literal status: 404 when the ref is
// unknown, otherwise 409 (the ref is known but not indexed, or branch
// metadata is unavailable). The validator returns only those two statuses;
// naming them as literals keeps every status this package writes visible to
// the #7674 writer guard.
func writeRefVerdict(w http.ResponseWriter, status int, message string) {
	if status == http.StatusNotFound {
		querycontract.WriteError(w, http.StatusNotFound, message)
		return
	}
	querycontract.WriteError(w, http.StatusConflict, message)
}
