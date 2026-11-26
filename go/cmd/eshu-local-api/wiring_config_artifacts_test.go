// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	contentartifacts "github.com/eshu-hq/eshu/go/internal/content/artifacts"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	artifacts "github.com/eshu-hq/eshu/go/internal/query/repositoryartifacts"
	storeartifacts "github.com/eshu-hq/eshu/go/internal/storage/postgres/config/artifacts"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// These tests flip the production loader's read mode, so they are serial:
// no t.Parallel here. Each resets the mode on cleanup.
func resetReads(t *testing.T) {
	t.Helper()
	artifacts.ResetProductionReads()
	t.Cleanup(artifacts.ResetProductionReads)
}

func modeGetenv(mode string) func(string) string {
	return func(key string) string {
		if key == "ESHU_CONFIG_ARTIFACT_READ_MODE" {
			return mode
		}
		return ""
	}
}

// TestConfigureConfigArtifactReadsRejectsUnknownMode pins fail-fast
// startup: an unknown mode errors instead of serving a mode it did not ask
// for.
func TestConfigureConfigArtifactReadsRejectsUnknownMode(t *testing.T) {
	resetReads(t)
	if err := configureConfigArtifactReads(modeGetenv("direct"), nil, nil, nil); err == nil {
		t.Fatal("configure(direct) = nil error, want startup to fail")
	}
}

// TestConfigureConfigArtifactReadsLiveIsDefault pins the default: unset or
// live needs no reader and configures nothing.
func TestConfigureConfigArtifactReadsLiveIsDefault(t *testing.T) {
	resetReads(t)
	for _, mode := range []string{"", "live"} {
		if err := configureConfigArtifactReads(modeGetenv(mode), nil, nil, nil); err != nil {
			t.Fatalf("configure(%q) error = %v, want nil", mode, err)
		}
	}
}

// TestConfigureConfigArtifactReadsRequiresAReader pins the second fail-fast:
// shadow or stored with no Postgres reader errors instead of silently
// serving live under a stored flag.
func TestConfigureConfigArtifactReadsRequiresAReader(t *testing.T) {
	resetReads(t)
	for _, mode := range []string{"shadow", "stored"} {
		if err := configureConfigArtifactReads(modeGetenv(mode), nil, nil, nil); err == nil {
			t.Fatalf("configure(%q, nil reader) = nil error, want startup to fail", mode)
		}
	}
}

// fakeStorageReader is a scripted storeartifacts.Reader: one answer per id
// with the requested extractor version recorded.
type fakeStorageReader struct {
	repos   map[string]storeartifacts.Repository
	version int
	calls   [][]string
	readErr error
}

func (f *fakeStorageReader) ReadStored(_ context.Context, repoIDs []string, extractorVersion int) ([]storeartifacts.Repository, error) {
	f.calls = append(f.calls, repoIDs)
	f.version = extractorVersion
	if f.readErr != nil {
		return nil, f.readErr
	}
	out := make([]storeartifacts.Repository, 0, len(repoIDs))
	for _, id := range repoIDs {
		if repo, ok := f.repos[id]; ok {
			out = append(out, repo)
		}
	}
	return out, nil
}

