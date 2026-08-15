// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	clicomponent "github.com/eshu-hq/eshu/go/internal/cli/component"
	"github.com/eshu-hq/eshu/go/internal/component"
)

// This file is the cobra wiring for the `eshu component` family. The command
// bodies live in go/internal/cli/component; everything here resolves flags,
// environment, and streams, then passes plain values across. Keep it that
// way: logic added here is logic nothing outside this binary can test.

const (
	componentHomeFlag                    = "component-home"
	componentTrustModeFlag               = "trust-mode"
	componentAllowIDFlag                 = "allow-id"
	componentAllowPublisherFlag          = "allow-publisher"
	componentRevokeIDFlag                = "revoke-id"
	componentRevokePublisherFlag         = "revoke-publisher"
	componentCosignBinaryFlag            = "cosign-binary"
	componentProvenanceIdentityFlag      = "provenance-certificate-identity"
	componentProvenanceIssuerFlag        = "provenance-oidc-issuer"
	componentProvenancePredicateTypeFlag = "provenance-predicate-type"
	componentInstanceFlag                = "instance"
	componentModeFlag                    = "mode"
	componentClaimsFlag                  = "claims"
	componentConfigFlag                  = "config"
	componentVersionFlag                 = "version"
	componentJSONFlag                    = "json"
	componentDryRunFlag                  = "dry-run"
	componentFixtureFlag                 = "fixture"
	componentInitIDFlag                  = "id"
	componentInitPublisherFlag           = "publisher"
	componentInitFactKindFlag            = "fact-kind"
	componentInitOutputFlag              = "output"
	componentSchemaCheckFlag             = "check"

	componentExtractionReadinessVerboseFlag = "verbose"
)

var componentCmd = &cobra.Command{
	Use:   "component",
	Short: "Manage optional Eshu components",
}

