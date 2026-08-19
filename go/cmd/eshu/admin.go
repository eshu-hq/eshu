// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/cli/admin"
	"github.com/eshu-hq/eshu/go/internal/secretcrypto"
	pgstorage "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/spf13/cobra"
)

// The `eshu admin` command tree. This file owns only process wiring: the
// cobra commands, their flags, reading those flags, building the *APIClient,
// and printing the decoded response. What each subcommand actually asks the
// API for -- endpoint, request body, and the replay safety checks -- lives in
// go/internal/cli/admin, which go/cmd/eshu cannot host because this is
// `package main` and cannot grow subdirectories (issue #6059, epic #6053).

var adminCmd = &cobra.Command{
	Use:   "admin",
	Short: "Administrative operations",
}

var adminFactsCmd = &cobra.Command{
	Use:   "facts",
	Short: "Fact work item administration",
}

func init() {
	rootCmd.AddCommand(adminCmd)
	adminCmd.AddCommand(adminFactsCmd)

	// admin reindex
	reindexCmd := &cobra.Command{
		Use:   "reindex",
		Short: "Queue a reindex request for the ingester",
		RunE:  runAdminReindex,
	}
	reindexCmd.Flags().String("ingester", "repository", "Ingester type")
	reindexCmd.Flags().String("scope", "workspace", "Reindex scope")
	reindexCmd.Flags().Bool("force", true, "Force reindex")
	addRemoteFlags(reindexCmd)
	adminCmd.AddCommand(reindexCmd)

	// admin tuning-report
	tuningCmd := &cobra.Command{
		Use:   "tuning-report",
		Short: "Show shared-projection tuning report",
		RunE:  runAdminTuningReport,
	}
	addRemoteFlags(tuningCmd)
	adminCmd.AddCommand(tuningCmd)

	// admin facts list
	factsListCmd := &cobra.Command{
		Use:   "list",
		Short: "List fact work items",
		RunE:  runAdminFactsList,
	}
	factsListCmd.Flags().String("status", "", "Filter by status")
	factsListCmd.Flags().String("repository-id", "", "Filter by repository ID")
	factsListCmd.Flags().String("source-run-id", "", "Filter by source run ID")
	factsListCmd.Flags().Int("limit", 50, "Maximum results")
	addRemoteFlags(factsListCmd)
	adminFactsCmd.AddCommand(factsListCmd)

	// admin facts decisions
	decisionsCmd := &cobra.Command{
		Use:   "decisions",
		Short: "List projection decisions",
		RunE:  runAdminFactsDecisions,
	}
	decisionsCmd.Flags().String("repository-id", "", "Filter by repository ID")
	decisionsCmd.Flags().String("source-run-id", "", "Filter by source run ID")
	decisionsCmd.Flags().Int("limit", 50, "Maximum results")
	addRemoteFlags(decisionsCmd)
	adminFactsCmd.AddCommand(decisionsCmd)

	// admin facts replay
	replayCmd := &cobra.Command{
		Use:   "replay",
		Short: "Replay failed work items back to pending",
		RunE:  runAdminFactsReplay,
	}
	replayCmd.Flags().String("work-item-id", "", "Specific work item ID")
	replayCmd.Flags().String("scope-id", "", "Filter by ingestion scope ID")
	replayCmd.Flags().String("stage", "", "Filter by stage (projector|reducer)")
	replayCmd.Flags().String("failure-class", "", "Filter by failure class")
	replayCmd.Flags().String("reason", "", "Required: why this replay is safe")
	replayCmd.Flags().String("idempotency-key", "", "Idempotency key; one is generated when empty")
	replayCmd.Flags().Bool("force", false, "Replay unsafe failure classes after addressing the cause")
	replayCmd.Flags().Int("limit", 25, "Maximum items to replay")
	addRemoteFlags(replayCmd)
	adminFactsCmd.AddCommand(replayCmd)

	// admin facts dead-letter
	deadLetterCmd := &cobra.Command{
		Use:   "dead-letter",
		Short: "Move work items to terminal failed state",
		RunE:  runAdminFactsDeadLetter,
	}
	deadLetterCmd.Flags().String("work-item-id", "", "Specific work item ID")
	deadLetterCmd.Flags().String("repository-id", "", "Filter by repository ID")
	deadLetterCmd.Flags().String("note", "", "Operator note")
	addRemoteFlags(deadLetterCmd)
	adminFactsCmd.AddCommand(deadLetterCmd)

	// admin facts skip
	skipCmd := &cobra.Command{
		Use:   "skip",
		Short: "Skip work items",
		RunE:  runAdminFactsSkip,
	}
	skipCmd.Flags().String("work-item-id", "", "Specific work item ID")
	skipCmd.Flags().String("note", "", "Operator note")
	addRemoteFlags(skipCmd)
	adminFactsCmd.AddCommand(skipCmd)

	// admin facts backfill
	backfillCmd := &cobra.Command{
		Use:   "backfill",
		Short: "Create a fact backfill request",
		RunE:  runAdminFactsBackfill,
	}
	backfillCmd.Flags().String("repository-id", "", "Repository ID to backfill")
	backfillCmd.Flags().String("source-run-id", "", "Source run ID")
	addRemoteFlags(backfillCmd)
	adminFactsCmd.AddCommand(backfillCmd)

	// admin facts replay-events
	replayEventsCmd := &cobra.Command{
		Use:   "replay-events",
		Short: "List replay audit events",
		RunE:  runAdminFactsReplayEvents,
	}
	replayEventsCmd.Flags().Int("limit", 50, "Maximum results")
	addRemoteFlags(replayEventsCmd)
	adminFactsCmd.AddCommand(replayEventsCmd)
}