// TestConfigArtifactStoredAdapterMapsStorageAnswers pins the adapter: trust
// follows the storage reason, the reason maps 1:1 as the reads-counter
// label, list_truncated rides along, and rows map field for field.
func TestConfigArtifactStoredAdapterMapsStorageAnswers(t *testing.T) {
	t.Parallel()

	storage := &fakeStorageReader{repos: map[string]storeartifacts.Repository{
		"repo-a": {
			RepoID: "repo-a", Reason: storeartifacts.ReasonTrusted,
			Rows: []storeartifacts.Row{{RawPath: "mod/app.hcl", RelativePath: "app.hcl", EvidenceKind: "terraform", NameScoped: true}},
		},
		"repo-b": {RepoID: "repo-b", Reason: storeartifacts.ReasonStaleVersion},
	}}
	adapter := configArtifactStoredAdapter{reader: storage}
	got, err := adapter.ReadStored(context.Background(), []string{"repo-a", "repo-b"})
	if err != nil {
		t.Fatalf("ReadStored() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("ReadStored() returned %d repositories, want 2", len(got))
	}
	if !got[0].Trusted || got[0].Reason != "" {
		t.Fatalf("repo-a = trusted %v reason %q, want trusted with an empty reason", got[0].Trusted, got[0].Reason)
	}
	row := got[0].Rows[0]
	if row.RawPath != "mod/app.hcl" || row.RelativePath != "app.hcl" || row.EvidenceKind != "terraform" || !row.NameScoped {
		t.Fatalf("repo-a row = %+v, want the stored row field for field", row)
	}
	if got[1].Trusted || got[1].Reason != string(storeartifacts.ReasonStaleVersion) {
		t.Fatalf("repo-b = trusted %v reason %q, want untrusted stale_version", got[1].Trusted, got[1].Reason)
	}
	if len(got[1].Rows) != 0 {
		t.Fatalf("repo-b carries %d rows, want none", len(got[1].Rows))
	}
	if storage.version != contentartifacts.ExtractorVersion {
		t.Fatalf("adapter extractor version = %d, want the ExtractorVersion constant (%d)", storage.version, contentartifacts.ExtractorVersion)
	}
}

// fakePGQueryer serves one fixed row set for the stored-read statement.
type fakePGQueryer struct {
	values [][]any
	calls  int
}

func (f *fakePGQueryer) QueryContext(ctx context.Context, _ string, _ ...any) (db.Rows, error) {
	f.calls++
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &fakePGRows{values: f.values}, nil
}

type fakePGRows struct {
	values [][]any
	next   int
}

func (r *fakePGRows) Next() bool {
	if r.next >= len(r.values) {
		return false
	}
	r.next++
	return true
}

func (r *fakePGRows) Scan(dest ...any) error {
	row := r.values[r.next-1]
	if len(dest) != len(row) {
		return errors.New("scan: destination count differs from column count")
	}
	for i, target := range dest {
		scanner, ok := target.(sql.Scanner)
		if !ok {
			return errors.New("scan: destination is not a sql.Scanner")
		}
		if err := scanner.Scan(row[i]); err != nil {
			return err
		}
	}
	return nil
}

func (r *fakePGRows) Err() error   { return nil }
func (r *fakePGRows) Close() error { return nil }

// wiringGraph serves one related repository.
type wiringGraph struct{}

func (wiringGraph) Run(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
	return []map[string]any{{"repo_id": "repo-a", "repo_name": "name-a"}}, nil
}

func (wiringGraph) RunSingle(_ context.Context, _ string, _ map[string]any) (map[string]any, error) {
	return nil, nil
}

// wiringContent serves every repository the anchor's Terraform module file
// and records every list and read by repository, so a test sees whether a
// related repository was read live.
type wiringContent struct {
	querycontract.ContentStore
	listed []string
	read   map[string]int
}

func (s *wiringContent) countRead(repoID string) {
	if s.read == nil {
		s.read = map[string]int{}
	}
	s.read[repoID]++
}

func wiringAnchorFiles(repoID string) []querycontract.FileContent {
	return []querycontract.FileContent{
		{RepoID: repoID, RelativePath: "infra/main.tf", Content: "module \"x\" {\n  source = \"./modules/x\"\n}\n"},
	}
}

func (s *wiringContent) ListRepoFiles(_ context.Context, repoID string, _ int) ([]querycontract.FileContent, error) {
	s.listed = append(s.listed, repoID)
	files := wiringAnchorFiles(repoID)
	for i := range files {
		files[i].Content = ""
	}
	return files, nil
}

func (s *wiringContent) GetFileContent(_ context.Context, repoID, relativePath string) (*querycontract.FileContent, error) {
	for _, file := range wiringAnchorFiles(repoID) {
		if file.RelativePath == relativePath {
			s.countRead(repoID)
			file := file
			return &file, nil
		}
	}
	return nil, nil
}

func (s *wiringContent) GetFileContents(_ context.Context, repoID string, paths []string) ([]querycontract.FileContent, error) {
	files := wiringAnchorFiles(repoID)
	byPath := map[string]querycontract.FileContent{}
	for _, file := range files {
		byPath[file.RelativePath] = file
	}
	out := make([]querycontract.FileContent, 0, len(paths))
	for _, path := range paths {
		if file, ok := byPath[path]; ok {
			s.countRead(repoID)
			out = append(out, file)
		}
	}
	return out, nil
}

func (s *wiringContent) NonBlankRepoFilePaths(_ context.Context, repoID string, paths []string) ([]string, error) {
	s.countRead(repoID)
	return append([]string(nil), paths...), nil
}

// trustedEmptyPGRow is one stored-read wire row for a trusted repository
// with no config artifacts: content_version 7, extractor 1, row_count 0,
// current version 7, NULL paths, whole window.
func trustedEmptyPGRow(repoID string) []any {
	return []any{repoID, int64(7), int64(1), int64(0), int64(7), nil, nil, nil, nil, false}
}

// TestConfigureStoredModeServesStoredRows is the vertical proof of the
// wiring slice: env stored, Postgres reader, adapter, leaf. The related
// repository answers from the stored statement while its content is never
// listed.
func TestConfigureStoredModeServesStoredRows(t *testing.T) {
	resetReads(t)
	pg := &fakePGQueryer{values: [][]any{trustedEmptyPGRow("repo-a")}}
	content := &wiringContent{}
	if err := configureConfigArtifactReads(modeGetenv("stored"), pg, nil, nil); err != nil {
		t.Fatalf("configure(stored) error = %v", err)
	}
	merged, err := artifacts.LoadSharedRepositoryConfigArtifacts(
		context.Background(), wiringGraph{}, content, "anchor", "svc", wiringAnchorFiles("anchor"),
	)
	if err != nil {
		t.Fatalf("load error = %v", err)
	}
	body, _ := json.Marshal(merged)
	if !strings.Contains(string(body), "infra/main.tf") || !strings.Contains(string(body), "svc") {
		t.Fatalf("stored body = %s, want the anchor live rows", body)
	}
	if strings.Contains(string(body), "name-a") {
		t.Fatalf("stored body = %s, want no repo-a rows: it is trusted-empty", body)
	}
	if content.read["repo-a"] != 0 {
		t.Fatalf("repo-a content reads = %d, want 0: it answers from the stored statement", content.read["repo-a"])
	}
	if pg.calls != 1 {
		t.Fatalf("stored statements = %d, want exactly 1", pg.calls)
	}
}

// TestConfigureShadowModeCountsTheCompare is the vertical proof of the
// shadow wiring: env shadow with an empty read model compares live rows
// against stored_missing and counts them on the meter while serving live.
func TestConfigureShadowModeCountsTheCompare(t *testing.T) {
	resetReads(t)
	pg := &fakePGQueryer{}
	content := &wiringContent{}
	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("wiring-test"))
	if err != nil {
		t.Fatalf("NewInstruments: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := configureConfigArtifactReads(modeGetenv("shadow"), pg, instruments, logger); err != nil {
		t.Fatalf("configure(shadow) error = %v", err)
	}
	merged, err := artifacts.LoadSharedRepositoryConfigArtifacts(
		context.Background(), wiringGraph{}, content, "anchor", "svc", wiringAnchorFiles("anchor"),
	)
	if err != nil {
		t.Fatalf("load error = %v", err)
	}
	body, _ := json.Marshal(merged)
	if !strings.Contains(string(body), "infra/main.tf") || !strings.Contains(string(body), "name-a") {
		t.Fatalf("shadow body = %s, want the live rows served", body)
	}
	if content.read["repo-a"] == 0 {
		t.Fatal("repo-a content was never read: the read tracker is blind, so the stored zero proves nothing")
	}
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}
	var storedMissing int64
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_config_artifact_shadow_compare_total" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("%s data = %T, want an int64 sum", m.Name, m.Data)
			}
			for _, point := range sum.DataPoints {
				if outcome, _ := point.Attributes.Value("outcome"); outcome.AsString() == "stored_missing" {
					storedMissing += point.Value
				}
			}
		}
	}
	if storedMissing != 1 {
		t.Fatalf("shadow stored_missing points = %d, want 1: the empty read model compares", storedMissing)
	}
}
