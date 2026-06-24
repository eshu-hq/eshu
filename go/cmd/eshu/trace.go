// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/spf13/cobra"
)

type traceServiceOptions struct {
	JSON        bool
	Repo        string
	Environment string
	ServiceID   string
}

type traceServiceEnvelope struct {
	Data  map[string]any     `json:"data"`
	Truth map[string]any     `json:"truth"`
	Error *traceServiceError `json:"error"`
}

type traceServiceError struct {
	Code       string         `json:"code,omitempty"`
	Message    string         `json:"message,omitempty"`
	Capability string         `json:"capability,omitempty"`
	Details    map[string]any `json:"details,omitempty"`
}

var traceFetchServiceStory = fetchTraceServiceStory

func init() {
	traceCmd := &cobra.Command{
		Use:   "trace",
		Short: "Trace code-to-runtime evidence",
	}
	serviceCmd := &cobra.Command{
		Use:   "service <name>",
		Short: "Trace how a service gets from source code to runtime",
		Args:  cobra.ExactArgs(1),
		RunE:  runTraceService,
	}
	addTraceServiceFlags(serviceCmd)
	addRemoteFlags(serviceCmd)
	traceCmd.AddCommand(serviceCmd)
	rootCmd.AddCommand(traceCmd)
}

func addTraceServiceFlags(cmd *cobra.Command) {
	cmd.Flags().Bool("json", false, "Write the canonical service trace envelope as JSON")
	cmd.Flags().String("repo", "", "Repository selector to disambiguate a service name")
	cmd.Flags().String("env", "", "Runtime environment selector to disambiguate a service name")
	cmd.Flags().String("service-id", "", "Exact service or workload id to trace instead of the display name")
}

func runTraceService(cmd *cobra.Command, args []string) error {
	opts, err := traceServiceOptionsFromCommand(cmd)
	if err != nil {
		return err
	}
	selector := strings.TrimSpace(args[0])
	if selector == "" {
		return commandExitError{message: "service name is required", code: 2}
	}

	envelope, err := traceFetchServiceStory(apiClientFromCmd(cmd), selector, opts)
	if err != nil {
		envelope = traceServiceEnvelope{
			Data: nil,
			Error: &traceServiceError{
				Code:    traceErrorCodeFromTransport(err),
				Message: err.Error(),
			},
		}
		return finishTraceService(cmd, opts, envelope, traceEnvelopeError(envelope.Error))
	}
	if envelope.Error != nil {
		return finishTraceService(cmd, opts, envelope, traceEnvelopeError(envelope.Error))
	}
	if envelope.Data == nil {
		envelope.Error = &traceServiceError{Code: "not_found", Message: "service story response did not include data"}
		return finishTraceService(cmd, opts, envelope, traceEnvelopeError(envelope.Error))
	}
	if freshness := traceServiceFreshnessState(envelope); freshness == "stale" || freshness == "building" {
		return finishTraceService(cmd, opts, envelope, commandExitError{
			message: fmt.Sprintf("service trace freshness is %s", freshness),
			code:    4,
		})
	}
	if status := traceServiceStatus(envelope); status == "partial" {
		return finishTraceService(cmd, opts, envelope, commandExitError{
			message: "service trace is partial",
			code:    5,
		})
	}
	return finishTraceService(cmd, opts, envelope, nil)
}

func traceServiceOptionsFromCommand(cmd *cobra.Command) (traceServiceOptions, error) {
	jsonOutput, err := cmd.Flags().GetBool("json")
	if err != nil {
		return traceServiceOptions{}, err
	}
	serviceID, err := cmd.Flags().GetString("service-id")
	if err != nil {
		return traceServiceOptions{}, err
	}
	repo, err := cmd.Flags().GetString("repo")
	if err != nil {
		return traceServiceOptions{}, err
	}
	environment, err := cmd.Flags().GetString("env")
	if err != nil {
		return traceServiceOptions{}, err
	}
	return traceServiceOptions{
		JSON:        jsonOutput,
		Repo:        strings.TrimSpace(repo),
		Environment: strings.TrimSpace(environment),
		ServiceID:   strings.TrimSpace(serviceID),
	}, nil
}

func fetchTraceServiceStory(client *APIClient, selector string, opts traceServiceOptions) (traceServiceEnvelope, error) {
	path := "/api/v0/services/" + url.PathEscape(strings.TrimSpace(selector)) + "/story"
	query := url.Values{}
	if opts.Repo != "" {
		query.Set("repo", opts.Repo)
	}
	if opts.Environment != "" {
		query.Set("environment", opts.Environment)
	}
	if opts.ServiceID != "" {
		query.Set("service_id", opts.ServiceID)
	}
	if encoded := query.Encode(); encoded != "" {
		path += "?" + encoded
	}
	var envelope traceServiceEnvelope
	if err := client.GetEnvelope(path, &envelope); err != nil {
		return traceServiceEnvelope{}, err
	}
	return envelope, nil
}

