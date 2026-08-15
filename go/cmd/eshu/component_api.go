// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	clicomponent "github.com/eshu-hq/eshu/go/internal/cli/component"
)

// This file is the cobra wiring for `eshu component inventory` and
// `eshu component diagnostics`. The fetch and render logic lives in
// go/internal/cli/component; this side resolves flags, builds the API
// client, classifies transport failures, and maps envelope error codes onto
// the process exit code -- the pieces that must stay in package main.

type componentAPIOptions struct {
	JSON  bool
	Limit int
}

// The fetch indirection points at the extracted package; it keeps the
// call sites in the run functions swappable in one place.
var (
	componentFetchInventory   = clicomponent.FetchInventory
	componentFetchDiagnostics = clicomponent.FetchDiagnostics
)

func init() {
	inventoryCmd := &cobra.Command{
		Use:   "inventory",
		Short: "List component extensions through the configured API",
		Args:  cobra.NoArgs,
		RunE:  runComponentInventory,
	}
	diagnosticsCmd := &cobra.Command{
		Use:   "diagnostics <component-id>",
		Short: "Read component extension diagnostics through the configured API",
		Args:  cobra.ExactArgs(1),
		RunE:  runComponentDiagnostics,
	}
	addComponentAPIFlags(inventoryCmd)
	inventoryCmd.Flags().Int("limit", clicomponent.InventoryDefaultLimit, "Maximum number of component rows to return")
	addComponentAPIFlags(diagnosticsCmd)
	componentCmd.AddCommand(inventoryCmd, diagnosticsCmd)
}

func addComponentAPIFlags(cmd *cobra.Command) {
	cmd.Flags().Bool("json", false, "Write the canonical component extension envelope as JSON")
	addRemoteFlags(cmd)
}

func runComponentInventory(cmd *cobra.Command, _ []string) error {
	opts, err := componentAPIOptionsFromCommand(cmd)
	if err != nil {
		return err
	}
	envelope, err := componentFetchInventory(apiClientFromCmd(cmd), opts.Limit)
	if err != nil {
		envelope = clicomponent.Envelope{Error: &clicomponent.EnvelopeError{
			Code:    traceErrorCodeFromTransport(err),
			Message: err.Error(),
		}}
		return finishComponentAPI(cmd, opts, envelope, componentAPIEnvelopeError(envelope.Error))
	}
	if envelope.Error != nil {
		return finishComponentAPI(cmd, opts, envelope, componentAPIEnvelopeError(envelope.Error))
	}
	return finishComponentAPI(cmd, opts, envelope, nil)
}

func runComponentDiagnostics(cmd *cobra.Command, args []string) error {
	opts, err := componentAPIOptionsFromCommand(cmd)
	if err != nil {
		return err
	}
	componentID := strings.TrimSpace(args[0])
	if componentID == "" {
		return commandExitError{message: "component id is required", code: 2}
	}
	envelope, err := componentFetchDiagnostics(apiClientFromCmd(cmd), componentID)
	if err != nil {
		envelope = clicomponent.Envelope{Error: &clicomponent.EnvelopeError{
			Code:    traceErrorCodeFromTransport(err),
			Message: err.Error(),
		}}
		return finishComponentAPI(cmd, opts, envelope, componentAPIEnvelopeError(envelope.Error))
	}
	if envelope.Error != nil {
		return finishComponentAPI(cmd, opts, envelope, componentAPIEnvelopeError(envelope.Error))
	}
	return finishComponentAPI(cmd, opts, envelope, nil)
}

func componentAPIOptionsFromCommand(cmd *cobra.Command) (componentAPIOptions, error) {
	jsonOutput, err := cmd.Flags().GetBool("json")
	if err != nil {
		return componentAPIOptions{}, err
	}
	opts := componentAPIOptions{JSON: jsonOutput}
	if cmd.Flags().Lookup("limit") != nil {
		limit, err := cmd.Flags().GetInt("limit")
		if err != nil {
			return componentAPIOptions{}, err
		}
		if limit < 1 || limit > clicomponent.InventoryMaxLimit {
			return componentAPIOptions{}, commandExitError{
				message: fmt.Sprintf("limit must be between 1 and %d", clicomponent.InventoryMaxLimit),
				code:    2,
			}
		}
		opts.Limit = limit
	}
	return opts, nil
}

// finishComponentAPI resolves the command's stream and hands terminal output
// to the extracted package.
func finishComponentAPI(cmd *cobra.Command, opts componentAPIOptions, envelope clicomponent.Envelope, err error) error {
	return clicomponent.FinishAPI(cmd.OutOrStdout(), opts.JSON, envelope, err)
}

// componentAPIEnvelopeError maps an envelope error onto the CLI's exit-code
// contract. The mapping table is traceExitCode, shared with `eshu trace` and
// `eshu map`; commandExitError is defined in this package, so the conversion
// stays here rather than moving out with the component logic.
func componentAPIEnvelopeError(e *clicomponent.EnvelopeError) error {
	if e == nil {
		return nil
	}
	message := strings.TrimSpace(e.Message)
	if message == "" {
		message = strings.TrimSpace(e.Code)
	}
	if message == "" {
		message = "component extension request failed"
	}
	return commandExitError{message: message, code: traceExitCode(e.Code)}
}
