// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materialization

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"go.opentelemetry.io/otel/metric"

	projfailure "github.com/eshu-hq/eshu/go/internal/projector/failure"
	codecall "github.com/eshu-hq/eshu/go/internal/reducer/code/call"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// DeltaScopeUnusableFailureClass is the durable failure_class a
// code_call_materialization work item dead-letters with when a
// delta-generation repository's changed paths cannot be qualified into a
// code-call file scope (#10023).
const DeltaScopeUnusableFailureClass = "code_call_delta_scope_unusable"

// maxDeltaScopeUnusableNamedRepos bounds how many repositories the error
// message and the log line name, so one pathological batch cannot flood the
// dead-letter row or the log. The count attribute always carries the total.
const maxDeltaScopeUnusableNamedRepos = 16

// deltaScopeUnusableError fails a code_call_materialization work item closed
// when a delta-generation repository has no usable file scope (#10023).
//
// A delta generation carries the changed files' facts only. Without a file
// scope the only refresh the handler can emit is the whole-repository one,
// whose retract deletes every unchanged file's CALLS edge while the delta's
// rows restore only the changed files' edges. The code-call projection lane
// has no terminal state for a poisoned shared intent (it retries every cycle
// and a stuck whole-scope refresh blocks its partition), so the failure is
// raised here, before any intent is written, where the reducer queue owns a
// real dead letter. It is not retryable: a retry rebuilds the same scope from
// the same facts.
//
// Production does not reach this yet: the git collector emits no
// code_call_materialization marker on a delta generation until #10049, so the
// fence is a guard placed ahead of that change.
//
// It wraps a projector/failure InputValidationError so the dead-letter triage
// details say triage=input_invalid (a collector repository fact the reducer
// cannot scope) instead of the projection_bug fallback. The durable
// failure_class column still carries [DeltaScopeUnusableFailureClass].
type deltaScopeUnusableError struct {
	scopeID      string
	generationID string
	// entries are "repo=reason" pairs, sorted by repository.
	entries []string
	// cause classifies the failure as invalid input for dead-letter triage.
	cause *projfailure.InputValidationError
}

func (e deltaScopeUnusableError) Error() string {
	named := e.entries
	suffix := ""
	if len(named) > maxDeltaScopeUnusableNamedRepos {
		suffix = fmt.Sprintf(" (+%d more)", len(named)-maxDeltaScopeUnusableNamedRepos)
		named = named[:maxDeltaScopeUnusableNamedRepos]
	}
	return fmt.Sprintf(
		"code call delta scope unusable for scope %s generation %s: %s%s; "+
			"refusing a whole-repository CALLS retract with delta-only rows, edges wait for the next full generation",
		e.scopeID, e.generationID, strings.Join(named, ", "), suffix,
	)
}

// Unwrap returns the input-validation cause so errors.As finds it during
// dead-letter triage.
func (e deltaScopeUnusableError) Unwrap() error {
	if e.cause == nil {
		return nil
	}
	return e.cause
}

// Retryable reports false: the unusable scope is a property of the
// generation's facts, so a retry fails the same way.
func (deltaScopeUnusableError) Retryable() bool { return false }

// FailureClass returns [DeltaScopeUnusableFailureClass] so the dead-letter
// row carries it instead of a generic triage class.
func (deltaScopeUnusableError) FailureClass() string { return DeltaScopeUnusableFailureClass }

// failClosedOnUnusableDeltaScopes returns a deltaScopeUnusableError when a
// repository that will get code-call intents (it has a projection context) is
// on a delta generation with an unusable file scope. It records the
// eshu_dp_code_call_delta_scope_unusable_total counter once per such
// repository and logs one ERROR line, then the caller returns the error before
// building or writing any intent. It returns nil when every delta repository
// has a usable scope.
func failClosedOnUnusableDeltaScopes(
	ctx context.Context,
	instruments *telemetry.Instruments,
	intent reducercontract.Intent,
	contextByRepoID map[string]sharedintent.ProjectionContext,
	scopes codecall.FileScopeBuildResult,
) error {
	if len(scopes.UnusableDeltaScopes) == 0 {
		return nil
	}
	repositoryIDs := make([]string, 0, len(scopes.UnusableDeltaScopes))
	for repositoryID := range scopes.UnusableDeltaScopes {
		if _, projected := contextByRepoID[repositoryID]; projected {
			repositoryIDs = append(repositoryIDs, repositoryID)
		}
	}
	if len(repositoryIDs) == 0 {
		return nil
	}
	sort.Strings(repositoryIDs)

	entries := make([]string, 0, len(repositoryIDs))
	for _, repositoryID := range repositoryIDs {
		reason := scopes.UnusableDeltaScopes[repositoryID]
		entries = append(entries, repositoryID+"="+string(reason))
		if instruments != nil && instruments.CodeCallDeltaScopeUnusable != nil {
			instruments.CodeCallDeltaScopeUnusable.Add(ctx, 1, metric.WithAttributes(
				telemetry.AttrReason(string(reason)),
			))
		}
	}
	err := deltaScopeUnusableError{
		scopeID:      intent.ScopeID,
		generationID: intent.GenerationID,
		entries:      entries,
	}
	err.cause = projfailure.NewInputValidationError(err.Error())
	named := entries
	if len(named) > maxDeltaScopeUnusableNamedRepos {
		named = named[:maxDeltaScopeUnusableNamedRepos]
	}
	slog.ErrorContext(
		ctx, "code call delta scope unusable, failing closed",
		log.ScopeID(intent.ScopeID),
		log.GenerationID(intent.GenerationID),
		log.Domain(string(intent.Domain)),
		slog.Any("unusable_delta_scopes", named),
		slog.Int("unusable_delta_scope_repo_count", len(entries)),
		telemetry.FailureClassAttr(DeltaScopeUnusableFailureClass),
		telemetry.PhaseAttr(telemetry.PhaseReduction),
	)
	return err
}
