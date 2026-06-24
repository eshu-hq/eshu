// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/spf13/cobra"
)

type freshnessGenerationsOptions struct {
	JSON          bool
	ScopeID       string
	Repository    string
	CollectorKind string
	SourceSystem  string
	GenerationID  string
	Status        string
	Limit         int
}

type freshnessGenerationsEnvelope struct {
	Data  map[string]any            `json:"data"`
	Truth map[string]any            `json:"truth"`
	Error *freshnessGenerationError `json:"error"`
}

type freshnessGenerationError struct {
	Code       string         `json:"code,omitempty"`
	Message    string         `json:"message,omitempty"`
	Capability string         `json:"capability,omitempty"`
	Details    map[string]any `json:"details,omitempty"`
}

var freshnessFetchGenerations = fetchFreshnessGenerations

func init() {
	freshnessCmd := &cobra.Command{
		Use:   "freshness",
		Short: "Inspect ingestion freshness and generation lifecycle",
	}
	generationsCmd := &cobra.Command{
		Use:   "generations",
		Short: "Drill into scope generation lifecycle history",
		Args:  cobra.NoArgs,
		RunE:  runFreshnessGenerations,
	}
	addFreshnessGenerationsFlags(generationsCmd)
	addRemoteFlags(generationsCmd)
	freshnessCmd.AddCommand(generationsCmd)
	freshnessCmd.AddCommand(newFreshnessChangedSinceCommand())
	freshnessCmd.AddCommand(newFreshnessServiceChangedSinceCommand())
	rootCmd.AddCommand(freshnessCmd)
}

func addFreshnessGenerationsFlags(cmd *cobra.Command) {
	cmd.Flags().Bool("json", false, "Write the canonical generation lifecycle envelope as JSON")
	cmd.Flags().String("scope-id", "", "Exact ingestion scope id to drill into")
	cmd.Flags().String("repository", "", "Canonical repository id (repository-kind scopes)")
	cmd.Flags().String("collector-kind", "", "Collector kind filter, for example git or terraform_state")
	cmd.Flags().String("source-system", "", "Source system filter, for example github")
	cmd.Flags().String("generation-id", "", "Exact generation id to drill into a single row")
	cmd.Flags().String("status", "", "Generation status filter (pending|active|superseded|completed|failed)")
	cmd.Flags().Int("limit", 50, "Maximum generation lifecycle rows to return (max 500)")
}

func runFreshnessGenerations(cmd *cobra.Command, _ []string) error {
	opts, err := freshnessGenerationsOptionsFromCommand(cmd)
	if err != nil {
		return err
	}

	envelope, err := freshnessFetchGenerations(apiClientFromCmd(cmd), opts)
	if err != nil {
		envelope = freshnessGenerationsEnvelope{
			Error: &freshnessGenerationError{
				Code:    traceErrorCodeFromTransport(err),
				Message: err.Error(),
			},
		}
		return finishFreshnessGenerations(cmd, opts, envelope, freshnessGenerationsEnvelopeError(envelope.Error))
	}
	if envelope.Error != nil {
		return finishFreshnessGenerations(cmd, opts, envelope, freshnessGenerationsEnvelopeError(envelope.Error))
	}
	return finishFreshnessGenerations(cmd, opts, envelope, nil)
}

func freshnessGenerationsOptionsFromCommand(cmd *cobra.Command) (freshnessGenerationsOptions, error) {
	jsonOutput, err := cmd.Flags().GetBool("json")
	if err != nil {
		return freshnessGenerationsOptions{}, err
	}
	scopeID, err := cmd.Flags().GetString("scope-id")
	if err != nil {
		return freshnessGenerationsOptions{}, err
	}
	repository, err := cmd.Flags().GetString("repository")
	if err != nil {
		return freshnessGenerationsOptions{}, err
	}
	collectorKind, err := cmd.Flags().GetString("collector-kind")
	if err != nil {
		return freshnessGenerationsOptions{}, err
	}
	sourceSystem, err := cmd.Flags().GetString("source-system")
	if err != nil {
		return freshnessGenerationsOptions{}, err
	}
	generationID, err := cmd.Flags().GetString("generation-id")
	if err != nil {
		return freshnessGenerationsOptions{}, err
	}
	statusFilter, err := cmd.Flags().GetString("status")
	if err != nil {
		return freshnessGenerationsOptions{}, err
	}
	limit, err := cmd.Flags().GetInt("limit")
	if err != nil {
		return freshnessGenerationsOptions{}, err
	}
	return freshnessGenerationsOptions{
		JSON:          jsonOutput,
		ScopeID:       strings.TrimSpace(scopeID),
		Repository:    strings.TrimSpace(repository),
		CollectorKind: strings.TrimSpace(collectorKind),
		SourceSystem:  strings.TrimSpace(sourceSystem),
		GenerationID:  strings.TrimSpace(generationID),
		Status:        strings.TrimSpace(statusFilter),
		Limit:         limit,
	}, nil
}

