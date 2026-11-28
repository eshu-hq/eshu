// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// deltaUnionCompletenessFailedMessage is the fixed client-facing text for a
// failed delta-union completeness step. A store error quotes SQL, hosts,
// and connection settings, so err.Error() never reaches the body.
const deltaUnionCompletenessFailedMessage = "delta-union completeness check failed"

// defaultDeltaUnionMaxScopes bounds one completeness check to 500 scopes:
// one anchored graph query per delta-active repository past that. The
// caller narrows the scope filter instead of paging silently.
const defaultDeltaUnionMaxScopes = 500

// deltaUnionScopeRequiredMessage rejects a check without an explicit scope
// filter: an empty filter fans out over every active generation plus one
// anchored graph query per repository. It mirrors the core text.
const deltaUnionScopeRequiredMessage = "delta union completeness requires at least one explicit scope filter: pass at least one ?scope= query value"

// DeltaUnionRepoOutcome is one repository's completeness verdict.
type DeltaUnionRepoOutcome string

const (
	// DeltaUnionRepoComplete means the graph holds exactly the ACTIVE set.
	DeltaUnionRepoComplete DeltaUnionRepoOutcome = "complete"
	// DeltaUnionRepoMismatch means the graph differs from the ACTIVE set
	// and names the diff for repair.
	DeltaUnionRepoMismatch DeltaUnionRepoOutcome = "mismatch"
	// DeltaUnionRepoSkipped means the scope carries no active generation,
	// or its active generation is not a delta, so the delta-union
	// comparison does not apply to it.
	DeltaUnionRepoSkipped DeltaUnionRepoOutcome = "skipped"
)

// DeltaUnionGeneration annotates one generation behind a verdict. Its keys
// stay capitalized: the core recovery type carries no JSON tags, and this
// port keeps byte parity with it.
type DeltaUnionGeneration struct {
	GenerationID string
	Status       string
	IsDelta      bool
}

// DeltaUnionRepoReport is one repository's completeness result.
type DeltaUnionRepoReport struct {
	RepoID      string                `json:"repo_id"`
	ScopeID     string                `json:"scope_id"`
	Outcome     DeltaUnionRepoOutcome `json:"outcome"`
	Base        DeltaUnionGeneration  `json:"base"`
	Active      DeltaUnionGeneration  `json:"active"`
	GraphFiles  int                   `json:"graph_files"`
	ActiveFiles int                   `json:"active_files"`
	Missing     []string              `json:"missing,omitempty"`
	Extra       []string              `json:"extra,omitempty"`
	Drifted     []string              `json:"drifted,omitempty"`
	Annotation  string                `json:"annotation,omitempty"`
}

// DeltaUnionCompletenessReport is the served completeness report: one entry
// per checked repository plus the mismatch count.
type DeltaUnionCompletenessReport struct {
	Repos      []DeltaUnionRepoReport `json:"repos"`
	Mismatches int                    `json:"mismatches"`
}

// DeltaUnionCapError reports a completeness check past its scope or
// repository cap. Its text holds only the counts and the cap, so the route
// answers it as its own narrow-the-filter message rather than a store
// error's text.
type DeltaUnionCapError struct {
	Count, Limit int
	Unit         string
}

// Error renders the narrow-the-filter text for count units past limit.
func (e *DeltaUnionCapError) Error() string {
	return fmt.Sprintf("delta union completeness covers %d %s, past the %d cap: narrow the scope filter", e.Count, e.Unit, e.Limit)
}

// DeltaUnionStore is the minimal port the delta-union completeness read
// runs through: one synchronous PG-plus-graph check over an explicit scope
// filter. The check never repairs; a graph outage answers as a local store
// error, never as a partial "complete" report. A reader-handle PG store
// plus a graph runner satisfies it, so serving this read needs no writer
// credential. The full ReadStore does not include it.
type DeltaUnionStore interface {
	CheckDeltaUnionCompleteness(ctx context.Context, scopeIDs []string) (DeltaUnionCompletenessReport, error)
}

// ServeDeltaUnionCompleteness answers the read-only prod completeness read
// (GET /api/v0/admin/delta-union/completeness?scope=...): nil store 503s,
// a missing scope filter 400s with the core-identical detail, a filter
// past the 500-scope cap 500s with the narrow-the-filter text, and success
// answers the report with core field parity. A store failure — including a
// graph outage — first offers onStoreError the fence verdicts (nil skips);
// then a client cancel answers 499, a cap error the narrow-the-filter
// text, and anything else the fixed 500. A failed check never answers a
// partial report.
func ServeDeltaUnionCompleteness(w http.ResponseWriter, r *http.Request, store DeltaUnionStore, onStoreError StoreErrorHandler) {
	if store == nil {
		writeError(w, http.StatusServiceUnavailable, "admin store not configured")
		return
	}

	scopes := make([]string, 0, len(r.URL.Query()["scope"]))
	for _, scopeID := range r.URL.Query()["scope"] {
		if strings.TrimSpace(scopeID) == "" {
			continue
		}
		scopes = append(scopes, strings.TrimSpace(scopeID))
	}
	if len(scopes) == 0 {
		writeError(w, http.StatusBadRequest, deltaUnionScopeRequiredMessage)
		return
	}
	if len(scopes) > defaultDeltaUnionMaxScopes {
		writeError(w, http.StatusInternalServerError, (&DeltaUnionCapError{Count: len(scopes), Limit: defaultDeltaUnionMaxScopes, Unit: "scopes"}).Error())
		return
	}

	report, err := store.CheckDeltaUnionCompleteness(r.Context(), scopes)
	if err != nil {
		if onStoreError != nil && onStoreError(w, r, err) {
			return
		}
		if clientCanceled(r.Context(), err) {
			writeError(w, clientClosedStatus, deltaUnionCompletenessFailedMessage)
			return
		}
		var overCap *DeltaUnionCapError
		if errors.As(err, &overCap) {
			writeError(w, http.StatusInternalServerError, overCap.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, deltaUnionCompletenessFailedMessage)
		return
	}

	if report.Repos == nil {
		report.Repos = []DeltaUnionRepoReport{}
	}
	writeJSON(w, http.StatusOK, report)
}
