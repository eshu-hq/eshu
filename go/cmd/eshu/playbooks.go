// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
)

type queryPlaybookResolveOptions struct {
	PlaybookID string
	Inputs     map[string]string
}

type queryPlaybookListEnvelope struct {
	Data  map[string]any      `json:"data"`
	Truth map[string]any      `json:"truth"`
	Error *queryPlaybookError `json:"error"`
}

type queryPlaybookResolveEnvelope struct {
	Data struct {
		Resolved struct {
			PlaybookID   string           `json:"playbook_id"`
			Version      string           `json:"version"`
			PromptFamily string           `json:"prompt_family"`
			Calls        []map[string]any `json:"calls"`
			FailureModes []map[string]any `json:"failure_modes"`
		} `json:"resolved"`
	} `json:"data"`
	Truth map[string]any      `json:"truth"`
	Error *queryPlaybookError `json:"error"`
}

type queryPlaybookError struct {
	Code       string         `json:"code,omitempty"`
	Message    string         `json:"message,omitempty"`
	Capability string         `json:"capability,omitempty"`
	Details    map[string]any `json:"details,omitempty"`
}

var queryPlaybookInputs []string

func init() {
	playbooksCmd := &cobra.Command{
		Use:   "playbooks",
		Short: "List and resolve deterministic query playbooks",
	}
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List query playbooks",
		Args:  cobra.NoArgs,
		RunE:  runQueryPlaybookList,
	}
	resolveCmd := &cobra.Command{
		Use:   "resolve <playbook-id>",
		Short: "Resolve a query playbook into bounded calls",
		Args:  cobra.ExactArgs(1),
		RunE:  runQueryPlaybookResolve,
	}
	resolveCmd.Flags().StringArrayVar(&queryPlaybookInputs, "input", nil, "Playbook input as key=value; repeat for multiple inputs")
	addRemoteFlags(listCmd)
	addRemoteFlags(resolveCmd)
	playbooksCmd.AddCommand(listCmd, resolveCmd)
	rootCmd.AddCommand(playbooksCmd)
}

func runQueryPlaybookList(cmd *cobra.Command, _ []string) error {
	var envelope queryPlaybookListEnvelope
	if err := apiClientFromCmd(cmd).GetEnvelope("/api/v0/query-playbooks", &envelope); err != nil {
		return err
	}
	return writeQueryPlaybookJSON(cmd.OutOrStdout(), envelope)
}

func runQueryPlaybookResolve(cmd *cobra.Command, args []string) error {
	inputs, err := parseQueryPlaybookInputs(queryPlaybookInputs)
	if err != nil {
		return err
	}
	envelope, err := fetchQueryPlaybookResolve(apiClientFromCmd(cmd), queryPlaybookResolveOptions{
		PlaybookID: args[0],
		Inputs:     inputs,
	})
	if err != nil {
		return err
	}
	return writeQueryPlaybookJSON(cmd.OutOrStdout(), envelope)
}

func fetchQueryPlaybookResolve(client *APIClient, opts queryPlaybookResolveOptions) (queryPlaybookResolveEnvelope, error) {
	var envelope queryPlaybookResolveEnvelope
	if err := client.PostEnvelope("/api/v0/query-playbooks/resolve", map[string]any{
		"playbook_id": strings.TrimSpace(opts.PlaybookID),
		"inputs":      opts.Inputs,
	}, &envelope); err != nil {
		return queryPlaybookResolveEnvelope{}, err
	}
	return envelope, nil
}

func parseQueryPlaybookInputs(raw []string) (map[string]string, error) {
	inputs := make(map[string]string, len(raw))
	for _, item := range raw {
		key, value, ok := strings.Cut(item, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" {
			return nil, fmt.Errorf("input must use key=value form")
		}
		inputs[key] = strings.TrimSpace(value)
	}
	return inputs, nil
}

func writeQueryPlaybookJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}
