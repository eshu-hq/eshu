// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package demo

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// The demo overlay binds ${ESHU_DEMO_BIND_ADDR:-127.0.0.1} with
// ${ESHU_DEMO_API_PORT:-18080} and ${ESHU_DEMO_MCP_PORT:-18091}. These read
// the same variables so a second demo that moves its ports to avoid the first
// one's is still probed where it actually listens.
//
// They are deliberately not 8080/8081: the demo runs its own Compose project
// so it never touches the operator's default stack, and an earlier draft
// hardcoded the default ports, which meant readiness read whatever was already
// listening there.
const (
	defaultAPIPort  = "18080"
	defaultMCPPort  = "18091"
	defaultBindAddr = "127.0.0.1"
)

// Environment variables the caller's lookup function is asked for. They are
// named here so an operator can grep for them, and passed in as a lookup
// rather than read from the process so this package stays free of process
// state.
const (
	// EnvBindAddr overrides the host the demo stack is probed on.
	EnvBindAddr = "ESHU_DEMO_BIND_ADDR"
	// EnvAPIPort overrides the published HTTP API port.
	EnvAPIPort = "ESHU_DEMO_API_PORT"
	// EnvMCPPort overrides the published MCP port.
	EnvMCPPort = "ESHU_DEMO_MCP_PORT"
	// EnvComposeFile overrides the overlay search in ResolveComposeFile.
	EnvComposeFile = "ESHU_DEMO_COMPOSE_FILE"
)

// APIBase is the demo stack's HTTP API base URL. getenv is the caller's
// environment lookup, normally os.Getenv.
func APIBase(getenv func(string) string) string { return base(getenv, defaultAPIPort, EnvAPIPort) }

// MCPBase is the demo stack's MCP base URL. getenv is the caller's
// environment lookup, normally os.Getenv.
func MCPBase(getenv func(string) string) string { return base(getenv, defaultMCPPort, EnvMCPPort) }

func base(getenv func(string) string, defaultPort, portEnv string) string {
	host := getenv(EnvBindAddr)
	if strings.TrimSpace(host) == "" {
		host = defaultBindAddr
	}
	port := getenv(portEnv)
	if strings.TrimSpace(port) == "" {
		port = defaultPort
	}
	return "http://" + host + ":" + port
}

// ResolveComposeFile walks up from dir looking for the overlay, so an
// installed binary invoked outside the repository root still finds it.
// EnvComposeFile, read through the caller's getenv, overrides the search
// entirely.
func ResolveComposeFile(dir string, getenv func(string) string) (string, error) {
	// ESHU_DEMO_COMPOSE_FILE is an operator-set override for where their own
	// demo overlay lives, the same trust level as the working directory this
	// function otherwise searches. The value is handed to docker compose -f,
	// never opened or included by this process, and an operator who can set it
	// can already run docker directly.
	if override := strings.TrimSpace(getenv(EnvComposeFile)); override != "" {
		return override, nil
	}
	current := dir
	for {
		candidate := filepath.Join(current, ComposeFileName)
		// #nosec G703 -- candidate is the walked directory plus a constant
		// filename; Stat only tests existence and the path is never opened here
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", fmt.Errorf(
				"could not find %s in %s or any parent directory\n"+
					"run eshu demo from an Eshu checkout, or set ESHU_DEMO_COMPOSE_FILE to the overlay path",
				ComposeFileName, dir)
		}
		current = parent
	}
}

// IndexStatus is the subset of /api/v0/status/index the demo waits on.
// Readiness is indexing completeness, never process health: a stack that is
// merely "up" answers the five demo questions wrongly or not at all.
type IndexStatus struct {
	// Status is the service's own health verdict ("healthy" when settled).
	Status string `json:"status"`
	// RepositoryCount is how many repositories are indexed so far.
	RepositoryCount int `json:"repository_count"`
	// Queue carries the outstanding backlog. Readiness is "no work left",
	// which is what distinguishes a settled stack from a merely running one.
	Queue struct {
		Outstanding int `json:"outstanding"`
	} `json:"queue"`
}

// Complete reports whether the demo can answer correctly.
//
// These field names are read from the live response, not assumed: the route
// returns status/repository_count/queue, and an earlier draft looked for
// "complete" and "repositories", which do not exist. It therefore reported
// zero repositories forever against a healthy stack.
//
// Ready means the service calls itself healthy, at least one repository is
// indexed, and no queued work remains. Dropping the queue check would let the
// demo ask its question mid-projection and get a thin answer.
func (s IndexStatus) Complete() bool {
	return s.Status == "healthy" && s.RepositoryCount > 0 && s.Queue.Outstanding == 0
}

// probeIndexStatus is the production readiness seam.
//
// waitReady swallows a probe error and reports the last observed status
// instead, so these errors only surface through Status. Wrapping them would
// change that message without telling the operator anything new.
//
//nolint:wrapcheck // transport errors reach the operator verbatim through Status
func probeIndexStatus(ctx context.Context, apiBase, apiKey string) (IndexStatus, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiBase+"/api/v0/status/index", nil)
	if err != nil {
		return IndexStatus{}, err
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return IndexStatus{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return IndexStatus{}, fmt.Errorf("index status: HTTP %d", resp.StatusCode)
	}
	var status IndexStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return IndexStatus{}, fmt.Errorf("decode index status: %w", err)
	}
	return status, nil
}
