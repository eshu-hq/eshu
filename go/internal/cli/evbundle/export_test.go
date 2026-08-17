// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package evbundle

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/evidencebundle"
)

var fixedCreatedAt = time.Date(2026, 7, 2, 8, 0, 0, 0, time.UTC)

// fixedClock is the caller-owned clock thunk for cases where how long the
// fetch takes is irrelevant. Ordering is pinned separately by
// TestExportLiveStampsCreatedAtAfterTheFetch.
func fixedClock() time.Time { return fixedCreatedAt }

// tickingFetcher answers the status routes from a stubFetcher while advancing
// a fake clock one step per GET, so wall time only moves DURING the fetch.
// That is what makes a CreatedAt sampled before the fetch distinguishable from
// one sampled after it: the two differ by exactly one step per status route.
type tickingFetcher struct {
	inner *stubFetcher
	now   time.Time
	step  time.Duration
}

func (f *tickingFetcher) Get(path string, result any) error {
	f.now = f.now.Add(f.step)
	return f.inner.Get(path, result)
}

// Now is the thunk handed to ExportLive as its clock.
func (f *tickingFetcher) Now() time.Time { return f.now }

func decodeBundle(t *testing.T, raw []byte) evidencebundle.Bundle {
	t.Helper()
	var bundle evidencebundle.Bundle
	if err := json.Unmarshal(raw, &bundle); err != nil {
		t.Fatalf("decode bundle: %v\n%s", err, raw)
	}
	return bundle
}

func TestExportDemoRendersAValidatedBundle(t *testing.T) {
	raw, err := ExportDemo("repo:demo/service")
	if err != nil {
		t.Fatalf("ExportDemo() error = %v", err)
	}
	bundle := decodeBundle(t, raw)
	if bundle.SchemaVersion != evidencebundle.SchemaVersion {
		t.Fatalf("SchemaVersion = %q, want %q", bundle.SchemaVersion, evidencebundle.SchemaVersion)
	}
	if bundle.Identity.ScopeID != "repo:demo/service" {
		t.Fatalf("ScopeID = %q", bundle.Identity.ScopeID)
	}
	if bundle.Validation.Status != "passed" {
		t.Fatalf("Validation.Status = %q, want passed once Validate returned nil", bundle.Validation.Status)
	}
	if err := evidencebundle.Validate(bundle); err != nil {
		t.Fatalf("exported bundle does not re-validate: %v", err)
	}
}

// TestExportDemoRefusesAScopeCarryingPrivateData covers the one
// caller-supplied string the demo path stamps into the artifact. The scope
// handle is content -- it lands in Identity.ScopeID and in every reproduce
// call's repo_id -- so a path or credential in it must abort the export, not
// ride along beside correctly-redacted fields.
func TestExportDemoRefusesAScopeCarryingPrivateData(t *testing.T) {
	for name, scope := range map[string]string{
		"local_path":     "repo:/Users/example/private/repo",
		"credential_url": "repo:https://svc:hunter2@example.com/x",
		// The "repo:" prefix puts a colon immediately before the host, which
		// is the delimiter that used to slip past both private-host rules.
		"private_host":    "repo:db.internal:5432",
		"private_address": "repo:10.0.5.3",
	} {
		t.Run(name, func(t *testing.T) {
			raw, err := ExportDemo(scope)
			if err == nil {
				t.Fatalf("ExportDemo(%q) error = nil, want a refusal\n%s", scope, raw)
			}
			if raw != nil {
				t.Fatalf("ExportDemo returned %d bytes alongside its error; a refused export must render nothing", len(raw))
			}
			if !strings.Contains(err.Error(), "validate generated evidence bundle") {
				t.Fatalf("error = %v, want it to name the validation step", err)
			}
		})
	}
}