func runAdminReindex(cmd *cobra.Command, args []string) error {
	ingester, _ := cmd.Flags().GetString("ingester")
	scope, _ := cmd.Flags().GetString("scope")
	force, _ := cmd.Flags().GetBool("force")
	result, err := admin.Reindex(apiClientFromCmd(cmd), admin.ReindexInput{
		Ingester: ingester,
		Scope:    scope,
		Force:    force,
	})
	if err != nil {
		return err
	}
	printJSON(result)
	return nil
}

func runAdminTuningReport(cmd *cobra.Command, args []string) error {
	result, err := admin.TuningReport(apiClientFromCmd(cmd))
	if err != nil {
		return err
	}
	printJSON(result)
	return nil
}

func runAdminFactsList(cmd *cobra.Command, args []string) error {
	status, _ := cmd.Flags().GetString("status")
	repoID, _ := cmd.Flags().GetString("repository-id")
	runID, _ := cmd.Flags().GetString("source-run-id")
	limit, _ := cmd.Flags().GetInt("limit")
	result, err := admin.ListWorkItems(apiClientFromCmd(cmd), admin.ListWorkItemsInput{
		Status:       status,
		RepositoryID: repoID,
		SourceRunID:  runID,
		Limit:        limit,
	})
	if err != nil {
		return err
	}
	printJSON(result)
	return nil
}

func runAdminFactsDecisions(cmd *cobra.Command, args []string) error {
	repoID, _ := cmd.Flags().GetString("repository-id")
	runID, _ := cmd.Flags().GetString("source-run-id")
	limit, _ := cmd.Flags().GetInt("limit")
	result, err := admin.ListDecisions(apiClientFromCmd(cmd), admin.ListDecisionsInput{
		RepositoryID: repoID,
		SourceRunID:  runID,
		Limit:        limit,
	})
	if err != nil {
		return err
	}
	printJSON(result)
	return nil
}

func runAdminFactsReplay(cmd *cobra.Command, args []string) error {
	workItemID, _ := cmd.Flags().GetString("work-item-id")
	scopeID, _ := cmd.Flags().GetString("scope-id")
	stage, _ := cmd.Flags().GetString("stage")
	failureClass, _ := cmd.Flags().GetString("failure-class")
	reason, _ := cmd.Flags().GetString("reason")
	idempotencyKey, _ := cmd.Flags().GetString("idempotency-key")
	force, _ := cmd.Flags().GetBool("force")
	limit, _ := cmd.Flags().GetInt("limit")

	result, err := admin.Replay(apiClientFromCmd(cmd), admin.ReplayInput{
		WorkItemID:     workItemID,
		ScopeID:        scopeID,
		Stage:          stage,
		FailureClass:   failureClass,
		Reason:         reason,
		IdempotencyKey: idempotencyKey,
		Force:          force,
		Limit:          limit,
	})
	if err != nil {
		return err
	}
	printJSON(result)
	return nil
}

func runAdminFactsDeadLetter(cmd *cobra.Command, args []string) error {
	workItemID, _ := cmd.Flags().GetString("work-item-id")
	repoID, _ := cmd.Flags().GetString("repository-id")
	note, _ := cmd.Flags().GetString("note")
	result, err := admin.DeadLetter(apiClientFromCmd(cmd), admin.DeadLetterInput{
		WorkItemID:   workItemID,
		RepositoryID: repoID,
		Note:         note,
	})
	if err != nil {
		return err
	}
	printJSON(result)
	return nil
}