func init() {
	rootCmd.AddCommand(componentCmd)

	inspectCmd := &cobra.Command{
		Use:   "inspect <manifest>",
		Short: "Inspect a component package manifest",
		Args:  cobra.ExactArgs(1),
		RunE:  runComponentInspect,
	}
	verifyCmd := &cobra.Command{
		Use:   "verify <manifest>",
		Short: "Verify a component package manifest against local trust policy",
		Args:  cobra.ExactArgs(1),
		RunE:  runComponentVerify,
	}
	installCmd := &cobra.Command{
		Use:   "install <manifest>",
		Short: "Install a verified local component package manifest",
		Args:  cobra.ExactArgs(1),
		RunE:  runComponentInstall,
	}
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List installed components",
		Args:  cobra.NoArgs,
		RunE:  runComponentList,
	}
	enableCmd := &cobra.Command{
		Use:   "enable <component-id>",
		Short: "Enable an installed component instance",
		Args:  cobra.ExactArgs(1),
		RunE:  runComponentEnable,
	}
	disableCmd := &cobra.Command{
		Use:   "disable <component-id>",
		Short: "Disable an installed component instance",
		Args:  cobra.ExactArgs(1),
		RunE:  runComponentDisable,
	}
	uninstallCmd := &cobra.Command{
		Use:   "uninstall <component-id>",
		Short: "Uninstall an inactive component package",
		Args:  cobra.ExactArgs(1),
		RunE:  runComponentUninstall,
	}
	conformCmd := &cobra.Command{
		Use:   "conform <manifest>",
		Short: "Run component extension conformance fixtures",
		Args:  cobra.ExactArgs(1),
		RunE:  runComponentConform,
	}
	initCmd := &cobra.Command{
		Use:   "init",
		Short: "Scaffold a new component package",
	}
	initCollectorCmd := &cobra.Command{
		Use:   "collector",
		Short: "Scaffold a collector component package",
		Args:  cobra.NoArgs,
		RunE:  runComponentInitCollector,
	}

	addComponentHomeFlag(installCmd)
	addComponentHomeFlag(listCmd)
	addComponentHomeFlag(enableCmd)
	addComponentHomeFlag(disableCmd)
	addComponentHomeFlag(uninstallCmd)
	addComponentHomeFlag(conformCmd)
	addTrustFlags(verifyCmd)
	addTrustFlags(installCmd)
	addOptionalTrustFlags(listCmd)
	for _, cmd := range []*cobra.Command{inspectCmd, verifyCmd, installCmd, listCmd, enableCmd, disableCmd, uninstallCmd, conformCmd} {
		addComponentJSONFlag(cmd)
	}
	addComponentJSONFlag(initCollectorCmd)
	installCmd.Flags().Bool(componentDryRunFlag, false, "Verify install and render the planned result without writing component state")
	enableCmd.Flags().String(componentInstanceFlag, "", "Collector instance ID to enable")
	enableCmd.Flags().String(componentModeFlag, "manual", "Collector activation mode")
	enableCmd.Flags().Bool(componentClaimsFlag, false, "Enable workflow claims for this component instance")
	enableCmd.Flags().String(componentConfigFlag, "", "Path to component instance configuration")
	enableCmd.Flags().Bool(componentDryRunFlag, false, "Validate activation and render the planned result without writing component state")
	disableCmd.Flags().String(componentInstanceFlag, "", "Collector instance ID to disable")
	uninstallCmd.Flags().String(componentVersionFlag, "", "Component version to uninstall")
	conformCmd.Flags().StringSlice(componentFixtureFlag, nil, "Collector SDK result fixture JSON file; repeat for multiple fixtures")
	conformCmd.Flags().String(componentModeFlag, "fixture", "Conformance mode: fixture or compose")
	initCollectorCmd.Flags().String(componentInitIDFlag, "", "Component ID, for example dev.example.collector.demo")
	initCollectorCmd.Flags().String(componentInitPublisherFlag, "", "Component publisher allowlist identity")
	initCollectorCmd.Flags().String(componentInitFactKindFlag, "", "Namespaced fact kind emitted by the scaffold")
	initCollectorCmd.Flags().String(componentInitOutputFlag, "", "Output directory; defaults to ./<component-id>")

	initCmd.AddCommand(initCollectorCmd)
	componentCmd.AddCommand(initCmd, inspectCmd, verifyCmd, installCmd, listCmd, enableCmd, disableCmd, uninstallCmd, conformCmd)
}

func init() {
	indexCmd := &cobra.Command{
		Use:   "index",
		Short: "Manage component extension index metadata",
	}
	indexVerifyCmd := &cobra.Command{
		Use:   "verify <index>",
		Short: "Verify a component extension index for local or CI publication gates",
		Args:  cobra.ExactArgs(1),
		RunE:  runComponentIndexVerify,
	}
	addComponentJSONFlag(indexVerifyCmd)
	indexCmd.AddCommand(indexVerifyCmd)
	componentCmd.AddCommand(indexCmd)
}

func init() {
	cmd := &cobra.Command{
		Use:   "extraction-readiness [collector-family]",
		Short: "Explain whether a collector is keep-in-tree, an extraction candidate, blocked, or external-ready",
		Long: "Report the advisory collector extraction readiness checklist. The output is " +
			"informational: it never moves code or changes runtime behavior. With no argument it " +
			"lists every collector family the extraction policy tracks; with a family argument it " +
			"explains that single family's per-criterion checklist.",
		Args: cobra.MaximumNArgs(1),
		RunE: runComponentExtractionReadiness,
	}
	cmd.Flags().Bool(componentJSONFlag, false, "Emit machine-readable JSON")
	cmd.Flags().Bool(componentExtractionReadinessVerboseFlag, false, "Show every criterion, not just blockers")
	componentCmd.AddCommand(cmd)
}

