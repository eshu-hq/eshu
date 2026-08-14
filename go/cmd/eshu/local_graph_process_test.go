// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"errors"
	"strings"
	"testing"
)

func TestResolveNornicDBBinaryPrefersHeadlessBinary(t *testing.T) {
	originalLookPath := localGraphLookPath
	originalReadVersion := localGraphReadVersion
	t.Cleanup(func() {
		localGraphLookPath = originalLookPath
		localGraphReadVersion = originalReadVersion
	})
	t.Setenv("ESHU_HOME", t.TempDir())
	t.Setenv("ESHU_NORNICDB_BINARY", "")

	localGraphLookPath = func(file string) (string, error) {
		switch file {
		case "nornicdb-headless":
			return "/eshu/bin/nornicdb-headless", nil
		case "nornicdb":
			return "/eshu/bin/nornicdb", nil
		default:
			return "", errors.New("unexpected binary lookup")
		}
	}
	localGraphReadVersion = func(binaryPath string) (string, error) {
		return "v1.0.42", nil
	}

	got, err := resolveNornicDBBinary()
	if err != nil {
		t.Fatalf("resolveNornicDBBinary() error = %v, want nil", err)
	}
	if got != "/eshu/bin/nornicdb-headless" {
		t.Fatalf("resolveNornicDBBinary() = %q, want headless path", got)
	}
}

func TestResolveNornicDBBinaryAllowsExplicitFullBinary(t *testing.T) {
	originalReadVersion := localGraphReadVersion
	t.Cleanup(func() {
		localGraphReadVersion = originalReadVersion
	})
	t.Setenv("ESHU_NORNICDB_BINARY", "/opt/nornicdb")
	localGraphReadVersion = func(binaryPath string) (string, error) {
		return "v1.0.42", nil
	}

	got, err := resolveNornicDBBinary()
	if err != nil {
		t.Fatalf("resolveNornicDBBinary() error = %v, want nil", err)
	}
	if got != "/opt/nornicdb" {
		t.Fatalf("resolveNornicDBBinary() = %q, want explicit path", got)
	}
}

func TestResolveNornicDBBinaryRejectsInvalidExplicitBinary(t *testing.T) {
	originalReadVersion := localGraphReadVersion
	t.Cleanup(func() {
		localGraphReadVersion = originalReadVersion
	})
	t.Setenv("ESHU_NORNICDB_BINARY", "/tmp/not-nornicdb")
	localGraphReadVersion = func(binaryPath string) (string, error) {
		return "", errors.New("unexpected output")
	}

	_, err := resolveNornicDBBinary()
	if err == nil {
		t.Fatal("resolveNornicDBBinary() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "verify nornicdb binary") {
		t.Fatalf("resolveNornicDBBinary() error = %q, want verification failure", err.Error())
	}
}

func TestParseNornicDBVersionOutputRequiresNornicDBPrefix(t *testing.T) {
	got, err := parseNornicDBVersionOutput("NornicDB v1.0.42\n")
	if err != nil {
		t.Fatalf("parseNornicDBVersionOutput() error = %v, want nil", err)
	}
	if got != "v1.0.42" {
		t.Fatalf("parseNornicDBVersionOutput() = %q, want %q", got, "v1.0.42")
	}

	_, err = parseNornicDBVersionOutput("not nornicdb\n")
	if err == nil {
		t.Fatal("parseNornicDBVersionOutput() error = nil, want non-nil")
	}
}