// TestExportLiveStampsCreatedAtAfterTheFetch pins WHEN the clock is read.
//
// Identity.CreatedAt is meant to be the instant the evidence finished being
// read, which is what the pre-extraction cmd/eshu path did: it called its
// time.Now wrapper inside the LiveBundleOptions literal, after the three
// status GETs had returned. Reading the clock at the call site instead --
// ExportLive(client, "", now()) -- compiles, passes every other test here, and
// silently moves CreatedAt earlier by the whole fetch duration on a slow
// stack. The parity test cannot catch it either, because it blanks CreatedAt
// before comparing.
//
// The fake clock advances only inside Get, so the two orderings differ by
// exactly three seconds -- one per status route -- and nothing else in the
// bundle moves.
func TestExportLiveStampsCreatedAtAfterTheFetch(t *testing.T) {
	fetcher := &tickingFetcher{
		inner: &stubFetcher{bodies: fullStatusBodies()},
		now:   fixedCreatedAt,
		step:  time.Second,
	}

	raw, err := ExportLive(fetcher, "", fetcher.Now)
	if err != nil {
		t.Fatalf("ExportLive() error = %v", err)
	}

	bundle := decodeBundle(t, raw)

	// Assert this is a SUCCESSFUL live export before asserting the timestamp.
	// A refusal returns no bytes, so a case that quietly became a rejection
	// would never reach the CreatedAt check and the guard would rot silently.
	if bundle.Validation.Status != "passed" {
		t.Fatalf("Validation.Status = %q, want passed; this case must be a successful live export, not a refusal", bundle.Validation.Status)
	}
	if err := evidencebundle.Validate(bundle); err != nil {
		t.Fatalf("exported live bundle does not re-validate: %v", err)
	}
	if len(fetcher.inner.asked) != 3 {
		t.Fatalf("fetched %d routes, want 3; the step-per-GET arithmetic below assumes all three ran", len(fetcher.inner.asked))
	}

	want := fixedCreatedAt.Add(3 * time.Second).Format(time.RFC3339)
	if bundle.Identity.CreatedAt != want {
		t.Fatalf("CreatedAt = %q, want %q (the post-fetch instant); %q is the fetch-START time, so the clock was read before FetchLiveSnapshot",
			bundle.Identity.CreatedAt, want, fixedCreatedAt.Format(time.RFC3339))
	}
}

func TestExportLiveComposesAStackWideBundle(t *testing.T) {
	raw, err := ExportLive(&stubFetcher{bodies: fullStatusBodies()}, "", fixedClock)
	if err != nil {
		t.Fatalf("ExportLive() error = %v", err)
	}
	bundle := decodeBundle(t, raw)
	if bundle.Identity.ScopeID != "live:local" {
		t.Fatalf("ScopeID = %q, want the stack-wide default", bundle.Identity.ScopeID)
	}
	if bundle.Identity.CreatedAt != fixedCreatedAt.Format(time.RFC3339) {
		t.Fatalf("CreatedAt = %q, want the caller-supplied fetch time", bundle.Identity.CreatedAt)
	}
	if bundle.Validation.Status != "passed" {
		t.Fatalf("Validation.Status = %q, want passed", bundle.Validation.Status)
	}
	if bundle.Contents.PipelineState == nil || bundle.Contents.PipelineState.RepositoryCount != 5 {
		t.Fatalf("PipelineState = %+v, want it populated from the status routes", bundle.Contents.PipelineState)
	}
	if err := evidencebundle.Validate(bundle); err != nil {
		t.Fatalf("exported live bundle does not re-validate: %v", err)
	}
}

func TestExportLiveFailsWhenAStatusRouteFails(t *testing.T) {
	sentinel := errors.New("status reader not configured")
	raw, err := ExportLive(&stubFetcher{
		bodies: fullStatusBodies(),
		errs:   map[string]error{PipelineEndpoint: sentinel},
	}, "", fixedClock)
	if err == nil {
		t.Fatalf("ExportLive() error = nil, want a failure\n%s", raw)
	}
	if raw != nil {
		t.Fatalf("ExportLive returned %d bytes alongside its error", len(raw))
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("error %v does not wrap the route error", err)
	}
}

