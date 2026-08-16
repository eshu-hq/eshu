// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"os"

	cliconfig "github.com/eshu-hq/eshu/go/internal/cli/config"
	clidoctor "github.com/eshu-hq/eshu/go/internal/cli/doctor"
	"github.com/spf13/cobra"
)

// runDoctor resolves the process facts `eshu doctor` reports on and hands them
// to internal/cli/doctor, which owns the report and its redaction.
func runDoctor(cmd *cobra.Command, _ []string) error {
	neo4jURI := os.Getenv("NEO4J_URI")
	if neo4jURI == "" {
		neo4jURI = cliconfig.ResolveValue("NEO4J_URI", "")
	}

	return clidoctor.Run(cmd.OutOrStdout(), clidoctor.Deps{
		ConfigDir:   cliconfig.Home(),
		EnvFilePath: cliconfig.EnvFilePath(),
		APIBaseURL:  NewAPIClient("", "", "").BaseURL,
		Neo4jURI:    neo4jURI,
		PostgresDSN: os.Getenv("ESHU_POSTGRES_DSN"),
	})
}
