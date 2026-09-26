// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/crossscope"
	"github.com/eshu-hq/eshu/go/internal/relationships"
)

// sharedEdgeTargetMissStandIn mirrors the shared-edge writer's
// targetMissingError contract (go/internal/storage/cypher/edge/writer): the
// writer imports this package, so this package's tests cannot import it. The
// reducer side keys only on that contract — Retryable, the
// SharedEdgeTargetNotReadyFailureClass class, and SampleTarget — and the
// writer's own tests pin that its real error satisfies it.
type sharedEdgeTargetMissStandIn struct{}

func (sharedEdgeTargetMissStandIn) Error() string {
	return `deployable_unit_edges batch of 1 row(s) has no graph target (sample repo "repo-edge-api" intent "i-1")`
}
func (sharedEdgeTargetMissStandIn) Retryable() bool { return true }
func (sharedEdgeTargetMissStandIn) FailureClass() string {
	return SharedEdgeTargetNotReadyFailureClass
}
func (sharedEdgeTargetMissStandIn) SampleTarget() (string, string) { return "repo-edge-api", "i-1" }

// classlessProbeFault mirrors the writer's targetProbeError contract: retryable
// with no failure class, so it counts toward the retry budget.
type classlessProbeFault struct{}

func (classlessProbeFault) Error() string   { return "target-existence probe failed" }
func (classlessProbeFault) Retryable() bool { return true }

// failingDeployableUnitEdgeWriter retracts cleanly and fails every write with
// writeErr, the shape the shared-edge writer returns when a target is absent.
type failingDeployableUnitEdgeWriter struct {
	recordingDeployableUnitEdgeWriter
	writeErr error
}

func (w *failingDeployableUnitEdgeWriter) WriteEdges(
	ctx context.Context,
	domain string,
	rows []SharedProjectionIntentRow,
	evidenceSource string,
) (SharedProjectionWriteReport, error) {
	_, _ = w.recordingDeployableUnitEdgeWriter.WriteEdges(ctx, domain, rows, evidenceSource)
	return SharedProjectionWriteReport{}, w.writeErr
}

// handleDeployableUnitWithWriteErr runs the real handler over one admitted
// deployable-unit edge whose write fails with writeErr.
func handleDeployableUnitWithWriteErr(t *testing.T, writeErr error, cycleStartedAt, enqueuedAt time.Time) error {
	t.Helper()
	return handleDeployableUnitWithLogger(t, writeErr, cycleStartedAt, enqueuedAt, nil)
}

// handleDeployableUnitWithLogger is handleDeployableUnitWithWriteErr with the
// handler's Logger set, so a test can observe where the bound-expiry WARN lands.
func handleDeployableUnitWithLogger(
	t *testing.T,
	writeErr error,
	cycleStartedAt, enqueuedAt time.Time,
	logger *slog.Logger,
) error {
	t.Helper()

	handler := DeployableUnitCorrelationHandler{
		FactLoader: &stubDeployableUnitFactLoader{
			envelopes: deployableUnitCorrelationEnvelopes("repo-edge-api", "edge-api", []map[string]any{{
				"repo_id":       "repo-edge-api",
				"language":      "dockerfile",
				"relative_path": "Dockerfile",
				"parsed_file_data": map[string]any{
					"dockerfile_stages": []any{map[string]any{"name": "runtime"}},
				},
			}}),
		},
		ResolvedLoader: &stubDeployableUnitResolvedLoader{resolved: []relationships.ResolvedRelationship{{
			SourceRepoID:     "repo-deployments",
			TargetRepoID:     "repo-edge-api",
			RelationshipType: relationships.RelDeploysFrom,
			Confidence:       0.94,
			Details: map[string]any{
				"evidence_kinds": []string{string(relationships.EvidenceKindArgoCDAppSource)},
			},
		}}},
		PhasePublisher: &recordingGraphProjectionPhasePublisher{},
		EdgeWriter:     &failingDeployableUnitEdgeWriter{writeErr: writeErr},
		Logger:         logger,
	}
	intent := deployableUnitIntent("edge-api")
	intent.CycleStartedAt = cycleStartedAt
	intent.EnqueuedAt = enqueuedAt

	_, err := handler.Handle(context.Background(), intent)
	if err == nil {
		t.Fatal("Handle() with a failing edge write succeeded, want the write error")
	}
	if !IsRetryable(err) {
		t.Fatalf("Handle() error = %v, want retryable", err)
	}
	return err
}

