// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package firstrun

import (
	"fmt"
	"net/http"
	"path/filepath"
	"time"
)

// RuntimeProbe is the set of injectable seams the runtime-detection
// step uses so it can be unit-tested without a live host, network, or PATH.
type RuntimeProbe struct {
	// APIHealthy reports whether an Eshu API answers a bounded /health check at
	// the given base URL. It must not treat a transport error as healthy.
	APIHealthy func(baseURL string) bool
	// LookPath resolves a binary on PATH, mirroring exec.LookPath semantics.
	LookPath func(file string) (string, error)
	// FileExists reports whether a path is present on disk.
	FileExists func(path string) bool
}

// firstRunRuntimeDetection is the resolved runtime shape plus the evidence used
// to choose it, so the summary can explain the decision truthfully.
type firstRunRuntimeDetection struct {
	Shape        RuntimeShape
	APIReachable bool
	Detail       string
	ComposeFile  string
}

// firstRunRequiredBinaries lists the binaries the local-binaries shape needs in
// order to index a repository and serve a bounded query.
var firstRunRequiredBinaries = []string{"eshu-bootstrap-index", "eshu-api"}

// detectFirstRunRuntime resolves the smallest truthful runtime shape. It prefers
// an already-reachable API (no start needed), then local binaries on PATH, then
// a Docker Compose stack discovered under workspaceRoot. It never claims a shape
// it cannot back with evidence.
func detectFirstRunRuntime(probe RuntimeProbe, baseURL, workspaceRoot string) firstRunRuntimeDetection {
	if probe.APIHealthy != nil && probe.APIHealthy(baseURL) {
		return firstRunRuntimeDetection{
			Shape:        ShapeExistingAPI,
			APIReachable: true,
			Detail:       fmt.Sprintf("API reachable at %s", baseURL),
		}
	}

	if missing := firstRunMissingBinaries(probe); len(missing) == 0 {
		return firstRunRuntimeDetection{
			Shape:  ShapeLocalBinaries,
			Detail: "local eshu binaries found on PATH",
		}
	}

	if composeFile := firstRunComposeFile(probe, workspaceRoot); composeFile != "" {
		return firstRunRuntimeDetection{
			Shape:       ShapeDockerCompose,
			Detail:      fmt.Sprintf("docker compose file found: %s", composeFile),
			ComposeFile: composeFile,
		}
	}

	return firstRunRuntimeDetection{
		Shape:  ShapeUnknown,
		Detail: "no reachable API, no local eshu binaries on PATH, and no docker compose file",
	}
}

// firstRunMissingBinaries returns the required binaries that are not on PATH.
func firstRunMissingBinaries(probe RuntimeProbe) []string {
	if probe.LookPath == nil {
		return append([]string(nil), firstRunRequiredBinaries...)
	}
	var missing []string
	for _, bin := range firstRunRequiredBinaries {
		if _, err := probe.LookPath(bin); err != nil {
			missing = append(missing, bin)
		}
	}
	return missing
}

// firstRunComposeFile returns the first docker-compose file present at the
// workspace root, or "" when none is found.
func firstRunComposeFile(probe RuntimeProbe, workspaceRoot string) string {
	if probe.FileExists == nil || workspaceRoot == "" {
		return ""
	}
	for _, name := range []string{"docker-compose.yaml", "docker-compose.yml", "compose.yaml", "compose.yml"} {
		candidate := filepath.Join(workspaceRoot, name)
		if probe.FileExists(candidate) {
			return candidate
		}
	}
	return ""
}

// APIHealthy performs a bounded /health check. A non-2xx status or any
// transport error is reported as not healthy; readiness is never inferred from
// the absence of a check.
func APIHealthy(baseURL string) bool {
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(baseURL + "/health")
	if err != nil {
		return false
	}
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}

// verifyFirstRunRuntime confirms the runtime is usable for the chosen shape. It
// only ever verifies; it does not start anything. Starting a runtime in this
// command is intentionally out of scope so first-run cannot mutate the host
// destructively. When the API is not reachable for a shape that needs it, the
// returned step is failed and carries actionable detail.
func verifyFirstRunRuntime(probe RuntimeProbe, detection firstRunRuntimeDetection, baseURL string, noStart bool) Step {
	switch detection.Shape {
	case ShapeExistingAPI:
		return Step{Name: "verify runtime", Status: StepOK, Detail: detection.Detail}
	case ShapeLocalBinaries:
		if probe.APIHealthy != nil && probe.APIHealthy(baseURL) {
			return Step{Name: "verify runtime", Status: StepOK, Detail: "local API reachable"}
		}
		return Step{
			Name:   "verify runtime",
			Status: StepFailed,
			Detail: firstRunStartHint(detection, baseURL, noStart, "eshu api start"),
		}
	case ShapeDockerCompose:
		if probe.APIHealthy != nil && probe.APIHealthy(baseURL) {
			return Step{Name: "verify runtime", Status: StepOK, Detail: "compose API reachable"}
		}
		return Step{
			Name:   "verify runtime",
			Status: StepFailed,
			Detail: firstRunStartHint(detection, baseURL, noStart, "docker compose up -d"),
		}
	default:
		return Step{
			Name:   "verify runtime",
			Status: StepFailed,
			Detail: detection.Detail,
		}
	}
}

// firstRunStartHint builds an actionable message explaining why a runtime is not
// yet usable and what the operator should run, without claiming first-run did
// it for them.
func firstRunStartHint(detection firstRunRuntimeDetection, baseURL string, noStart bool, command string) string {
	mode := "auto-start is not performed by first-run"
	if noStart {
		mode = "--no-start was set"
	}
	return fmt.Sprintf(
		"API not reachable at %s (%s); start the %s runtime with: %s",
		baseURL, mode, detection.Shape, command,
	)
}
