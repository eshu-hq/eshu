// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/eshu-hq/eshu/go/internal/component"
)

// The environment variables below configure the same component trust policy
// the API, MCP server, and coordinator read at startup. They are the CLI's
// fallback when the matching flag is not passed, so a shell that already
// carries the service policy judges components the way the services do.
const (
	envComponentTrustMode        = "ESHU_COMPONENT_TRUST_MODE"
	envComponentAllowIDs         = "ESHU_COMPONENT_ALLOW_IDS"
	envComponentAllowPublishers  = "ESHU_COMPONENT_ALLOW_PUBLISHERS"
	envComponentRevokeIDs        = "ESHU_COMPONENT_REVOKE_IDS"
	envComponentRevokePublishers = "ESHU_COMPONENT_REVOKE_PUBLISHERS"
)

// componentPolicyFromFlags builds the trust policy for a command from its
// flags and the process environment.
func componentPolicyFromFlags(cmd *cobra.Command) component.Policy {
	return componentPolicyFromFlagsAndEnv(cmd, os.Getenv)
}

// componentPolicyFromFlagsAndEnv builds the trust policy for a command. For
// the trust mode and the allow and revoke lists, an explicitly passed flag
// wins, then the matching ESHU_COMPONENT_* variable, then the flag default.
// The provenance and cosign settings stay flag-only.
func componentPolicyFromFlagsAndEnv(cmd *cobra.Command, getenv func(string) string) component.Policy {
	mode := stringFlagWithEnv(cmd, componentTrustModeFlag, getenv(envComponentTrustMode))
	allowedIDs := listFlagWithEnv(cmd, componentAllowIDFlag, getenv(envComponentAllowIDs))
	allowedPublishers := listFlagWithEnv(cmd, componentAllowPublisherFlag, getenv(envComponentAllowPublishers))
	revokedIDs := listFlagWithEnv(cmd, componentRevokeIDFlag, getenv(envComponentRevokeIDs))
	revokedPublishers := listFlagWithEnv(cmd, componentRevokePublisherFlag, getenv(envComponentRevokePublishers))
	cosignBinary, _ := cmd.Flags().GetString(componentCosignBinaryFlag)
	provenanceIdentity, _ := cmd.Flags().GetString(componentProvenanceIdentityFlag)
	provenanceIssuer, _ := cmd.Flags().GetString(componentProvenanceIssuerFlag)
	provenancePredicateType, _ := cmd.Flags().GetString(componentProvenancePredicateTypeFlag)
	policy := component.Policy{
		Mode:              mode,
		AllowedIDs:        allowedIDs,
		AllowedPublishers: allowedPublishers,
		RevokedIDs:        revokedIDs,
		RevokedPublishers: revokedPublishers,
		Provenance: component.ProvenancePolicy{
			CertificateIdentity: provenanceIdentity,
			OIDCIssuer:          provenanceIssuer,
			PredicateType:       provenancePredicateType,
		},
	}
	if mode == component.TrustModeStrict {
		policy.ProvenanceVerifier = component.CosignProvenanceVerifier{Command: cosignBinary}
	}
	return policy
}

// stringFlagWithEnv returns the flag value when the operator passed the flag,
// else the trimmed environment value when set, else the flag's own value.
func stringFlagWithEnv(cmd *cobra.Command, name string, envValue string) string {
	value, _ := cmd.Flags().GetString(name)
	if flagPassed(cmd, name) {
		return value
	}
	if trimmed := strings.TrimSpace(envValue); trimmed != "" {
		return trimmed
	}
	return value
}

// listFlagWithEnv is stringFlagWithEnv for a comma-separated list: the
// environment value splits on commas and drops blank entries.
func listFlagWithEnv(cmd *cobra.Command, name string, envValue string) []string {
	values, _ := cmd.Flags().GetStringSlice(name)
	if flagPassed(cmd, name) {
		return values
	}
	var fromEnv []string
	for _, field := range strings.Split(envValue, ",") {
		if trimmed := strings.TrimSpace(field); trimmed != "" {
			fromEnv = append(fromEnv, trimmed)
		}
	}
	if len(fromEnv) > 0 {
		return fromEnv
	}
	return values
}

// flagPassed reports whether the operator set the flag on the command line.
func flagPassed(cmd *cobra.Command, name string) bool {
	flag := cmd.Flags().Lookup(name)
	return flag != nil && flag.Changed
}
