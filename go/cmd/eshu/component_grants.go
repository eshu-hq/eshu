// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"time"

	"github.com/spf13/cobra"

	clicomponent "github.com/eshu-hq/eshu/go/internal/cli/component"
)

// This file is the cobra wiring for the `eshu component grant`,
// `revoke-grant`, and `grants` subcommands: core-issued producer
// authorizations for first-party producers to emit approved core-owned fact
// kinds. Like the rest of the component family, everything here resolves
// flags, environment, and streams, then passes plain values to
// internal/cli/component; logic added here is logic nothing outside this
// binary can test.

// componentGrantProducerFlag is the optional producer filter on
// `eshu component grants`. RunGrants never renders the name, so it lives
// with the wiring instead of the cli package flag registry.
const componentGrantProducerFlag = "producer"

func init() {
	grantCmd := &cobra.Command{
		Use:   "grant <producer-id>",
		Short: "Record a core-issued producer grant for a core-owned fact kind",
		Args:  cobra.ExactArgs(1),
		RunE:  runComponentGrant,
	}
	revokeGrantCmd := &cobra.Command{
		Use:   "revoke-grant <producer-id>",
		Short: "Revoke a recorded producer grant so emissions fail closed",
		Args:  cobra.ExactArgs(1),
		RunE:  runComponentRevokeGrant,
	}
	grantsCmd := &cobra.Command{
		Use:   "grants",
		Short: "List recorded producer grants",
		Args:  cobra.NoArgs,
		RunE:  runComponentGrants,
	}

	addComponentHomeFlag(grantCmd)
	addComponentHomeFlag(revokeGrantCmd)
	addComponentHomeFlag(grantsCmd)
	addComponentJSONFlag(grantCmd)
	addComponentJSONFlag(revokeGrantCmd)
	addComponentJSONFlag(grantsCmd)
	grantCmd.Flags().String(clicomponent.VersionFlag, "", "Granted manifest version to bind")
	grantCmd.Flags().String(clicomponent.GrantKindFlag, "", "Core-owned fact kind the producer may emit")
	grantCmd.Flags().StringSlice(clicomponent.GrantSchemaVersionFlag, nil, "Covered fact schema version; repeat for multiple versions")
	grantCmd.Flags().String(clicomponent.GrantScopeFlag, "", "Source scope the grant is valid for")
	grantCmd.Flags().Duration(clicomponent.GrantExpiresInFlag, 0, "Grant lifetime from issuance, for example 720h")
	revokeGrantCmd.Flags().String(clicomponent.VersionFlag, "", "Granted manifest version to revoke")
	revokeGrantCmd.Flags().String(clicomponent.GrantKindFlag, "", "Core-owned fact kind to revoke")
	revokeGrantCmd.Flags().String(clicomponent.GrantScopeFlag, "", "Source scope to revoke")
	grantsCmd.Flags().String(componentGrantProducerFlag, "", "Show only grants for one producer ID")

	componentCmd.AddCommand(grantCmd, revokeGrantCmd, grantsCmd)
}

func runComponentGrant(cmd *cobra.Command, args []string) error {
	version, err := cmd.Flags().GetString(clicomponent.VersionFlag)
	if err != nil {
		return err
	}
	kind, err := cmd.Flags().GetString(clicomponent.GrantKindFlag)
	if err != nil {
		return err
	}
	schemaVersions, err := cmd.Flags().GetStringSlice(clicomponent.GrantSchemaVersionFlag)
	if err != nil {
		return err
	}
	scope, err := cmd.Flags().GetString(clicomponent.GrantScopeFlag)
	if err != nil {
		return err
	}
	expiresIn, err := cmd.Flags().GetDuration(clicomponent.GrantExpiresInFlag)
	if err != nil {
		return err
	}
	return clicomponent.RunGrant(
		cmd.OutOrStdout(),
		componentJSONEnabled(cmd),
		componentHomeFromFlags(cmd),
		args[0],
		version,
		kind,
		schemaVersions,
		scope,
		expiresIn,
		time.Now(),
	)
}

func runComponentRevokeGrant(cmd *cobra.Command, args []string) error {
	version, err := cmd.Flags().GetString(clicomponent.VersionFlag)
	if err != nil {
		return err
	}
	kind, err := cmd.Flags().GetString(clicomponent.GrantKindFlag)
	if err != nil {
		return err
	}
	scope, err := cmd.Flags().GetString(clicomponent.GrantScopeFlag)
	if err != nil {
		return err
	}
	return clicomponent.RunRevokeGrant(
		cmd.OutOrStdout(),
		componentJSONEnabled(cmd),
		componentHomeFromFlags(cmd),
		args[0],
		version,
		kind,
		scope,
	)
}

func runComponentGrants(cmd *cobra.Command, _ []string) error {
	producer, err := cmd.Flags().GetString(componentGrantProducerFlag)
	if err != nil {
		return err
	}
	return clicomponent.RunGrants(
		cmd.OutOrStdout(),
		componentJSONEnabled(cmd),
		componentHomeFromFlags(cmd),
		producer,
	)
}