// TestExportLiveRefusesASnapshotCarryingAPrivateEndpoint plants a locating
// address inside a free-text status value -- the shape a name-keyed redactor
// cannot see -- and proves the export aborts instead of rendering it.
func TestExportLiveRefusesASnapshotCarryingAPrivateEndpoint(t *testing.T) {
	bodies := fullStatusBodies()
	bodies[PipelineEndpoint] = `{
		"health": {"state": "degraded", "reasons": ["dial tcp 10.42.7.9:5432: connection refused"]},
		"queue": {"total": 1}
	}`
	raw, err := ExportLive(&stubFetcher{bodies: bodies}, "", fixedClock)
	if err == nil {
		t.Fatalf("ExportLive() error = nil, want a refusal\n%s", raw)
	}
	if bytes.Contains(raw, []byte("10.42.7.9")) {
		t.Fatalf("the address reached the rendered bytes:\n%s", raw)
	}
	if !strings.Contains(err.Error(), "validate live evidence bundle") {
		t.Fatalf("error = %v, want it to name the validation step", err)
	}
}

// TestWriteBundleCreatesTheFileOwnerOnly covers the CREATE path only, which is
// the whole of what the 0600 argument buys. "Creates", not "writes": os.WriteFile
// hands perm to open(2), which ignores it for an existing path, so the mode is a
// create-time default and not a guarantee about the file at outPath. The paired
// case below pins that boundary so the name cannot quietly re-broaden.
func TestWriteBundleCreatesTheFileOwnerOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bundle.json")
	if err := WriteBundle(&bytes.Buffer{}, []byte("{}\n"), path); err != nil {
		t.Fatalf("WriteBundle() error = %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat bundle: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("bundle mode = %04o, want 0600", perm)
	}
	raw, err := os.ReadFile(path) // #nosec G304 -- test-owned temp path
	if err != nil {
		t.Fatalf("read bundle: %v", err)
	}
	if string(raw) != "{}\n" {
		t.Fatalf("bundle contents = %q", raw)
	}
}

// TestWriteBundleLeavesAnExistingFilesModeAlone documents the boundary of the
// case above rather than blessing it. An operator who pre-creates the --out
// path keeps whatever mode they chose, so the doc comment on WriteBundle says
// the 0600 applies on creation. If someone later makes WriteBundle chmod the
// path -- a real behavior change, and the operator's call to ask for -- this
// test is what tells them to update that sentence in the same commit.
func TestWriteBundleLeavesAnExistingFilesModeAlone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bundle.json")
	if err := os.WriteFile(path, []byte("stale"), 0o644); err != nil {
		t.Fatalf("seed pre-existing bundle: %v", err)
	}
	// Chmod explicitly: os.WriteFile's perm is masked by the process umask, so
	// a runner with umask 077 would seed 0600 and this case would assert
	// against a mode it never actually set.
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatalf("chmod pre-existing bundle: %v", err)
	}
	if err := WriteBundle(&bytes.Buffer{}, []byte("{}\n"), path); err != nil {
		t.Fatalf("WriteBundle() error = %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat bundle: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o644 {
		t.Fatalf("bundle mode = %04o, want the pre-existing 0644; WriteBundle now changes the mode of a path the operator chose, so update its doc comment", perm)
	}
	// The contents must still have been replaced -- this pins the mode, not a
	// refusal to write.
	raw, err := os.ReadFile(path) // #nosec G304 -- test-owned temp path
	if err != nil {
		t.Fatalf("read bundle: %v", err)
	}
	if string(raw) != "{}\n" {
		t.Fatalf("bundle contents = %q, want the new bytes", raw)
	}
}

