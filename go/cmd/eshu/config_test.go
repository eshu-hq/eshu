package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetConfigValuePersistsApiKey(t *testing.T) {
	home := t.TempDir()
	t.Setenv(appHomeEnvVar, home)

	if err := setConfigValue("ESHU_API_KEY", "local-compose-token"); err != nil {
		t.Fatalf("setConfigValue() error = %v, want nil", err)
	}

	got := resolveConfigValue("ESHU_API_KEY", "")
	if got != "local-compose-token" {
		t.Fatalf("resolveConfigValue() = %q, want %q", got, "local-compose-token")
	}

	envBytes, err := os.ReadFile(filepath.Join(home, envFileName))
	if err != nil {
		t.Fatalf("ReadFile() error = %v, want nil", err)
	}
	if !strings.Contains(string(envBytes), "ESHU_API_KEY=local-compose-token") {
		t.Fatalf(".env = %q, want persisted token", string(envBytes))
	}
}

func TestConfigureDatabaseBackendPersistsNornicDBSelection(t *testing.T) {
	home := t.TempDir()
	t.Setenv(appHomeEnvVar, home)

	if _, err := configureDatabaseBackend("nornicdb"); err != nil {
		t.Fatalf("configureDatabaseBackend() error = %v, want nil", err)
	}

	if got := resolveConfigValue("ESHU_GRAPH_BACKEND", ""); got != "nornicdb" {
		t.Fatalf("ESHU_GRAPH_BACKEND = %q, want nornicdb", got)
	}
	if got := resolveConfigValue("DEFAULT_DATABASE", ""); got != "nornic" {
		t.Fatalf("DEFAULT_DATABASE = %q, want nornic", got)
	}
	if got := resolveConfigValue("ESHU_NEO4J_DATABASE", ""); got != "nornic" {
		t.Fatalf("ESHU_NEO4J_DATABASE = %q, want nornic", got)
	}
}

func TestConfigureDatabaseBackendPersistsNeo4jSelection(t *testing.T) {
	home := t.TempDir()
	t.Setenv(appHomeEnvVar, home)

	if _, err := configureDatabaseBackend("neo4j"); err != nil {
		t.Fatalf("configureDatabaseBackend() error = %v, want nil", err)
	}

	if got := resolveConfigValue("ESHU_GRAPH_BACKEND", ""); got != "neo4j" {
		t.Fatalf("ESHU_GRAPH_BACKEND = %q, want neo4j", got)
	}
	if got := resolveConfigValue("DEFAULT_DATABASE", ""); got != "neo4j" {
		t.Fatalf("DEFAULT_DATABASE = %q, want neo4j", got)
	}
	if got := resolveConfigValue("ESHU_NEO4J_DATABASE", ""); got != "neo4j" {
		t.Fatalf("ESHU_NEO4J_DATABASE = %q, want neo4j", got)
	}
}
