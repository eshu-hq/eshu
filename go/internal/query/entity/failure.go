// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import (
	"context"
	"errors"
	"net/http"

	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/tracing"
)

// Fixed response messages for a service context, investigation, or story read
// that failed for a reason that is not a fence or graph verdict (#7626). A
// backend error quotes SQL, Cypher, and host detail, so the client sees only
// one of these; the error itself goes to the request span through the tracing
// server-failure helpers.
const (
	serviceContextQueryFailedMessage            = "service context query failed"
	serviceContextEnrichmentFailedMessage       = "service context enrichment failed"
	serviceInvestigationQueryFailedMessage      = "service investigation query failed"
	serviceInvestigationEnrichmentFailedMessage = "service investigation enrichment failed"
	serviceStoryQueryFailedMessage              = "service story query failed"
	serviceStoryEnrichmentFailedMessage         = "service story enrichment failed"
	serviceStoryCICDEvidenceFailedMessage       = "service story ci/cd evidence load failed"
	serviceStorySupplyChainFailedMessage        = "service story supply chain evidence enrichment failed"
)

// Fixed response messages for the entity resolve, entity context, and
// workload context and story routes (#7674), one per route step, with the
// same contract as the service messages above.
const (
	resolveEntityQueryFailedMessage            = "entity resolve query failed"
	resolveEntityEnrichmentFailedMessage       = "entity resolve enrichment failed"
	resolveEntityRepoIdentityFailedMessage     = "entity resolve repository identity hydration failed"
	resolveEntityContentFallbackFailedMessage  = "entity resolve content fallback failed"
	resolveGlobalContentEntitiesFailedMessage  = "global entity resolve failed"
	resolveCanonicalContentEntityFailedMessage = "canonical content entity resolve failed"
	resolveCanonicalRepoIdentityFailedMessage  = "canonical content entity repository identity hydration failed"
	resolveWorkloadEntitiesFailedMessage       = "workload entity resolve failed"
	entityContextQueryFailedMessage            = "entity context query failed"
	entityContextContentFallbackFailedMessage  = "entity context content fallback failed"
	entityContextRepoIdentityFailedMessage     = "entity context repository identity hydration failed"
	entityContextEnrichmentFailedMessage       = "entity context enrichment failed"
	workloadContextQueryFailedMessage          = "workload context query failed"
	workloadContextEnrichmentFailedMessage     = "workload context enrichment failed"
	workloadStoryQueryFailedMessage            = "workload story query failed"
	workloadStoryEnrichmentFailedMessage       = "workload story enrichment failed"
)

// writeReadFailure answers a failed read step of an entity route.
// querycontract.WriteGraphReadError runs first, so a reader fence answers the
// retryable 503 and a graph verdict its 503 or 504. Anything else answers
// message through tracing.WriteServerFailure: 500 with err recorded on the
// request span, or 499 with only the client-cancel event when the caller
// canceled the request. err's text never reaches the body.
func writeReadFailure(w http.ResponseWriter, r *http.Request, err error, capability, message string) {
	if querycontract.WriteGraphReadError(w, r, err, capability) {
		return
	}
	tracing.WriteServerFailure(w, r, err, http.StatusInternalServerError, message)
}

// unavailableSentinel reports whether err is a configuration-state 503 for
// sentinel: err wraps sentinel and ctx, the request context, was not canceled
// by the client. When it holds, it records err on ctx's span with sentinel's
// text as the Error description, and the caller answers 503 with that
// sentinel's own text, never err's, because a wrapper can quote backend
// detail. Name the sentinel at the write site (ErrFoo.Error()) so the
// #7674 guard sees fixed text. The answer carries no Retry-After: the state
// does not clear on its own. When it does not hold, the caller's
// tracing.WriteServerFailure step answers (499 for a client cancel). Call it
// after querycontract.WriteGraphReadError.
func unavailableSentinel(ctx context.Context, err, sentinel error) bool {
	if !errors.Is(err, sentinel) || tracing.ClientCanceled(ctx, err) {
		return false
	}
	span := trace.SpanFromContext(ctx)
	span.RecordError(err)
	span.SetStatus(codes.Error, sentinel.Error())
	return true
}
