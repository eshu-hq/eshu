package jira

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// FailureClass is a bounded workflow retry class for Jira provider failures.
type FailureClass = string

const (
	// FailurePermissionHidden marks Jira permission or issue-security denials.
	FailurePermissionHidden FailureClass = "permission_hidden"
	// FailureDeleted marks missing or deleted Jira issues and sites.
	FailureDeleted FailureClass = "deleted"
	// FailureArchived marks archived Jira projects or issues.
	FailureArchived FailureClass = "archived"
	// FailureRateLimited marks Jira rate limiting as retryable.
	FailureRateLimited FailureClass = "rate_limited"
	// FailureRetryable marks transient transport or provider failures.
	FailureRetryable FailureClass = "retryable"
	// FailureTerminal marks malformed or otherwise non-retryable failures.
	FailureTerminal FailureClass = "terminal"
)

// ErrArchivedIssue marks Jira archived issue or project responses when the
// provider exposes the state without a distinct status code.
var ErrArchivedIssue = errors.New("jira issue is archived")

// JiraError is a bounded provider error. It deliberately omits tokens, URLs,
// and raw response bodies from Error().
type JiraError struct {
	StatusCode      int
	Message         string
	RetryAfter      time.Duration
	RateLimitReason string
	Cause           error
}

// Error returns a bounded provider failure message safe for logs and status.
func (e JiraError) Error() string {
	message := strings.TrimSpace(e.Message)
	if message == "" {
		message = "jira request failed"
	}
	if e.StatusCode != 0 {
		return fmt.Sprintf("%s: status %d", message, e.StatusCode)
	}
	return message
}

// Unwrap returns the underlying transport or decode cause when present.
func (e JiraError) Unwrap() error {
	return e.Cause
}

// FailureClass returns the bounded workflow retry class for this provider
// failure.
func (e JiraError) FailureClass() string {
	switch {
	case e.StatusCode == http.StatusUnauthorized || e.StatusCode == http.StatusForbidden:
		return FailurePermissionHidden
	case e.StatusCode == http.StatusNotFound:
		return FailureDeleted
	case e.StatusCode == http.StatusGone:
		return FailureArchived
	case e.StatusCode == http.StatusTooManyRequests:
		return FailureRateLimited
	case e.StatusCode >= http.StatusInternalServerError:
		return FailureRetryable
	case e.StatusCode != 0:
		return FailureTerminal
	default:
		return FailureRetryable
	}
}

// TerminalFailure reports whether workflow should stop retrying the current
// work item until configuration or provider state changes.
func (e JiraError) TerminalFailure() bool {
	switch e.FailureClass() {
	case FailurePermissionHidden, FailureDeleted, FailureArchived, FailureTerminal:
		return true
	default:
		return false
	}
}

// ProviderFailure is a bounded Jira provider failure returned to claim
// handling.
type ProviderFailure struct {
	failureClass string
	terminal     bool
	cause        error
}

// Error returns a bounded failure string safe for logs and status.
func (f ProviderFailure) Error() string {
	if f.failureClass == "" {
		return "jira provider failure"
	}
	return "jira provider failure: " + f.failureClass
}

// Unwrap returns the underlying provider error.
func (f ProviderFailure) Unwrap() error {
	return f.cause
}

// FailureClass returns the workflow retry class for this provider failure.
func (f ProviderFailure) FailureClass() string {
	if f.failureClass == "" {
		return FailureRetryable
	}
	return f.failureClass
}

// TerminalFailure reports whether workflow should stop retrying this claim.
func (f ProviderFailure) TerminalFailure() bool {
	return f.terminal
}

func classifiedProviderFailure(err error) ProviderFailure {
	if errors.Is(err, ErrArchivedIssue) {
		return ProviderFailure{failureClass: FailureArchived, terminal: true, cause: err}
	}
	var jiraErr JiraError
	if errors.As(err, &jiraErr) {
		return ProviderFailure{
			failureClass: jiraErr.FailureClass(),
			terminal:     jiraErr.TerminalFailure(),
			cause:        err,
		}
	}
	return ProviderFailure{failureClass: FailureRetryable, cause: err}
}

// PartialCollectionError preserves bounded collection counters when a Jira
// fetch fails after accepting part of a page set.
type PartialCollectionError struct {
	Stage string
	Stats CollectionStats
	Cause error
}

// Error returns a bounded failure string safe for logs and status.
func (e PartialCollectionError) Error() string {
	stage := strings.TrimSpace(e.Stage)
	if stage == "" {
		stage = "collection"
	}
	return "jira partial collection failure: " + stage
}

// Unwrap returns the underlying provider or transport error.
func (e PartialCollectionError) Unwrap() error {
	return e.Cause
}
