package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/eshu-hq/eshu/go/internal/component"
)

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

func runComponentInspect(cmd *cobra.Command, args []string) error {
	manifest, err := component.LoadManifest(args[0])
	if err != nil {
		return renderComponentError(cmd, "inspect", err)
	}
	if componentJSONEnabled(cmd) {
		payload := newComponentCLIOutput("inspect", "inspected")
		componentPayload := manifestCLIComponent(manifest)
		payload.Component = &componentPayload
		return writeComponentJSON(cmd.OutOrStdout(), payload)
	}
	_, err = fmt.Fprintf(
		cmd.OutOrStdout(),
		"%s\t%s\t%s\t%s\n",
		manifest.Metadata.ID,
		manifest.Metadata.Name,
		manifest.Metadata.Publisher,
		manifest.Metadata.Version,
	)
	return err
}

func runComponentVerify(cmd *cobra.Command, args []string) error {
	manifest, err := component.LoadManifest(args[0])
	if err != nil {
		return renderComponentError(cmd, "verify", err)
	}
	result := componentPolicyFromFlags(cmd).Verify(manifest)
	if !result.Allowed {
		return renderComponentVerificationError(cmd, "verify", result, componentVerificationFailure(result))
	}
	if componentJSONEnabled(cmd) {
		payload := newComponentCLIOutput("verify", "verified")
		componentPayload := manifestCLIComponent(manifest)
		payload.Component = &componentPayload
		payload.Verification = &result
		return writeComponentJSON(cmd.OutOrStdout(), payload)
	}
	_, err = fmt.Fprintf(
		cmd.OutOrStdout(), "verified %s@%s with %s policy\n",
		manifest.Metadata.ID,
		manifest.Metadata.Version,
		result.Mode,
	)
	return err
}

func runComponentInstall(cmd *cobra.Command, args []string) error {
	manifest, err := component.LoadManifest(args[0])
	if err != nil {
		return renderComponentError(cmd, "install", err)
	}
	result := componentPolicyFromFlags(cmd).Verify(manifest)
	if !result.Allowed {
		return renderComponentVerificationError(cmd, "install", result, componentVerificationFailure(result))
	}
	if componentDryRunEnabled(cmd) {
		if componentJSONEnabled(cmd) {
			payload := newComponentCLIOutput("install", "would_install")
			componentPayload := manifestCLIComponent(manifest)
			payload.DryRun = true
			payload.Component = &componentPayload
			payload.Verification = &result
			return writeComponentJSON(cmd.OutOrStdout(), payload)
		}
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "would install %s@%s\n", manifest.Metadata.ID, manifest.Metadata.Version)
		return err
	}
	registry := component.NewRegistry(componentHomeFromFlags(cmd))
	installed, err := registry.Install(args[0], result)
	if err != nil {
		return renderComponentError(cmd, "install", err)
	}
	if componentJSONEnabled(cmd) {
		payload := newComponentCLIOutput("install", "installed")
		componentPayload := installedCLIComponent(installed, []string{component.RegistryStateInstalled})
		payload.Component = &componentPayload
		payload.Verification = &result
		return writeComponentJSON(cmd.OutOrStdout(), payload)
	}
	_, err = fmt.Fprintf(cmd.OutOrStdout(), "installed %s@%s\n", installed.ID, installed.Version)
	return err
}

func runComponentList(cmd *cobra.Command, _ []string) error {
	readback, err := component.NewRegistry(componentHomeFromFlags(cmd)).Readback(componentPolicyFromFlags(cmd))
	if err != nil {
		return renderComponentError(cmd, "list", err)
	}
	if componentJSONEnabled(cmd) {
		payload := newComponentCLIOutput("list", "listed")
		payload.Components = make([]componentCLIComponent, 0, len(readback))
		for _, entry := range readback {
			payload.Components = append(payload.Components, readbackCLIComponent(entry))
		}
		return writeComponentJSON(cmd.OutOrStdout(), payload)
	}
	if len(readback) == 0 {
		_, err = fmt.Fprintln(cmd.OutOrStdout(), "no components installed")
		return err
	}
	for _, installed := range readback {
		if _, err := fmt.Fprintf(
			cmd.OutOrStdout(),
			"%s\t%s\t%s\tstates=%s\tactivations=%d\n",
			installed.ID,
			installed.Version,
			installed.TrustMode,
			strings.Join(installed.States, ","),
			len(installed.Activations),
		); err != nil {
			return err
		}
	}
	return nil
}

