// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package selector

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/testutil/failure"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// resolveFailureMounter serves the shared writeResolveFailure helper for one
// mapped error, so testutil/failure can pin its wire answers.
type resolveFailureMounter struct{ err error }

func (m resolveFailureMounter) Mount(mux *http.ServeMux) {
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeResolveFailure(w, r, m.err, "selector")
	})
}

// TestWriteResolveFailureLookupBranchAnswersOnTheHelper pins the
// lookup-failure branch of the shared selector helper on testutil/failure: a
// backing-read failure answers 500 with LookupFailureMessage and marks the
// request span, a caller cancel answers 499, an inner cancel on a live
// request is still a 500 server fault, and a stale reader answers the 503
// fence verdict. The handler wraps only the plain fault in a LookupError,
// which is what ResolveExactForAccess returns for a failed backing read
// (#7626); a cancel and a fence pass through unwrapped so the helper pins
// their real verdicts. The 404 and 400 sentinels stay in
// TestWriteResolveFailureAnswersSelectorAnswersWithTheirOwnText.
func TestWriteResolveFailureLookupBranchAnswersOnTheHelper(t *testing.T) {
	t.Parallel()
	failure.Run(t, failure.Suite{Routes: []failure.Route{{
		Name:        "selector lookup failure",
		Path:        "/resolve",
		Message:     LookupFailureMessage,
		PlainDetail: true,
		Handler: func(err error) failure.Mounter {
			mapped := err
			if !errors.Is(err, context.Canceled) && !errors.Is(err, db.ErrReaderStale) {
				mapped = LookupError{Err: err}
			}
			return resolveFailureMounter{err: mapped}
		},
	}}})
}
