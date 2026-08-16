// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/eshu-hq/eshu/go/internal/cli/apierr"
	"github.com/eshu-hq/eshu/go/internal/cli/trace"
)

// traceFetchServiceStory is the seam the command tests replace with a stub. It
// exists so a test can drive runTraceService's decision tree without a live API.
var traceFetchServiceStory = func(client *APIClient, selector string, query trace.ServiceQuery) (trace.ServiceEnvelope, error) {
	return trace.FetchServiceStory(client, selector, query)
}

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
	query, jsonOutput, err := traceServiceRequestFromCommand(cmd)
	if err != nil {
		return err
	}
	selector := strings.TrimSpace(args[0])
	if selector == "" {
		return commandExitError{message: "service name is required", code: 2}
	}

	envelope, err := traceFetchServiceStory(apiClientFromCmd(cmd), selector, query)
	if err != nil {
		envelope = trace.ServiceEnvelope{
			Data: nil,
			Error: &trace.ServiceError{
				Code:    traceErrorCodeFromTransport(err),
				Message: err.Error(),
			},
		}
		return finishTraceService(cmd, jsonOutput, envelope, traceEnvelopeError(envelope.Error))
	}
	if envelope.Error != nil {
		return finishTraceService(cmd, jsonOutput, envelope, traceEnvelopeError(envelope.Error))
	}
	if envelope.Data == nil {
		envelope.Error = &trace.ServiceError{Code: "not_found", Message: "service story response did not include data"}
		return finishTraceService(cmd, jsonOutput, envelope, traceEnvelopeError(envelope.Error))
	}
	if freshness := trace.ServiceFreshnessState(envelope); freshness == "stale" || freshness == "building" {
		return finishTraceService(cmd, jsonOutput, envelope, commandExitError{
			message: fmt.Sprintf("service trace freshness is %s", freshness),
			code:    4,
		})
	}
	if status := trace.ServiceStatus(envelope); status == "partial" {
		return finishTraceService(cmd, jsonOutput, envelope, commandExitError{
			message: "service trace is partial",
			code:    5,
		})
	}
	return finishTraceService(cmd, jsonOutput, envelope, nil)
}

// traceServiceRequestFromCommand reads the command's flags into the query
// selectors the API call sends and the output mode the wrapper keeps for
// itself. The two travel separately on purpose: internal/cli/trace's
// ServiceQuery carries only what shapes the request, so a caller cannot
// mistake the output mode for something the fetch or the renderers read.
func traceServiceRequestFromCommand(cmd *cobra.Command) (trace.ServiceQuery, bool, error) {
	jsonOutput, err := cmd.Flags().GetBool("json")
	if err != nil {
		return trace.ServiceQuery{}, false, err
	}
	serviceID, err := cmd.Flags().GetString("service-id")
	if err != nil {
		return trace.ServiceQuery{}, false, err
	}
	repo, err := cmd.Flags().GetString("repo")
	if err != nil {
		return trace.ServiceQuery{}, false, err
	}
	environment, err := cmd.Flags().GetString("env")
	if err != nil {
		return trace.ServiceQuery{}, false, err
	}
	return trace.ServiceQuery{
		Repo:        strings.TrimSpace(repo),
		Environment: strings.TrimSpace(environment),
		ServiceID:   strings.TrimSpace(serviceID),
	}, jsonOutput, nil
}

func finishTraceService(cmd *cobra.Command, jsonOutput bool, envelope trace.ServiceEnvelope, err error) error {
	if jsonOutput {
		if writeErr := writeTraceJSON(cmd.OutOrStdout(), envelope); writeErr != nil {
			return writeErr
		}
		return err
	}
	if err != nil {
		var renderErr error
		if envelope.Error != nil && envelope.Error.Code == "ambiguous" {
			renderErr = trace.RenderServiceError(cmd.OutOrStdout(), envelope)
		} else if envelope.Error == nil && envelope.Data != nil {
			renderErr = trace.RenderServiceSummary(cmd.OutOrStdout(), envelope)
		}
		if renderErr != nil {
			return renderErr
		}
		return err
	}
	return trace.RenderServiceSummary(cmd.OutOrStdout(), envelope)
}

// traceEnvelopeError converts an envelope error into the typed exit error the
// command returns. It stays here rather than moving to internal/cli/trace
// because commandExitError and the exit-code table are both cmd/eshu's.
func traceEnvelopeError(e *trace.ServiceError) error {
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

// traceErrorCodeFromTransport classifies a failed API call for `eshu trace`
// and component_api. It has three copies, not one: change.ErrorCodeFromTransport,
// freshness.ErrorCodeFromTransport, and entitymap's transportErrorCode (which
// now serves `eshu map`), all taken because cmd/eshu is package main and cannot
// be imported. Edit this body and every copy needs the same edit;
// TestTransportErrorCodeParity pins the first two and TestEntityMapTransportClassifierMatchesTrace pins the third.
func traceErrorCodeFromTransport(err error) string {
	if err != nil && strings.Contains(err.Error(), "connection refused") {
		return "backend_unavailable"
	}
	if err != nil && strings.Contains(err.Error(), "request failed") {
		return "backend_unavailable"
	}
	// apierr.StatusCode reports false for a nil error, so the substring checks
	// above keep their precedence and no nil guard is needed here.
	if status, ok := apierr.StatusCode(err); ok {
		switch status {
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

// The envelope value readers (traceMap / traceSlice / traceString / traceInt
// / traceStrings) lived here until the component extraction (#6139) and this
// trace extraction (#6059) together removed their last cmd/eshu callers.
// Their surviving copies are the mapValue / sliceValue / stringValue /
// intValue / stringsValue sets in go/internal/cli/change,
// go/internal/cli/freshness, go/internal/cli/component, and
// go/internal/cli/trace, plus entitymap's differently named set.
// TestEnvelopeReaderParity compares the same-named sets per role, and the
// entitymap twin tests pin that family's set against internal/cli/trace's.
