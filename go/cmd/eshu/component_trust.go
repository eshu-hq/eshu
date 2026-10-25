// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"os"

	"github.com/spf13/cobra"

	clicomponent "github.com/eshu-hq/eshu/go/internal/cli/component"
	"github.com/eshu-hq/eshu/go/internal/component"
)

// This file holds the component trust-policy wiring shared by every
// `eshu component` command that verifies a package: the trust flags and the
// policy builder that resolves them against the ESHU_COMPONENT_* variables.
// Split out of component.go to keep that file under the 500-line cap.

func addTrustFlags(cmd *cobra.Command) {
	addTrustFlagsWithDefault(cmd, component.TrustModeDisabled)
}

func addOptionalTrustFlags(cmd *cobra.Command) {
	addTrustFlagsWithDefault(cmd, "")
}

func addTrustFlagsWithDefault(cmd *cobra.Command, defaultMode string) {
	cmd.Flags().String(componentTrustModeFlag, defaultMode, "Component trust mode: disabled, allowlist, or strict")
	cmd.Flags().StringSlice(componentAllowIDFlag, nil, "Allowed component ID")
	cmd.Flags().StringSlice(componentAllowPublisherFlag, nil, "Allowed component publisher")
	cmd.Flags().StringSlice(componentRevokeIDFlag, nil, "Revoked component ID")
	cmd.Flags().StringSlice(componentRevokePublisherFlag, nil, "Revoked component publisher")
	cmd.Flags().String(componentCosignBinaryFlag, "", "Cosign verifier binary for strict component trust")
	cmd.Flags().String(componentProvenanceIdentityFlag, "", "Expected Sigstore certificate identity for strict component trust")
	cmd.Flags().String(componentProvenanceIssuerFlag, "", "Expected Sigstore OIDC issuer for strict component trust")
	cmd.Flags().String(
		componentCoreVersionFlag,
		"",
		"Eshu core version the operator asserts for compatibleCore checks (default: this build's version)",
	)
	cmd.Flags().String(
		componentProvenancePredicateTypeFlag,
		component.DefaultProvenancePredicateType,
		"Cosign attestation predicate type for strict component trust",
	)
}

// componentPolicyFromFlags builds the trust policy for a command from its
// flags and the process environment.
func componentPolicyFromFlags(cmd *cobra.Command) component.Policy {
	return componentPolicyFromFlagsAndEnv(cmd, os.Getenv)
}

// componentPolicyFromFlagsAndEnv builds the trust policy for a command. For
// the trust mode and the allow lists, a passed flag wins, then the matching
// ESHU_COMPONENT_* variable, then the flag default. The revoke lists are the
// union of the flags and the variables, so no flag can empty a deny list. The
// core version follows the same flag-then-variable order: an empty result
// keeps the build version (#9090). It is a compatibility input, not a trust
// input, so it applies only when some trust input is configured; on its own it
// would make an unconfigured `list` policy non-zero and re-verify every
// installed component. The provenance and cosign settings stay flag-only.
func componentPolicyFromFlagsAndEnv(cmd *cobra.Command, getenv func(string) string) component.Policy {
	str := func(flagName, envName string) string {
		value, _ := cmd.Flags().GetString(flagName)
		return clicomponent.FallbackString(value, flagPassed(cmd, flagName), getenv(envName))
	}
	list := func(flagName, envName string) []string {
		value, _ := cmd.Flags().GetStringSlice(flagName)
		return clicomponent.FallbackList(value, flagPassed(cmd, flagName), getenv(envName))
	}
	revoke := func(flagName, envName string) []string {
		value, _ := cmd.Flags().GetStringSlice(flagName)
		return clicomponent.UnionList(value, getenv(envName))
	}
	mode := str(componentTrustModeFlag, envComponentTrustMode)
	cosignBinary, _ := cmd.Flags().GetString(componentCosignBinaryFlag)
	provenanceIdentity, _ := cmd.Flags().GetString(componentProvenanceIdentityFlag)
	provenanceIssuer, _ := cmd.Flags().GetString(componentProvenanceIssuerFlag)
	provenancePredicateType, _ := cmd.Flags().GetString(componentProvenancePredicateTypeFlag)
	if !flagPassed(cmd, componentProvenancePredicateTypeFlag) && provenanceIdentity == "" && provenanceIssuer == "" {
		// The predicate-type flag has a non-empty default. Carrying it alone
		// would make an unconfigured policy non-zero, and a non-zero policy
		// re-verifies every installed component in list. A blank value is
		// equivalent for strict mode, which defaults the predicate itself.
		provenancePredicateType = ""
	}
	policy := component.Policy{
		Mode:              mode,
		AllowedIDs:        list(componentAllowIDFlag, envComponentAllowIDs),
		AllowedPublishers: list(componentAllowPublisherFlag, envComponentAllowPublishers),
		RevokedIDs:        revoke(componentRevokeIDFlag, envComponentRevokeIDs),
		RevokedPublishers: revoke(componentRevokePublisherFlag, envComponentRevokePublishers),
		Provenance: component.ProvenancePolicy{
			CertificateIdentity: provenanceIdentity,
			OIDCIssuer:          provenanceIssuer,
			PredicateType:       provenancePredicateType,
		},
	}
	if mode == component.TrustModeStrict {
		policy.ProvenanceVerifier = component.CosignProvenanceVerifier{Command: cosignBinary}
	}
	if componentTrustConfigured(policy) {
		policy.CoreVersion = str(componentCoreVersionFlag, envComponentCoreVersion)
	}
	return policy
}

// componentTrustConfigured reports whether a policy carries any trust input.
// Install and verify always do (their trust mode defaults to disabled); `list`
// does only when the operator set one.
func componentTrustConfigured(policy component.Policy) bool {
	return policy.Mode != "" ||
		len(policy.AllowedIDs) > 0 ||
		len(policy.AllowedPublishers) > 0 ||
		len(policy.RevokedIDs) > 0 ||
		len(policy.RevokedPublishers) > 0 ||
		policy.Provenance != (component.ProvenancePolicy{}) ||
		policy.ProvenanceVerifier != nil
}

// flagPassed reports whether the operator set the flag on the command line.
func flagPassed(cmd *cobra.Command, name string) bool {
	flag := cmd.Flags().Lookup(name)
	return flag != nil && flag.Changed
}
