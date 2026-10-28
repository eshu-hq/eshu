// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/parser/pythondep"
)

func TestPythonRuntimeNativeHandoffAndPrivateTransport(t *testing.T) {
	repoRoot := t.TempDir()
	writeCollectorTestFile(t, filepath.Join(repoRoot, "pyproject.toml"), "[project]\nrequires-python = '>=3.10'\n[tool.poetry.dependencies]\npython = '^3.11'\nrequests = '^2'\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "Pipfile"), "[requires]\npython_version = '3.12'\npython_full_version = '3.12.1'\n[packages]\nrequests = '*'\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "setup.py"), "from setuptools import setup\nsetup(python_requires='>=3.9')\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "setup.cfg"), "[options]\npython_requires = 3.8.1\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "runtime.txt"), "python-3.11.2\n")
	observed := time.Date(2026, time.October, 28, 0, 0, 0, 0, time.UTC)
	snapshot, err := (NativeRepositorySnapshotter{Now: func() time.Time { return observed }}).SnapshotRepository(context.Background(), SelectedRepository{RepoPath: repoRoot})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.PythonRuntime) != 3 {
		t.Fatalf("native Python handoffs = %d", len(snapshot.PythonRuntime))
	}
	for _, parsed := range snapshot.FileData {
		if _, ok := parsed["python_runtime_evidence"]; ok {
			t.Fatal("Python private transport reached FileData")
		}
	}
	collected := buildStreamingGeneration(repoRoot, testCollectorRepositoryMetadata(repoRoot), "run-python", observed, snapshot, false, "")
	envelopes := drainFactChannel(collected.Facts)
	declarations := decodeDeclarationList(t, envelopes)
	want := map[string]string{
		"pyproject_requires_python": ">=3.10", "poetry_python": "^3.11",
		"pipfile_python_version": "3.12", "pipfile_python_full_version": "3.12.1",
		"setup_py_python_requires": ">=3.9", "setup_cfg_python_requires": "3.8.1", "runtime_txt": "python-3.11.2",
	}
	if len(declarations) != len(want) {
		t.Fatalf("declarations = %+v", declarations)
	}
	for _, got := range declarations {
		if want[got.SourceKind] != got.RawSpec || got.Runtime != "python" {
			t.Errorf("unexpected declaration %+v", got)
		}
		delete(want, got.SourceKind)
	}
	if len(want) != 0 {
		t.Errorf("missing source kinds: %v", want)
	}
	for _, envelope := range envelopes {
		payload, err := json.Marshal(envelope.Payload)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(payload), "python_runtime_evidence") {
			t.Fatalf("private transport persisted in %s fact", envelope.FactKind)
		}
	}
}

func TestPythonContentVersionFixtureHasSevenRuntimeSources(t *testing.T) {
	items := contentVersionFixtureFacts(t, nil)
	want := map[string]string{
		"pyproject_requires_python":   "manifests/python/pyproject.toml|4.0|>=3.10",
		"poetry_python":               "manifests/python/pyproject.toml|8.0|^3.11",
		"pipfile_python_version":      "manifests/python/Pipfile|2.0|3.12",
		"pipfile_python_full_version": "manifests/python/Pipfile|3.0|3.12.1",
		"setup_cfg_python_requires":   "manifests/python/setup.cfg|2.0|>=3.9",
		"setup_py_python_requires":    "manifests/python/setup.py|2.0|>=3.8",
		"runtime_txt":                 "manifests/python/runtime.txt|1.0|python-3.11.2",
	}
	got := map[string]string{}
	count := 0
	for _, item := range items {
		if item.Kind != facts.RuntimeDeclarationVersionFactKind {
			continue
		}
		var record struct {
			StableKey string `json:"stable_key"`
			Payload   struct {
				RepoID       string `json:"repo_id"`
				RelativePath string `json:"relative_path"`
				SourceKind   string `json:"source_kind"`
				RawSpec      string `json:"raw_spec"`
				LineNumber   int    `json:"line_number"`
				OrderIndex   int    `json:"order_index"`
			} `json:"payload"`
		}
		if err := json.Unmarshal([]byte(item.Line), &record); err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(record.Payload.RelativePath, "manifests/python/") {
			continue
		}
		count++
		stableKey := facts.StableID(facts.RuntimeDeclarationVersionFactKind, map[string]any{
			"repo_id":       record.Payload.RepoID,
			"relative_path": record.Payload.RelativePath,
			"line_number":   record.Payload.LineNumber,
			"runtime":       "python",
			"order_index":   record.Payload.OrderIndex,
		})
		if record.StableKey != stableKey {
			t.Fatalf("identity for %s = %q, want %q", record.Payload.SourceKind, record.StableKey, stableKey)
		}
		got[record.Payload.SourceKind] = fmt.Sprintf("%s|%d.%d|%s", record.Payload.RelativePath,
			record.Payload.LineNumber, record.Payload.OrderIndex, record.Payload.RawSpec)
	}
	if count != len(want) || !reflect.DeepEqual(got, want) {
		t.Fatalf("Python content-version runtime evidence = %#v (%d rows), want %#v", got, count, want)
	}
}

