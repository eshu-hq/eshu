// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/spf13/cobra"
)

type entityMapOptions struct {
	From         string
	FromType     string
	Repo         string
	Environment  string
	Relationship string
	Depth        int
	Limit        int
	JSON         bool
}

type entityMapEnvelope struct {
	Data  map[string]any  `json:"data"`
	Truth map[string]any  `json:"truth"`
	Error *entityMapError `json:"error"`
}

type entityMapError struct {
	Code       string         `json:"code,omitempty"`
	Message    string         `json:"message,omitempty"`
	Capability string         `json:"capability,omitempty"`
	Details    map[string]any `json:"details,omitempty"`
}

var entityMapFetch = fetchEntityMap

func init() {
	cmd := &cobra.Command{
		Use:   "map --from <thing>",
		Short: "Map a bounded code-to-cloud entity neighborhood",
		Args:  cobra.NoArgs,
		RunE:  runMapFrom,
	}
	addEntityMapFlags(cmd)
	addRemoteFlags(cmd)
	rootCmd.AddCommand(cmd)
}

func addEntityMapFlags(cmd *cobra.Command) {
	cmd.Flags().String("from", "", "Entity handle to map, such as terraform/aws_lb.main or workload:checkout")
	cmd.Flags().String("type", "", "Entity type hint such as service, repository, terraform_resource, k8s_resource, or file")
	cmd.Flags().String("repo", "", "Repository selector used to narrow resolution")
	cmd.Flags().String("env", "", "Environment selector used to narrow runtime/resource relationships")
	cmd.Flags().String("relationship", "", "Relationship type filter, such as DEPENDS_ON or PROVISIONS_DEPENDENCY_FOR")
	cmd.Flags().Int("depth", 1, "Maximum relationship depth to traverse")
	cmd.Flags().Int("limit", 25, "Maximum mapped relationships to return")
	cmd.Flags().Bool("json", false, "Write the canonical entity map envelope as JSON")
}

func runMapFrom(cmd *cobra.Command, _ []string) error {
	opts, err := entityMapOptionsFromCommand(cmd)
	if err != nil {
		return err
	}
	if opts.From == "" {
		return commandExitError{message: "--from is required", code: 2}
	}

	envelope, err := entityMapFetch(apiClientFromCmd(cmd), opts)
	if err != nil {
		envelope = entityMapEnvelope{
			Error: &entityMapError{
				Code:    entityMapErrorCodeFromTransport(err),
				Message: err.Error(),
			},
		}
		return finishEntityMap(cmd, opts, envelope, entityMapEnvelopeError(envelope.Error))
	}
	if envelope.Error != nil {
		return finishEntityMap(cmd, opts, envelope, entityMapEnvelopeError(envelope.Error))
	}
	if freshness := entityMapFreshnessState(envelope); freshness == "stale" || freshness == "building" {
		return finishEntityMap(cmd, opts, envelope, commandExitError{
			message: fmt.Sprintf("entity map freshness is %s", freshness),
			code:    4,
		})
	}
	status := traceString(envelope.Data, "status")
	switch status {
	case "ambiguous":
		return finishEntityMap(cmd, opts, envelope, commandExitError{
			message: "entity map selector is ambiguous",
			code:    3,
		})
	case "no_match":
		return finishEntityMap(cmd, opts, envelope, commandExitError{
			message: "entity map selector did not match a supported entity",
			code:    2,
		})
	}
	return finishEntityMap(cmd, opts, envelope, nil)
}

func entityMapOptionsFromCommand(cmd *cobra.Command) (entityMapOptions, error) {
	from, err := cmd.Flags().GetString("from")
	if err != nil {
		return entityMapOptions{}, err
	}
	fromType, err := cmd.Flags().GetString("type")
	if err != nil {
		return entityMapOptions{}, err
	}
	repo, err := cmd.Flags().GetString("repo")
	if err != nil {
		return entityMapOptions{}, err
	}
	environment, err := cmd.Flags().GetString("env")
	if err != nil {
		return entityMapOptions{}, err
	}
	relationship, err := cmd.Flags().GetString("relationship")
	if err != nil {
		return entityMapOptions{}, err
	}
	depth, err := cmd.Flags().GetInt("depth")
	if err != nil {
		return entityMapOptions{}, err
	}
	limit, err := cmd.Flags().GetInt("limit")
	if err != nil {
		return entityMapOptions{}, err
	}
	jsonOutput, err := cmd.Flags().GetBool("json")
	if err != nil {
		return entityMapOptions{}, err
	}
	return entityMapOptions{
		From:         strings.TrimSpace(from),
		FromType:     strings.TrimSpace(fromType),
		Repo:         strings.TrimSpace(repo),
		Environment:  strings.TrimSpace(environment),
		Relationship: strings.ToUpper(strings.TrimSpace(relationship)),
		Depth:        depth,
		Limit:        limit,
		JSON:         jsonOutput,
	}, nil
}

func fetchEntityMap(client *APIClient, opts entityMapOptions) (entityMapEnvelope, error) {
	body := map[string]any{
		"from":         opts.From,
		"from_type":    opts.FromType,
		"repo_id":      opts.Repo,
		"environment":  opts.Environment,
		"relationship": opts.Relationship,
		"depth":        opts.Depth,
		"limit":        opts.Limit,
	}
	var envelope entityMapEnvelope
	if err := client.PostEnvelope("/api/v0/impact/entity-map", body, &envelope); err != nil {
		return entityMapEnvelope{}, err
	}
	return envelope, nil
}

