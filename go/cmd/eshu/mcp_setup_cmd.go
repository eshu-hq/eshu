// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/eshu-hq/eshu/go/internal/mcp"
)

// mcpSetupLongHelp is the `eshu mcp setup` (and `eshu m`) long help text. It
// describes the three auth postures (issue #5169, F-8) rather than
// advertising the shared ${ESHU_API_KEY} as the hosted default: that
// credential is now the shared-key posture, opt-in only.
const mcpSetupLongHelp = "Print platform-specific MCP client config and optionally install it.\n\n" +
	"By default this prints a safe snippet and writes nothing. Use --write to\n" +
	"merge the eshu server entry into the platform config, preserving existing\n" +
	"servers and keys. Use --hosted with --service-url for an HTTP endpoint.\n\n" +
	"--auth selects the credential story for hosted setup: auto (default)\n" +
	"probes the server's RFC 9728 discovery route and resolves per-user token\n" +
	"or SSO; sso forces the OAuth flow; token forces the per-user\n" +
	"${ESHU_MCP_TOKEN} reference; shared-key (or --shared-key) forces the\n" +
	"legacy admin/dev ${ESHU_API_KEY} credential, never emitted by default.\n" +
	"No posture ever prints a raw secret."

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
	cmd.Flags().String("auth", "auto", "MCP auth posture for hosted setup: auto, sso, token, or shared-key")
	cmd.Flags().Bool("shared-key", false, "Force the legacy shared ESHU_API_KEY credential (admin/dev only)")
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
	authFlag, _ := cmd.Flags().GetString("auth")
	sharedKey, _ := cmd.Flags().GetBool("shared-key")

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

	posture, err := resolveAuthPosture(authFlag, sharedKey, hosted, hostedPostureProbe, req.ServiceURL)
	if err != nil {
		return err
	}
	req.Posture = posture.Posture
	req.Issuers = posture.Issuers
	req.PreregisteredClientID = posture.PreregisteredClientID
	if posture.Warning != "" {
		printWarning(posture.Warning)
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
// In token posture, when no --api-key/ESHU_API_KEY was resolved, the client
// falls back to ESHU_MCP_TOKEN so verification exercises the same credential
// the emitted snippet actually wires -- otherwise a token-posture --verify run
// would silently probe unauthenticated even though the real client will
// authenticate.
func mcpSetupVerify(cmd *cobra.Command, platform *mcpPlatform, req mcpSetupRequest) error {
	snippet, err := renderSetupSnippet(platform, req)
	if err != nil {
		snippet = ""
	}

	var health healthProber
	var query queryProber
	if req.Mode == modeHostedHTTP {
		client := apiClientFromCmd(cmd)
		if req.Posture == postureToken && strings.TrimSpace(client.APIKey) == "" {
			if token := strings.TrimSpace(os.Getenv(mcpTokenEnvVar)); token != "" {
				client.APIKey = token
			}
		}
		health = apiHealthProber{client: client}
		query = apiQueryProber{client: client}
	}

	report := runVerification(snippet, mcp.ReadOnlyTools, health, query)
	fmt.Print(postureVerifyHeader(req) + renderVerifyReport(report))
	if !report.allOK() {
		return fmt.Errorf("mcp setup verification failed")
	}
	return nil
}

// postureVerifyHeader names the detected auth posture and probe outcome as a
// leading line before the staged verification report, so `--verify` output is
// self-explanatory about which credential story it exercised. Empty for local
// stdio mode, which carries no credential to name.
func postureVerifyHeader(req mcpSetupRequest) string {
	if req.Mode != modeHostedHTTP {
		return ""
	}
	switch req.Posture {
	case postureSSO:
		if len(req.Issuers) > 0 {
			return fmt.Sprintf("Auth posture: sso (issuer: %s)\n", req.Issuers[0])
		}
		return "Auth posture: sso\n"
	case postureSharedKey:
		return "Auth posture: shared-key (admin/dev " + apiKeyEnvVar + ")\n"
	default: // postureToken
		return "Auth posture: token (per-user " + mcpTokenEnvVar + ")\n"
	}
}
