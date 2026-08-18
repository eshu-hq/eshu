// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/eshu-hq/eshu/go/internal/cli/reposelector"
)

// resolveRepositorySelectorFromFlags reads whichever repository selector flag
// the command declares and resolves it to a canonical repository ID. An empty
// result means the command was given no selector, which every caller treats as
// "not scoped to a repository" rather than as an error.
//
// The flag reading stays here because cobra flags are process state; the
// matching and the API listing behind it live in
// go/internal/cli/reposelector.
func resolveRepositorySelectorFromFlags(cmd *cobra.Command, client *APIClient) (string, error) {
	selector, exact, err := readRepositorySelectorFlag(cmd)
	if err != nil {
		return "", err
	}
	if selector == "" {
		return "", nil
	}
	if exact {
		return selector, nil
	}
	if client == nil {
		return "", missingAPIClientError(selector)
	}
	return reposelector.Resolve(client, selector)
}

// missingAPIClientError reproduces the error a nil client produced before the
// selector logic moved to go/internal/cli/reposelector. The check has to
// happen here, on the concrete pointer: a nil *APIClient boxed into
// reposelector.Getter is a non-nil interface, so it slips that package's own
// nil guard and panics inside APIClient.do instead. Callers build the client
// with apiClientFromCmd, which never returns nil, so this is defensive -- but
// preserving it keeps the extraction behaviour-preserving rather than
// behaviour-preserving-for-today's-callers.
func missingAPIClientError(selector string) error {
	return fmt.Errorf("resolve repo selector %q: missing API client", selector)
}

// readRepositorySelectorFlag returns the selector value, whether it is already
// an exact repository ID, and any flag-read error. --repo is a fuzzy selector
// that needs resolving; --repo-id is exact and bypasses resolution entirely,
// which is what lets an operator skip the repository listing when they already
// hold the canonical ID.
func readRepositorySelectorFlag(cmd *cobra.Command) (string, bool, error) {
	if cmd == nil {
		return "", false, nil
	}
	if cmd.Flags().Lookup("repo") != nil {
		value, err := cmd.Flags().GetString("repo")
		if err != nil {
			return "", false, fmt.Errorf("read repo flag: %w", err)
		}
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value), false, nil
		}
	}
	if cmd.Flags().Lookup("repo-id") != nil {
		value, err := cmd.Flags().GetString("repo-id")
		if err != nil {
			return "", false, fmt.Errorf("read repo-id flag: %w", err)
		}
		return strings.TrimSpace(value), true, nil
	}
	return "", false, nil
}
