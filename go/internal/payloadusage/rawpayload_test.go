// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package payloadusage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckRawPayloadConventionFailsOnNewRawRead(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeRawPayloadFixture(t, dir, "consumer.go", `package fixture

import "github.com/eshu-hq/eshu/go/internal/facts"

func repoID(env facts.Envelope) string {
	repoID, _ := env.Payload["repo_id"].(string)
	return repoID
}
`)

	findings, err := CheckRawPayloadConvention(RawPayloadConfig{
		RepoRoot:      dir,
		Dirs:          []string{dir},
		MaxExemptions: 0,
	})
	if err != nil {
		t.Fatalf("CheckRawPayloadConvention() error = %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("len(findings) = %d, want 1: %+v", len(findings), findings)
	}
	finding := findings[0]
	if finding.Path != "consumer.go" || finding.Accessor != rawPayloadIndexAccessor || finding.Key != "repo_id" {
		t.Fatalf("finding = %+v, want consumer.go payload_index repo_id", finding)
	}
}

func TestCheckRawPayloadConventionFailsOnAliasedPayloadRead(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeRawPayloadFixture(t, dir, "consumer.go", `package fixture

import "github.com/eshu-hq/eshu/go/internal/facts"

func repoID(env facts.Envelope) string {
	payload := env.Payload
	repoID, _ := payload["repo_id"].(string)
	return repoID
}
`)

	findings, err := CheckRawPayloadConvention(RawPayloadConfig{
		RepoRoot:      dir,
		Dirs:          []string{dir},
		MaxExemptions: 0,
	})
	if err != nil {
		t.Fatalf("CheckRawPayloadConvention() error = %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("len(findings) = %d, want 1: %+v", len(findings), findings)
	}
	finding := findings[0]
	if finding.Path != "consumer.go" || finding.Accessor != rawPayloadIndexAccessor || finding.Key != "repo_id" {
		t.Fatalf("finding = %+v, want consumer.go payload_index repo_id", finding)
	}
}

func TestCheckRawPayloadConventionFailsOnPayloadHelperKey(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeRawPayloadFixture(t, dir, "consumer.go", `package fixture

import "github.com/eshu-hq/eshu/go/internal/facts"

func repoID(env facts.Envelope) string {
	return requireString(env.Payload, "repo_id")
}

func requireString(payload map[string]any, key string) string {
	raw, _ := payload[key].(string)
	return raw
}
`)

	findings, err := CheckRawPayloadConvention(RawPayloadConfig{
		RepoRoot:      dir,
		Dirs:          []string{dir},
		MaxExemptions: 0,
	})
	if err != nil {
		t.Fatalf("CheckRawPayloadConvention() error = %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("len(findings) = %d, want 1: %+v", len(findings), findings)
	}
	finding := findings[0]
	if finding.Path != "consumer.go" || finding.Accessor != "requireString" || finding.Key != "repo_id" {
		t.Fatalf("finding = %+v, want consumer.go requireString repo_id", finding)
	}
}

func TestCheckRawPayloadConventionAllowsDecodeSeamFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeRawPayloadFixture(t, dir, "factschema_decode_fixture.go", `package fixture

import "github.com/eshu-hq/eshu/go/internal/facts"

func decode(env facts.Envelope) string {
	repoID, _ := env.Payload["repo_id"].(string)
	return repoID
}
`)

	findings, err := CheckRawPayloadConvention(RawPayloadConfig{
		RepoRoot:      dir,
		Dirs:          []string{dir},
		MaxExemptions: 0,
	})
	if err != nil {
		t.Fatalf("CheckRawPayloadConvention() error = %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("len(findings) = %d, want 0: %+v", len(findings), findings)
	}
}

func TestCheckRawPayloadConventionRejectsExemptionGrowth(t *testing.T) {
	t.Parallel()

	_, err := CheckRawPayloadConvention(RawPayloadConfig{
		RepoRoot:      t.TempDir(),
		Dirs:          nil,
		Exemptions:    []RawPayloadExemption{{Path: "consumer.go", Accessor: rawPayloadIndexAccessor, Key: "repo_id"}},
		MaxExemptions: 0,
	})
	if err == nil {
		t.Fatal("CheckRawPayloadConvention() error = nil, want exemption-growth error")
	}
	if !strings.Contains(err.Error(), "exemption list grew") {
		t.Fatalf("error = %v, want exemption-growth wording", err)
	}
}

func TestCheckRawPayloadConventionHonorsExplicitExemption(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeRawPayloadFixture(t, dir, "consumer.go", `package fixture

import "github.com/eshu-hq/eshu/go/internal/facts"

func repoID(env facts.Envelope) string {
	return payloadString(env.Payload, "repo_id")
}
`)

	findings, err := CheckRawPayloadConvention(RawPayloadConfig{
		RepoRoot: dir,
		Dirs:     []string{dir},
		Exemptions: []RawPayloadExemption{
			{Path: "consumer.go", Accessor: "payloadString", Key: "repo_id"},
		},
		MaxExemptions: 1,
	})
	if err != nil {
		t.Fatalf("CheckRawPayloadConvention() error = %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("len(findings) = %d, want 0: %+v", len(findings), findings)
	}
}

func TestGateFailsOnNewRawRead(t *testing.T) {
	t.Parallel()

	reducerDir := writeDecodeSurface(t, fixtureDecodeFile, `package reducer

import "github.com/eshu-hq/eshu/go/internal/facts"

func reduce(env facts.Envelope) string {
	resource, _ := decodeAWSResource(env)
	return resource.ResourceID
}
`)
	relationshipsDir := t.TempDir()
	writeRawPayloadFixture(t, relationshipsDir, "raw_consumer.go", `package relationships

import "github.com/eshu-hq/eshu/go/internal/facts"

func repoID(env facts.Envelope) string {
	repoID, _ := env.Payload["repo_id"].(string)
	return repoID
}
`)

	_, _, err := Gate(Paths{
		RepoRoot:         repoRoot(t),
		ReducerDir:       reducerDir,
		ProjectorDir:     t.TempDir(),
		QueryDir:         t.TempDir(),
		LoaderDir:        t.TempDir(),
		RelationshipsDir: relationshipsDir,
		ReplayDir:        t.TempDir(),
	})
	if err == nil {
		t.Fatal("Gate() error = nil, want raw payload convention failure")
	}
	if !strings.Contains(err.Error(), "raw fact payload") || !strings.Contains(err.Error(), "raw_consumer.go") {
		t.Fatalf("Gate() error = %v, want raw payload finding naming raw_consumer.go", err)
	}
}

func writeRawPayloadFixture(t *testing.T, dir string, name string, contents string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(contents), 0o600); err != nil {
		t.Fatalf("write raw payload fixture: %v", err)
	}
}