func init() {
	cmd := &cobra.Command{
		Use:   "schema-versions",
		Short: "List core fact-kind schema versions or classify a collector fact version",
		Long: "Report the schema version each core reducer or query consumer currently " +
			"supports for every core fact kind. With --check fact_kind=version it classifies " +
			"a collector's fact schema version as supported, unsupported_major, " +
			"unsupported_minor, or unknown_kind, and exits non-zero when the version is not " +
			"supported. The command is read-only and never changes runtime behavior.",
		Args: cobra.NoArgs,
		RunE: runComponentSchemaVersions,
	}
	cmd.Flags().Bool(componentJSONFlag, false, "Emit machine-readable JSON")
	cmd.Flags().String(componentSchemaCheckFlag, "", "Classify one fact version as fact_kind=schema_version")
	componentCmd.AddCommand(cmd)
}

func runComponentInspect(cmd *cobra.Command, args []string) error {
	return clicomponent.RunInspect(cmd.OutOrStdout(), componentJSONEnabled(cmd), args[0])
}

func runComponentVerify(cmd *cobra.Command, args []string) error {
	return clicomponent.RunVerify(cmd.OutOrStdout(), componentJSONEnabled(cmd), componentPolicyFromFlags(cmd), args[0])
}

func runComponentInstall(cmd *cobra.Command, args []string) error {
	return clicomponent.RunInstall(
		cmd.OutOrStdout(),
		componentJSONEnabled(cmd),
		componentDryRunEnabled(cmd),
		componentHomeFromFlags(cmd),
		componentPolicyFromFlags(cmd),
		args[0],
	)
}

func runComponentList(cmd *cobra.Command, _ []string) error {
	return clicomponent.RunList(
		cmd.OutOrStdout(),
		componentJSONEnabled(cmd),
		componentHomeFromFlags(cmd),
		componentPolicyFromFlags(cmd),
	)
}

func runComponentEnable(cmd *cobra.Command, args []string) error {
	instanceID, err := cmd.Flags().GetString(componentInstanceFlag)
	if err != nil {
		return err
	}
	mode, err := cmd.Flags().GetString(componentModeFlag)
	if err != nil {
		return err
	}
	claimsEnabled, err := cmd.Flags().GetBool(componentClaimsFlag)
	if err != nil {
		return err
	}
	configPath, err := cmd.Flags().GetString(componentConfigFlag)
	if err != nil {
		return err
	}
	request := component.Activation{
		InstanceID:    instanceID,
		Mode:          mode,
		ClaimsEnabled: claimsEnabled,
		ConfigPath:    configPath,
	}
	return clicomponent.RunEnable(
		cmd.OutOrStdout(),
		componentJSONEnabled(cmd),
		componentDryRunEnabled(cmd),
		componentHomeFromFlags(cmd),
		args[0],
		request,
	)
}

func runComponentDisable(cmd *cobra.Command, args []string) error {
	instanceID, err := cmd.Flags().GetString(componentInstanceFlag)
	if err != nil {
		return err
	}
	return clicomponent.RunDisable(
		cmd.OutOrStdout(),
		componentJSONEnabled(cmd),
		componentHomeFromFlags(cmd),
		args[0],
		instanceID,
	)
}

func runComponentUninstall(cmd *cobra.Command, args []string) error {
	version, err := cmd.Flags().GetString(componentVersionFlag)
	if err != nil {
		return err
	}
	return clicomponent.RunUninstall(
		cmd.OutOrStdout(),
		componentJSONEnabled(cmd),
		componentHomeFromFlags(cmd),
		args[0],
		version,
	)
}

func runComponentConform(cmd *cobra.Command, args []string) error {
	fixtures, err := cmd.Flags().GetStringSlice(componentFixtureFlag)
	if err != nil {
		return err
	}
	mode, err := cmd.Flags().GetString(componentModeFlag)
	if err != nil {
		return err
	}
	return clicomponent.RunConform(
		cmd.Context(),
		cmd.OutOrStdout(),
		componentJSONEnabled(cmd),
		componentHomeFromFlags(cmd),
		args[0],
		fixtures,
		mode,
	)
}

