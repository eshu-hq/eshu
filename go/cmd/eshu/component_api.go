// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

type componentAPIOptions struct {
	JSON  bool
	Limit int
}

type componentAPIEnvelope struct {
	Data  map[string]any     `json:"data"`
	Truth map[string]any     `json:"truth"`
	Error *componentAPIError `json:"error"`
}

type componentAPIError struct {
	Code       string         `json:"code,omitempty"`
	Message    string         `json:"message,omitempty"`
	Capability string         `json:"capability,omitempty"`
	Details    map[string]any `json:"details,omitempty"`
}

var (
	componentFetchInventory   = fetchComponentInventory
	componentFetchDiagnostics = fetchComponentDiagnostics
)

const (
	componentInventoryDefaultLimit = 100
	componentInventoryMaxLimit     = 500
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
	inventoryCmd.Flags().Int("limit", componentInventoryDefaultLimit, "Maximum number of component rows to return")
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
		envelope = componentAPIEnvelope{Error: &componentAPIError{
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
		envelope = componentAPIEnvelope{Error: &componentAPIError{
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
		if limit < 1 || limit > componentInventoryMaxLimit {
			return componentAPIOptions{}, commandExitError{
				message: fmt.Sprintf("limit must be between 1 and %d", componentInventoryMaxLimit),
				code:    2,
			}
		}
		opts.Limit = limit
	}
	return opts, nil
}

func fetchComponentInventory(client *APIClient, limit int) (componentAPIEnvelope, error) {
	if limit == 0 {
		limit = componentInventoryDefaultLimit
	}
	params := url.Values{}
	params.Set("limit", strconv.Itoa(limit))
	var envelope componentAPIEnvelope
	if err := client.GetEnvelope("/api/v0/component-extensions?"+params.Encode(), &envelope); err != nil {
		return componentAPIEnvelope{}, err
	}
	return envelope, nil
}

func fetchComponentDiagnostics(client *APIClient, componentID string) (componentAPIEnvelope, error) {
	path := "/api/v0/component-extensions/" + url.PathEscape(componentID) + "/diagnostics"
	var envelope componentAPIEnvelope
	if err := client.GetEnvelope(path, &envelope); err != nil {
		return componentAPIEnvelope{}, err
	}
	return envelope, nil
}

func finishComponentAPI(
	cmd *cobra.Command,
	opts componentAPIOptions,
	envelope componentAPIEnvelope,
	err error,
) error {
	if opts.JSON {
		if writeErr := writeTraceJSON(cmd.OutOrStdout(), envelope); writeErr != nil {
			return writeErr
		}
		return err
	}
	if err != nil {
		if renderErr := renderComponentAPIError(cmd.OutOrStdout(), envelope); renderErr != nil {
			return renderErr
		}
		return err
	}
	return renderComponentAPISummary(cmd.OutOrStdout(), envelope)
}

func renderComponentAPIError(w io.Writer, envelope componentAPIEnvelope) error {
	if envelope.Error == nil {
		return nil
	}
	_, err := fmt.Fprintf(w, "Component extension error (%s): %s\n", envelope.Error.Code, envelope.Error.Message)
	return err
}

func renderComponentAPISummary(w io.Writer, envelope componentAPIEnvelope) error {
	if component := traceMap(envelope.Data, "component"); component != nil {
		return renderComponentAPIRow(w, component)
	}
	if freshness := traceString(traceMap(envelope.Truth, "freshness"), "state"); freshness != "" {
		if _, err := fmt.Fprintf(w, "Truth freshness: %s\n", freshness); err != nil {
			return err
		}
	}
	count := traceInt(envelope.Data, "count")
	totalCount := traceInt(envelope.Data, "total_count")
	limit := traceInt(envelope.Data, "limit")
	truncated := traceBool(envelope.Data, "truncated")
	if totalCount > 0 || limit > 0 || truncated {
		if _, err := fmt.Fprintf(
			w,
			"Component extensions: %d of %d (limit=%d, truncated=%t)\n",
			count,
			totalCount,
			limit,
			truncated,
		); err != nil {
			return err
		}
	} else if _, err := fmt.Fprintf(w, "Component extensions: %d\n", count); err != nil {
		return err
	}
	for _, raw := range traceSlice(envelope.Data, "components") {
		row, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if err := renderComponentAPIRow(w, row); err != nil {
			return err
		}
	}
	return nil
}

func renderComponentAPIRow(w io.Writer, row map[string]any) error {
	states := traceStrings(row["states"])
	if _, err := fmt.Fprintf(
		w,
		"%s@%s states=%s\n",
		traceString(row, "id"),
		traceString(row, "version"),
		strings.Join(states, ","),
	); err != nil {
		return err
	}
	if diagnostics := traceMap(row, "diagnostics"); diagnostics != nil {
		if reason := traceString(diagnostics, "policy_reason"); reason != "" {
			_, err := fmt.Fprintf(w, "  policy=%s reason=%s\n", traceString(diagnostics, "policy_code"), reason)
			return err
		}
	}
	return nil
}

func componentAPIEnvelopeError(e *componentAPIError) error {
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
