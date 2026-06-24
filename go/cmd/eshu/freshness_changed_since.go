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

type freshnessChangedSinceOptions struct {
	JSON              bool
	ScopeID           string
	Repository        string
	SinceGenerationID string
	SinceObservedAt   string
	SampleLimit       int
}

var freshnessFetchChangedSince = fetchFreshnessChangedSince

// newFreshnessChangedSinceCommand builds the `eshu freshness changed-since`
// subcommand. It diffs a prior generation against the current active generation
// of a repository scope and renders the bounded per-category delta summary.
func newFreshnessChangedSinceCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "changed-since",
		Short: "Summarize what changed since a prior generation or instant",
		Args:  cobra.NoArgs,
		RunE:  runFreshnessChangedSince,
	}
	cmd.Flags().Bool("json", false, "Write the canonical changed-since envelope as JSON")
	cmd.Flags().String("scope-id", "", "Exact ingestion scope id (required unless --repository is set)")
	cmd.Flags().String("repository", "", "Canonical repository id (required unless --scope-id is set)")
	cmd.Flags().String("since-generation-id", "", "Prior generation id to diff from")
	cmd.Flags().String("since-observed-at", "", "RFC3339 instant; diff from the generation observed at or before this time")
	cmd.Flags().Int("sample-limit", 25, "Maximum sample handles per classification per category (max 200)")
	addRemoteFlags(cmd)
	return cmd
}

func runFreshnessChangedSince(cmd *cobra.Command, _ []string) error {
	opts, err := freshnessChangedSinceOptionsFromCommand(cmd)
	if err != nil {
		return err
	}

	envelope, err := freshnessFetchChangedSince(apiClientFromCmd(cmd), opts)
	if err != nil {
		envelope = freshnessGenerationsEnvelope{
			Error: &freshnessGenerationError{
				Code:    traceErrorCodeFromTransport(err),
				Message: err.Error(),
			},
		}
		return finishFreshnessChangedSince(cmd, opts, envelope, freshnessGenerationsEnvelopeError(envelope.Error))
	}
	if envelope.Error != nil {
		return finishFreshnessChangedSince(cmd, opts, envelope, freshnessGenerationsEnvelopeError(envelope.Error))
	}
	return finishFreshnessChangedSince(cmd, opts, envelope, nil)
}

func freshnessChangedSinceOptionsFromCommand(cmd *cobra.Command) (freshnessChangedSinceOptions, error) {
	jsonOutput, err := cmd.Flags().GetBool("json")
	if err != nil {
		return freshnessChangedSinceOptions{}, err
	}
	scopeID, err := cmd.Flags().GetString("scope-id")
	if err != nil {
		return freshnessChangedSinceOptions{}, err
	}
	repository, err := cmd.Flags().GetString("repository")
	if err != nil {
		return freshnessChangedSinceOptions{}, err
	}
	sinceGenerationID, err := cmd.Flags().GetString("since-generation-id")
	if err != nil {
		return freshnessChangedSinceOptions{}, err
	}
	sinceObservedAt, err := cmd.Flags().GetString("since-observed-at")
	if err != nil {
		return freshnessChangedSinceOptions{}, err
	}
	sampleLimit, err := cmd.Flags().GetInt("sample-limit")
	if err != nil {
		return freshnessChangedSinceOptions{}, err
	}
	return freshnessChangedSinceOptions{
		JSON:              jsonOutput,
		ScopeID:           strings.TrimSpace(scopeID),
		Repository:        strings.TrimSpace(repository),
		SinceGenerationID: strings.TrimSpace(sinceGenerationID),
		SinceObservedAt:   strings.TrimSpace(sinceObservedAt),
		SampleLimit:       sampleLimit,
	}, nil
}

func fetchFreshnessChangedSince(client *APIClient, opts freshnessChangedSinceOptions) (freshnessGenerationsEnvelope, error) {
	query := url.Values{}
	if opts.ScopeID != "" {
		query.Set("scope_id", opts.ScopeID)
	}
	if opts.Repository != "" {
		query.Set("repository", opts.Repository)
	}
	if opts.SinceGenerationID != "" {
		query.Set("since_generation_id", opts.SinceGenerationID)
	}
	if opts.SinceObservedAt != "" {
		query.Set("since_observed_at", opts.SinceObservedAt)
	}
	if opts.SampleLimit > 0 {
		query.Set("sample_limit", fmt.Sprintf("%d", opts.SampleLimit))
	}
	path := "/api/v0/freshness/changed-since"
	if encoded := query.Encode(); encoded != "" {
		path += "?" + encoded
	}
	var envelope freshnessGenerationsEnvelope
	if err := client.GetEnvelope(path, &envelope); err != nil {
		return freshnessGenerationsEnvelope{}, err
	}
	return envelope, nil
}

func finishFreshnessChangedSince(cmd *cobra.Command, opts freshnessChangedSinceOptions, envelope freshnessGenerationsEnvelope, err error) error {
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
	return renderFreshnessChangedSinceSummary(cmd.OutOrStdout(), envelope)
}

func renderFreshnessChangedSinceSummary(w io.Writer, envelope freshnessGenerationsEnvelope) error {
	data := envelope.Data
	if freshness := traceString(traceMap(envelope.Truth, "freshness"), "state"); freshness != "" {
		if _, err := fmt.Fprintf(w, "Truth freshness: %s\n", freshness); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(
		w,
		"Changed since %s -> %s (scope=%s)\n",
		changedSinceBaselineLabel(data),
		traceString(data, "current_active_generation_id"),
		traceString(data, "scope_id"),
	); err != nil {
		return err
	}
	if traceBool(data, "unavailable") {
		if _, err := fmt.Fprintln(w, "  diff unavailable: scope has no current active generation"); err != nil {
			return err
		}
		return nil
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

func renderChangedSinceCategory(w io.Writer, category map[string]any) error {
	name := traceString(category, "category")
	if traceBool(category, "unavailable") {
		_, err := fmt.Fprintf(w, "  %-16s unavailable\n", name)
		return err
	}
	counts := traceMap(category, "counts")
	_, err := fmt.Fprintf(
		w,
		"  %-16s added=%d updated=%d unchanged=%d retired=%d superseded=%d\n",
		name,
		traceInt(counts, "added"),
		traceInt(counts, "updated"),
		traceInt(counts, "unchanged"),
		traceInt(counts, "retired"),
		traceInt(counts, "superseded"),
	)
	return err
}

func changedSinceBaselineLabel(data map[string]any) string {
	if gen := traceString(data, "since_generation_id"); gen != "" {
		return gen
	}
	if at := traceString(data, "since_observed_at"); at != "" {
		return at
	}
	return "(unknown)"
}
