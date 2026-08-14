// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package report

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reportbundle"
)

// credentialSentinel stands in for a password a reporter pasted into a target.
// It is a bare token with no sensitive-looking key in front of it, because that
// is the shape the bundle's redaction cannot see: every rule in
// internal/reportbundle matches on a KEY name, and URL userinfo has none.
const credentialSentinel = "ESHU6059SENTINELc47e09"

// captureRenderings is every place a capture can put text in front of a person:
// the returned error, the JSON a caller writes to stdout, and the bytes
// WriteBundle leaves on disk. A redaction assertion has to cover all three —
// the file is the one that gets attached to a public issue, and it is the one a
// stdout-only assertion misses.
func captureRenderings(t *testing.T, result CaptureResult, err error) map[string]string {
	t.Helper()
	out := map[string]string{"bundle_json": string(result.JSON)}
	if err != nil {
		out["error"] = err.Error()
	}
	if len(result.JSON) > 0 {
		path := filepath.Join(t.TempDir(), "bundle.json")
		if writeErr := WriteBundle(path, result.JSON); writeErr != nil {
			t.Fatalf("WriteBundle() error = %v", writeErr)
		}
		onDisk, readErr := os.ReadFile(path) // #nosec G304 -- test-owned temp path
		if readErr != nil {
			t.Fatalf("read written bundle: %v", readErr)
		}
		out["on_disk"] = string(onDisk)
	}
	return out
}

// TestCaptureBundle_RefusesTargetCarryingURLCredentials is the regression test
// for a credential pasted into the tool or endpoint as URL userinfo
// (`https://user:PASSWORD@host/...`).
//
// Every redaction rule in internal/reportbundle matches an object KEY name, and
// SplitTargetQuery converts a target's query string back into keys so those
// rules can reach it. Userinfo sits BEFORE the "?", so the split never sees it
// and no key name exists to match: the password landed verbatim in query.target
// of a bundle stamped `"profile": "public"`, `"rules": []`, and
// `"status": "passed"`. It leaked and certified that it had screened.
//
// The character immediately before the secret is varied on purpose. The
// username and password positions differ by exactly that ("//" versus ":"), and
// a check anchored on one passes for the case it was written against while
// missing the other.
func TestCaptureBundle_RefusesTargetCarryingURLCredentials(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/eshu.envelope+json")
		_, _ = w.Write([]byte(`{"data":{"owner":"platform-team"},"truth":{"level":"exact","profile":"local_authoritative"},"error":null}`))
	}))
	defer server.Close()

	tests := []struct {
		name     string
		wantFlag string
		opts     CaptureOptions
	}{
		{
			// Secret in the password position, preceded by ":".
			name:     "tool carries userinfo password",
			wantFlag: "--tool",
			opts: CaptureOptions{
				Endpoint: "/api/v0/services/checkout/story",
				Tool:     "https://svc:" + credentialSentinel + "@mcp.internal:5432/tool",
			},
		},
		{
			// Secret in the username position, preceded by "//".
			name:     "tool carries userinfo username",
			wantFlag: "--tool",
			opts: CaptureOptions{
				Endpoint: "/api/v0/services/checkout/story",
				Tool:     "https://" + credentialSentinel + "@mcp.internal:5432/tool",
			},
		},
		{
			name:     "endpoint carries userinfo password",
			wantFlag: "--endpoint",
			opts: CaptureOptions{
				Endpoint: "https://svc:" + credentialSentinel + "@api.internal:8080/api/v0/services/checkout/story",
			},
		},
		{
			// Userinfo plus a query string: the query half already had a rule,
			// the userinfo half must not ride in behind it.
			name:     "endpoint carries userinfo alongside a query string",
			wantFlag: "--endpoint",
			opts: CaptureOptions{
				Endpoint: "https://svc:" + credentialSentinel + "@api.internal:8080/api/v0/x?repo=demo%2Fservice",
			},
		},
		{
			// The same credential written without "//". net/url reports this
			// as scheme "svc" with the rest in Opaque and User nil, so the
			// guard's original parsed.User test passed it straight through:
			// the password landed in query.target of a bundle stamped
			// "profile": "public", "rules": [].
			name:     "tool carries userinfo written without //",
			wantFlag: "--tool",
			opts: CaptureOptions{
				Endpoint: "/api/v0/services/checkout/story",
				Tool:     "svc:" + credentialSentinel + "@mcp.internal:5432/tool",
			},
		},
		{
			name:     "endpoint carries userinfo written without //",
			wantFlag: "--endpoint",
			opts: CaptureOptions{
				Endpoint: "svc:" + credentialSentinel + "@api.internal:5432/api/v0/services/checkout/story",
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result, err := CaptureBundle(&httpEnvelopeClient{baseURL: server.URL}, tt.opts)
			if err == nil {
				t.Fatalf("CaptureBundle() error = nil, want a refusal; bundle = %s", result.JSON)
			}
			var credentialErr *TargetCredentialError
			if !errors.As(err, &credentialErr) {
				t.Fatalf("CaptureBundle() error = %v (%T), want *TargetCredentialError so the CLI can map the exit code", err, err)
			}
			if credentialErr.Flag != tt.wantFlag {
				t.Errorf("TargetCredentialError.Flag = %q, want %q", credentialErr.Flag, tt.wantFlag)
			}
			for where, text := range captureRenderings(t, result, err) {
				if strings.Contains(text, credentialSentinel) {
					t.Errorf("credential sentinel reached %s:\n%s", where, text)
				}
			}
		})
	}
}

