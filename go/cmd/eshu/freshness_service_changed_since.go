// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/spf13/cobra"
)

type freshnessServiceChangedSinceOptions struct {
	JSON              bool
	ServiceID         string
	SinceGenerationID string
	SampleLimit       int
}

var freshnessFetchServiceChangedSince = fetchFreshnessServiceChangedSince

// newFreshnessServiceChangedSinceCommand builds the
// `eshu freshness service-changed-since` subcommand. It diffs a prior service
// materialization generation against the current active generation of a service
// and renders the bounded per-family delta summary (#1943).
func newFreshnessServiceChangedSinceCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "service-changed-since",
		Short: "Summarize what changed for a service since a prior service generation",
		Args:  cobra.NoArgs,
		RunE:  runFreshnessServiceChangedSince,
	}
	cmd.Flags().Bool("json", false, "Write the canonical service changed-since envelope as JSON")
	cmd.Flags().String("service-id", "", "Exact service id whose evidence lineage to diff (required)")
	cmd.Flags().String("since-generation-id", "", "Prior service materialization generation id to diff from (required)")
	cmd.Flags().Int("sample-limit", 25, "Maximum sample handles per classification per family (max 200)")
	addRemoteFlags(cmd)
	return cmd
}

func runFreshnessServiceChangedSince(cmd *cobra.Command, _ []string) error {
	opts, err := freshnessServiceChangedSinceOptionsFromCommand(cmd)
	if err != nil {
		return err
	}

	envelope, err := freshnessFetchServiceChangedSince(apiClientFromCmd(cmd), opts)
	if err != nil {
		envelope = freshnessGenerationsEnvelope{
			Error: &freshnessGenerationError{
				Code:    traceErrorCodeFromTransport(err),
				Message: err.Error(),
			},
		}
		return finishFreshnessServiceChangedSince(cmd, opts, envelope, freshnessGenerationsEnvelopeError(envelope.Error))
	}
	if envelope.Error != nil {
		return finishFreshnessServiceChangedSince(cmd, opts, envelope, freshnessGenerationsEnvelopeError(envelope.Error))
	}
	return finishFreshnessServiceChangedSince(cmd, opts, envelope, nil)
}

func freshnessServiceChangedSinceOptionsFromCommand(cmd *cobra.Command) (freshnessServiceChangedSinceOptions, error) {
	jsonOutput, err := cmd.Flags().GetBool("json")
	if err != nil {
		return freshnessServiceChangedSinceOptions{}, err
	}
	serviceID, err := cmd.Flags().GetString("service-id")
	if err != nil {
		return freshnessServiceChangedSinceOptions{}, err
	}
	sinceGenerationID, err := cmd.Flags().GetString("since-generation-id")
	if err != nil {
		return freshnessServiceChangedSinceOptions{}, err
	}
	sampleLimit, err := cmd.Flags().GetInt("sample-limit")
	if err != nil {
		return freshnessServiceChangedSinceOptions{}, err
	}
	return freshnessServiceChangedSinceOptions{
		JSON:              jsonOutput,
		ServiceID:         strings.TrimSpace(serviceID),
		SinceGenerationID: strings.TrimSpace(sinceGenerationID),
		SampleLimit:       sampleLimit,
	}, nil
}

func fetchFreshnessServiceChangedSince(client *APIClient, opts freshnessServiceChangedSinceOptions) (freshnessGenerationsEnvelope, error) {
	query := url.Values{}
	if opts.ServiceID != "" {
		query.Set("service_id", opts.ServiceID)
	}
	if opts.SinceGenerationID != "" {
		query.Set("since_generation_id", opts.SinceGenerationID)
	}
	if opts.SampleLimit > 0 {
		query.Set("sample_limit", fmt.Sprintf("%d", opts.SampleLimit))
	}
	path := "/api/v0/freshness/services/changed-since"
	if encoded := query.Encode(); encoded != "" {
		path += "?" + encoded
	}
	var envelope freshnessGenerationsEnvelope
	if err := client.GetEnvelope(path, &envelope); err != nil {
		return freshnessGenerationsEnvelope{}, err
	}
	return envelope, nil
}

func finishFreshnessServiceChangedSince(cmd *cobra.Command, opts freshnessServiceChangedSinceOptions, envelope freshnessGenerationsEnvelope, err error) error {
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
	return renderFreshnessServiceChangedSinceSummary(cmd.OutOrStdout(), envelope)
}

func renderFreshnessServiceChangedSinceSummary(w io.Writer, envelope freshnessGenerationsEnvelope) error {
	data := envelope.Data
	if freshness := traceString(traceMap(envelope.Truth, "freshness"), "state"); freshness != "" {
		if _, err := fmt.Fprintf(w, "Truth freshness: %s\n", freshness); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(
		w,
		"Service changed since %s -> %s (service=%s)\n",
		traceString(data, "since_generation_id"),
		traceString(data, "current_active_generation_id"),
		traceString(data, "service_id"),
	); err != nil {
		return err
	}
	if traceBool(data, "unavailable") {
		_, err := fmt.Fprintln(w, "  diff unavailable: service has no current active generation")
		return err
	}
	for _, raw := range traceSlice(data, "categories") {
		category, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if err := renderChangedSinceCategory(w, category); err != nil {
			return err
		}
	}
	return nil
}
