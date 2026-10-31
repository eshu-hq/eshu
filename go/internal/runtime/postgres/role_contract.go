// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"fmt"
	"strings"

	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
)

// LoadRoleAttestedConfig validates pool routing and binds the compiled API or
// MCP workload to its protected writer and reader contracts before any dial.
func LoadRoleAttestedConfig(getenv func(string) string, workload runtimecfg.PostgresWorkload) (Config, error) {
	cfg, err := LoadConfig(getenv)
	if err != nil {
		return Config{}, err
	}
	if err := ConfigureRoleAttestors(&cfg, getenv, workload); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// ConfigureRoleAttestors binds code-owned API or MCP route classes to both
// physical pool validators. Open refuses boundary mode unless both are set.
func ConfigureRoleAttestors(cfg *Config, getenv func(string) string, workload runtimecfg.PostgresWorkload) error {
	if cfg == nil || getenv == nil {
		return fmt.Errorf("postgres role attestor configuration is required")
	}
	if workload != runtimecfg.PostgresWorkloadAPI && workload != runtimecfg.PostgresWorkloadMCP {
		return fmt.Errorf("postgres API/MCP role attestor workload is invalid")
	}
	if cfg.roleBoundaryConfigured != (strings.TrimSpace(getenv("ESHU_POSTGRES_ROLE_CONTRACT_JSON")) != "") {
		return fmt.Errorf("postgres role boundary configuration changed after pool load")
	}
	writer, err := runtimecfg.LoadPostgresRoleContract(getenv, workload, runtimecfg.PostgresRouteWriter)
	if err != nil {
		return err
	}
	reader, err := runtimecfg.LoadPostgresRoleContract(getenv, workload, runtimecfg.PostgresRouteReader)
	if err != nil {
		return err
	}
	if cfg.roleBoundaryConfigured && (writer == nil || reader == nil) {
		return fmt.Errorf("postgres role boundary requires both route contracts")
	}
	if writer != nil {
		cfg.WriterRoleAttestor = writer.Attest
		cfg.ReaderRoleAttestor = reader.Attest
	}
	return nil
}
