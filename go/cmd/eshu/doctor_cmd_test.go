// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// TestDoctorDoesNotPrintNeo4jCredentials proves that `eshu doctor` does not
// echo a credential-bearing Bolt URI. A Bolt URI carries its password in
// userinfo -- the repo's own screen-shape evidence uses
// bolt://neo4j:S3NT1NEL@graph.example.com:7687 as the canonical form -- and
// doctor output is the first thing an operator pastes into a bug report.
func TestDoctorDoesNotPrintNeo4jCredentials(t *testing.T) {
	const secret = "S3NT1NEL"
	const uri = "bolt://neo4j:" + secret + "@graph.example.com:7687"

	t.Setenv("NEO4J_URI", uri)
	// Keep the run away from any real config directory.
	t.Setenv("ESHU_HOME", t.TempDir())

	out := captureDoctorStdout(t)

	if strings.Contains(out, secret) {
		t.Fatalf("doctor output leaked the Bolt password %q.\noutput:\n%s", secret, out)
	}
	if strings.Contains(out, uri) {
		t.Fatalf("doctor output leaked the whole credential-bearing URI.\noutput:\n%s", out)
	}
}

// captureDoctorStdout runs runDoctor with os.Stdout redirected and returns
// everything it wrote. runDoctor prints through the package-level fmt helpers
// rather than an injected writer, so swapping os.Stdout is the only seam.
func captureDoctorStdout(t *testing.T) string {
	t.Helper()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v, want nil", err)
	}
	saved := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = saved }()

	done := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		done <- buf.String()
	}()

	if err := runDoctor(&cobra.Command{}, nil); err != nil {
		t.Fatalf("runDoctor() error = %v, want nil", err)
	}
	_ = w.Close()
	os.Stdout = saved

	return <-done
}
