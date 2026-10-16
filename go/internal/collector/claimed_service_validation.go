// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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

// withFailure stamps a failure class and message plus the bounded structured
// classification (operation, status code, terminal flag) onto a claim
// mutation so the control store records why the claim failed in queryable
// form. operation is the claim-stage fallback (for example "collect_failure"
// or "identity_mismatch"); an error-carried operation token wins when
// present. failureClass is the already-resolved class. terminal is the route
// truth (true for FailClaimTerminal callers); the error's own terminal flag
// is OR-ed in so the persisted flag never understates a terminal cause.
// Only bounded values persist: the sanitized operation token, the numeric
// status, the class, and the message from err.Error, which stays bounded for
// provider failures. No URL, response body, or credential is added here.
func withFailure(
	mutation workflow.ClaimMutation,
	operation string,
	failureClass string,
	terminal bool,
	err error,
) workflow.ClaimMutation {
	mutation.FailureClass = failureClass
	if err != nil {
		mutation.FailureMessage = err.Error()
	}
	mutation.FailureOperation = failureOperationOf(err, operation)
	mutation.FailureStatusCode = failureStatusCodeOf(err)
	mutation.FailureTerminal = terminal || isTerminalFailure(err)
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

// failureOperation is implemented by errors that carry their own bounded
// provider operation token. It is matched structurally so the collector
// package does not import the SDK that defines the concrete failure type.
type failureOperation interface {
	FailureOperation() string
}

// failureStatusCode is implemented by errors that carry their own bounded
// provider HTTP status code. It is matched structurally for the same reason.
type failureStatusCode interface {
	FailureStatusCode() int
}

// maxFailureOperationLength bounds a claim-stage operation before it is
// persisted, mirroring the SDK token bound.
const maxFailureOperationLength = 64

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

// failureOperationOf returns the error's own bounded operation token when it
// implements failureOperation with a non-blank value, otherwise the
// sanitized claim-stage fallback. The result is always safe for status rows:
// a sanitized [a-z0-9_] token of bounded length.
func failureOperationOf(err error, fallback string) string {
	var carried failureOperation
	if errors.As(err, &carried) {
		if value := sanitizeFailureOperation(carried.FailureOperation()); value != "" {
			return value
		}
	}
	return sanitizeFailureOperation(fallback)
}

// failureStatusCodeOf returns the error's bounded HTTP status code when it
// implements failureStatusCode, otherwise 0 for unknown. Only 100-599
// survives; anything else is reported as unknown.
func failureStatusCodeOf(err error) int {
	var carried failureStatusCode
	if errors.As(err, &carried) {
		if status := carried.FailureStatusCode(); status >= 100 && status <= 599 {
			return status
		}
	}
	return 0
}

// sanitizeFailureOperation reduces an operation token to lowercase [a-z0-9_]
// of bounded length so claim-stage fallbacks obey the same redaction bound
// as error-carried tokens: no URLs, response bodies, or credentials. Blank
// input stays blank (unrecorded); non-blank input with no surviving token
// becomes "unknown_operation".
func sanitizeFailureOperation(operation string) string {
	trimmed := strings.TrimSpace(operation)
	if trimmed == "" {
		return ""
	}
	lowered := strings.ToLower(trimmed)
	var builder strings.Builder
	builder.Grow(len(lowered))
	for _, r := range lowered {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' {
			builder.WriteRune(r)
		} else {
			builder.WriteByte('_')
		}
	}
	token := strings.Trim(builder.String(), "_")
	if token == "" {
		return "unknown_operation"
	}
	if len(token) > maxFailureOperationLength {
		token = strings.Trim(token[:maxFailureOperationLength], "_")
		if token == "" {
			return "unknown_operation"
		}
	}
	return token
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