// TestCaptureBundle_AcceptsBenignAtSignInPath keeps the refusal above from
// becoming a ban on "@". An "@" inside a path segment is not an authority
// component and carries no credential; net/url is what decides the difference,
// rather than a hand-written character rule of the kind that has already been
// wrong elsewhere in this repository.
func TestCaptureBundle_AcceptsBenignAtSignInPath(t *testing.T) {
	t.Parallel()

	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/eshu.envelope+json")
		_, _ = w.Write([]byte(`{"data":{"owner":"platform-team"},"truth":{"level":"exact","profile":"local_authoritative"},"error":null}`))
	}))
	defer server.Close()

	const endpoint = "/api/v0/owners/dev@example.com/services"
	result, err := CaptureBundle(&httpEnvelopeClient{baseURL: server.URL}, CaptureOptions{Endpoint: endpoint})
	if err != nil {
		t.Fatalf("CaptureBundle() error = %v, want nil for a path-segment @", err)
	}
	if gotPath != endpoint {
		t.Errorf("request path = %q, want %q issued unchanged", gotPath, endpoint)
	}
	if result.Bundle.Query.Target != endpoint {
		t.Errorf("Query.Target = %q, want the benign path recorded unchanged", result.Bundle.Query.Target)
	}
}

// TestCheckTargetCredentials_AcceptsTargetsWithoutAnAuthority pins the other
// side of the refusal: the guard re-reads a "//"-less target as an authority,
// and that rewrite must not turn ordinary targets into credentials. A tool
// name such as `mcp:tool/name` parses as a scheme plus a rootless path, and
// reading it as an authority would make "mcp" a host and "tool" a port.
//
// The last two cases are the documented boundary, not an oversight: an "@"
// after the first "/" of an opaque target, or in a target with no scheme at
// all, reads as the path it looks like — the same rule that keeps
// `/api/v0/owners/dev@example.com/services` working.
func TestCheckTargetCredentials_AcceptsTargetsWithoutAnAuthority(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value string
	}{
		{name: "plain tool name", value: "wrong_answer_story"},
		{name: "tool name with a scheme and a rootless path", value: "mcp:tool/name"},
		{name: "endpoint path", value: "/api/v0/services/checkout/story"},
		{name: "@ in a path segment", value: "/api/v0/owners/dev@example.com/services"},
		{name: "@ after the first / of an opaque target", value: "mcp:tool/dev@example.com"},
		{name: "@ in a target with no scheme and no //", value: "dev@example.com/services"},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if err := checkTargetCredentials("--tool", tt.value); err != nil {
				t.Errorf("checkTargetCredentials(%q) = %v, want nil", tt.value, err)
			}
		})
	}
}

