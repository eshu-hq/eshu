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
	componentHomeFlag            = "component-home"
	componentTrustModeFlag       = "trust-mode"
	componentAllowIDFlag         = "allow-id"
	componentAllowPublisherFlag  = "allow-publisher"
	componentRevokeIDFlag        = "revoke-id"
	componentRevokePublisherFlag = "revoke-publisher"
	componentInstanceFlag        = "instance"
	componentModeFlag            = "mode"
	componentClaimsFlag          = "claims"
	componentConfigFlag          = "config"
	componentVersionFlag         = "version"
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

	addComponentHomeFlag(installCmd)
	addComponentHomeFlag(listCmd)
	addComponentHomeFlag(enableCmd)
	addComponentHomeFlag(disableCmd)
	addComponentHomeFlag(uninstallCmd)
	addTrustFlags(verifyCmd)
	addTrustFlags(installCmd)
	enableCmd.Flags().String(componentInstanceFlag, "", "Collector instance ID to enable")
	enableCmd.Flags().String(componentModeFlag, "manual", "Collector activation mode")
	enableCmd.Flags().Bool(componentClaimsFlag, false, "Enable workflow claims for this component instance")
	enableCmd.Flags().String(componentConfigFlag, "", "Path to component instance configuration")
	disableCmd.Flags().String(componentInstanceFlag, "", "Collector instance ID to disable")
	uninstallCmd.Flags().String(componentVersionFlag, "", "Component version to uninstall")

	componentCmd.AddCommand(inspectCmd, verifyCmd, installCmd, listCmd, enableCmd, disableCmd, uninstallCmd)
}

func runComponentInspect(cmd *cobra.Command, args []string) error {
	manifest, err := component.LoadManifest(args[0])
	if err != nil {
		return err
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
		return err
	}
	result := componentPolicyFromFlags(cmd).Verify(manifest)
	if !result.Allowed {
		return fmt.Errorf("component verification failed: %s", result.Reason)
	}
	_, err = fmt.Fprintf(cmd.OutOrStdout(), "verified %s@%s with %s policy\n",
		manifest.Metadata.ID,
		manifest.Metadata.Version,
		result.Mode,
	)
	return err
}

func runComponentInstall(cmd *cobra.Command, args []string) error {
	manifest, err := component.LoadManifest(args[0])
	if err != nil {
		return err
	}
	result := componentPolicyFromFlags(cmd).Verify(manifest)
	registry := component.NewRegistry(componentHomeFromFlags(cmd))
	installed, err := registry.Install(args[0], result)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(cmd.OutOrStdout(), "installed %s@%s\n", installed.ID, installed.Version)
	return err
}

func runComponentList(cmd *cobra.Command, _ []string) error {
	components, err := component.NewRegistry(componentHomeFromFlags(cmd)).List()
	if err != nil {
		return err
	}
	if len(components) == 0 {
		_, err = fmt.Fprintln(cmd.OutOrStdout(), "no components installed")
		return err
	}
	for _, installed := range components {
		if _, err := fmt.Fprintf(
			cmd.OutOrStdout(),
			"%s\t%s\t%s\tactivations=%d\n",
			installed.ID,
			installed.Version,
			installed.TrustMode,
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
		return fmt.Errorf("--%s is required", componentInstanceFlag)
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
	activation, err := component.NewRegistry(componentHomeFromFlags(cmd)).Enable(args[0], component.Activation{
		InstanceID:    instanceID,
		Mode:          mode,
		ClaimsEnabled: claimsEnabled,
		ConfigPath:    configPath,
	})
	if err != nil {
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
		return fmt.Errorf("--%s is required", componentInstanceFlag)
	}
	if err := component.NewRegistry(componentHomeFromFlags(cmd)).Disable(args[0], instanceID); err != nil {
		return err
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
		return fmt.Errorf("--%s is required", componentVersionFlag)
	}
	if err := component.NewRegistry(componentHomeFromFlags(cmd)).Uninstall(args[0], version); err != nil {
		return err
	}
	_, err = fmt.Fprintf(cmd.OutOrStdout(), "uninstalled %s@%s\n", args[0], version)
	return err
}

func addComponentHomeFlag(cmd *cobra.Command) {
	cmd.Flags().String(componentHomeFlag, "", "Component registry home directory")
}

func addTrustFlags(cmd *cobra.Command) {
	cmd.Flags().String(componentTrustModeFlag, component.TrustModeDisabled, "Component trust mode: disabled, allowlist, or strict")
	cmd.Flags().StringSlice(componentAllowIDFlag, nil, "Allowed component ID")
	cmd.Flags().StringSlice(componentAllowPublisherFlag, nil, "Allowed component publisher")
	cmd.Flags().StringSlice(componentRevokeIDFlag, nil, "Revoked component ID")
	cmd.Flags().StringSlice(componentRevokePublisherFlag, nil, "Revoked component publisher")
}

func componentPolicyFromFlags(cmd *cobra.Command) component.Policy {
	mode, _ := cmd.Flags().GetString(componentTrustModeFlag)
	allowedIDs, _ := cmd.Flags().GetStringSlice(componentAllowIDFlag)
	allowedPublishers, _ := cmd.Flags().GetStringSlice(componentAllowPublisherFlag)
	revokedIDs, _ := cmd.Flags().GetStringSlice(componentRevokeIDFlag)
	revokedPublishers, _ := cmd.Flags().GetStringSlice(componentRevokePublisherFlag)
	return component.Policy{
		Mode:              mode,
		AllowedIDs:        allowedIDs,
		AllowedPublishers: allowedPublishers,
		RevokedIDs:        revokedIDs,
		RevokedPublishers: revokedPublishers,
	}
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
