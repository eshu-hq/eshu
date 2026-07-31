// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/relationships/tfstatebackend"
)

// newDefaultedLocatorTestHandler builds a handler wired to resolve exactly
// one config-owner row, with CommitAnchor.LocatorDefaulted mirroring the
// requested value, and a JSON logger capturing every emitted log line.
func newDefaultedLocatorTestHandler(defaulted bool) (TerraformConfigStateDriftHandler, *bytes.Buffer) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	rows := []tfstatebackend.TerraformBackendRow{{
		RepoID:           "repo-local",
		ScopeID:          "repo:repo-local@1",
		CommitID:         "ccc",
		CommitObservedAt: time.Now(),
		BackendKind:      "s3",
		LocatorHash:      "hash-1",
		LocatorDefaulted: defaulted,
	}}
	resolver := tfstatebackend.NewResolver(&stubBackendQuery{rows: rows})
	return TerraformConfigStateDriftHandler{
		Resolver:       resolver,
		EvidenceLoader: &stubDriftLoader{},
		Logger:         logger,
	}, &logs
}

// TestDriftHandlerLogsDefaultedLocatorResolution is the regression test for
// issue #5594's telemetry follow-up: a successful ownership resolution whose
// locator came from Terraform's implicit default (not an authored attribute)
// must be distinguishable in logs from an ordinary explicit resolution, or an
// operator debugging a wrong config-owner match cannot tell the two apart.
func TestDriftHandlerLogsDefaultedLocatorResolution(t *testing.T) {
	t.Parallel()

	h, logs := newDefaultedLocatorTestHandler(true)
	if _, err := h.Handle(context.Background(), validIntent()); err != nil {
		t.Fatalf("Handle() err = %v", err)
	}

	got := logs.String()
	if !strings.Contains(got, "drift candidate resolved via defaulted locator") {
		t.Fatalf("logs = %s, want the defaulted-locator log line", got)
	}
	if !strings.Contains(got, `"locator_defaulted":true`) {
		t.Fatalf("logs = %s, want locator_defaulted=true field", got)
	}
	if !strings.Contains(got, `"owner_repo_id":"repo-local"`) {
		t.Fatalf("logs = %s, want owner_repo_id field", got)
	}
}

// TestDriftHandlerDoesNotLogDefaultedLocatorForExplicitResolution proves the
// new log line is silent on the ordinary explicit-locator path, so this fix
// adds no log volume to the pre-existing S3 (and explicit-path local)
// resolution flow.
func TestDriftHandlerDoesNotLogDefaultedLocatorForExplicitResolution(t *testing.T) {
	t.Parallel()

	h, logs := newDefaultedLocatorTestHandler(false)
	if _, err := h.Handle(context.Background(), validIntent()); err != nil {
		t.Fatalf("Handle() err = %v", err)
	}

	if got := logs.String(); strings.Contains(got, "drift candidate resolved via defaulted locator") {
		t.Fatalf("logs = %s, want no defaulted-locator log line for an explicit locator", got)
	}
}