// TestRequestErrorWithoutURL covers the failure rendering: the transport error
// quotes the full request URL, and the substituted path is the reporter's own
// endpoint, which can carry userinfo of its own. The guard is repeated inside
// this function rather than relying on CaptureBundle's up-front refusal
// reaching it first — an ordering assumption is what the original defect was
// made of.
func TestRequestErrorWithoutURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		err      error
		safePath string
		wantKeep string
	}{
		{
			name:     "query string dropped from the request URL",
			err:      &url.Error{Op: "Get", URL: "http://host/x?api_key=" + credentialSentinel, Err: errors.New("connection refused")},
			safePath: "/api/v0/services/checkout/story",
			wantKeep: "/api/v0/services/checkout/story",
		},
		{
			name:     "userinfo dropped from the substituted path",
			err:      &url.Error{Op: "Get", URL: "http://host/x", Err: errors.New("connection refused")},
			safePath: "https://svc:" + credentialSentinel + "@db.internal:5432/api/v0/x",
			wantKeep: "db.internal",
		},
		{
			name:     "wrapped url.Error is still unwrapped",
			err:      errors.Join(errors.New("request failed"), &url.Error{Op: "Post", URL: "http://host/x?token=" + credentialSentinel, Err: errors.New("EOF")}),
			safePath: "/api/v0/query",
			wantKeep: "/api/v0/query",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := requestErrorWithoutURL(tt.err, tt.safePath)
			if got == nil {
				t.Fatalf("requestErrorWithoutURL() = nil, want an error")
			}
			if strings.Contains(got.Error(), credentialSentinel) {
				t.Errorf("error echoed the credential:\n%s", got.Error())
			}
			if !strings.Contains(got.Error(), tt.wantKeep) {
				t.Errorf("error = %q, want it to keep %q so the reporter can act on it", got.Error(), tt.wantKeep)
			}
		})
	}
}

// TestRequestErrorWithoutURL_PassesThroughNonURLErrors pins the branch that
// returns the error untouched, including the documented gap: an HTTP error
// whose body echoes the request URL is not a *url.Error and is not redacted
// here.
func TestRequestErrorWithoutURL_PassesThroughNonURLErrors(t *testing.T) {
	t.Parallel()

	original := errors.New("decode response: unexpected end of JSON input")
	if got := requestErrorWithoutURL(original, "/api/v0/x"); !errors.Is(got, original) {
		t.Errorf("requestErrorWithoutURL() = %v, want the original error preserved", got)
	}
}

