// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graphinstall

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Synthetic sentinels. These are not real credentials and must never be
// replaced with one: the whole point is that a failing assertion prints the
// value it found, so a real secret here would leak into CI logs.
const (
	sentinelUser     = "eshu-leak-probe-user"
	sentinelPassword = "eshu-leak-probe-password-NOT-REAL"
	sentinelToken    = "eshu-leak-probe-token-NOT-REAL"
)

// credentialSentinels are every substring that must not survive into an
// operator-facing artifact. The username is included deliberately: a
// deploy-token scheme commonly carries the secret in the *user* position
// (GitLab CI job tokens, for one), so redacting only the password half would
// still leak.
var credentialSentinels = []string{sentinelUser, sentinelPassword, sentinelToken}

// assertNoSentinel fails when any credential sentinel survives into text.
// Both the positive and the negative tests call this one helper so a guard
// cannot pass by asserting against a different rule than its sibling.
func assertNoSentinel(t *testing.T, label, text string) {
	t.Helper()
	for _, sentinel := range credentialSentinels {
		if strings.Contains(text, sentinel) {
			t.Fatalf("%s leaked credential sentinel %q\nfull value: %s", label, sentinel, text)
		}
	}
}

// credentialArchiveServer serves a valid NornicDB archive and returns both the
// archive bytes and a credential-bearing URL pointing at it. The credentials
// are in the URL only -- the handler ignores them -- so the download succeeds
// and the test observes what Install does with the reference, not a transport
// failure.
func credentialArchiveServer(t *testing.T, withQueryToken bool) (archive []byte, credentialURL string) {
	t.Helper()

	sourceBinary := filepath.Join(t.TempDir(), "nornicdb-headless")
	writeFakeNornicDBBinaryAt(t, sourceBinary, "NornicDB v1.0.42\n")
	archivePath := filepath.Join(t.TempDir(), "nornicdb-headless-darwin-arm64.tar.gz")
	archive = writeTarGzWithBinary(t, archivePath, "release/bin/nornicdb-headless", sourceBinary)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/gzip")
		_, _ = w.Write(archive)
	}))
	t.Cleanup(server.Close)

	hostPart := strings.TrimPrefix(server.URL, "http://")
	credentialURL = "http://" + sentinelUser + ":" + sentinelPassword + "@" + hostPart +
		"/nornicdb-headless-darwin-arm64.tar.gz"
	if withQueryToken {
		credentialURL += "?token=" + sentinelToken
	}
	return archive, credentialURL
}

// TestInstallDoesNotPersistDownloadCredentials is the regression test for the
// `eshu graph install --from https://user:pw@host/build.tar.gz` leak: the raw
// reference was assigned verbatim to preparedInstallSource.SourcePath, which
// reaches three separate operator-facing sinks.
func TestInstallDoesNotPersistDownloadCredentials(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a Unix executable script")
	}

	for _, tc := range []struct {
		name       string
		queryToken bool
	}{
		{name: "userinfo credentials", queryToken: false},
		{name: "userinfo plus query token", queryToken: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			homeDir := t.TempDir()
			t.Setenv("ESHU_HOME", homeDir)
			archive, credentialURL := credentialArchiveServer(t, tc.queryToken)

			result, err := Install(Options{
				From:        credentialURL,
				SHA256:      sha256BytesHex(archive),
				ReadVersion: execNornicDBVersion,
			})
			if err != nil {
				t.Fatalf("Install() error = %v, want nil", err)
			}

			// Sink 1: the JSON printed to stdout by `eshu install nornicdb`
			// (cmd/eshu/graph_install_cmd.go calls printJSON(result)).
			assertNoSentinel(t, "Result.SourcePath", result.SourcePath)

			// Sink 2: the 0600 manifest persisted on disk. Asserted over the
			// raw file bytes, not the decoded struct, so a credential landing
			// in any other manifest field is caught too.
			manifestPath := filepath.Join(homeDir, "graph-backends", "nornicdb", "manifest.json")
			content, err := os.ReadFile(manifestPath) // #nosec G304 -- test-controlled temp path
			if err != nil {
				t.Fatalf("os.ReadFile(manifest) error = %v, want nil", err)
			}
			assertNoSentinel(t, "manifest.json", string(content))

			// The redacted form must still identify the host and artifact, or
			// an operator cannot tell which source produced this install.
			if !strings.Contains(result.SourcePath, "nornicdb-headless-darwin-arm64.tar.gz") {
				t.Fatalf("SourcePath = %q, want it to still name the artifact", result.SourcePath)
			}
		})
	}
}