func TestWriteBundleFallsBackToTheWriter(t *testing.T) {
	for name, path := range map[string]string{"empty": "", "whitespace": "   "} {
		t.Run(name, func(t *testing.T) {
			var out bytes.Buffer
			if err := WriteBundle(&out, []byte("{}\n"), path); err != nil {
				t.Fatalf("WriteBundle() error = %v", err)
			}
			if out.String() != "{}\n" {
				t.Fatalf("writer got %q", out.String())
			}
		})
	}
}

func TestWriteBundleReportsAnUnwritablePath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing-dir", "bundle.json")
	err := WriteBundle(&bytes.Buffer{}, []byte("{}\n"), path)
	if err == nil {
		t.Fatal("WriteBundle() error = nil for a path under a missing directory")
	}
	if !strings.Contains(err.Error(), "write evidence bundle") {
		t.Fatalf("error = %v, want it to name the write step", err)
	}
}

func TestReadBundleInputPrefersThePathOverTheReader(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bundle.json")
	if err := os.WriteFile(path, []byte(`{"from":"file"}`), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	raw, err := ReadBundleInput(strings.NewReader(`{"from":"stdin"}`), path)
	if err != nil {
		t.Fatalf("ReadBundleInput() error = %v", err)
	}
	if string(raw) != `{"from":"file"}` {
		t.Fatalf("read %q, want the file contents", raw)
	}
}

func TestReadBundleInputFallsBackToTheReader(t *testing.T) {
	raw, err := ReadBundleInput(strings.NewReader(`{"from":"stdin"}`), "  ")
	if err != nil {
		t.Fatalf("ReadBundleInput() error = %v", err)
	}
	if string(raw) != `{"from":"stdin"}` {
		t.Fatalf("read %q, want the reader contents", raw)
	}
}

func TestReadBundleInputReportsAMissingFile(t *testing.T) {
	_, err := ReadBundleInput(strings.NewReader(""), filepath.Join(t.TempDir(), "absent.json"))
	if err == nil {
		t.Fatal("ReadBundleInput() error = nil for a missing file")
	}
	if !strings.Contains(err.Error(), "read evidence bundle") {
		t.Fatalf("error = %v, want it to name the read step", err)
	}
}

func TestValidateBundlePrintsAPassedVerdict(t *testing.T) {
	raw, err := ExportDemo("repo:demo/service")
	if err != nil {
		t.Fatalf("ExportDemo() error = %v", err)
	}
	var out bytes.Buffer
	if err := ValidateBundle(&out, raw); err != nil {
		t.Fatalf("ValidateBundle() error = %v", err)
	}
	if out.String() != "evidence bundle validation: passed\n" {
		t.Fatalf("verdict = %q", out.String())
	}
}

func TestValidateBundlePrintsAFailedVerdictAndReturnsTheReason(t *testing.T) {
	bundle := evidencebundle.BuildDemoBundle(evidencebundle.DemoBundleOptions{ScopeID: "repo:demo/service"})
	bundle.Source.Repository = "/Users/example/private/repo"
	raw, err := evidencebundle.RenderJSON(bundle)
	if err != nil {
		t.Fatalf("RenderJSON() error = %v", err)
	}
	var out bytes.Buffer
	err = ValidateBundle(&out, raw)
	if err == nil {
		t.Fatal("ValidateBundle() error = nil, want a failure")
	}
	if !strings.Contains(err.Error(), "local absolute path") {
		t.Fatalf("error = %v, want the canary reason", err)
	}
	if out.String() != "evidence bundle validation: failed\n" {
		t.Fatalf("verdict = %q", out.String())
	}
}

func TestValidateBundleReportsUndecodableInput(t *testing.T) {
	var out bytes.Buffer
	err := ValidateBundle(&out, []byte("not json"))
	if err == nil {
		t.Fatal("ValidateBundle() error = nil for undecodable input")
	}
	if !strings.Contains(err.Error(), "decode evidence bundle") {
		t.Fatalf("error = %v, want it to name the decode step", err)
	}
	if out.Len() != 0 {
		t.Fatalf("verdict = %q, want nothing printed before a decode failure", out.String())
	}
}
