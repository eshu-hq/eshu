// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// TestDoctorDoesNotPrintNeo4jCredentials proves that `eshu doctor` does not
// echo a credential-bearing Bolt URI. A Bolt URI carries its password in
// userinfo -- the repo's own screen-shape evidence uses
// bolt://neo4j:S3NT1NEL@graph.example.com:7687 as the canonical form -- and
// doctor output is the first thing an operator pastes into a bug report.
//
// This is the end-to-end pin through the cobra wrapper. The redaction itself is
// screened in internal/cli/doctor's doctor_redaction_test.go; what this adds is
// that the wrapper actually routes the resolved URI through that package rather
// than printing it itself.
func TestDoctorDoesNotPrintNeo4jCredentials(t *testing.T) {
	const secret = "S3NT1NEL"
	const uri = "bolt://neo4j:" + secret + "@graph.example.com:7687"

	t.Setenv("NEO4J_URI", uri)
	// Keep the run away from any real config directory.
	t.Setenv("ESHU_HOME", t.TempDir())
	// Pin the API endpoint at a closed port so the probe fails fast and
	// deterministically. Left unset, NewAPIClient resolves ESHU_SERVICE_URL from
	// the environment and the settings file, so on a machine that has it set
	// this test would fire a live request at a real -- possibly
	// credential-bearing -- endpoint, and otherwise wait on localhost:8080.
	t.Setenv("ESHU_SERVICE_URL", "http://127.0.0.1:1")

	out := runDoctorCapturingOutput(t)

	if strings.Contains(out, secret) {
		t.Fatalf("doctor output leaked the Bolt password %q.\noutput:\n%s", secret, out)
	}
	if strings.Contains(out, uri) {
		t.Fatalf("doctor output leaked the whole credential-bearing URI.\noutput:\n%s", out)
	}
	// Assert the report was actually produced. Without this the leak checks
	// would pass just as well on empty output.
	if !strings.Contains(out, "Eshu Diagnostics") {
		t.Fatalf("doctor produced no report; the leak assertions above proved nothing.\noutput:\n%s", out)
	}
}

// runDoctorCapturingOutput runs runDoctor against a command whose output writer
// is a buffer, and returns what it wrote.
//
// runDoctor renders through cmd.OutOrStdout(), so SetOut is the direct seam --
// no os.Stdout swapping required, and the capture cannot pick up writes from
// anything else running in the process.
func runDoctorCapturingOutput(t *testing.T) string {
	t.Helper()

	var buf bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&buf)

	if err := runDoctor(cmd, nil); err != nil {
		t.Fatalf("runDoctor() error = %v, want nil", err)
	}
	return buf.String()
}
