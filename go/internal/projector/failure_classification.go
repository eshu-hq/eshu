package projector

import (
	"context"
	"errors"
	"fmt"
	"net"
	"reflect"
	"regexp"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/queue"
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// FailureClass is the stable high-level category for resolution and projection
// failures. These values are persisted to the durable work queue.
type FailureClass string

const (
	FailureClassInputInvalid          FailureClass = "input_invalid"
	FailureClassProjectionBug         FailureClass = "projection_bug"
	FailureClassDependencyUnavailable FailureClass = "dependency_unavailable"
	FailureClassResourceExhausted     FailureClass = "resource_exhausted"
	FailureClassTimeout               FailureClass = "timeout"
	FailureClassUnknown               FailureClass = "unknown"
)

// RetryDisposition is the operator-facing retry guidance for one failed work
// item.
type RetryDisposition string

const (
	RetryDispositionRetryable    RetryDisposition = "retryable"
	RetryDispositionNonRetryable RetryDisposition = "non_retryable"
	RetryDispositionManualReview RetryDisposition = "manual_review"
)

// FailureClassification is classified failure metadata ready for durable queue
// persistence.
type FailureClassification struct {
	FailureStage      string
	ErrorClass        string
	FailureClass      FailureClass
	FailureCode       string
	RetryDisposition  RetryDisposition
	RetryAfterSeconds int
}

const neo4jTransientCodePrefix = "Neo.TransientError."
const neo4jTransientRetrySeconds = 15

// StageError wraps one error with the projection stage that raised it.
type StageError struct {
	Stage string
	Cause error
}

func (e *StageError) Error() string {
	return fmt.Sprintf("%s: %s", e.Stage, e.Cause)
}

func (e *StageError) Unwrap() error {
	return e.Cause
}

// NewStageError wraps an error with a projection stage name.
func NewStageError(stage string, cause error) *StageError {
	return &StageError{Stage: stage, Cause: cause}
}

// InputValidationError represents invalid input that should not be retried.
type InputValidationError struct {
	Message string
}

func (e *InputValidationError) Error() string {
	return e.Message
}

// NewInputValidationError creates an input validation error.
func NewInputValidationError(msg string) *InputValidationError {
	return &InputValidationError{Message: msg}
}

// ResourceExhaustedError represents resource exhaustion (e.g. OOM).
type ResourceExhaustedError struct {
	Message string
}

func (e *ResourceExhaustedError) Error() string {
	return e.Message
}

// NewResourceExhaustedError creates a resource exhaustion error.
func NewResourceExhaustedError(msg string) *ResourceExhaustedError {
	return &ResourceExhaustedError{Message: msg}
}

// ClassifyFailure maps one projection error into durable failure metadata.
func ClassifyFailure(err error, failureStage string) FailureClassification {
	resolvedStage, underlying := unwrapStageError(err, failureStage)

	// Neo4j transient errors are retryable with a backoff.
	if code := neo4jErrorCode(underlying); code != "" && strings.HasPrefix(code, neo4jTransientCodePrefix) {
		return FailureClassification{
			FailureStage:      resolvedStage,
			ErrorClass:        errorClassName(underlying),
			FailureClass:      FailureClassDependencyUnavailable,
			FailureCode:       normalizeNeo4jCode(code),
			RetryDisposition:  RetryDispositionRetryable,
			RetryAfterSeconds: neo4jTransientRetrySeconds,
		}
	}

	// Context deadline exceeded or canceled — treat as timeout.
	if errors.Is(underlying, context.DeadlineExceeded) || errors.Is(underlying, context.Canceled) {
		return FailureClassification{
			FailureStage:     resolvedStage,
			ErrorClass:       errorClassName(underlying),
			FailureClass:     FailureClassTimeout,
			FailureCode:      failureCode(underlying),
			RetryDisposition: RetryDispositionRetryable,
		}
	}

	// Input validation errors are non-retryable.
	var inputErr *InputValidationError
	if errors.As(underlying, &inputErr) {
		return FailureClassification{
			FailureStage:     resolvedStage,
			ErrorClass:       errorClassName(underlying),
			FailureClass:     FailureClassInputInvalid,
			FailureCode:      failureCode(underlying),
			RetryDisposition: RetryDispositionNonRetryable,
		}
	}

	// Network errors are retryable dependency failures.
	var netErr *net.OpError
	if errors.As(underlying, &netErr) {
		return FailureClassification{
			FailureStage:     resolvedStage,
			ErrorClass:       errorClassName(underlying),
			FailureClass:     FailureClassDependencyUnavailable,
			FailureCode:      failureCode(underlying),
			RetryDisposition: RetryDispositionRetryable,
		}
	}

	// Resource exhaustion requires manual review.
	var resourceErr *ResourceExhaustedError
	if errors.As(underlying, &resourceErr) {
		return FailureClassification{
			FailureStage:     resolvedStage,
			ErrorClass:       errorClassName(underlying),
			FailureClass:     FailureClassResourceExhausted,
			FailureCode:      failureCode(underlying),
			RetryDisposition: RetryDispositionManualReview,
		}
	}

	// Default: projection bug, requires manual review.
	return FailureClassification{
		FailureStage:     resolvedStage,
		ErrorClass:       errorClassName(underlying),
		FailureClass:     FailureClassProjectionBug,
		FailureCode:      failureCode(underlying),
		RetryDisposition: RetryDispositionManualReview,
	}
}

// ToFailureRecord converts a FailureClassification into a queue.FailureRecord
// suitable for durable persistence.
func (fc FailureClassification) ToFailureRecord(message string) queue.FailureRecord {
	details := fmt.Sprintf(
		"stage=%s class=%s code=%s disposition=%s",
		fc.FailureStage, fc.FailureClass, fc.FailureCode, fc.RetryDisposition,
	)
	return queue.FailureRecord{
		FailureClass: string(fc.FailureClass),
		Message:      message,
		Details:      details,
	}
}

// unwrapStageError returns the underlying error and the best-known failure
// stage. If the error is a StageError, the stage from the wrapper is used.
func unwrapStageError(err error, fallbackStage string) (string, error) {
	var stageErr *StageError
	if errors.As(err, &stageErr) {
		return stageErr.Stage, stageErr.Cause
	}
	return fallbackStage, err
}

// neo4jErrorCode returns the Neo4j server error code if the error (or any
// wrapped error) is a *neo4j.Neo4jError from the driver.
func neo4jErrorCode(err error) string {
	var neo4jErr *neo4jdriver.Neo4jError
	if errors.As(err, &neo4jErr) {
		code := strings.TrimSpace(neo4jErr.Code)
		if code != "" {
			return code
		}
	}
	return ""
}

var camelBoundary = regexp.MustCompile(`([a-z0-9])([A-Z])`)

// normalizeNeo4jCode converts a dotted Neo4j error code like
// "Neo.TransientError.Transaction.DeadlockDetected" into a snake_case string.
func normalizeNeo4jCode(code string) string {
	normalized := strings.ReplaceAll(code, ".", "__")
	normalized = camelBoundary.ReplaceAllString(normalized, "${1}_${2}")
	normalized = strings.ToLower(normalized)
	return normalized
}

// errorClassName returns the Go type name for an error value.
func errorClassName(err error) string {
	t := reflect.TypeOf(err)
	if t == nil {
		return "nil"
	}
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	return t.Name()
}

// failureCode returns a stable snake_case identifier derived from the error's
// type name.
func failureCode(err error) string {
	t := reflect.TypeOf(err)
	if t == nil {
		return "nil_error"
	}
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	// Use package path + name for uniqueness.
	name := t.Name()
	if name == "" {
		// For anonymous types, use the string representation.
		name = t.String()
	}
	pkg := t.PkgPath()
	if pkg != "" {
		// Use only the last segment of the package path.
		parts := strings.Split(pkg, "/")
		name = parts[len(parts)-1] + "_" + name
	}

	// Convert camelCase and dots to snake_case.
	result := camelBoundary.ReplaceAllString(name, "${1}_${2}")
	result = strings.ReplaceAll(result, ".", "_")
	result = strings.ReplaceAll(result, "*", "")
	result = strings.ToLower(result)
	return result
}
