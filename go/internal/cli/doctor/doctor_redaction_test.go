// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package doctor

import (
	"bytes"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// sentinel is planted inside a value rather than at a token boundary, so a
// screen that only matches whole fields cannot pass by accident.
const sentinel = "S3NT1NEL"

// TestRunRedactsTheBoltPassword is the regression screen for the leak this
// package was extracted to fix: `eshu doctor` printed NEO4J_URI verbatim, and
// a Bolt URI carries its password in userinfo. The repo's own screen-shape
// evidence uses this exact URI form.
func TestRunRedactsTheBoltPassword(t *testing.T) {
	out := runDoctor(t, Deps{
		Neo4jURI: "bolt://neo4j:" + sentinel + "@graph.example.com:7687",
	})

	if strings.Contains(out, sentinel) {
		t.Fatalf("report leaked the Bolt password.\n%s", out)
	}
	// The host is deliberately kept: an operator needs to recognise which
	// backend was configured, and the host is not the secret.
	if !strings.Contains(out, "graph.example.com:7687") {
		t.Fatalf("report dropped the host, which operators need to recognise the target.\n%s", out)
	}
}

// TestRunRedactsACredentialInTheAPIQueryString covers the other composed
// target. The API base URL is operator-configured and can carry a token in its
// query string, which is the shape that survived every name-keyed redactor.
func TestRunRedactsACredentialInTheAPIQueryString(t *testing.T) {
	out := runDoctor(t, Deps{
		APIBaseURL: "http://127.0.0.1:8080/x?api_key=" + sentinel,
	})

	if strings.Contains(out, sentinel) {
		t.Fatalf("report leaked the API query-string credential.\n%s", out)
	}
}

// TestRunNeverPrintsThePostgresDSN pins presence-only reporting. A DSN is
// credentials end to end, so unlike the Bolt URI there is no safe remainder.
func TestRunNeverPrintsThePostgresDSN(t *testing.T) {
	dsn := "postgres://eshu:" + sentinel + "@db.example.com:5432/eshu"
	out := runDoctor(t, Deps{PostgresDSN: dsn})

	if strings.Contains(out, sentinel) || strings.Contains(out, "db.example.com") {
		t.Fatalf("report leaked the Postgres DSN.\n%s", out)
	}
	if !strings.Contains(out, "Postgres DSN configured") {
		t.Fatalf("report lost the presence line.\n%s", out)
	}
}

// TestRunReportsMissingConfigurationWithoutInventingIt keeps the empty-state
// branches honest: unset values must read as unset, not as a redacted marker
// that looks like something is configured.
func TestRunReportsMissingConfigurationWithoutInventingIt(t *testing.T) {
	out := runDoctor(t, Deps{})

	for _, want := range []string{
		"Neo4j URI not configured",
		"Postgres DSN not configured",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("report missing %q\n%s", want, out)
		}
	}
}

// runDoctor exercises Run against a fully described machine so the result does
// not depend on the host the test runs on. Callers override only what they are
// asserting about.
func runDoctor(t *testing.T, deps Deps) string {
	t.Helper()

	if deps.Stat == nil {
		deps.Stat = func(string) (os.FileInfo, error) { return nil, os.ErrNotExist }
	}
	if deps.LookPath == nil {
		deps.LookPath = func(string) (string, error) { return "", os.ErrNotExist }
	}
	if deps.HTTPClient == nil {
		// A client whose transport always errors keeps the probe off the
		// network without depending on a port being closed.
		deps.HTTPClient = &http.Client{
			Timeout:   time.Second,
			Transport: errorTransport{},
		}
	}

	var buf bytes.Buffer
	if err := Run(&buf, deps); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	return buf.String()
}

type errorTransport struct{}

func (errorTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, os.ErrDeadlineExceeded
}
