// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package securityalerts

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

// githubAlertsDisabledMessage is the lowercase fragment of the message GitHub
// returns with a 403 for a repository whose Dependabot alerts are switched
// off ("Dependabot alerts are disabled for this repository."). The REST
// reference lists 403 for the endpoint without naming a cause, so the match
// rests on that message text; a body that does not carry it is treated as an
// ordinary auth denial.
const githubAlertsDisabledMessage = "dependabot alerts are disabled"

// readGitHubErrorBody reads at most maxGitHubErrorBodyBytes of a failed
// response body. It reports ok=false when the body could not be read in full
// or holds more than the cap, so a cut-off body is never interpreted.
func readGitHubErrorBody(body io.Reader) (data []byte, ok bool) {
	data, err := io.ReadAll(io.LimitReader(body, maxGitHubErrorBodyBytes+1))
	if err != nil || len(data) > maxGitHubErrorBodyBytes {
		return nil, false
	}
	return data, true
}

// githubBodySaysAlertsDisabled reports whether a complete JSON error body has
// a string "message" naming disabled Dependabot alerts. The message text is
// only compared, never stored or returned.
func githubBodySaysAlertsDisabled(data []byte) bool {
	var envelope struct {
		Message any `json:"message"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return false
	}
	message, ok := envelope.Message.(string)
	return ok && strings.Contains(strings.ToLower(message), githubAlertsDisabledMessage)
}

// markAlertsDisabled sets AlertsDisabled when the failure is a 403 that no
// rate-limit signal explains and its complete JSON body says Dependabot alerts
// are disabled (#8805). The class stays auth_denied, so repository-scope
// targets keep the terminal failure; only the org fan-out reads the marker. A
// body that is unreadable, cut off, not JSON, or worded differently leaves it
// false. The bodyComplete guard is redundant with readGitHubErrorBody, which
// returns no data for an incomplete body, and is kept so the contract reads
// from this function alone.
func (e *GitHubDependabotError) markAlertsDisabled(body []byte, bodyComplete bool) {
	if e.StatusCode != http.StatusForbidden || !bodyComplete ||
		e.FailureClass() != GitHubDependabotFailureAuthDenied {
		return
	}
	e.AlertsDisabled = githubBodySaysAlertsDisabled(body)
}
