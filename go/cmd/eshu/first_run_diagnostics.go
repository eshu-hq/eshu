// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import "strings"

// attachFirstRunDiagnostic classifies a failure signal and, when a class
// matches, attaches the diagnostic to the result. The underlying error remains
// on the failing step and on runErr, so attaching a diagnosis never hides the
// root cause; an unmatched signal leaves the raw error to speak for itself.
func attachFirstRunDiagnostic(result firstRunResult, signal onboardingSignal) firstRunResult {
	if diagnostic, ok := classifyOnboardingFailure(signal); ok {
		result.Diagnostic = &diagnostic
	}
	return result
}

// firstRunVerifySignal builds the classifier input for a failed runtime-verify
// step. It carries the detected shape, any missing required binaries, the
// resolved MCP endpoint for the API-vs-MCP heuristic, and the preserved verify
// error so a matched diagnostic still surfaces the root cause.
func firstRunVerifySignal(deps firstRunDeps, detection firstRunRuntimeDetection, baseURL string, verifyErr error) onboardingSignal {
	composeDetected := detection.ComposeFile != "" || detection.Shape == firstRunShapeDockerCompose
	signal := onboardingSignal{
		Step:            onboardingStepVerify,
		Shape:           detection.Shape,
		Underlying:      verifyErr,
		RuntimeFailed:   true,
		RuntimeDetail:   detection.Detail,
		ComposeDetected: composeDetected,
		MCPEndpoint:     resolveFirstRunMCPEndpoint(),
		APIBaseURL:      baseURL,
	}
	// Missing binaries only diagnose the local-binaries path. When Compose is the
	// chosen or detected runtime, absent eshu-* binaries on the host are expected
	// and must not mask the real compose-unhealthy cause.
	if !composeDetected {
		signal.MissingBinaries = firstRunMissingBinaries(deps.Probe)
	}
	return signal
}

// firstRunReadinessSignal builds the classifier input for a failed readiness or
// index step. The queue snapshot distinguishes failed/dead-letter work from an
// index that is merely still building, and the readiness verdict supplies the
// stable reason. The underlying error is preserved verbatim.
func firstRunReadinessSignal(deps firstRunDeps, client *APIClient, detection firstRunRuntimeDetection, indexed firstRunIndexOutcome, runErr error) onboardingSignal {
	composeShape := detection.Shape == firstRunShapeDockerCompose || detection.ComposeFile != ""
	signal := onboardingSignal{
		Step:           onboardingStepReadiness,
		Shape:          detection.Shape,
		Underlying:     runErr,
		RepoPathDenied: composeShape && indexErrorLooksLikePathVisibility(runErr),
	}
	status, verdict, ok := firstRunReadinessStatus(deps, client)
	if ok {
		signal.Queue = status.Queue
		signal.Readiness = verdict
	}
	// Reflect a known terminal/building state even when the live status fetch is
	// unavailable, so the readiness completeness label still informs the class.
	if !ok && strings.EqualFold(indexed.Completeness, "failed") {
		signal.Readiness = scanReadinessVerdict{Terminal: true, Reason: indexed.Readiness}
	}
	return signal
}

// firstRunReadinessStatus fetches the live pipeline status and readiness verdict
// for diagnosis. It returns ok=false when the status seam is unavailable or the
// fetch fails, so the classifier falls back to the preserved error.
func firstRunReadinessStatus(deps firstRunDeps, client *APIClient) (scanPipelineStatus, scanReadinessVerdict, bool) {
	if deps.FetchStatus == nil {
		return scanPipelineStatus{}, scanReadinessVerdict{}, false
	}
	status, err := deps.FetchStatus(client)
	if err != nil {
		return scanPipelineStatus{}, scanReadinessVerdict{}, false
	}
	return status, evaluateScanReadiness(status), true
}

// firstRunQuerySignal builds the classifier input for a failed bounded query.
// An HTTP 401/403 maps to the auth-mismatch class; the underlying error is
// preserved for any class and for the unmatched fallback.
func firstRunQuerySignal(queryErr error) onboardingSignal {
	return onboardingSignal{
		Step:       onboardingStepQuery,
		Underlying: queryErr,
	}
}

// firstRunEmptyRepoSignal builds the classifier input for a successful query
// that returned zero repositories. It is advisory: the run still succeeded, but
// the operator has nothing to query, so the no-repositories diagnosis guides the
// next action.
func firstRunEmptyRepoSignal() onboardingSignal {
	return onboardingSignal{
		Step:          onboardingStepQuery,
		EmptyRepoList: true,
		Underlying:    nil,
	}
}

// indexErrorLooksLikePathVisibility reports whether an index/bootstrap error
// reads like the repository path not being visible to the runtime (typically a
// Docker volume that was not mounted). The match is conservative and only
// supplements the preserved error; it never replaces it.
func indexErrorLooksLikePathVisibility(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, marker := range []string{
		"no such file or directory",
		"not visible inside",
		"not a directory",
		"cannot access",
		"mount",
	} {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
}

// isEmptyRepositoriesAnswer reports whether the bounded query answer indicates
// an empty index. It matches the stable phrasing emitted by runFirstRunQuery.
func isEmptyRepositoriesAnswer(answer string) bool {
	return strings.Contains(answer, "returned 0 repositories")
}

// resolveFirstRunMCPEndpoint reads a configured MCP endpoint from the
// environment or config so the API-vs-MCP heuristic can flag a misrouted URL.
// An empty result means no endpoint is configured and the heuristic is skipped.
func resolveFirstRunMCPEndpoint() string {
	if value := strings.TrimSpace(resolveConfigValue("ESHU_MCP_URL", "")); value != "" {
		return value
	}
	return strings.TrimSpace(resolveConfigValue("ESHU_MCP_ENDPOINT", ""))
}