func finishEntityMap(cmd *cobra.Command, opts entityMapOptions, envelope entityMapEnvelope, err error) error {
	if opts.JSON {
		if writeErr := writeTraceJSON(cmd.OutOrStdout(), envelope); writeErr != nil {
			return writeErr
		}
		return err
	}
	if err != nil {
		_ = renderEntityMapError(cmd.OutOrStdout(), envelope)
		return err
	}
	return renderEntityMapSummary(cmd.OutOrStdout(), envelope)
}

func renderEntityMapError(w io.Writer, envelope entityMapEnvelope) error {
	status := traceString(envelope.Data, "status")
	if status != "ambiguous" {
		return nil
	}
	if _, err := fmt.Fprintln(w, "Map selector is ambiguous. Add --type, --repo, or --env."); err != nil {
		return err
	}
	resolution := traceMap(envelope.Data, "resolution")
	for _, candidate := range traceSlice(resolution, "candidates") {
		row, ok := candidate.(map[string]any)
		if !ok {
			continue
		}
		id := traceString(row, "id")
		name := traceString(row, "name")
		repoID := traceString(row, "repo_id")
		if _, err := fmt.Fprintf(w, "- %s", traceFirstString(id, name, "<unknown>")); err != nil {
			return err
		}
		if name != "" && name != id {
			if _, err := fmt.Fprintf(w, " name=%s", name); err != nil {
				return err
			}
		}
		if repoID != "" {
			if _, err := fmt.Fprintf(w, " repo=%s", repoID); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(w); err != nil {
			return err
		}
	}
	return nil
}

func renderEntityMapSummary(w io.Writer, envelope entityMapEnvelope) error {
	data := envelope.Data
	resolution := traceMap(data, "resolution")
	selected := traceMap(resolution, "selected")
	if _, err := fmt.Fprintf(w, "Map: %s\n", traceString(data, "from")); err != nil {
		return err
	}
	if len(selected) > 0 {
		if _, err := fmt.Fprintf(
			w,
			"Resolved: %s %s",
			entityMapDisplayLabel(selected),
			traceFirstString(traceString(selected, "id"), traceString(selected, "name")),
		); err != nil {
			return err
		}
		if name := traceString(selected, "name"); name != "" {
			if _, err := fmt.Fprintf(w, " (%s)", name); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(w); err != nil {
			return err
		}
	}
	sections := traceMap(data, "sections")
	for _, section := range []struct {
		key   string
		title string
	}{
		{"defined_by", "Defined by"},
		{"deployed_by", "Deployed by"},
		{"runs_as", "Runs as"},
		{"depends_on", "Depends on"},
		{"consumed_by", "Consumed by"},
	} {
		if err := renderEntityMapSection(w, section.title, traceSlice(sections, section.key)); err != nil {
			return err
		}
	}
	evidence := traceMap(data, "evidence")
	if _, err := fmt.Fprintf(w, "Evidence: %d relationships\n", traceInt(evidence, "relationship_count")); err != nil {
		return err
	}
	if truncated, ok := evidence["truncated"].(bool); ok && truncated {
		if _, err := fmt.Fprintln(w, "Truncated: true"); err != nil {
			return err
		}
	}
	return nil
}

func renderEntityMapSection(w io.Writer, title string, rows []any) error {
	if len(rows) == 0 {
		return nil
	}
	if _, err := fmt.Fprintf(w, "%s:\n", title); err != nil {
		return err
	}
	for _, item := range rows {
		row, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if _, err := fmt.Fprintf(
			w,
			"- %s %s",
			traceString(row, "relationship_type"),
			traceFirstString(traceString(row, "entity_name"), traceString(row, "entity_id")),
		); err != nil {
			return err
		}
		if repoID := traceString(row, "repo_id"); repoID != "" {
			if _, err := fmt.Fprintf(w, " repo=%s", repoID); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(w); err != nil {
			return err
		}
	}
	return nil
}

func entityMapDisplayLabel(selected map[string]any) string {
	labels := traceStrings(selected["labels"])
	if len(labels) == 0 {
		return "Entity"
	}
	return labels[0]
}

func entityMapEnvelopeError(e *entityMapError) error {
	if e == nil {
		return nil
	}
	message := strings.TrimSpace(e.Message)
	if message == "" {
		message = strings.TrimSpace(e.Code)
	}
	if message == "" {
		message = "entity map failed"
	}
	return commandExitError{message: message, code: traceExitCode(e.Code)}
}

func entityMapFreshnessState(envelope entityMapEnvelope) string {
	freshness := traceMap(envelope.Truth, "freshness")
	return traceString(freshness, "state")
}

func entityMapErrorCodeFromTransport(err error) string {
	if err == nil {
		return ""
	}
	var httpErr *apiHTTPError
	if errors.As(err, &httpErr) && httpErr.StatusCode == http.StatusConflict {
		return "ambiguous"
	}
	return traceErrorCodeFromTransport(err)
}