// TestDeployableUnitCorrelationTargetDeferralIsBoundedByElapsedTime is the
// #7268 liveness proof. The shared-edge target deferral is non-counting, so
// attempt_count freezes and cannot bound it; without an elapsed bound a
// deployment Repository that never appears would retry forever. Past
// crossscope.ProducerReadinessMaxWait since the repair cycle began, the handler
// must replace it with a COUNTING retryable error (no class, no Unwrap to the
// classed miss) so the ordinary budget dead-letters it loudly.
func TestDeployableUnitCorrelationTargetDeferralIsBoundedByElapsedTime(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	pastBound := now.Add(-crossscope.ProducerReadinessMaxWait - time.Minute)
	miss := sharedEdgeTargetMissStandIn{}

	t.Run("inside the bound stays non-counting", func(t *testing.T) {
		t.Parallel()
		err := handleDeployableUnitWithWriteErr(t, miss, now.Add(-time.Minute), now.Add(-time.Hour))
		if class, ok := failureClassOf(err); !ok || class != SharedEdgeTargetNotReadyFailureClass {
			t.Fatalf("failure class = %q (found=%v), want %q", class, ok, SharedEdgeTargetNotReadyFailureClass)
		}
	})

	t.Run("past the bound counts toward the retry budget", func(t *testing.T) {
		t.Parallel()
		err := handleDeployableUnitWithWriteErr(t, miss, pastBound, pastBound)
		if class, ok := failureClassOf(err); ok {
			t.Fatalf("failure class = %q past the bound, want none so the retry budget counts and dead-letters it", class)
		}
		var standIn sharedEdgeTargetMissStandIn
		if errors.As(err, &standIn) {
			t.Fatal("bounded error unwraps to the classed miss; errors.As would leak the non-counting class")
		}
		for _, want := range []string{"target still absent", "elapsed", crossscope.ProducerReadinessMaxWait.String(), "repo-edge-api"} {
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("error %q missing %q: the operator must see the elapsed wait, the bound, and the miss detail", err, want)
			}
		}
	})

	t.Run("cycle start anchors the bound, not first enqueue", func(t *testing.T) {
		t.Parallel()
		err := handleDeployableUnitWithWriteErr(t, miss, now.Add(-time.Minute), pastBound)
		if class, ok := failureClassOf(err); !ok || class != SharedEdgeTargetNotReadyFailureClass {
			t.Fatalf("failure class = %q (found=%v), want the non-counting class for a freshly reopened row", class, ok)
		}
	})

	t.Run("unknown elapsed time keeps deferring", func(t *testing.T) {
		t.Parallel()
		err := handleDeployableUnitWithWriteErr(t, miss, time.Time{}, time.Time{})
		if class, ok := failureClassOf(err); !ok || class != SharedEdgeTargetNotReadyFailureClass {
			t.Fatalf("failure class = %q (found=%v), want the non-counting class when the anchor is unknown", class, ok)
		}
	})

	t.Run("a classless probe fault passes through unchanged and counting", func(t *testing.T) {
		t.Parallel()
		probeFault := classlessProbeFault{}
		err := handleDeployableUnitWithWriteErr(t, probeFault, pastBound, pastBound)
		if class, ok := failureClassOf(err); ok {
			t.Fatalf("probe fault failure class = %q, want none (fail closed, counting)", class)
		}
		if !errors.Is(err, probeFault) {
			t.Fatalf("Handle() error = %v, want the probe fault returned unchanged", err)
		}
	})
}

// TestBoundSharedEdgeTargetDeferralLogsTheBoundTrip pins the operator signal
// for a bound expiry: one WARN naming the domain, scope, generation, elapsed
// wait, bound, and the sample repository and intent, and never attempt_count,
// which the non-counting class froze.
func TestBoundSharedEdgeTargetDeferralLogsTheBoundTrip(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	now := time.Now().UTC()
	intent := deployableUnitIntent("edge-api")
	intent.CycleStartedAt = now.Add(-crossscope.ProducerReadinessMaxWait - time.Minute)

	wrapped := fmt.Errorf("write deployable unit correlation edges: %w", sharedEdgeTargetMissStandIn{})
	if got := boundSharedEdgeTargetDeferral(wrapped, intent, now, logger); got == wrapped {
		t.Fatal("boundSharedEdgeTargetDeferral() returned the deferral unchanged past the bound")
	}
	out := buf.String()
	for _, want := range []string{
		"level=WARN",
		"shared edge target absent past the wait bound",
		"domain=deployable_unit_correlation",
		"scope_id=repository:test-scope",
		"generation_id=generation-1",
		"elapsed_since_cycle_start=",
		"max_wait=30m0s",
		"sample_repo_id=repo-edge-api",
		"sample_intent_id=i-1",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("bound-trip WARN missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "attempt") {
		t.Fatalf("bound-trip WARN names attempt_count, which the non-counting class froze:\n%s", out)
	}

	buf.Reset()
	intent.CycleStartedAt = now.Add(-time.Minute)
	if got := boundSharedEdgeTargetDeferral(wrapped, intent, now, logger); got != wrapped {
		t.Fatalf("boundSharedEdgeTargetDeferral() inside the bound = %v, want the deferral unchanged", got)
	}
	if buf.Len() != 0 {
		t.Fatalf("inside the bound logged %q, want nothing (the writer already WARNs each miss)", buf.String())
	}
}

// TestDeployableUnitCorrelationHandlerLogsBoundExpiryToItsLogger pins the
// operator signal's destination (#7268 review): the bound-expiry WARN must land
// on the handler's configured Logger, the structured reducer logger main.go
// wires, and not on slog.Default(), which nothing in production configures.
func TestDeployableUnitCorrelationHandlerLogsBoundExpiryToItsLogger(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	pastBound := time.Now().UTC().Add(-crossscope.ProducerReadinessMaxWait - time.Minute)

	handleDeployableUnitWithLogger(t, sharedEdgeTargetMissStandIn{}, pastBound, pastBound, logger)

	out := buf.String()
	for _, want := range []string{
		"level=WARN",
		"shared edge target absent past the wait bound",
		"domain=deployable_unit_correlation",
		"sample_repo_id=repo-edge-api",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("handler logger missing %q:\n%s", want, out)
		}
	}
}