// TestSafeErrorPath covers the redaction helper on its own, including the
// unparseable input it fails closed on.
func TestSafeErrorPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		path string
		want string
	}{
		{name: "plain path is untouched", path: "/api/v0/x", want: "/api/v0/x"},
		{name: "path-segment @ is untouched", path: "/api/v0/owners/dev@example.com/x", want: "/api/v0/owners/dev@example.com/x"},
		{name: "userinfo is replaced", path: "https://svc:" + credentialSentinel + "@db.internal/x", want: "https://redacted@db.internal/x"},
		{name: "userinfo written without // is replaced", path: "svc:" + credentialSentinel + "@db.internal:5432/x", want: "redacted@db.internal:5432/x"},
		{name: "a rootless path is not read as an authority", path: "mcp:tool/name", want: "mcp:tool/name"},
		{name: "userinfo that cannot be re-read is replaced wholesale", path: "svc:" + credentialSentinel + "@db.internal:notaport/x", want: "[unparseable endpoint]"},
		{name: "unparseable path is replaced wholesale", path: "http://host/x\x7f" + credentialSentinel, want: "[unparseable endpoint]"},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := safeErrorPath(tt.path); got != tt.want {
				t.Errorf("safeErrorPath(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

// TestValidateBundle_RequirePublic proves the verdict line and the error both
// land, on both outcomes.
func TestValidateBundle_RequirePublic(t *testing.T) {
	t.Parallel()

	publicBundle, err := reportbundle.Capture(reportbundle.CaptureInput{
		Surface: "api",
		Target:  "/api/v0/services/checkout/story",
	})
	if err != nil {
		t.Fatalf("Capture() error = %v, want nil", err)
	}
	privateBundle, err := reportbundle.Capture(reportbundle.CaptureInput{
		Surface:         "api",
		Target:          "/api/v0/services/checkout/story",
		IncludePayloads: true,
	})
	if err != nil {
		t.Fatalf("Capture() error = %v, want nil", err)
	}

	tests := []struct {
		name        string
		bundle      reportbundle.Bundle
		wantErr     bool
		wantVerdict string
	}{
		{name: "public bundle passes", bundle: publicBundle, wantErr: false, wantVerdict: "report bundle validation: passed"},
		{name: "private-triage bundle fails", bundle: privateBundle, wantErr: true, wantVerdict: "report bundle validation: failed"},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			raw, err := json.Marshal(tt.bundle)
			if err != nil {
				t.Fatalf("marshal bundle: %v", err)
			}
			var out bytes.Buffer
			err = ValidateBundle(&out, raw, true)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateBundle() error = %v, wantErr %v (output: %s)", err, tt.wantErr, out.String())
			}
			if !strings.Contains(out.String(), tt.wantVerdict) {
				t.Errorf("verdict = %q, want %q", out.String(), tt.wantVerdict)
			}
		})
	}
}

// TestValidateBundle_RejectsUndecodableInput pins the decode failure path,
// which is what a maintainer hits when they point --from at the wrong file.
func TestValidateBundle_RejectsUndecodableInput(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	err := ValidateBundle(&out, []byte("not json"), false)
	if err == nil {
		t.Fatalf("ValidateBundle() error = nil, want a decode failure")
	}
	if !strings.Contains(err.Error(), "decode report bundle") {
		t.Errorf("error = %v, want it to name the decode step", err)
	}
	if out.Len() != 0 {
		t.Errorf("verdict = %q, want nothing written for input that never reached the gate", out.String())
	}
}

// TestReadBundleInput covers both sources and the failure a missing --from
// path produces.
func TestReadBundleInput(t *testing.T) {
	t.Parallel()

	t.Run("reads stdin when no path is given", func(t *testing.T) {
		t.Parallel()
		got, err := ReadBundleInput(strings.NewReader(`{"a":1}`), "  ")
		if err != nil {
			t.Fatalf("ReadBundleInput() error = %v, want nil", err)
		}
		if string(got) != `{"a":1}` {
			t.Errorf("ReadBundleInput() = %q, want the stdin bytes", got)
		}
	})

	t.Run("reads the file when a path is given", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), "bundle.json")
		if err := os.WriteFile(path, []byte(`{"b":2}`), 0o600); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
		got, err := ReadBundleInput(strings.NewReader("ignored"), path)
		if err != nil {
			t.Fatalf("ReadBundleInput() error = %v, want nil", err)
		}
		if string(got) != `{"b":2}` {
			t.Errorf("ReadBundleInput() = %q, want the file bytes", got)
		}
	})

	t.Run("reports a missing file", func(t *testing.T) {
		t.Parallel()
		_, err := ReadBundleInput(strings.NewReader(""), filepath.Join(t.TempDir(), "absent.json"))
		if err == nil {
			t.Fatalf("ReadBundleInput() error = nil, want a read failure")
		}
		if !strings.Contains(err.Error(), "read report bundle") {
			t.Errorf("error = %v, want it to name the read step", err)
		}
	})
}

// TestWriteBundle_IsOwnerOnly pins the mode: a private-triage bundle carries raw
// payloads, and the mode must not depend on which profile produced the bytes.
func TestWriteBundle_IsOwnerOnly(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "bundle.json")
	if err := WriteBundle(path, []byte("{}\n")); err != nil {
		t.Fatalf("WriteBundle() error = %v, want nil", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat written bundle: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("bundle mode = %04o, want 0600", got)
	}
}
