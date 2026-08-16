// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package doctor

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/cli/evidredact"
)

// serviceBinaries are the Eshu service executables `eshu doctor` reports on.
// They are the processes an operator has to be able to start by name; a
// missing one explains a stack that will not come up.
var serviceBinaries = []string{
	"eshu-api",
	"eshu-mcp-server",
	"eshu-bootstrap-index",
	"eshu-ingester",
	"eshu-reducer",
}

// healthTimeout bounds the API probe. Doctor is a diagnostic an operator runs
// while something is already wrong, so a hung endpoint must not hang the
// report -- an unreachable API is itself a finding worth printing.
const healthTimeout = 3 * time.Second

// Deps are the process facts Run reads. They are parameters rather than direct
// calls so a test can describe a broken machine without being run on one.
type Deps struct {
	// ConfigDir is the resolved Eshu config directory.
	ConfigDir string
	// EnvFilePath is the resolved path of the persisted settings file.
	EnvFilePath string
	// APIBaseURL is the base URL the health probe targets.
	APIBaseURL string
	// Neo4jURI is the configured graph Bolt URI, already resolved from the
	// environment and the settings file by the caller.
	Neo4jURI string
	// PostgresDSN is the configured Postgres DSN. Only its presence is
	// reported; the value never reaches the output.
	PostgresDSN string

	// Stat reports whether a path exists and whether it is a directory.
	// Defaults to os.Stat.
	Stat func(string) (os.FileInfo, error)
	// LookPath resolves an executable name against PATH. Defaults to
	// exec.LookPath.
	LookPath func(string) (string, error)
	// HTTPClient performs the health probe. Defaults to a client bounded by
	// healthTimeout.
	HTTPClient *http.Client
}

// Run writes the diagnostic report to out and returns nil.
//
// Every check is advisory: doctor reports what it found and does not fail the
// command, because an operator running it already knows something is wrong and
// wants the whole picture rather than the first problem.
//
// No value that can carry a credential reaches out. The Bolt URI is written
// through evidredact.Endpoint, which replaces userinfo and strips the query
// and fragment, and the Postgres DSN is reported by presence only.
func Run(out io.Writer, deps Deps) error {
	stat := deps.Stat
	if stat == nil {
		stat = os.Stat
	}
	lookPath := deps.LookPath
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	client := deps.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: healthTimeout}
	}

	_, _ = fmt.Fprintln(out, "Eshu Diagnostics")
	_, _ = fmt.Fprintln(out, strings.Repeat("-", 40))

	if info, err := stat(deps.ConfigDir); err == nil && info.IsDir() {
		_, _ = fmt.Fprintf(out, "  [ok] Config directory exists: %s\n", deps.ConfigDir)
	} else {
		_, _ = fmt.Fprintf(out, "  [!!] Config directory missing: %s\n", deps.ConfigDir)
	}

	if _, err := stat(deps.EnvFilePath); err == nil {
		_, _ = fmt.Fprintf(out, "  [ok] Config file exists: %s\n", deps.EnvFilePath)
	} else {
		_, _ = fmt.Fprintf(out, "  [!!] Config file missing: %s\n", deps.EnvFilePath)
	}

	for _, bin := range serviceBinaries {
		if path, err := lookPath(bin); err == nil {
			_, _ = fmt.Fprintf(out, "  [ok] %s found: %s\n", bin, path)
		} else {
			_, _ = fmt.Fprintf(out, "  [!!] %s not found in PATH\n", bin)
		}
	}

	reportAPIHealth(out, client, deps.APIBaseURL)

	// The Bolt URI carries its password in userinfo, so the raw value never
	// reaches the report -- see doctor_redaction_test.go for the screen.
	if deps.Neo4jURI != "" {
		_, _ = fmt.Fprintf(out, "  [ok] Neo4j URI configured: %s\n", evidredact.Endpoint(deps.Neo4jURI))
	} else {
		_, _ = fmt.Fprintf(out, "  [!!] Neo4j URI not configured (set NEO4J_URI)\n")
	}

	// Presence only. A DSN is credentials end to end, and unlike the Bolt URI
	// there is no host-shaped remainder worth showing an operator.
	if deps.PostgresDSN != "" {
		_, _ = fmt.Fprintf(out, "  [ok] Postgres DSN configured\n")
	} else {
		_, _ = fmt.Fprintf(out, "  [!!] Postgres DSN not configured (set ESHU_POSTGRES_DSN)\n")
	}

	return nil
}

// healthURL appends the /health path segment to baseURL structurally.
//
// Plain concatenation is wrong for a base URL that carries a query, which is a
// shape this package explicitly supports: "http://host/x?api_key=t" + "/health"
// yields "http://host/x?api_key=t/health", appending the segment to the query
// VALUE rather than the path. The probe then targets an endpoint that does not
// exist and doctor misreports the API as unreachable.
//
// A base URL that will not parse falls back to concatenation, trimming any
// trailing slash: an unparseable URL is going to fail the probe either way, and
// reporting that failure is more useful than reporting nothing.
func healthURL(baseURL string) string {
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Host == "" {
		return strings.TrimRight(baseURL, "/") + "/health"
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/health"
	return parsed.String()
}

// reportAPIHealth probes baseURL's /health and writes one line describing what
// happened. The base URL is written through evidredact.Endpoint because an
// operator may point the CLI at a URL carrying a token in its query string.
func reportAPIHealth(out io.Writer, client *http.Client, baseURL string) {
	safe := evidredact.Endpoint(baseURL)

	resp, err := client.Get(healthURL(baseURL)) //nolint:noctx // #nosec G704 -- baseURL is the locally-configured Eshu API endpoint, not user-supplied input.
	if err != nil {
		_, _ = fmt.Fprintf(out, "  [!!] API not reachable at %s\n", safe)
		return
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusOK {
		_, _ = fmt.Fprintf(out, "  [ok] API healthy at %s\n", safe)
		return
	}
	_, _ = fmt.Fprintf(out, "  [!!] API returned status %d at %s\n", resp.StatusCode, safe)
}
