// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/spf13/cobra"

	"github.com/eshu-hq/eshu/go/internal/cli/admin"
	"github.com/eshu-hq/eshu/go/internal/secretcrypto"
	pgstorage "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

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