func finishTraceService(cmd *cobra.Command, opts traceServiceOptions, envelope traceServiceEnvelope, err error) error {
	if opts.JSON {
		if writeErr := writeTraceJSON(cmd.OutOrStdout(), envelope); writeErr != nil {
			return writeErr
		}
		return err
	}
	if err != nil {
		var renderErr error
		if envelope.Error != nil && envelope.Error.Code == "ambiguous" {
			renderErr = renderTraceServiceError(cmd.OutOrStdout(), envelope)
		} else if envelope.Error == nil && envelope.Data != nil {
			renderErr = renderTraceServiceSummary(cmd.OutOrStdout(), envelope)
		}
		if renderErr != nil {
			return renderErr
		}
		return err
	}
	return renderTraceServiceSummary(cmd.OutOrStdout(), envelope)
}

func renderTraceServiceError(w io.Writer, envelope traceServiceEnvelope) error {
	if envelope.Error == nil || envelope.Error.Code != "ambiguous" {
		return nil
	}
	if _, err := fmt.Fprintln(w, "Service selector is ambiguous. Add --service-id, --repo, or --env."); err != nil {
		return err
	}
	candidates := traceSlice(envelope.Error.Details, "candidates")
	if len(candidates) == 0 {
		candidates = traceSlice(envelope.Data, "candidates")
	}
	for _, candidate := range candidates {
		row, ok := candidate.(map[string]any)
		if !ok {
			continue
		}
		serviceID := traceFirstString(traceString(row, "service_id"), traceString(row, "id"))
		serviceName := traceFirstString(traceString(row, "service_name"), traceString(row, "name"))
		repoID := traceString(row, "repo_id")
		environment := traceString(row, "environment")
		if _, err := fmt.Fprintf(
			w,
			"- %s",
			traceFirstString(serviceID, serviceName, "<unknown>"),
		); err != nil {
			return err
		}
		if serviceName != "" && serviceName != serviceID {
			if _, err := fmt.Fprintf(w, " name=%s", serviceName); err != nil {
				return err
			}
		}
		if repoID != "" {
			if _, err := fmt.Fprintf(w, " repo=%s", repoID); err != nil {
				return err
			}
		}
		if environment != "" {
			if _, err := fmt.Fprintf(w, " env=%s", environment); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(w); err != nil {
			return err
		}
	}
	return nil
}

func renderTraceServiceSummary(w io.Writer, envelope traceServiceEnvelope) error {
	data := envelope.Data
	identity := traceMap(data, "service_identity")
	serviceName := traceFirstString(
		traceString(identity, "service_name"),
		traceString(identity, "name"),
		traceString(data, "service_name"),
		traceString(identity, "service_id"),
	)
	repoID := traceString(identity, "repo_id")
	repoName := traceString(identity, "repo_name")
	if repoName == "" {
		repoName = "<unknown>"
	}
	coverage := traceMap(traceMap(data, "investigation"), "coverage_summary")
	coverageState := traceFirstString(traceString(coverage, "state"), "unknown")
	coverageReason := traceString(coverage, "reason")
	limitations := traceStrings(identity["limitations"])
	if len(limitations) == 0 {
		limitations = traceStrings(data["limitations"])
	}

	if _, err := fmt.Fprintf(w, "Service: %s\n", traceFirstString(serviceName, "<unknown>")); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Repository: %s (%s)\n", traceFirstString(repoID, "<unknown>"), repoName); err != nil {
		return err
	}
	if status := traceString(identity, "materialization_status"); status != "" {
		if _, err := fmt.Fprintf(w, "Materialization: %s\n", status); err != nil {
			return err
		}
	}
	if basis := traceString(identity, "query_basis"); basis != "" {
		if _, err := fmt.Fprintf(w, "Basis: %s\n", basis); err != nil {
			return err
		}
	}
	if err := renderTraceTruthFreshness(w, envelope); err != nil {
		return err
	}
	if err := renderTraceCodeToRuntime(w, data); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Deployment lanes: %d\n", len(traceSlice(data, "deployment_lanes"))); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Runtime instances: %d\n", traceRuntimeInstanceCount(data)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Upstream dependencies: %d\n", len(traceSlice(data, "upstream_dependencies"))); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Downstream consumers: %d\n", traceDownstreamConsumerCount(data)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Coverage: %s\n", coverageState); err != nil {
		return err
	}
	if coverageReason != "" {
		if _, err := fmt.Fprintf(w, "Coverage reason: %s\n", coverageReason); err != nil {
			return err
		}
	}
	if len(limitations) > 0 {
		if _, err := fmt.Fprintln(w, "What to worry about:"); err != nil {
			return err
		}
		for _, limitation := range limitations {
			if _, err := fmt.Fprintf(w, "- %s\n", limitation); err != nil {
				return err
			}
		}
	}
	return nil
}