func runComponentEnable(cmd *cobra.Command, args []string) error {
	instanceID, err := cmd.Flags().GetString(componentInstanceFlag)
	if err != nil {
		return err
	}
	if strings.TrimSpace(instanceID) == "" {
		err := component.Errorf(component.ErrorCodeInvalidInput, "--%s is required", componentInstanceFlag)
		return renderComponentError(cmd, "enable", err)
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
	registry := component.NewRegistry(componentHomeFromFlags(cmd))
	request := component.Activation{
		InstanceID:    instanceID,
		Mode:          mode,
		ClaimsEnabled: claimsEnabled,
		ConfigPath:    configPath,
	}
	var activation component.Activation
	if componentDryRunEnabled(cmd) {
		activation, err = registry.PlanEnable(args[0], request)
	} else {
		activation, err = registry.Enable(args[0], request)
	}
	if err != nil {
		return renderComponentError(cmd, "enable", err)
	}
	if componentJSONEnabled(cmd) {
		status := "enabled"
		if componentDryRunEnabled(cmd) {
			status = "would_enable"
		}
		payload := newComponentCLIOutput("enable", status)
		payload.DryRun = componentDryRunEnabled(cmd)
		activationPayload := activationCLIOutput(activation)
		payload.Activation = &activationPayload
		componentPayload := componentCLIComponent{ID: args[0]}
		payload.Component = &componentPayload
		return writeComponentJSON(cmd.OutOrStdout(), payload)
	}
	if componentDryRunEnabled(cmd) {
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "would enable %s instance %s\n", args[0], activation.InstanceID)
		return err
	}
	_, err = fmt.Fprintf(cmd.OutOrStdout(), "enabled %s instance %s\n", args[0], activation.InstanceID)
	return err
}

func runComponentDisable(cmd *cobra.Command, args []string) error {
	instanceID, err := cmd.Flags().GetString(componentInstanceFlag)
	if err != nil {
		return err
	}
	if strings.TrimSpace(instanceID) == "" {
		err := component.Errorf(component.ErrorCodeInvalidInput, "--%s is required", componentInstanceFlag)
		return renderComponentError(cmd, "disable", err)
	}
	if err := component.NewRegistry(componentHomeFromFlags(cmd)).Disable(args[0], instanceID); err != nil {
		return renderComponentError(cmd, "disable", err)
	}
	if componentJSONEnabled(cmd) {
		payload := newComponentCLIOutput("disable", "disabled")
		componentPayload := componentCLIComponent{ID: args[0]}
		activationPayload := componentCLIActivation{InstanceID: instanceID}
		payload.Component = &componentPayload
		payload.Activation = &activationPayload
		return writeComponentJSON(cmd.OutOrStdout(), payload)
	}
	_, err = fmt.Fprintf(cmd.OutOrStdout(), "disabled %s instance %s\n", args[0], instanceID)
	return err
}

func runComponentUninstall(cmd *cobra.Command, args []string) error {
	version, err := cmd.Flags().GetString(componentVersionFlag)
	if err != nil {
		return err
	}
	if strings.TrimSpace(version) == "" {
		err := component.Errorf(component.ErrorCodeInvalidInput, "--%s is required", componentVersionFlag)
		return renderComponentError(cmd, "uninstall", err)
	}
	if err := component.NewRegistry(componentHomeFromFlags(cmd)).Uninstall(args[0], version); err != nil {
		return renderComponentError(cmd, "uninstall", err)
	}
	if componentJSONEnabled(cmd) {
		payload := newComponentCLIOutput("uninstall", "uninstalled")
		componentPayload := componentCLIComponent{ID: args[0], Version: version}
		payload.Component = &componentPayload
		return writeComponentJSON(cmd.OutOrStdout(), payload)
	}
	_, err = fmt.Fprintf(cmd.OutOrStdout(), "uninstalled %s@%s\n", args[0], version)
	return err
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
