// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"

	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
)

func TestAccessRequiresBothRoleAttestorsInBoundaryMode(t *testing.T) {
	env := map[string]string{
		"ESHU_POSTGRES_DSN":                "postgres://writer:secret@primary/db",
		"ESHU_POSTGRES_READ_DSN":           "postgres://reader:secret@primary/db",
		"ESHU_POSTGRES_READ_TOPOLOGY":      "primary",
		"ESHU_POSTGRES_ROLE_CONTRACT_JSON": `{"policy_revision":1}`,
	}
	cfg, err := LoadConfig(func(key string) string { return env[key] })
	if err != nil {
		t.Fatal(err)
	}
	if access, err := Open(context.Background(), cfg, nil); err == nil || !strings.Contains(err.Error(), "role attestors") {
		if access != nil {
			_ = access.Close()
		}
		t.Fatalf("Open() error = %v, want missing role attestors before dial", err)
	}
}

func TestConfigureRoleAttestorsBindsAPIRoutes(t *testing.T) {
	env := map[string]string{
		"ESHU_POSTGRES_DSN":           "postgres://writer:secret@primary/db",
		"ESHU_POSTGRES_READ_DSN":      "postgres://reader:secret@primary/db",
		"ESHU_POSTGRES_READ_TOPOLOGY": "primary",
		"ESHU_POSTGRES_ROLE_CONTRACT_JSON": `{"policy_revision":7,"database_identity":"91d1dfab-4726-449a-948e-d459ee2fd0eb","database":"db","routes":{
"writer":{"contract_id":"api-writer-7","session_role":"writer","effective_role":"writer"},
"reader":{"contract_id":"api-reader-7","session_role":"reader","effective_role":"reader"}}}`,
	}
	cfg, err := LoadConfig(func(key string) string { return env[key] })
	if err != nil {
		t.Fatal(err)
	}
	if err := ConfigureRoleAttestors(&cfg, func(key string) string { return env[key] }, runtimecfg.PostgresWorkloadAPI); err != nil {
		t.Fatal(err)
	}
	if cfg.WriterRoleAttestor == nil || cfg.ReaderRoleAttestor == nil {
		t.Fatal("API writer or reader role attestor missing")
	}
}

func TestRoleAttestedConfigRejectsEmptyRequiredContract(t *testing.T) {
	env := map[string]string{
		"ESHU_POSTGRES_ROLE_BOUNDARY_REQUIRED": "true",
		"ESHU_POSTGRES_DSN":                    "invalid-dsn",
	}
	_, err := LoadRoleAttestedConfig(func(key string) string { return env[key] }, runtimecfg.PostgresWorkloadAPI)
	if err == nil || !strings.Contains(err.Error(), "ESHU_POSTGRES_ROLE_CONTRACT_JSON") {
		t.Fatalf("required role boundary error = %v, want empty contract denial before dial", err)
	}
}