func renderTraceTruthFreshness(w io.Writer, envelope traceServiceEnvelope) error {
	freshness := traceMap(envelope.Truth, "freshness")
	state := traceString(freshness, "state")
	if state == "" {
		return nil
	}
	if _, err := fmt.Fprintf(w, "Truth freshness: %s\n", state); err != nil {
		return err
	}
	if detail := traceFirstString(traceString(freshness, "detail"), traceString(envelope.Truth, "reason")); detail != "" {
		if _, err := fmt.Fprintf(w, "Freshness detail: %s\n", detail); err != nil {
			return err
		}
	}
	return nil
}

func traceServiceFreshnessState(envelope traceServiceEnvelope) string {
	return traceString(traceMap(envelope.Truth, "freshness"), "state")
}

func traceServiceStatus(envelope traceServiceEnvelope) string {
	return traceString(traceMap(envelope.Data, "code_to_runtime_trace"), "status")
}

func traceRuntimeInstanceCount(data map[string]any) int {
	if instances := traceSlice(data, "runtime_instances"); len(instances) > 0 {
		return len(instances)
	}
	return len(traceSlice(traceMap(data, "service_identity"), "instances"))
}

func traceDownstreamConsumerCount(data map[string]any) int {
	downstream := data["downstream_consumers"]
	switch typed := downstream.(type) {
	case []any:
		return len(typed)
	case []map[string]any:
		return len(typed)
	case map[string]any:
		total := traceInt(typed, "graph_dependent_count") + traceInt(typed, "content_consumer_count")
		if total > 0 {
			return total
		}
		return len(traceSlice(typed, "items"))
	default:
		return 0
	}
}

func traceEnvelopeError(e *traceServiceError) error {
	if e == nil {
		return nil
	}
	message := strings.TrimSpace(e.Message)
	if message == "" {
		message = strings.TrimSpace(e.Code)
	}
	if message == "" {
		message = "service trace failed"
	}
	return commandExitError{message: message, code: traceExitCode(e.Code)}
}

func traceExitCode(code string) int {
	switch strings.TrimSpace(code) {
	case "ambiguous":
		return 3
	case "index_building", "stale":
		return 4
	case "capability_degraded", "partial":
		return 5
	case "unsupported_capability":
		return 6
	case "invalid_argument", "not_found", "scope_not_found":
		return 2
	default:
		return 1
	}
}

func traceErrorCodeFromTransport(err error) string {
	var httpErr *apiHTTPError
	if err != nil && strings.Contains(err.Error(), "connection refused") {
		return "backend_unavailable"
	}
	if err != nil && strings.Contains(err.Error(), "request failed") {
		return "backend_unavailable"
	}
	if err != nil && errors.As(err, &httpErr) {
		switch httpErr.StatusCode {
		case 400:
			return "invalid_argument"
		case 404:
			return "not_found"
		case 501:
			return "unsupported_capability"
		case 503:
			return "backend_unavailable"
		default:
			return "api_error"
		}
	}
	return "api_error"
}

func writeTraceJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

func traceMap(parent map[string]any, key string) map[string]any {
	if parent == nil {
		return nil
	}
	if typed, ok := parent[key].(map[string]any); ok {
		return typed
	}
	return nil
}

func traceSlice(parent map[string]any, key string) []any {
	if parent == nil {
		return nil
	}
	switch typed := parent[key].(type) {
	case []any:
		return typed
	case []map[string]any:
		rows := make([]any, 0, len(typed))
		for _, row := range typed {
			rows = append(rows, row)
		}
		return rows
	default:
		return nil
	}
}

func traceString(parent map[string]any, key string) string {
	if parent == nil {
		return ""
	}
	if value, ok := parent[key].(string); ok {
		return strings.TrimSpace(value)
	}
	return ""
}

func traceInt(parent map[string]any, key string) int {
	if parent == nil {
		return 0
	}
	switch value := parent[key].(type) {
	case int:
		return value
	case float64:
		return int(value)
	default:
		return 0
	}
}

func traceStrings(value any) []string {
	switch typed := value.(type) {
	case []string:
		return typed
	case []any:
		values := make([]string, 0, len(typed))
		for _, item := range typed {
			if value, ok := item.(string); ok && strings.TrimSpace(value) != "" {
				values = append(values, strings.TrimSpace(value))
			}
		}
		return values
	default:
		return nil
	}
}

func traceFirstString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
