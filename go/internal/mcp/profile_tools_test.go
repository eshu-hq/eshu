// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	codedivergencetools "github.com/eshu-hq/eshu/go/internal/mcp/code/divergence"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"gopkg.in/yaml.v3"
)

// toolsListNames serves one tools/list call against s and returns the listed
// tool names in order.
func toolsListNames(t *testing.T, s *Server) []string {
	t.Helper()
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`
	req := httptest.NewRequest("POST", "/mcp/message", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.handleHTTPMessage(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("tools/list status = %d, want 200", rec.Code)
	}
	var resp jsonrpcResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode tools/list response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("tools/list error: %v", resp.Error)
	}
	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("tools/list result = %#v, want object", resp.Result)
	}
	rawTools, ok := result["tools"].([]any)
	if !ok {
		t.Fatalf("tools/list tools = %#v, want array", result["tools"])
	}
	names := make([]string, 0, len(rawTools))
	for _, raw := range rawTools {
		tool, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("tool entry = %#v, want object", raw)
		}
		name, ok := tool["name"].(string)
		if !ok {
			t.Fatalf("tool name = %#v, want string", tool["name"])
		}
		names = append(names, name)
	}
	return names
}

func profileTestServer(profile querycontract.QueryProfile, opts ...ServerOption) *Server {
	mux := http.NewServeMux()
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	return NewServer(mux, logger, append([]ServerOption{WithQueryProfile(profile)}, opts...)...)
}

// TestToolsListHidesCodeDivergenceOnProduction is the #7726 contract: the
// code-divergence tools are unsupported on the production profile
// (capability-matrix code_divergence.findings, ProductionMax nil), so a
// production-profile server must not advertise them.
func TestToolsListHidesCodeDivergenceOnProduction(t *testing.T) {
	names := toolsListNames(t, profileTestServer(querycontract.ProfileProduction))
	listed := make(map[string]bool, len(names))
	for _, name := range names {
		listed[name] = true
	}
	for _, tool := range codedivergencetools.Tools() {
		if listed[tool.Name] {
			t.Errorf("tools/list on production lists %q, want hidden", tool.Name)
		}
	}
	if got, want := len(names), len(ReadOnlyTools())-len(codedivergencetools.Tools()); got != want {
		t.Errorf("tools/list on production lists %d tools, want %d", got, want)
	}
	if !listed[ReadOnlyTools()[0].Name] {
		t.Errorf("tools/list on production hides %q, want listed", ReadOnlyTools()[0].Name)
	}
}

// TestToolsListShowsCodeDivergenceOnLocalProfiles proves the filter is
// production-only: every local profile and the default server (no profile
// option) list the full surface including the divergence family.
func TestToolsListShowsCodeDivergenceOnLocalProfiles(t *testing.T) {
	profiles := []querycontract.QueryProfile{
		querycontract.ProfileLocalLightweight,
		querycontract.ProfileLocalAuthoritative,
		querycontract.ProfileLocalFullStack,
	}
	for _, profile := range profiles {
		names := toolsListNames(t, profileTestServer(profile))
		listed := make(map[string]bool, len(names))
		for _, name := range names {
			listed[name] = true
		}
		for _, tool := range codedivergencetools.Tools() {
			if !listed[tool.Name] {
				t.Errorf("tools/list on %s hides %q, want listed", profile, tool.Name)
			}
		}
		if got, want := len(names), len(ReadOnlyTools()); got != want {
			t.Errorf("tools/list on %s lists %d tools, want %d", profile, got, want)
		}
	}

	// No profile option: the long-standing full list, unchanged.
	mux := http.NewServeMux()
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	names := toolsListNames(t, NewServer(mux, logger))
	if got, want := len(names), len(ReadOnlyTools()); got != want {
		t.Errorf("default tools/list lists %d tools, want %d", got, want)
	}
}

// TestProductionHiddenToolsTrackDivergenceFamily locks the hidden set to the
// family's own registration: a future fourth divergence tool hides
// automatically instead of leaking onto the production list.
func TestProductionHiddenToolsTrackDivergenceFamily(t *testing.T) {
	hidden := productionHiddenToolNames()
	family := codedivergencetools.Tools()
	if len(hidden) != len(family) {
		t.Fatalf("hidden tools = %d, want %d (the divergence family)", len(hidden), len(family))
	}
	for _, tool := range family {
		if !hidden[tool.Name] {
			t.Errorf("hidden set lacks family tool %q", tool.Name)
		}
	}
}

// matrixProfilesForTest mirrors the per-profile cells of one
// capability-matrix row: only the status field is needed here.
type matrixProfilesForTest map[string]struct {
	Status string `yaml:"status"`
}

// TestProductionHiddenToolsMatchCapabilityMatrix is the tripwire that keeps
// the hide list authoritative: every hidden tool must belong to a capability
// whose production cell is unsupported, and the matrix row for
// code_divergence.findings must name exactly the hidden tools. When remote
// validation lands and the row flips to supported, this test fails until the
// hide list is lifted with it.
func TestProductionHiddenToolsMatchCapabilityMatrix(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	specsDir := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "..", "..", "specs"))
	paths := []string{filepath.Join(specsDir, "capability-matrix.v1.yaml")}
	fragments, err := filepath.Glob(filepath.Join(specsDir, "capability-matrix", "*.yaml"))
	if err != nil {
		t.Fatalf("glob matrix fragments: %v", err)
	}
	paths = append(paths, fragments...)

	type matrixRow struct {
		Capability string                `yaml:"capability"`
		Tools      []string              `yaml:"tools"`
		Profiles   matrixProfilesForTest `yaml:"profiles"`
	}
	type matrixFile struct {
		Capabilities []matrixRow `yaml:"capabilities"`
	}
	rows := make(map[string]matrixRow)
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		var file matrixFile
		if err := yaml.Unmarshal(raw, &file); err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		for _, row := range file.Capabilities {
			rows[row.Capability] = row
		}
	}

	row, ok := rows["code_divergence.findings"]
	if !ok {
		t.Fatal("capability-matrix has no code_divergence.findings row")
	}
	if got := row.Profiles["production"].Status; got != "unsupported" {
		t.Errorf("code_divergence.findings production status = %q, want unsupported", got)
	}
	hidden := productionHiddenToolNames()
	if len(row.Tools) != len(hidden) {
		t.Fatalf("matrix row names %d tools, hidden set has %d", len(row.Tools), len(hidden))
	}
	for _, name := range row.Tools {
		if !hidden[name] {
			t.Errorf("matrix tool %q is not in the production hidden set", name)
		}
	}
}
