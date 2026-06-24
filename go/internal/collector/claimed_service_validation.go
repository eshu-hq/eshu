package collector

import (
	"errors"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// validateClaimedGeneration verifies that the source-produced generation
// matches the claimed work item's identity (scope, source system, collector
// kind, generation id) before it is committed. The terraform-state collector
// resolves its generation identity at commit time, so it validates the
// generation against its scope and requires a non-blank freshness hint instead
// of comparing generation ids directly.
func validateClaimedGeneration(item workflow.WorkItem, collected CollectedGeneration) error {
	if collected.Scope.ScopeID != item.ScopeID {
		return fmt.Errorf("claimed scope_id %q produced scope_id %q", item.ScopeID, collected.Scope.ScopeID)
	}
	if collected.Scope.SourceSystem != item.SourceSystem {
		return fmt.Errorf("claimed source_system %q produced source_system %q", item.SourceSystem, collected.Scope.SourceSystem)
	}
	if collected.Scope.CollectorKind != item.CollectorKind {
		return fmt.Errorf("claimed collector_kind %q produced collector_kind %q", item.CollectorKind, collected.Scope.CollectorKind)
	}
	if item.CollectorKind == scope.CollectorTerraformState {
		if err := collected.Generation.ValidateForScope(collected.Scope); err != nil {
			return fmt.Errorf("validate claimed terraform state generation: %w", err)
		}
		if strings.TrimSpace(collected.Generation.FreshnessHint) == "" {
			return fmt.Errorf("claimed terraform state generation freshness hint must not be blank")
		}
		return nil
	}
	if collected.Generation.GenerationID != item.GenerationID {
		return fmt.Errorf("claimed generation_id %q produced generation_id %q", item.GenerationID, collected.Generation.GenerationID)
	}
	if collected.Generation.GenerationID != item.SourceRunID {
		return fmt.Errorf("claimed source_run_id %q produced generation_id %q", item.SourceRunID, collected.Generation.GenerationID)
	}
	return nil
}

// withFailure stamps a failure class and message onto a claim mutation so the
// control store records why the claim failed.
func withFailure(mutation workflow.ClaimMutation, failureClass string, err error) workflow.ClaimMutation {
	mutation.FailureClass = failureClass
	if err != nil {
		mutation.FailureMessage = err.Error()
	}
	return mutation
}

// classifiedFailure is implemented by errors that carry their own bounded
// failure class, letting a collector override the runner's default class.
type classifiedFailure interface {
	FailureClass() string
}

// terminalFailure is implemented by errors that must route to FailClaimTerminal
// rather than being retried.
type terminalFailure interface {
	TerminalFailure() bool
}

// classifiedFailureClass returns the error's own failure class when it
// implements classifiedFailure with a non-blank value, otherwise the fallback.
func classifiedFailureClass(err error, fallback string) string {
	var classified classifiedFailure
	if errors.As(err, &classified) {
		if value := strings.TrimSpace(classified.FailureClass()); value != "" {
			return value
		}
	}
	return fallback
}

// isTerminalFailure reports whether err (or a wrapped cause) requests terminal
// failure handling.
func isTerminalFailure(err error) bool {
	var terminal terminalFailure
	return errors.As(err, &terminal) && terminal.TerminalFailure()
}

// drainHeartbeatError non-blockingly reads a pending heartbeat-loop error, if
// any, so the caller can surface a background heartbeat failure before
// completing the claim.
func drainHeartbeatError(errc <-chan error) error {
	select {
	case err := <-errc:
		return err
	default:
		return nil
	}
}