func runAdminFactsSkip(cmd *cobra.Command, args []string) error {
	workItemID, _ := cmd.Flags().GetString("work-item-id")
	note, _ := cmd.Flags().GetString("note")
	result, err := admin.Skip(apiClientFromCmd(cmd), admin.SkipInput{
		WorkItemID: workItemID,
		Note:       note,
	})
	if err != nil {
		return err
	}
	printJSON(result)
	return nil
}

func runAdminFactsBackfill(cmd *cobra.Command, args []string) error {
	repoID, _ := cmd.Flags().GetString("repository-id")
	runID, _ := cmd.Flags().GetString("source-run-id")
	result, err := admin.Backfill(apiClientFromCmd(cmd), admin.BackfillInput{
		RepositoryID: repoID,
		SourceRunID:  runID,
	})
	if err != nil {
		return err
	}
	printJSON(result)
	return nil
}

func runAdminFactsReplayEvents(cmd *cobra.Command, args []string) error {
	limit, _ := cmd.Flags().GetInt("limit")
	result, err := admin.ListReplayEvents(apiClientFromCmd(cmd), limit)
	if err != nil {
		return err
	}
	printJSON(result)
	return nil
}

// eshu admin initial-credential / reset-initial-credential (epic #4962,
// issue #4963).
//
// This file is the process wiring for both subcommands: it opens the direct
// Postgres connection (sql.Open("pgx", dsn) from ESHU_POSTGRES_DSN, the
// existing cmd/eshu pattern at local_host_config.go:227), resolves the
// data-encryption keyring from the environment, reads the --username flag,
// and prints the result. The retrieval and reset logic itself lives in
// go/internal/cli/admin (issue #6059, epic #6053), which is where the
// rationale for going straight to Postgres rather than through the API is
// recorded.
//
// The retrieved or regenerated plaintext is printed to stdout exactly once
// per invocation and is never logged or written to any file by this command.
const (
	adminCredentialDSNEnv = "ESHU_POSTGRES_DSN" // #nosec G101 -- environment variable name, not a credential
)

func init() {
	initialCredentialCmd := &cobra.Command{
		Use:   "initial-credential",
		Short: "Retrieve the one-time generated bootstrap admin credential",
		RunE:  runAdminInitialCredential,
	}
	adminCmd.AddCommand(initialCredentialCmd)

	resetInitialCredentialCmd := &cobra.Command{
		Use:   "reset-initial-credential",
		Short: "Regenerate and reseal the bootstrap admin credential",
		Long: "reset-initial-credential atomically rotates the bootstrap admin's " +
			"password AND re-enrolls its MFA recovery-code factor (issue #5602), " +
			"so the printed recovery code below actually authenticates. It never " +
			"touches a TOTP factor the admin enrolled after bootstrap. Use this " +
			"when the original one-time credential was lost, expired under the " +
			"configured data-encryption key, or already consumed.",
		RunE: runAdminResetInitialCredential,
	}
	resetInitialCredentialCmd.Flags().String("username", "", "Username to seal into the new credential bundle; required only if the prior credential cannot be recovered to carry it forward")
	adminCmd.AddCommand(resetInitialCredentialCmd)
}

func runAdminInitialCredential(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	db, err := openAdminCredentialDB(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	keyring, err := secretcrypto.KeyringFromEnv(os.Getenv)
	if err != nil {
		return fmt.Errorf("resolve data-encryption key: %w", err)
	}

	payload, err := admin.RetrieveInitialCredential(ctx, pgstorage.SQLDB{DB: db}, keyring)
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintf(cmd.OutOrStdout(),
		"username:      %s\npassword:      %s\nrecovery code: %s\n",
		payload.Username, payload.Password, payload.RecoveryCode)
	return nil
}

func runAdminResetInitialCredential(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	db, err := openAdminCredentialDB(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	keyring, err := secretcrypto.KeyringFromEnv(os.Getenv)
	if err != nil {
		return fmt.Errorf("resolve data-encryption key: %w", err)
	}

	username, _ := cmd.Flags().GetString("username")
	payload, err := admin.ResetInitialCredential(ctx, pgstorage.SQLDB{DB: db}, keyring, username)
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintf(cmd.OutOrStdout(),
		"username:      %s\npassword:      %s\nrecovery code: %s\n",
		payload.Username, payload.Password, payload.RecoveryCode)
	return nil
}

// openAdminCredentialDB opens a direct Postgres connection from
// ESHU_POSTGRES_DSN, mirroring local_host_config.go:227's
// applyLocalBootstrap pattern.
func openAdminCredentialDB(ctx context.Context) (*sql.DB, error) {
	dsn := strings.TrimSpace(os.Getenv(adminCredentialDSNEnv))
	if dsn == "" {
		return nil, fmt.Errorf("%s is required", adminCredentialDSNEnv)
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open postgres connection: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping postgres connection: %w", err)
	}
	return db, nil
}