// TestRedactSourceRef pins the exact rendering, so a future edit that widens
// or narrows the rule has to say so here.
func TestRedactSourceRef(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "userinfo is replaced whole",
			raw:  "https://" + sentinelUser + ":" + sentinelPassword + "@host/build.tar.gz",
			want: "https://REDACTED@host/build.tar.gz",
		},
		{
			name: "bare username is still a credential",
			raw:  "https://" + sentinelToken + "@host/build.tar.gz",
			want: "https://REDACTED@host/build.tar.gz",
		},
		{
			name: "whole query string is dropped",
			raw:  "https://host/build.tar.gz?token=" + sentinelToken + "&arch=arm64",
			want: "https://host/build.tar.gz?REDACTED",
		},
		{
			name: "presigned signature has no credential-shaped name and is dropped anyway",
			raw:  "https://host/build.tar.gz?X-Amz-Signature=" + sentinelToken,
			want: "https://host/build.tar.gz?REDACTED",
		},
		{
			name: "clean url is untouched",
			raw:  "https://host/build.tar.gz",
			want: "https://host/build.tar.gz",
		},
		{
			name: "local path is returned unchanged",
			raw:  "/opt/nornicdb/build.tar.gz",
			want: "/opt/nornicdb/build.tar.gz",
		},
		{
			name: "empty stays empty",
			raw:  "   ",
			want: "",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := redactSourceRef(tc.raw); got != tc.want {
				t.Fatalf("redactSourceRef(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

// TestDownloadURLWithQueryStringIsClassifiedByPath guards a bug found while
// reproducing the credential leak: archive detection ran against the whole
// reference, so any download URL carrying a query string -- which is every
// presigned S3/GCS URL and most artifact-CDN URLs -- was classified as a bare
// binary. The install then tried to exec the tarball and failed with a
// "permission denied" fork/exec error that named no cause an operator could
// act on.
func TestDownloadURLWithQueryStringIsClassifiedByPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a Unix executable script")
	}

	t.Setenv("ESHU_HOME", t.TempDir())
	archive, credentialURL := credentialArchiveServer(t, true)

	result, err := Install(Options{
		From:        credentialURL,
		SHA256:      sha256BytesHex(archive),
		ReadVersion: execNornicDBVersion,
	})
	if err != nil {
		t.Fatalf("Install() error = %v, want nil", err)
	}
	if result.SourceKind != string(sourceDownloadedArchive) {
		t.Fatalf("SourceKind = %q, want %q -- a query string must not change how the artifact is classified",
			result.SourceKind, sourceDownloadedArchive)
	}
}

// TestChecksumMismatchErrorDoesNotLeakCredentials covers install.go's
// `sha256 mismatch for %q` error, which formats SourcePath into a message the
// CLI prints to stderr on a failed install.
func TestChecksumMismatchErrorDoesNotLeakCredentials(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a Unix executable script")
	}

	t.Setenv("ESHU_HOME", t.TempDir())
	_, credentialURL := credentialArchiveServer(t, true)

	_, err := Install(Options{
		From:        credentialURL,
		SHA256:      strings.Repeat("0", 64),
		ReadVersion: execNornicDBVersion,
	})
	if err == nil {
		t.Fatal("Install() error = nil, want a checksum mismatch")
	}
	if !strings.Contains(err.Error(), "sha256 mismatch") {
		t.Fatalf("error = %v, want a sha256 mismatch (the path under test)", err)
	}
	assertNoSentinel(t, "sha256 mismatch error", err.Error())
}

// TestVersionVerificationErrorDoesNotLeakCredentials covers the
// `verify nornicdb source binary %q` errors, which formatted the raw
// sourceRef rather than the redacted SourcePath.
func TestVersionVerificationErrorDoesNotLeakCredentials(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a Unix executable script")
	}

	t.Setenv("ESHU_HOME", t.TempDir())
	_, credentialURL := credentialArchiveServer(t, true)

	_, err := Install(Options{
		From: credentialURL,
		ReadVersion: func(string) (string, error) {
			return "", os.ErrPermission
		},
	})
	if err == nil {
		t.Fatal("Install() error = nil, want a version verification failure")
	}
	if !strings.Contains(err.Error(), "verify nornicdb source binary") {
		t.Fatalf("error = %v, want the verify-source-binary path", err)
	}
	assertNoSentinel(t, "verify source binary error", err.Error())
}

// TestDownloadTransportErrorDoesNotLeakCredentials covers the *url.Error
// branch specifically. net/http masks only the password half of userinfo and
// keeps the entire query string, so wrapping the transport error directly --
// rather than unwrapping to its cause -- re-leaks what the redacted URL in the
// same message just removed. The 404 test below takes a different branch and
// cannot catch this.
func TestDownloadTransportErrorDoesNotLeakCredentials(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	hostPart := strings.TrimPrefix(server.URL, "http://")
	server.Close() // nothing is listening now, so the request fails in transport

	t.Setenv("ESHU_HOME", t.TempDir())
	credentialURL := "http://" + sentinelUser + ":" + sentinelPassword + "@" + hostPart +
		"/build.tar.gz?token=" + sentinelToken

	_, err := Install(Options{
		From:        credentialURL,
		ReadVersion: execNornicDBVersion,
	})
	if err == nil {
		t.Fatal("Install() error = nil, want a transport failure")
	}
	if !strings.Contains(err.Error(), "download nornicdb source") {
		t.Fatalf("error = %v, want the download-source path", err)
	}
	assertNoSentinel(t, "download transport error", err.Error())
}

// TestDownloadFailureErrorDoesNotLeakCredentials covers downloadInstallSource's
// own errors, which formatted the raw source URL. This one never reaches
// SourcePath at all, so redacting at assignment does not cover it.
func TestDownloadFailureErrorDoesNotLeakCredentials(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	t.Setenv("ESHU_HOME", t.TempDir())
	hostPart := strings.TrimPrefix(server.URL, "http://")
	credentialURL := "http://" + sentinelUser + ":" + sentinelPassword + "@" + hostPart +
		"/missing.tar.gz?token=" + sentinelToken

	_, err := Install(Options{
		From:        credentialURL,
		ReadVersion: execNornicDBVersion,
	})
	if err == nil {
		t.Fatal("Install() error = nil, want a download failure")
	}
	if !strings.Contains(err.Error(), "download nornicdb source") {
		t.Fatalf("error = %v, want the download-source path", err)
	}
	assertNoSentinel(t, "download failure error", err.Error())
}
