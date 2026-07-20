// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/eshu-hq/eshu/go/internal/mcp"
)

// mcpSetupLongHelp is the `eshu mcp setup` (and `eshu m`) long help text.
const mcpSetupLongHelp = "Print platform-specific MCP client config and optionally install it.\n\n" +
	"By default this prints a safe snippet and writes nothing. Use --write to\n" +
	"merge the eshu server entry into the platform config, preserving existing\n" +
	"servers and keys. Use --hosted with --service-url for an HTTP endpoint;\n" +
	"the bearer token is emitted as a ${ESHU_API_KEY} reference, never inline."

// addMCPSetupFlags registers the flags shared by `eshu mcp setup` and its `eshu
// m` alias on cmd. Both commands must expose the identical flag set: a command
// that forgets one of these silently reads a zero value for it instead of
// failing, which is why registration is centralized here rather than repeated
// per command (see service.go's mAlias wiring history).
func addMCPSetupFlags(cmd *cobra.Command) {
	cmd.Flags().String("platform", "generic", "Target MCP client: "+strings.Join(supportedPlatformNames(), ", "))
	cmd.Flags().Bool("hosted", false, "Generate hosted HTTP setup instead of local stdio")
	cmd.Flags().Bool("write", false, "Merge the config into the platform's file instead of printing it")
	cmd.Flags().String("target", "", "Override the file path used by --write")
	cmd.Flags().Bool("verify", false, "Run staged verification (config, reachable, tools, first query)")
	addRemoteFlags(cmd)
}

// newMCPSetupCmd builds the `eshu mcp setup` command.
func newMCPSetupCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Configure IDE and CLI MCP integrations",
		Long:  mcpSetupLongHelp,
		RunE:  runMCPSetup,
	}
	addMCPSetupFlags(cmd)
	return cmd
}

// newMCPSetupAliasCmd builds the `eshu m` shortcut for `eshu mcp setup`.
func newMCPSetupAliasCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "m",
		Short:  "Shortcut for 'eshu mcp setup'",
		Hidden: false,
		RunE:   runMCPSetup,
	}
	addMCPSetupFlags(cmd)
	return cmd
}

func runMCPSetup(cmd *cobra.Command, args []string) error {
	platformName, _ := cmd.Flags().GetString("platform")
	hosted, _ := cmd.Flags().GetBool("hosted")
	write, _ := cmd.Flags().GetBool("write")
	target, _ := cmd.Flags().GetString("target")
	verify, _ := cmd.Flags().GetBool("verify")

	platform, err := resolvePlatform(platformName)
	if err != nil {
		return err
	}

	req := mcpSetupRequest{Mode: modeLocalStdio}
	if hosted {
		req.Mode = modeHostedHTTP
		client := apiClientFromCmd(cmd)
		req.ServiceURL = client.BaseURL
		req.APIKey = client.APIKey
	}

	if write {
		return mcpSetupWrite(platform, req, target)
	}

	if verify {
		return mcpSetupVerify(cmd, platform, req)
	}

	snippet, err := renderSetupSnippet(platform, req)
	if err != nil {
		return err
	}
	fmt.Print(snippet)
	return nil
}

// mcpSetupWrite merges the eshu entry into the platform config file and reports
// where it landed. It never prints a raw token.
func mcpSetupWrite(platform *mcpPlatform, req mcpSetupRequest, target string) error {
	if !platform.Writable {
		return fmt.Errorf("platform %q does not support --write; print the snippet and add it to %s manually",
			platform.Name, platform.TargetFile)
	}
	path := strings.TrimSpace(target)
	if path == "" {
		def, err := defaultWriteTarget(platform)
		if err != nil {
			return err
		}
		path = def
	}
	if err := writeMCPServerConfig(platform, req, path); err != nil {
		return err
	}
	printSuccess(fmt.Sprintf("Merged eshu MCP server into %s", describeWriteTarget(path)))
	return nil
}

// mcpSetupVerify runs the staged verification, reusing the API client for hosted
// reachability and the embedded read-only MCP tool surface for tool visibility.
func mcpSetupVerify(cmd *cobra.Command, platform *mcpPlatform, req mcpSetupRequest) error {
	snippet, err := renderSetupSnippet(platform, req)
	if err != nil {
		snippet = ""
	}

	var health healthProber
	var query queryProber
	if req.Mode == modeHostedHTTP {
		client := apiClientFromCmd(cmd)
		health = apiHealthProber{client: client}
		query = apiQueryProber{client: client}
	}

	report := runVerification(snippet, mcp.ReadOnlyTools, health, query)
	fmt.Print(renderVerifyReport(report))
	if !report.allOK() {
		return fmt.Errorf("mcp setup verification failed")
	}
	return nil
}