func fetchFreshnessGenerations(client *APIClient, opts freshnessGenerationsOptions) (freshnessGenerationsEnvelope, error) {
	query := url.Values{}
	if opts.ScopeID != "" {
		query.Set("scope_id", opts.ScopeID)
	}
	if opts.Repository != "" {
		query.Set("repository", opts.Repository)
	}
	if opts.CollectorKind != "" {
		query.Set("collector_kind", opts.CollectorKind)
	}
	if opts.SourceSystem != "" {
		query.Set("source_system", opts.SourceSystem)
	}
	if opts.GenerationID != "" {
		query.Set("generation_id", opts.GenerationID)
	}
	if opts.Status != "" {
		query.Set("status", opts.Status)
	}
	if opts.Limit > 0 {
		query.Set("limit", fmt.Sprintf("%d", opts.Limit))
	}
	path := "/api/v0/freshness/generations"
	if encoded := query.Encode(); encoded != "" {
		path += "?" + encoded
	}
	var envelope freshnessGenerationsEnvelope
	if err := client.GetEnvelope(path, &envelope); err != nil {
		return freshnessGenerationsEnvelope{}, err
	}
	return envelope, nil
}

func finishFreshnessGenerations(cmd *cobra.Command, opts freshnessGenerationsOptions, envelope freshnessGenerationsEnvelope, err error) error {
	if opts.JSON {
		if writeErr := writeFreshnessGenerationsJSON(cmd.OutOrStdout(), envelope); writeErr != nil {
			return writeErr
		}
		return err
	}
	if err != nil {
		if renderErr := renderFreshnessGenerationsError(cmd.OutOrStdout(), envelope); renderErr != nil {
			return renderErr
		}
		return err
	}
	return renderFreshnessGenerationsSummary(cmd.OutOrStdout(), envelope)
}

func renderFreshnessGenerationsError(w io.Writer, envelope freshnessGenerationsEnvelope) error {
	if envelope.Error == nil {
		return nil
	}
	_, err := fmt.Fprintf(w, "Generation lifecycle error (%s): %s\n", envelope.Error.Code, envelope.Error.Message)
	return err
}

func renderFreshnessGenerationsSummary(w io.Writer, envelope freshnessGenerationsEnvelope) error {
	data := envelope.Data
	if freshness := traceString(traceMap(envelope.Truth, "freshness"), "state"); freshness != "" {
		if _, err := fmt.Fprintf(w, "Truth freshness: %s\n", freshness); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(w, "Generations: %d (truncated=%t)\n", traceInt(data, "count"), traceBool(data, "truncated")); err != nil {
		return err
	}
	for _, raw := range traceSlice(data, "generations") {
		row, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		marker := " "
		if traceBool(row, "is_active") {
			marker = "*"
		}
		if _, err := fmt.Fprintf(
			w,
			"%s %s status=%s scope=%s trigger=%s",
			marker,
			traceString(row, "generation_id"),
			traceString(row, "status"),
			traceString(row, "scope_id"),
			traceString(row, "trigger_kind"),
		); err != nil {
			return err
		}
		if queue := traceMap(row, "queue_status"); queue != nil {
			if _, err := fmt.Fprintf(
				w,
				" queue[outstanding=%d failed=%d dead_letter=%d]",
				traceInt(queue, "outstanding"),
				traceInt(queue, "failed"),
				traceInt(queue, "dead_letter"),
			); err != nil {
				return err
			}
		}
		if failure := traceMap(row, "latest_failure"); failure != nil {
			if _, err := fmt.Fprintf(w, " failure=%s", traceString(failure, "failure_class")); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(w); err != nil {
			return err
		}
	}
	return nil
}

func freshnessGenerationsEnvelopeError(e *freshnessGenerationError) error {
	if e == nil {
		return nil
	}
	message := strings.TrimSpace(e.Message)
	if message == "" {
		message = strings.TrimSpace(e.Code)
	}
	if message == "" {
		message = "generation lifecycle request failed"
	}
	return commandExitError{message: message, code: traceExitCode(e.Code)}
}

func writeFreshnessGenerationsJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

func traceBool(parent map[string]any, key string) bool {
	if parent == nil {
		return false
	}
	if value, ok := parent[key].(bool); ok {
		return value
	}
	return false
}