func runComponentInitCollector(cmd *cobra.Command, _ []string) error {
	id, _ := cmd.Flags().GetString(componentInitIDFlag)
	publisher, _ := cmd.Flags().GetString(componentInitPublisherFlag)
	factKind, _ := cmd.Flags().GetString(componentInitFactKindFlag)
	output, _ := cmd.Flags().GetString(componentInitOutputFlag)
	return clicomponent.RunInitCollector(cmd.OutOrStdout(), componentJSONEnabled(cmd), id, publisher, factKind, output)
}

func runComponentIndexVerify(cmd *cobra.Command, args []string) error {
	return clicomponent.RunIndexVerify(cmd.OutOrStdout(), componentJSONEnabled(cmd), args[0])
}

func runComponentExtractionReadiness(cmd *cobra.Command, args []string) error {
	asJSON, err := cmd.Flags().GetBool(componentJSONFlag)
	if err != nil {
		return err
	}
	verbose, err := cmd.Flags().GetBool(componentExtractionReadinessVerboseFlag)
	if err != nil {
		return err
	}
	return clicomponent.RunExtractionReadiness(cmd.OutOrStdout(), asJSON, verbose, args)
}

func runComponentSchemaVersions(cmd *cobra.Command, _ []string) error {
	asJSON, err := cmd.Flags().GetBool(componentJSONFlag)
	if err != nil {
		return err
	}
	check, err := cmd.Flags().GetString(componentSchemaCheckFlag)
	if err != nil {
		return err
	}
	return clicomponent.RunSchemaVersions(cmd.OutOrStdout(), asJSON, check)
}

func addComponentHomeFlag(cmd *cobra.Command) {
	cmd.Flags().String(componentHomeFlag, "", "Component registry home directory")
}

func addComponentJSONFlag(cmd *cobra.Command) {
	cmd.Flags().Bool(componentJSONFlag, false, "Write stable JSON output")
}

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
		componentProvenancePredicateTypeFlag,
		component.DefaultProvenancePredicateType,
		"Cosign attestation predicate type for strict component trust",
	)
}

// componentJSONEnabled reads the shared --json flag, treating a command that
// never registered it as text-mode.
func componentJSONEnabled(cmd *cobra.Command) bool {
	if cmd.Flags().Lookup(componentJSONFlag) == nil {
		return false
	}
	enabled, _ := cmd.Flags().GetBool(componentJSONFlag)
	return enabled
}

// componentDryRunEnabled reads the shared --dry-run flag, treating a command
// that never registered it as a real run.
func componentDryRunEnabled(cmd *cobra.Command) bool {
	if cmd.Flags().Lookup(componentDryRunFlag) == nil {
		return false
	}
	enabled, _ := cmd.Flags().GetBool(componentDryRunFlag)
	return enabled
}

func componentPolicyFromFlags(cmd *cobra.Command) component.Policy {
	mode, _ := cmd.Flags().GetString(componentTrustModeFlag)
	allowedIDs, _ := cmd.Flags().GetStringSlice(componentAllowIDFlag)
	allowedPublishers, _ := cmd.Flags().GetStringSlice(componentAllowPublisherFlag)
	revokedIDs, _ := cmd.Flags().GetStringSlice(componentRevokeIDFlag)
	revokedPublishers, _ := cmd.Flags().GetStringSlice(componentRevokePublisherFlag)
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

func componentHomeFromFlags(cmd *cobra.Command) string {
	home, _ := cmd.Flags().GetString(componentHomeFlag)
	if strings.TrimSpace(home) != "" {
		return home
	}
	if envHome := strings.TrimSpace(os.Getenv("ESHU_COMPONENT_HOME")); envHome != "" {
		return envHome
	}
	if eshuHome := strings.TrimSpace(os.Getenv("ESHU_HOME")); eshuHome != "" {
		return filepath.Join(eshuHome, "components")
	}
	userHome, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(userHome) == "" {
		return ""
	}
	return filepath.Join(userHome, ".eshu", "components")
}