func TestPythonRuntimeTXTMetadataDropsCredentialBody(t *testing.T) {
	seed := "0123456789abcdef-secret"
	snapshot, envelopes := snapshotWithPolicy(t, parser.DefaultRegistry(), "runtime.txt", "python-3.12.1\n# "+seed+"\n")
	if got := snapshot.ContentStores["runtime.txt"].Policy; got != parser.ContentStoreMetadata {
		t.Fatalf("content policy = %s", got)
	}
	if got := contentFact(t, envelopes, "runtime.txt").Payload["content_body"]; got != "" {
		t.Fatalf("metadata body = %q", got)
	}
	assertNoSeedInAnyFact(t, envelopes, seed)
	declarations := decodeDeclarationList(t, envelopes)
	if len(declarations) != 1 || declarations[0].RawSpec != "python-3.12.1" {
		t.Fatalf("runtime declarations = %+v", declarations)
	}
}

func TestPythonRuntimeHandoffRejectsChangedBody(t *testing.T) {
	repoRoot := t.TempDir()
	path := filepath.Join(repoRoot, "pyproject.toml")
	writeCollectorTestFile(t, path, "[project]\nrequires-python = '3.12'\n")
	observed := time.Date(2026, time.October, 28, 0, 0, 0, 0, time.UTC)
	snapshot, err := (NativeRepositorySnapshotter{Now: func() time.Time { return observed }}).SnapshotRepository(context.Background(), SelectedRepository{RepoPath: repoRoot})
	if err != nil {
		t.Fatal(err)
	}
	writeCollectorTestFile(t, path, "[project]\nrequires-python = '3.13'\n")
	collected := buildStreamingGeneration(repoRoot, testCollectorRepositoryMetadata(repoRoot), "run-python-changed", observed, snapshot, false, "")
	if got := decodeDeclarationList(t, drainFactChannel(collected.Facts)); len(got) != 0 {
		t.Fatalf("mixed source revisions: %+v", got)
	}
	if collected.Generation.PreparseIdentityDropped == nil || !collected.Generation.PreparseIdentityDropped.Load() {
		t.Fatal("changed Python body did not request retry")
	}
}

func TestPythonRuntimeBareCRDigestBindsOriginalRevision(t *testing.T) {
	for _, tc := range []struct{ name, path, body string }{
		{"pyproject", "pyproject.toml", "[project]\rrequires-python = '3.12'\r"},
		{"pipfile", "Pipfile", "[requires]\rpython_version = '3.12'\r"},
		{"setup", "setup.py", "from setuptools import setup\rsetup(python_requires='3.12')\r"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repoRoot := t.TempDir()
			writeCollectorTestFile(t, filepath.Join(repoRoot, tc.path), tc.body)
			observed := time.Date(2026, time.October, 28, 0, 0, 0, 0, time.UTC)
			snapshot, err := (NativeRepositorySnapshotter{Now: func() time.Time { return observed }}).SnapshotRepository(context.Background(), SelectedRepository{RepoPath: repoRoot})
			if err != nil {
				t.Fatal(err)
			}
			if snapshot.PythonRuntime[tc.path].Digest != pythondep.PythonRuntimeDigest([]byte(tc.body)) {
				t.Fatalf("evidence digest does not bind raw %s bytes", tc.path)
			}
			collected := buildStreamingGeneration(repoRoot, testCollectorRepositoryMetadata(repoRoot), "run-python-cr", observed, snapshot, false, "")
			if got := decodeDeclarationList(t, drainFactChannel(collected.Facts)); len(got) != 1 || got[0].RawSpec != "3.12" {
				t.Fatalf("bare-CR declarations = %+v", got)
			}
			if collected.Generation.PreparseIdentityDropped != nil && collected.Generation.PreparseIdentityDropped.Load() {
				t.Fatal("unchanged bare-CR input requested retry")
			}
		})
	}
}

func TestPythonRuntimeMalformedNativeParseHasDeterministicSkip(t *testing.T) {
	repoRoot := t.TempDir()
	writeCollectorTestFile(t, filepath.Join(repoRoot, "pyproject.toml"), "[project]\nrequires-python = '3.12'\ninvalid = [\n")
	observed := time.Date(2026, time.October, 28, 0, 0, 0, 0, time.UTC)
	snapshot, err := (NativeRepositorySnapshotter{Now: func() time.Time { return observed }}).SnapshotRepository(context.Background(), SelectedRepository{RepoPath: repoRoot})
	if err != nil {
		t.Fatal(err)
	}
	collected := buildStreamingGeneration(repoRoot, testCollectorRepositoryMetadata(repoRoot), "run-python-invalid", observed, snapshot, false, "")
	if got := decodeDeclarationList(t, drainFactChannel(collected.Facts)); len(got) != 0 {
		t.Fatalf("malformed Python emitted %+v", got)
	}
	if collected.Generation.PreparseIdentityDropped != nil && collected.Generation.PreparseIdentityDropped.Load() {
		t.Fatal("unchanged malformed Python requested retry")
	}
}

func TestPythonRuntimeWrongTypeTransportNeverPersists(t *testing.T) {
	parsed := map[string]any{"path": filepath.Join(t.TempDir(), "pyproject.toml"), "python_runtime_evidence": map[string]any{"raw": "secret"}}
	snapshot := &RepositorySnapshot{}
	collectPythonRuntimeSnapshot(filepath.Dir(parsed["path"].(string)), []map[string]any{parsed}, snapshot)
	if _, present := parsed["python_runtime_evidence"]; present {
		t.Fatal("wrong-type private transport remained in parsed file")
	}
	if len(snapshot.PythonRuntime) != 0 || !snapshot.PythonRuntimeExpected {
		t.Fatalf("wrong-type transport accepted: %+v", snapshot.PythonRuntime)
	}
}
